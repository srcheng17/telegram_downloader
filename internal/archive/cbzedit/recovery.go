package cbzedit

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

const (
	preparedManifestName = "prepared.json"
	preservedFileName    = "pre-restore-archive.bin"
	maxPreparedBytes     = 16 << 10
)

type preparedManifest struct {
	Version  int      `json:"version"`
	Prepared Prepared `json:"prepared"`
}

// VerifyOriginalBackup checks both the private manifest and every byte of the
// independently copied original. It can be used while an operation is still
// preparing and its complete Prepared descriptor is not in PostgreSQL yet.
func VerifyOriginalBackup(ctx context.Context, backupRoot *os.Root, operationID, originalSHA string, originalSize int64) error {
	if err := validateOperationID(operationID); err != nil || !validDigest(originalSHA) || originalSize <= 0 {
		return ErrBackup
	}
	if err := verifyBackup(ctx, backupRoot, operationID+"/"+backupFileName, originalSHA, originalSize); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrBackup
	}
	return nil
}

// VerifyBackup is the equivalent check when a Prepared descriptor was saved.
func VerifyBackup(ctx context.Context, backupRoot *os.Root, prepared Prepared) error {
	if err := validatePrepared(prepared); err != nil {
		return ErrBackup
	}
	return VerifyOriginalBackup(ctx, backupRoot, prepared.OperationID, prepared.OriginalSHA256, prepared.OriginalSize)
}

// persistPrepared writes a separate durable manifest only after the archive
// temporary has been fully verified. The original-backup manifest stays
// available if a crash occurs earlier in Prepare.
func persistPrepared(backupRoot *os.Root, prepared Prepared) error {
	if err := validatePrepared(prepared); err != nil {
		return err
	}
	operationRoot, err := privateOperationRoot(backupRoot, prepared.OperationID, false)
	if err != nil {
		return err
	}
	defer operationRoot.Close()
	data, err := json.Marshal(preparedManifest{Version: 1, Prepared: prepared})
	if err != nil || len(data)+1 > maxPreparedBytes {
		return ErrBackup
	}
	data = append(data, '\n')
	if err := writePrivateRecord(operationRoot, preparedManifestName, data); err != nil {
		return err
	}
	if _, err := readPrepared(operationRoot); err != nil {
		return err
	}
	return syncDirectory(backupRoot)
}

func writePrivateRecord(root *os.Root, name string, data []byte) error {
	if existing, err := readPrivateRecord(root, name, len(data)); err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return ErrBackup
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tempName, file, err := newBackupTemporary(root)
	if err != nil {
		return err
	}
	defer root.Remove(tempName)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := root.Link(tempName, name); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	stored, err := readPrivateRecord(root, name, len(data))
	if err != nil || !bytes.Equal(stored, data) {
		return ErrBackup
	}
	return syncDirectory(root)
}

func readPrivateRecord(root *os.Root, name string, maximum int) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() <= 0 || info.Size() > int64(maximum) {
		return nil, ErrBackup
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, ErrBackup
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil || len(data) != int(info.Size()) {
		return nil, ErrBackup
	}
	return data, nil
}

func readPrepared(root *os.Root) (*Prepared, error) {
	data, err := readPrivateRecord(root, preparedManifestName, maxPreparedBytes)
	if err != nil {
		return nil, err
	}
	var record preparedManifest
	if err := json.Unmarshal(data, &record); err != nil || record.Version != 1 {
		return nil, ErrBackup
	}
	if err := validatePrepared(record.Prepared); err != nil {
		return nil, ErrBackup
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(data, append(canonical, '\n')) {
		return nil, ErrBackup
	}
	return &record.Prepared, nil
}

func readBackupManifest(root *os.Root, operationID string) (backupManifest, error) {
	data, err := readPrivateRecord(root, "manifest.json", 512)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return backupManifest{}, os.ErrNotExist
		}
		return backupManifest{}, ErrBackup
	}
	var record backupManifest
	if err := json.Unmarshal(data, &record); err != nil || record.Version != 1 || record.OperationID != operationID || !validDigest(record.SHA256) || record.Bytes <= 0 || !bytes.Equal(data, expectedManifest(operationID, record.SHA256, record.Bytes)) {
		return backupManifest{}, ErrBackup
	}
	return record, nil
}

// Probe performs a bounded, read-only crash check using one operation's
// private manifests. A caller must still hold its per-book lock before using
// ProbePrepared to resume Commit. Probe never replaces or deletes a file.
func Probe(ctx context.Context, sourceRoot, backupRoot *os.Root, relativePath, operationID, originalSHA string) (ProbeResult, error) {
	result := ProbeResult{State: ProbeMissing}
	if _, err := relativeParts(relativePath); err != nil || !strings.EqualFold(path.Ext(relativePath), ".cbz") || validateOperationID(operationID) != nil || !validDigest(originalSHA) || sourceRoot == nil || backupRoot == nil {
		return result, ErrInvalidArchive
	}
	rootInfo, err := backupRoot.Stat(".")
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode().Perm()&0077 != 0 {
		return result, ErrBackup
	}
	info, err := backupRoot.Lstat(operationID)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, ErrBackup
	}
	operationRoot, err := privateOperationRoot(backupRoot, operationID, false)
	if err != nil {
		return result, ErrBackup
	}
	defer operationRoot.Close()
	manifest, err := readBackupManifest(operationRoot, operationID)
	if errors.Is(err, os.ErrNotExist) {
		// The process can stop after making the private operation directory
		// but before the original manifest has been installed. Commit cannot
		// run from that stage, so an unchanged target can be abandoned.
		if _, preparedErr := operationRoot.Lstat(preparedManifestName); preparedErr == nil || !errors.Is(preparedErr, os.ErrNotExist) {
			return result, ErrBackup
		}
		result.State = ProbeIncomplete
		current, inspectErr := inspectRoot(ctx, sourceRoot, relativePath, normalizeLimits(Limits{}))
		if inspectErr != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			result.State = ProbeConflict
			return result, nil
		}
		result.CurrentSHA256, result.CurrentSize = current.SHA256, current.Size
		if current.SHA256 != originalSHA {
			result.State = ProbeConflict
			return result, nil
		}
		if _, backupErr := operationRoot.Lstat(backupFileName); backupErr == nil {
			result.BackupExists = true
			result.BackupValid = verifyBackupFile(ctx, operationRoot, backupFileName, originalSHA, current.Size) == nil
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
		} else if !errors.Is(backupErr, os.ErrNotExist) {
			return result, ErrBackup
		}
		return result, nil
	}
	if err != nil || manifest.SHA256 != originalSHA {
		return result, ErrBackup
	}
	result.State = ProbeIncomplete
	result.OriginalSize = manifest.Bytes
	if _, err := operationRoot.Lstat(backupFileName); err == nil {
		result.BackupExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, ErrBackup
	}
	if err := VerifyOriginalBackup(ctx, backupRoot, operationID, originalSHA, manifest.Bytes); err != nil {
		return result, err
	}
	result.BackupValid = true
	prepared, err := readPrepared(operationRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, ErrBackup
	}
	if prepared != nil {
		if prepared.OperationID != operationID || prepared.RelativePath != relativePath || prepared.OriginalSHA256 != originalSHA || prepared.OriginalSize != manifest.Bytes {
			return result, ErrBackup
		}
		result.Prepared = prepared
	}
	if prepared != nil {
		if _, err := operationRoot.Lstat(preservedFileName); err == nil {
			if err := verifyBackupFile(ctx, operationRoot, preservedFileName, prepared.NewSHA256, prepared.NewSize); err != nil {
				return result, ErrBackup
			}
			result.PreservedCurrent = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return result, ErrBackup
		}
	}
	limits := normalizeLimits(Limits{})
	if prepared != nil {
		limits = normalizeLimits(prepared.Limits)
	}
	current, err := inspectRoot(ctx, sourceRoot, relativePath, limits)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		result.State = ProbeConflict
		return result, nil
	}
	result.CurrentSHA256, result.CurrentSize = current.SHA256, current.Size
	if prepared == nil {
		if current.SHA256 != originalSHA || current.Size != manifest.Bytes {
			result.State = ProbeConflict
		}
		return result, nil
	}
	if current.SHA256 == prepared.NewSHA256 && current.Size == prepared.NewSize {
		result.State = ProbeCommitted
		return result, nil
	}
	if current.SHA256 != originalSHA || current.Size != manifest.Bytes {
		result.State = ProbeConflict
		return result, nil
	}
	if result.PreservedCurrent {
		result.State = ProbeRestored
		return result, nil
	}
	parts, _ := relativeParts(prepared.RelativePath)
	parent, err := checkedParent(sourceRoot, parts[:len(parts)-1])
	if err != nil {
		result.State = ProbeConflict
		return result, nil
	}
	defer parent.Close()
	tempName := path.Base(prepared.TemporaryPath)
	if _, err := parent.Lstat(tempName); errors.Is(err, os.ErrNotExist) {
		return result, nil
	} else if err != nil {
		result.State = ProbeConflict
		return result, nil
	}
	result.TemporaryExists = true
	next, err := inspectTemporary(ctx, parent, tempName, normalizeLimits(prepared.Limits))
	if err != nil || next.SHA256 != prepared.NewSHA256 || next.Size != prepared.NewSize || digest(next.ComicInfo) != prepared.ComicInfoSHA256 {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		result.State = ProbeConflict
		return result, nil
	}
	result.State = ProbePrepared
	return result, nil
}

// Restore explicitly replaces this operation's version with its verified
// original. It first preserves the current, new version as another independent
// private copy. Only an exact match to prepared.NewSHA256 may be replaced; an
// external writer's later version is never overwritten by this method.
func Restore(ctx context.Context, sourceRoot, backupRoot *os.Root, prepared Prepared) (RestoreResult, error) {
	result := RestoreResult{}
	if err := validatePrepared(prepared); err != nil {
		return result, err
	}
	if err := VerifyBackup(ctx, backupRoot, prepared); err != nil {
		return result, err
	}
	current, err := inspectRoot(ctx, sourceRoot, prepared.RelativePath, normalizeLimits(prepared.Limits))
	if err != nil || current.SHA256 != prepared.NewSHA256 || current.Size != prepared.NewSize {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, ErrConflict
	}
	if err := checkFreeSpace(sourceRoot, backupRoot, max(prepared.OriginalSize, prepared.NewSize)); err != nil {
		return result, err
	}
	if err := preserveCurrent(ctx, sourceRoot, backupRoot, prepared); err != nil {
		return result, err
	}
	result.PreservedRef = prepared.OperationID + "/" + preservedFileName
	parent, base, openedCurrent, info, err := openBook(sourceRoot, prepared.RelativePath)
	if err != nil {
		return result, err
	}
	defer parent.Close()
	defer openedCurrent.Close()
	operationRoot, err := privateOperationRoot(backupRoot, prepared.OperationID, false)
	if err != nil {
		return result, ErrBackup
	}
	defer operationRoot.Close()
	tempName, temporary, err := newTemporary(parent, prepared.OperationID)
	if err != nil {
		return result, err
	}
	defer parent.Remove(tempName)
	original, err := operationRoot.Open(backupFileName)
	if err != nil {
		temporary.Close()
		return result, ErrBackup
	}
	copyResult, copyErr := hashCopy(ctx, temporary, io.LimitReader(original, prepared.OriginalSize+1))
	closeOriginalErr := original.Close()
	if copyErr == nil {
		copyErr = closeOriginalErr
	}
	if copyErr == nil {
		copyErr = temporary.Sync()
	}
	closeTempErr := temporary.Close()
	if copyErr == nil {
		copyErr = closeTempErr
	}
	if copyErr != nil {
		return result, copyErr
	}
	if copyResult.size != prepared.OriginalSize || copyResult.digest != prepared.OriginalSHA256 {
		return result, ErrBackup
	}
	copyState, err := inspectTemporary(ctx, parent, tempName, normalizeLimits(prepared.Limits))
	if err != nil || copyState.SHA256 != prepared.OriginalSHA256 || copyState.Size != prepared.OriginalSize {
		return result, ErrBackup
	}
	latest, err := inspectRoot(ctx, sourceRoot, prepared.RelativePath, normalizeLimits(prepared.Limits))
	if err != nil || latest.SHA256 != prepared.NewSHA256 || latest.Size != prepared.NewSize {
		return result, ErrConflict
	}
	if err := matchSourcePermissions(parent, tempName, info); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := parent.Rename(tempName, base); err != nil {
		return result, fmt.Errorf("restore CBZ: %w", err)
	}
	result.FileRestored = true
	result.SHA256 = prepared.OriginalSHA256
	if err := syncDirectory(parent); err != nil {
		return result, err
	}
	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	readback, err := inspectRoot(verifyCtx, sourceRoot, prepared.RelativePath, normalizeLimits(prepared.Limits))
	if err != nil || readback.SHA256 != prepared.OriginalSHA256 || readback.Size != prepared.OriginalSize {
		return result, ErrConflict
	}
	return result, nil
}

func preserveCurrent(ctx context.Context, sourceRoot, backupRoot *os.Root, prepared Prepared) error {
	operationRoot, err := privateOperationRoot(backupRoot, prepared.OperationID, false)
	if err != nil {
		return ErrBackup
	}
	defer operationRoot.Close()
	if _, err := operationRoot.Lstat(preservedFileName); err == nil {
		return verifyBackupFile(ctx, operationRoot, preservedFileName, prepared.NewSHA256, prepared.NewSize)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrBackup
	}
	parent, _, current, info, err := openBook(sourceRoot, prepared.RelativePath)
	if err != nil {
		return err
	}
	defer parent.Close()
	defer current.Close()
	if info.Size() != prepared.NewSize {
		return ErrConflict
	}
	tempName, temp, err := newBackupTemporary(operationRoot)
	if err != nil {
		return err
	}
	defer operationRoot.Remove(tempName)
	copyResult, copyErr := hashCopy(ctx, temp, io.LimitReader(current, prepared.NewSize+1))
	if copyErr == nil {
		copyErr = temp.Sync()
	}
	closeErr := temp.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return copyErr
	}
	if copyResult.size != prepared.NewSize || copyResult.digest != prepared.NewSHA256 {
		return ErrConflict
	}
	if err := verifyBackupFile(ctx, operationRoot, tempName, prepared.NewSHA256, prepared.NewSize); err != nil {
		return ErrBackup
	}
	if err := operationRoot.Link(tempName, preservedFileName); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := verifyBackupFile(ctx, operationRoot, preservedFileName, prepared.NewSHA256, prepared.NewSize); err != nil {
		return ErrBackup
	}
	if err := syncDirectory(operationRoot); err != nil {
		return err
	}
	return syncDirectory(backupRoot)
}

// AbandonIncomplete removes only bounded orphan temporary files belonging to
// an operation that never reached a valid prepared/committed state. It leaves
// the independent original backup and manifests in place for audit. The
// caller must hold the book lock and persist the terminal operation state.
func AbandonIncomplete(ctx context.Context, sourceRoot, backupRoot *os.Root, relativePath, operationID, originalSHA string) error {
	probe, err := Probe(ctx, sourceRoot, backupRoot, relativePath, operationID, originalSHA)
	if err != nil {
		return err
	}
	if probe.State != ProbeMissing && probe.State != ProbeIncomplete {
		return ErrConflict
	}
	limits := normalizeLimits(Limits{})
	if probe.Prepared != nil {
		limits = normalizeLimits(probe.Prepared.Limits)
	}
	current, err := inspectRoot(ctx, sourceRoot, relativePath, limits)
	if err != nil || current.SHA256 != originalSHA {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrConflict
	}
	parts, err := relativeParts(relativePath)
	if err != nil {
		return err
	}
	parent, err := checkedParent(sourceRoot, parts[:len(parts)-1])
	if err != nil {
		return err
	}
	defer parent.Close()
	directory, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	const maxDirectoryEntries = 10000
	entries, err := directory.ReadDir(maxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > maxDirectoryEntries {
		return ErrLimit
	}
	prefix := ".komga-edit-" + operationID + "-"
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".tmp") {
			continue
		}
		nonce := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".tmp")
		if len(nonce) != 32 || strings.ToLower(nonce) != nonce {
			continue
		}
		if _, err := hex.DecodeString(nonce); err != nil {
			continue
		}
		info, err := parent.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			return ErrConflict
		}
		if err := parent.Remove(name); err != nil {
			return err
		}
	}
	return syncDirectory(parent)
}
