package tasks

import (
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	domainkomga "github.com/ryancheng/telegram-downloader/internal/domain/komga"
)

var ErrKomgaTargetConflict = errors.New("Komga target already contains different content")
var ErrKomgaUnsafeTarget = errors.New("Komga target is not a safe regular file")

type KomgaCopyConfig struct{ Root string }
type KomgaCopier struct{ root string }

func NewKomgaCopier(cfg KomgaCopyConfig) *KomgaCopier {
	return &KomgaCopier{root: strings.TrimSpace(cfg.Root)}
}
func (c *KomgaCopier) TargetPath(fileName, seriesName string) (string, error) {
	return domainkomga.BuildTargetPath(c.root, fileName, seriesName)
}

// CopyFromPath is idempotent by exact contents. Publication uses link(2)'s
// create-if-absent behavior, never a replacing rename or a partial target file.
// A retry after a lost response simply verifies the existing regular file.
func (c *KomgaCopier) CopyFromPath(sourcePath, fileName, seriesName string) (string, error) {
	target, err := c.TargetPath(fileName, seriesName)
	if err != nil {
		return "", err
	}
	rootInfo, err := os.Lstat(c.root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", ErrKomgaUnsafeTarget
	}
	root, err := os.OpenRoot(c.root)
	if err != nil {
		return "", err
	}
	defer root.Close()
	relative, err := filepath.Rel(c.root, target)
	if err != nil || !filepath.IsLocal(relative) {
		return "", ErrKomgaUnsafeTarget
	}
	dir := filepath.Dir(relative)
	if err := root.Mkdir(dir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := root.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrKomgaUnsafeTarget
	}
	parent, err := root.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	source, err := os.Open(strings.TrimSpace(sourcePath))
	if err != nil {
		return "", err
	}
	defer source.Close()
	info, err = source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrArtifactUnavailable
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, source); err != nil {
		return "", err
	}
	expected := hash.Sum(nil)
	base := filepath.Base(relative)
	if exists, err := verifyKomgaExisting(parent, base, expected); err != nil || exists {
		if err != nil {
			return "", err
		}
		return target, nil
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	tempName := ".komga-copy-" + uuid.NewString() + ".tmp"
	temp, err := parent.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	defer parent.Remove(tempName)
	defer temp.Close()
	hash.Reset()
	if _, err := io.Copy(io.MultiWriter(temp, hash), source); err != nil {
		return "", err
	}
	if string(hash.Sum(nil)) != string(expected) {
		return "", ErrKomgaTargetConflict
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := parent.Link(tempName, base); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if _, err := verifyKomgaExisting(parent, base, expected); err != nil {
		return "", err
	}
	directory, err := parent.Open(".")
	if err != nil {
		return "", err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return "", err
	}
	return target, nil
}

func verifyKomgaExisting(parent *os.Root, base string, expected []byte) (bool, error) {
	entry, err := parent.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !entry.Mode().IsRegular() {
		return true, ErrKomgaUnsafeTarget
	}
	file, err := parent.Open(base)
	if err != nil {
		return true, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(entry, opened) {
		return true, ErrKomgaUnsafeTarget
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return true, err
	}
	if string(hash.Sum(nil)) != string(expected) {
		return true, ErrKomgaTargetConflict
	}
	return true, nil
}
