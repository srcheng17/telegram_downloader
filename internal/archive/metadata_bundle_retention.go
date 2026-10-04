package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

const maxRetainedSource int64 = 500 << 20

// RetainMetadataBundle is safe to call for a partial, failed extraction. It
// keeps the entire input as well as individually recoverable metadata bytes.
// Callers may release an upload source only after persisting this manifest.
func RetainMetadataBundle(ctx context.Context, bundle *MetadataBundle, privateDir string) (RetentionManifest, error) {
	manifest := RetentionManifest{Version: 1, Files: []RetainedFile{}}
	root, err := privateRoot(privateDir)
	if err != nil {
		return manifest, err
	}
	defer root.Close()
	if bundle != nil && bundle.SourcePath != "" {
		info, err := os.Lstat(bundle.SourcePath)
		if err != nil || !info.Mode().IsRegular() {
			return manifest, errors.New("retention source is not a regular file")
		}
		source, err := os.Open(bundle.SourcePath)
		if err != nil {
			return manifest, err
		}
		opened, err := source.Stat()
		if err != nil || !os.SameFile(info, opened) {
			source.Close()
			return manifest, errors.New("retention source changed")
		}
		file, err := retainFile(ctx, root, "source-archive.bin", "source_archive", "", source, maxRetainedSource)
		closeErr := source.Close()
		if err != nil {
			return manifest, err
		}
		if closeErr != nil {
			return manifest, closeErr
		}
		manifest.Files = append(manifest.Files, file)
		manifest.SourceRetained = true
	}
	if bundle != nil {
		if len(bundle.RawMetadata) > 256 {
			return manifest, ErrMetadataLimit
		}
		total := int64(len(bundle.ZIPComment))
		for index, entry := range bundle.RawMetadata {
			total += int64(len(entry.Data))
			if len(entry.Data) > int(MaxMetadataEntryBytes) || total > MaxMetadataTotalBytes {
				return manifest, ErrMetadataLimit
			}
			name := fmt.Sprintf("metadata-%03d.bin", index+1)
			file, err := retainFile(ctx, root, name, entry.Kind, entry.Name, bytes.NewReader(entry.Data), MaxMetadataEntryBytes)
			if err != nil {
				return manifest, err
			}
			manifest.Files = append(manifest.Files, file)
		}
		if len(bundle.ZIPComment) > 0 {
			file, err := retainFile(ctx, root, "zip-comment.bin", "zip_comment", "ZIP comment", bytes.NewReader(bundle.ZIPComment), 65535)
			if err != nil {
				return manifest, err
			}
			manifest.Files = append(manifest.Files, file)
		}
	}
	if err = taskcore.ValidateRetentionManifest(manifest); err != nil {
		return manifest, err
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	if _, err = retainFile(ctx, root, "source-manifest.json", "", "", bytes.NewReader(raw), MaxMetadataTotalBytes); err != nil {
		return manifest, err
	}
	return manifest, nil
}

// RetainEffectiveMetadata records confirmed/derived values which have no XML
// mapping, together with provenance, outside the publicly downloadable CBZ.
func RetainEffectiveMetadata(ctx context.Context, privateDir string, raw []byte, manifest RetentionManifest) (RetentionManifest, error) {
	root, err := privateRoot(privateDir)
	if err != nil {
		return manifest, err
	}
	defer root.Close()
	file, err := retainFile(ctx, root, "effective-metadata.json", "custom_metadata", "", bytes.NewReader(raw), MaxMetadataTotalBytes)
	if err != nil {
		return manifest, err
	}
	manifest.Files = append(manifest.Files, file)
	if err = taskcore.ValidateRetentionManifest(manifest); err != nil {
		return manifest, err
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	_, err = retainFile(ctx, root, "metadata-manifest.json", "", "", bytes.NewReader(data), MaxMetadataTotalBytes)
	return manifest, err
}
func privateRoot(dir string) (*os.Root, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("private retention directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private retention directory must be owner-only and not a symlink")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, errors.New("private retention directory changed")
	}
	return root, nil
}
func retainFile(ctx context.Context, root *os.Root, name, kind, original string, reader io.Reader, limit int64) (RetainedFile, error) {
	result := RetainedFile{Name: name, Kind: kind, OriginalName: original}
	temporary := "retention-tmp-" + uuid.NewString() + ".bin"
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	defer root.Remove(temporary)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(contextReader{ctx: ctx, reader: reader}, limit+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return result, copyErr
	}
	if written > limit {
		return result, errors.New("retained file exceeds limit")
	}
	if syncErr != nil {
		return result, syncErr
	}
	if closeErr != nil {
		return result, closeErr
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Bytes = written
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if err = verifyRetained(ctx, root, temporary, result); err != nil {
		return result, err
	}
	// Atomic no-clobber publication. Existing bytes are accepted only when their
	// size/hash match, making repeat retention safe without replacing evidence.
	if err = root.Link(temporary, name); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return result, err
		}
		if err = verifyRetained(ctx, root, name, result); err != nil {
			return result, err
		}
	}
	directory, err := root.Open(".")
	if err != nil {
		return result, err
	}
	err = directory.Sync()
	closeErr = directory.Close()
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	return result, nil
}
func verifyRetained(ctx context.Context, root *os.Root, name string, expected RetainedFile) error {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != expected.Bytes {
		return errors.New("retained file verification failed")
	}
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, contextReader{ctx: ctx, reader: file})
	if err != nil {
		return err
	}
	if size != expected.Bytes || hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		return errors.New("retained file digest mismatch")
	}
	return nil
}
