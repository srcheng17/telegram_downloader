package cbzedit

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

func privateOperationRoot(root *os.Root, id string, create bool) (*os.Root, error) {
	if root == nil {
		return nil, ErrBackup
	}
	info, err := root.Stat(".")
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrBackup
	}
	if err := validateOperationID(id); err != nil {
		return nil, ErrBackup
	}
	if create {
		if err := root.Mkdir(id, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	operationInfo, err := root.Lstat(id)
	if err != nil || !operationInfo.IsDir() || operationInfo.Mode()&os.ModeSymlink != 0 || operationInfo.Mode().Perm()&0077 != 0 {
		return nil, ErrBackup
	}
	operationRoot, err := root.OpenRoot(id)
	if err != nil {
		return nil, err
	}
	opened, err := operationRoot.Stat(".")
	if err != nil || !os.SameFile(operationInfo, opened) {
		operationRoot.Close()
		return nil, ErrBackup
	}
	return operationRoot, nil
}

func copyBackup(ctx context.Context, sourceRoot, backupRoot *os.Root, relative, id, expectedSHA string, expectedSize int64) (string, error) {
	operationRoot, err := privateOperationRoot(backupRoot, id, true)
	if err != nil {
		return "", err
	}
	defer operationRoot.Close()
	backupPath := id + "/" + backupFileName
	if _, err := operationRoot.Lstat(backupFileName); err == nil {
		if err := verifyBackupFile(ctx, operationRoot, backupFileName, expectedSHA, expectedSize); err != nil {
			return "", err
		}
		if err := ensureBackupManifest(operationRoot, id, expectedSHA, expectedSize); err != nil {
			return "", err
		}
		return backupPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent, _, original, info, err := openBook(sourceRoot, relative)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	defer original.Close()
	if info.Size() != expectedSize {
		return "", ErrConflict
	}
	tempName, temp, err := newBackupTemporary(operationRoot)
	if err != nil {
		return "", err
	}
	defer operationRoot.Remove(tempName)
	copyHash, err := hashCopy(ctx, temp, io.LimitReader(original, expectedSize+1))
	if syncErr := temp.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if copyHash.size != expectedSize || copyHash.digest != expectedSHA {
		return "", ErrConflict
	}
	if err := verifyBackupFile(ctx, operationRoot, tempName, expectedSHA, expectedSize); err != nil {
		return "", err
	}
	if err := operationRoot.Link(tempName, backupFileName); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		if err := verifyBackupFile(ctx, operationRoot, backupFileName, expectedSHA, expectedSize); err != nil {
			return "", err
		}
	}
	if err := ensureBackupManifest(operationRoot, id, expectedSHA, expectedSize); err != nil {
		return "", err
	}
	if err := syncDirectory(operationRoot); err != nil {
		return "", err
	}
	if err := syncDirectory(backupRoot); err != nil {
		return "", err
	}
	return backupPath, nil
}

func verifyBackup(ctx context.Context, backupRoot *os.Root, relative, sha string, size int64) error {
	parts := strings.Split(relative, "/")
	if len(parts) != 2 || parts[1] != backupFileName || !validDigest(sha) || size <= 0 {
		return ErrBackup
	}
	operationRoot, err := privateOperationRoot(backupRoot, parts[0], false)
	if err != nil {
		return err
	}
	defer operationRoot.Close()
	if err := verifyBackupManifest(operationRoot, parts[0], sha, size); err != nil {
		return err
	}
	return verifyBackupFile(ctx, operationRoot, backupFileName, sha, size)
}

type backupManifest struct {
	Version     int    `json:"version"`
	OperationID string `json:"operation_id"`
	SHA256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
}

func expectedManifest(id, sha string, size int64) []byte {
	data, _ := json.Marshal(backupManifest{Version: 1, OperationID: id, SHA256: sha, Bytes: size})
	return append(data, '\n')
}

func ensureBackupManifest(root *os.Root, id, sha string, size int64) error {
	if _, err := root.Lstat("manifest.json"); err == nil {
		return verifyBackupManifest(root, id, sha, size)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	name, file, err := newBackupTemporary(root)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	if _, err := file.Write(expectedManifest(id, sha, size)); err != nil {
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
	if err := root.Link(name, "manifest.json"); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := verifyBackupManifest(root, id, sha, size); err != nil {
		return err
	}
	return syncDirectory(root)
}

func verifyBackupManifest(root *os.Root, id, sha string, size int64) error {
	info, err := root.Lstat("manifest.json")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 512 {
		return ErrBackup
	}
	file, err := root.Open("manifest.json")
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 513))
	if err != nil || !bytes.Equal(data, expectedManifest(id, sha, size)) {
		return ErrBackup
	}
	return nil
}

func verifyBackupFile(ctx context.Context, root *os.Root, name, sha string, size int64) error {
	info, err := root.Lstat(name)
	if err != nil {
		return ErrBackup
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != size {
		return ErrBackup
	}
	file, err := root.Open(name)
	if err != nil {
		return ErrBackup
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return ErrBackup
	}
	sum, err := hashReader(ctx, io.LimitReader(file, size+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrBackup
	}
	if hex.EncodeToString(sum[:]) != sha {
		return ErrBackup
	}
	return nil
}

type copyDigest struct {
	size   int64
	digest string
}

func hashCopy(ctx context.Context, destination io.Writer, source io.Reader) (copyDigest, error) {
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(destination, hash), contextReader{ctx, source})
	return copyDigest{size: size, digest: hex.EncodeToString(hash.Sum(nil))}, err
}

func newBackupTemporary(root *os.Root) (string, *os.File, error) {
	for i := 0; i < 5; i++ {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", nil, err
		}
		name := ".backup-" + hex.EncodeToString(nonce[:]) + ".tmp"
		file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return name, file, err
	}
	return "", nil, ErrBackup
}
