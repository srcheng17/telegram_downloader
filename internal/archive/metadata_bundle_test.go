package archive

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type bundleEntry struct{ name, body string }

func writeBundleFixture(t *testing.T, entries []bundleEntry, comment string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.cbz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	writer.SetComment(comment)
	for _, entry := range entries {
		member, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		member.Write([]byte(entry.body))
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()
	return path
}
func TestBundleNaturalOrderIdentityAndDurableMetadataRetention(t *testing.T) {
	source := writeBundleFixture(t, []bundleEntry{{"章/10.jpg", "ten"}, {"章/2.jpg", "two-a"}, {"章/2.jpg", "two-b"}, {"章/1.jpg", "one"}, {"other/1.jpg", "other"}, {"meta/ComicInfo.xml", "<ComicInfo><Title>Source</Title></ComicInfo>"}, {"MetronInfo.xml", "<MetronInfo/>"}, {"extra.txt", "unexported attachment"}}, `{"ComicBookInfo/1.0":{"title":"competing"}}`)
	bundle, err := NewExtractor(ExtractorConfig{}).ExtractWithMetadata(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"other", "one", "two-a", "two-b", "ten"}
	for i, image := range bundle.Images {
		if string(image.Data) != want[i] || bundle.Pages[i].OutputIndex != i {
			t.Fatal("page order or duplicate identity lost")
		}
	}
	if bundle.PageOrderKnown || bundle.OutputComment != "" || len(bundle.RawMetadata) != 2 || len(bundle.Warnings) == 0 {
		t.Fatal("ambiguity/competing metadata not retained")
	}
	private := filepath.Join(t.TempDir(), "private")
	manifest, err := RetainMetadataBundle(context.Background(), bundle, private)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.SourceRetained || len(manifest.Files) != 4 {
		t.Fatal("retention incomplete")
	}
	for _, file := range manifest.Files {
		data, err := os.ReadFile(filepath.Join(private, file.Name))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != file.SHA256 || int64(len(data)) != file.Bytes {
			t.Fatal("retained bytes mismatch")
		}
		info, _ := os.Stat(filepath.Join(private, file.Name))
		if info.Mode().Perm() != 0600 {
			t.Fatal("private evidence permissions")
		}
	}
	original, _ := os.ReadFile(source)
	retained, _ := os.ReadFile(filepath.Join(private, "source-archive.bin"))
	if string(original) != string(retained) {
		t.Fatal("source not preserved byte-for-byte")
	}
	if _, err = RetainMetadataBundle(context.Background(), bundle, private); err != nil {
		t.Fatal("identical repeat retention failed")
	}
}
func TestDuplicateOversizedAndMalformedArchiveKeepOriginal(t *testing.T) {
	for _, entries := range [][]bundleEntry{{{"1.jpg", "page"}, {"ComicInfo.xml", "<ComicInfo/>"}, {"ComicInfo.xml", "<ComicInfo/>"}}, {{"1.jpg", "page"}, {"ComicInfo.xml", strings.Repeat("x", int(MaxMetadataEntryBytes)+1)}}} {
		source := writeBundleFixture(t, entries, "")
		bundle, err := NewExtractor(ExtractorConfig{}).ExtractWithMetadata(context.Background(), source)
		if !errors.Is(err, ErrMultipleComicInfo) && !errors.Is(err, ErrMetadataLimit) {
			t.Fatalf("expected metadata failure: %v", err)
		}
		manifest, err := RetainMetadataBundle(context.Background(), bundle, filepath.Join(t.TempDir(), "private"))
		if err != nil || !manifest.SourceRetained {
			t.Fatal("failed archive not retained")
		}
		if _, err = os.Stat(source); err != nil {
			t.Fatal("original removed")
		}
	}
	source := filepath.Join(t.TempDir(), "bad.cbz")
	os.WriteFile(source, []byte("broken zip"), 0600)
	bundle, err := NewExtractor(ExtractorConfig{}).ExtractWithMetadata(context.Background(), source)
	if err == nil {
		t.Fatal("broken zip accepted")
	}
	manifest, err := RetainMetadataBundle(context.Background(), bundle, filepath.Join(t.TempDir(), "private"))
	if err != nil || !manifest.SourceRetained {
		t.Fatal("broken original not retained")
	}
}
func TestPlainCommentAndKnownPageOrder(t *testing.T) {
	source := writeBundleFixture(t, []bundleEntry{{"1.jpg", "one"}, {"2.jpg", "two"}, {"10.jpg", "ten"}}, "Personal archive note")
	bundle, err := NewExtractor(ExtractorConfig{}).ExtractWithMetadata(context.Background(), source)
	if err != nil || !bundle.PageOrderKnown || bundle.OutputComment != "Personal archive note" {
		t.Fatal("plain comment/order lost")
	}
}

func TestCorruptImageCRCAndSymlinkRetentionFailClosed(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "crc.cbz")
	file, _ := os.Create(source)
	writer := zip.NewWriter(file)
	member, _ := writer.CreateHeader(&zip.FileHeader{Name: "1.jpg", Method: zip.Store})
	member.Write([]byte("unique-uncompressed-image"))
	writer.Close()
	file.Close()
	raw, _ := os.ReadFile(source)
	index := strings.Index(string(raw), "unique-uncompressed-image")
	if index < 0 {
		t.Fatal("fixture lacks stored image")
	}
	raw[index] ^= 0xff
	os.WriteFile(source, raw, 0600)
	bundle, err := NewExtractor(ExtractorConfig{}).ExtractWithMetadata(context.Background(), source)
	if err == nil {
		t.Fatal("CRC-corrupt image accepted")
	}
	manifest, err := RetainMetadataBundle(context.Background(), bundle, filepath.Join(dir, "private"))
	if err != nil || !manifest.SourceRetained {
		t.Fatal("CRC-corrupt original not recoverable")
	}
	symlink := filepath.Join(dir, "source-link.cbz")
	if err = os.Symlink(source, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err = RetainMetadataBundle(context.Background(), &MetadataBundle{SourcePath: symlink}, filepath.Join(dir, "private-link-source")); err == nil {
		t.Fatal("source symlink followed")
	}
	privateLink := filepath.Join(dir, "private-link")
	os.Symlink(filepath.Join(dir, "private"), privateLink)
	if _, err = RetainMetadataBundle(context.Background(), bundle, privateLink); err == nil {
		t.Fatal("private destination symlink followed")
	}
}
