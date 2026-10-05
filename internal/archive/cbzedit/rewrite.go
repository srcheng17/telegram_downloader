package cbzedit

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const backupFileName = "source-archive.bin"

// Prepare keeps the original in place. The application must persist the
// returned hashes and paths before calling Commit; that record is also the
// crash-recovery anchor if a process stops between rename and database update.
func Prepare(ctx context.Context, sourceRoot, backupRoot *os.Root, req PrepareRequest) (Prepared, error) {
	limits := normalizeLimits(req.Limits)
	if err := validateOperationID(req.OperationID); err != nil {
		return Prepared{}, err
	}
	if !validDigest(req.ExpectedSHA256) || len(req.NewComicInfo) == 0 || int64(len(req.NewComicInfo)) > limits.MaxXMLBytes {
		return Prepared{}, ErrInvalidXML
	}
	old, err := inspectRoot(ctx, sourceRoot, req.RelativePath, limits)
	if err != nil {
		return Prepared{}, err
	}
	if old.SHA256 != req.ExpectedSHA256 {
		return Prepared{}, ErrConflict
	}
	if old.HasComicInfo && bytes.Equal(old.ComicInfo, req.NewComicInfo) {
		return Prepared{}, ErrNoChange
	}
	if err := validateXMLChange(old, req.NewComicInfo, req.ChangedElements, req.AllowPageCountCorrection); err != nil {
		return Prepared{}, err
	}
	if err := checkFreeSpace(sourceRoot, backupRoot, old.Size); err != nil {
		return Prepared{}, err
	}
	backupPath, err := copyBackup(ctx, sourceRoot, backupRoot, req.RelativePath, req.OperationID, old.SHA256, old.Size)
	if err != nil {
		return Prepared{}, err
	}
	parent, _, source, info, err := openBook(sourceRoot, req.RelativePath)
	if err != nil {
		return Prepared{}, err
	}
	defer parent.Close()
	defer source.Close()
	if info.Size() != old.Size {
		return Prepared{}, ErrConflict
	}
	tempBase, temporary, err := newTemporary(parent, req.OperationID)
	if err != nil {
		return Prepared{}, err
	}
	defer func() {
		if temporary != nil {
			_ = temporary.Close()
			_ = parent.Remove(tempBase)
		}
	}()
	reader, err := zip.NewReader(source, info.Size())
	if err != nil {
		return Prepared{}, ErrInvalidArchive
	}
	writer := zip.NewWriter(temporary)
	if err := writer.SetComment(reader.Comment); err != nil {
		return Prepared{}, err
	}
	for index, member := range reader.File {
		if err := ctx.Err(); err != nil {
			return Prepared{}, err
		}
		if index == old.xmlIndex {
			if err := writeXML(writer, req.NewComicInfo); err != nil {
				return Prepared{}, err
			}
		} else if err := writer.Copy(member); err != nil {
			return Prepared{}, err
		}
	}
	if old.xmlIndex < 0 {
		if err := writeXML(writer, req.NewComicInfo); err != nil {
			return Prepared{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return Prepared{}, err
	}
	if err := temporary.Sync(); err != nil {
		return Prepared{}, err
	}
	if err := temporary.Close(); err != nil {
		return Prepared{}, err
	}
	temporary = nil
	// A caller can persist Prepared only after the fully written temp has been
	// reopened, decompressed, compared with the original and fsynced.
	newState, err := inspectTemporary(ctx, parent, tempBase, limits)
	if err != nil {
		_ = parent.Remove(tempBase)
		return Prepared{}, err
	}
	if err := compareArchives(old, newState, req.NewComicInfo); err != nil {
		_ = parent.Remove(tempBase)
		return Prepared{}, err
	}
	current, err := inspectRoot(ctx, sourceRoot, req.RelativePath, limits)
	if err != nil || current.SHA256 != old.SHA256 {
		_ = parent.Remove(tempBase)
		return Prepared{}, ErrConflict
	}
	if err := syncDirectory(parent); err != nil {
		_ = parent.Remove(tempBase)
		return Prepared{}, err
	}
	if err := ctx.Err(); err != nil {
		_ = parent.Remove(tempBase)
		return Prepared{}, err
	}
	prepared := Prepared{
		RelativePath: req.RelativePath, TemporaryPath: path.Join(path.Dir(req.RelativePath), tempBase),
		BackupPath: backupPath, OperationID: req.OperationID,
		OriginalSHA256: old.SHA256, NewSHA256: newState.SHA256,
		OriginalSize: old.Size, NewSize: newState.Size,
		ComicInfoSHA256:          digest(req.NewComicInfo),
		ChangedElements:          append([]string(nil), req.ChangedElements...),
		AllowPageCountCorrection: req.AllowPageCountCorrection, Limits: limits,
	}
	if err := persistPrepared(backupRoot, prepared); err != nil {
		_ = parent.Remove(tempBase)
		return Prepared{}, err
	}
	return prepared, nil
}

func writeXML(writer *zip.Writer, data []byte) error {
	member, err := writer.CreateHeader(&zip.FileHeader{Name: "ComicInfo.xml", Method: zip.Deflate})
	if err != nil {
		return err
	}
	_, err = member.Write(data)
	return err
}

func validateXMLChange(old archiveState, raw []byte, changed []string, allowPageCount bool) error {
	newXML, err := parseSafeXML(raw, old.PageCount)
	if err != nil {
		return err
	}
	previous := parsedXML{values: map[string]string{}}
	if old.HasComicInfo {
		previous, err = parseSafeXML(old.ComicInfo, old.PageCount)
		if err != nil {
			return err
		}
	}
	if previous.hasPages != newXML.hasPages || !reflect.DeepEqual(previous.pages, newXML.pages) {
		return ErrInvalidXML
	}
	allowed := map[string]bool{}
	for _, name := range changed {
		if name == "Pages" || name == "PageCount" && !allowPageCount || allowed[name] {
			return ErrInvalidXML
		}
		_, before := previous.values[name]
		_, after := newXML.values[name]
		if !before && !after {
			return ErrInvalidXML
		}
		allowed[name] = true
	}
	for name, value := range previous.values {
		nextValue, present := newXML.values[name]
		if !allowed[name] && (!present || nextValue != value) {
			return ErrInvalidXML
		}
	}
	for name, value := range newXML.values {
		oldValue, present := previous.values[name]
		if !allowed[name] && (!present || oldValue != value) {
			return ErrInvalidXML
		}
	}
	if allowed["PageCount"] {
		count, err := strconv.Atoi(newXML.values["PageCount"])
		if err != nil || count != old.PageCount {
			return ErrInvalidXML
		}
	}
	return nil
}

func compareArchives(old, next archiveState, newXML []byte) error {
	if next.comment != old.comment || !next.HasComicInfo || !bytes.Equal(next.ComicInfo, newXML) || next.PageCount != old.PageCount {
		return ErrInvalidArchive
	}
	if old.xmlIndex < 0 {
		if len(next.entries) != len(old.entries)+1 || next.xmlIndex != len(old.entries) {
			return ErrInvalidArchive
		}
	} else if len(next.entries) != len(old.entries) || next.xmlIndex != old.xmlIndex {
		return ErrInvalidArchive
	}
	for i := range old.entries {
		if i != old.xmlIndex && old.entries[i] != next.entries[i] {
			return ErrInvalidArchive
		}
	}
	return nil
}

func inspectTemporary(ctx context.Context, parent *os.Root, name string, limits Limits) (archiveState, error) {
	info, err := parent.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return archiveState{}, ErrInvalidArchive
	}
	file, err := parent.Open(name)
	if err != nil {
		return archiveState{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return archiveState{}, ErrConflict
	}
	return inspectFile(ctx, file, info.Size(), limits)
}

// Commit atomically replaces the source only after verifying the independent
// original backup, prepared temp and still-current source. A successful rename
// cannot be rolled back implicitly if a later fsync/readback fails.
func Commit(ctx context.Context, sourceRoot, backupRoot *os.Root, prepared Prepared) (CommitResult, error) {
	result := CommitResult{}
	if err := validatePrepared(prepared); err != nil {
		return result, err
	}
	limits := normalizeLimits(prepared.Limits)
	if err := verifyBackup(ctx, backupRoot, prepared.BackupPath, prepared.OriginalSHA256, prepared.OriginalSize); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, ErrBackup
	}
	old, err := inspectRoot(ctx, sourceRoot, prepared.RelativePath, limits)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, ErrConflict
	}
	if old.SHA256 != prepared.OriginalSHA256 || old.Size != prepared.OriginalSize {
		return result, ErrConflict
	}
	parent, base, original, _, err := openBook(sourceRoot, prepared.RelativePath)
	if err != nil {
		return result, err
	}
	defer parent.Close()
	defer original.Close()
	tempBase := path.Base(prepared.TemporaryPath)
	next, err := inspectTemporary(ctx, parent, tempBase, limits)
	if err != nil {
		return result, err
	}
	if next.SHA256 != prepared.NewSHA256 || next.Size != prepared.NewSize || digest(next.ComicInfo) != prepared.ComicInfoSHA256 {
		return result, ErrConflict
	}
	if err := validateXMLChange(old, next.ComicInfo, prepared.ChangedElements, prepared.AllowPageCountCorrection); err != nil {
		return result, err
	}
	if err := compareArchives(old, next, next.ComicInfo); err != nil {
		return result, err
	}
	current, err := inspectRoot(ctx, sourceRoot, prepared.RelativePath, limits)
	if err != nil || current.SHA256 != prepared.OriginalSHA256 {
		return result, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	originalInfo, err := original.Stat()
	if err != nil {
		return result, err
	}
	if err := matchSourcePermissions(parent, tempBase, originalInfo); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := parent.Rename(tempBase, base); err != nil {
		return result, fmt.Errorf("replace CBZ: %w", err)
	}
	result.FileCommitted = true
	result.SHA256 = prepared.NewSHA256
	if err := syncDirectory(parent); err != nil {
		return result, err
	}
	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	readback, err := inspectRoot(verifyCtx, sourceRoot, prepared.RelativePath, limits)
	if err != nil || readback.SHA256 != prepared.NewSHA256 || digest(readback.ComicInfo) != prepared.ComicInfoSHA256 {
		return result, ErrConflict
	}
	return result, nil
}

func matchSourcePermissions(parent *os.Root, tempName string, original os.FileInfo) error {
	if original.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return ErrInvalidArchive
	}
	temporary, err := parent.Open(tempName)
	if err != nil {
		return err
	}
	defer temporary.Close()
	current, err := parent.Lstat(tempName)
	if err != nil {
		return err
	}
	opened, err := temporary.Stat()
	if err != nil || !os.SameFile(current, opened) {
		return ErrConflict
	}
	oldOwner, oldOK := original.Sys().(*syscall.Stat_t)
	newOwner, newOK := opened.Sys().(*syscall.Stat_t)
	if !oldOK || !newOK {
		return ErrInvalidArchive
	}
	if oldOwner.Uid != newOwner.Uid || oldOwner.Gid != newOwner.Gid {
		if err := temporary.Chown(int(oldOwner.Uid), int(oldOwner.Gid)); err != nil {
			return err
		}
	}
	if err := temporary.Chmod(original.Mode().Perm()); err != nil {
		return err
	}
	return temporary.Sync()
}

// Abort removes only this operation's still-hidden temporary file. The
// independently copied backup remains available for audit and recovery.
func Abort(sourceRoot *os.Root, prepared Prepared) error {
	if err := validatePrepared(prepared); err != nil {
		return err
	}
	parts, _ := relativeParts(prepared.RelativePath)
	parent, err := checkedParent(sourceRoot, parts[:len(parts)-1])
	if err != nil {
		return err
	}
	defer parent.Close()
	err = parent.Remove(path.Base(prepared.TemporaryPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func validatePrepared(prepared Prepared) error {
	if err := validateOperationID(prepared.OperationID); err != nil {
		return err
	}
	parts, err := relativeParts(prepared.RelativePath)
	if err != nil || !strings.EqualFold(path.Ext(prepared.RelativePath), ".cbz") || !validDigest(prepared.OriginalSHA256) || !validDigest(prepared.NewSHA256) || !validDigest(prepared.ComicInfoSHA256) {
		return ErrInvalidArchive
	}
	tempParts, err := relativeParts(prepared.TemporaryPath)
	if err != nil || len(tempParts) != len(parts) || !reflect.DeepEqual(tempParts[:len(parts)-1], parts[:len(parts)-1]) || !strings.HasPrefix(tempParts[len(tempParts)-1], ".komga-edit-"+prepared.OperationID+"-") || !strings.HasSuffix(tempParts[len(tempParts)-1], ".tmp") {
		return ErrInvalidArchive
	}
	if prepared.BackupPath != prepared.OperationID+"/"+backupFileName || prepared.OriginalSize <= 0 || prepared.NewSize <= 0 {
		return ErrInvalidArchive
	}
	return nil
}

func validateOperationID(id string) error {
	if len(id) < 8 || len(id) > 80 {
		return ErrInvalidArchive
	}
	for _, r := range id {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				if r != '-' {
					return ErrInvalidArchive
				}
			}
		}
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func newTemporary(parent *os.Root, id string) (string, *os.File, error) {
	for i := 0; i < 5; i++ {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", nil, err
		}
		name := ".komga-edit-" + id + "-" + hex.EncodeToString(nonce[:]) + ".tmp"
		file, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return name, file, err
	}
	return "", nil, ErrConflict
}

func checkFreeSpace(sourceRoot, backupRoot *os.Root, originalSize int64) error {
	for _, root := range []*os.Root{sourceRoot, backupRoot} {
		if root == nil {
			return ErrBackup
		}
		var stat syscall.Statfs_t
		if err := syscall.Statfs(root.Name(), &stat); err != nil {
			return fmt.Errorf("check CBZ edit space: %w", err)
		}
		if stat.Bavail == 0 || uint64(stat.Bavail)*uint64(stat.Bsize) < uint64(originalSize)*2+1<<20 {
			return ErrLimit
		}
	}
	return nil
}

func syncDirectory(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err != nil {
		return err
	}
	return closeErr
}
