package cbzedit

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const originalXML = `<ComicInfo><Title>Old</Title><PageCount>1</PageCount><Pages><Page Image="0" Type="FrontCover"></Page></Pages></ComicInfo>`
const updatedXML = `<ComicInfo><Title>New</Title><PageCount>1</PageCount><Pages><Page Image="0" Type="FrontCover"></Page></Pages></ComicInfo>`

func TestStandardComicInfoNamespacesAreEditable(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	withNamespaces := strings.Replace(originalXML, "<ComicInfo>", `<ComicInfo xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">`, 1)
	fixtureCBZ(t, filepath.Join(mediaDir, "book.cbz"), withNamespaces, []string{"1.jpg", "ComicInfo.xml"})
	before, err := Inspect(ctx, media, "book.cbz", Limits{})
	if err != nil {
		t.Fatalf("standard ComicInfo namespace declarations rejected: %v", err)
	}
	prepared, err := Prepare(ctx, media, backup, PrepareRequest{
		RelativePath: "book.cbz", OperationID: "operation-namespace", ExpectedSHA256: before.SHA256,
		NewComicInfo: []byte(updatedXML), ChangedElements: []string{"Title"},
	})
	if err != nil {
		t.Fatalf("prepare standard ComicInfo: %v", err)
	}
	if _, err := Commit(ctx, media, backup, prepared); err != nil {
		t.Fatalf("commit standard ComicInfo: %v", err)
	}
	if after, err := Inspect(ctx, media, "book.cbz", Limits{}); err != nil || string(after.ComicInfo) != updatedXML {
		t.Fatalf("readback after rewrite: %v", err)
	}

	fixtureCBZ(t, filepath.Join(mediaDir, "book.cbz"), strings.Replace(originalXML, "<ComicInfo>", `<ComicInfo private="value">`, 1), []string{"1.jpg", "ComicInfo.xml"})
	if _, err := Inspect(ctx, media, "book.cbz", Limits{}); !errors.Is(err, ErrInvalidXML) {
		t.Fatalf("unsafe root attribute accepted: %v", err)
	}
}

func fixtureCBZ(t *testing.T, fileName string, xml string, names []string) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	if err := writer.SetComment("reader note"); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Comment: "entry note"}
		member, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		content := []byte("attachment-" + name)
		if name == "ComicInfo.xml" {
			content = []byte(xml)
		}
		if _, err := member.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileName, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func roots(t *testing.T) (*os.Root, *os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	mediaDir := filepath.Join(dir, "media")
	backupDir := filepath.Join(dir, "private")
	if err := os.Mkdir(mediaDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	media, err := os.OpenRoot(mediaDir)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.OpenRoot(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { media.Close(); backup.Close() })
	return media, backup, mediaDir
}

func TestPrepareCommitPreservesUnchangedMembersAndPrivateOriginal(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	if err := os.Mkdir(filepath.Join(mediaDir, "series"), 0700); err != nil {
		t.Fatal(err)
	}
	original := fixtureCBZ(t, filepath.Join(mediaDir, "series", "book.cbz"), originalXML, []string{"10.jpg", "ComicInfo.xml", "2.png", "notes.txt"})
	before, err := Inspect(ctx, media, "series/book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(ctx, media, backup, PrepareRequest{
		RelativePath: "series/book.cbz", OperationID: "operation-12345678", ExpectedSHA256: before.SHA256,
		NewComicInfo: []byte(updatedXML), ChangedElements: []string{"Title"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(prepared.TemporaryPath), ".komga-edit-") || filepath.Ext(prepared.TemporaryPath) != ".tmp" {
		t.Fatalf("unsafe temporary path %q", prepared.TemporaryPath)
	}
	if current, err := os.ReadFile(filepath.Join(mediaDir, "series", "book.cbz")); err != nil || !bytes.Equal(current, original) {
		t.Fatal("Prepare changed the original")
	}
	backupBytes, err := backup.ReadFile(prepared.BackupPath)
	if err != nil || !bytes.Equal(backupBytes, original) {
		t.Fatal("backup is not an independent original copy")
	}
	result, err := Commit(ctx, media, backup, prepared)
	if err != nil || !result.FileCommitted || result.SHA256 != prepared.NewSHA256 {
		t.Fatalf("commit result=%+v err=%v", result, err)
	}
	after, err := Inspect(ctx, media, "series/book.cbz", Limits{})
	if err != nil || string(after.ComicInfo) != updatedXML {
		t.Fatalf("readback=%+v err=%v", after, err)
	}
	oldState, err := inspectBackupState(ctx, backup, prepared.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	newState, err := inspectRoot(ctx, media, "series/book.cbz", normalizeLimits(Limits{}))
	if err != nil || compareArchives(oldState, newState, []byte(updatedXML)) != nil {
		t.Fatalf("archive changed outside XML: %v", err)
	}
	if _, err := media.Lstat(prepared.TemporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("committed temp was not consumed")
	}
	if got, err := backup.ReadFile(prepared.BackupPath); err != nil || !bytes.Equal(got, original) {
		t.Fatal("backup changed after commit")
	}
}

func inspectBackupState(ctx context.Context, root *os.Root, relative string) (archiveState, error) {
	file, err := root.Open(relative)
	if err != nil {
		return archiveState{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return archiveState{}, err
	}
	return inspectFile(ctx, file, info.Size(), normalizeLimits(Limits{}))
}

func TestPrepareAppendXMLAndAbortOnConflict(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	path := filepath.Join(mediaDir, "book.cbz")
	original := fixtureCBZ(t, path, "", []string{"1.jpg", "extra.bin"})
	before, err := Inspect(ctx, media, "book.cbz", Limits{})
	if err != nil || before.HasComicInfo {
		t.Fatalf("inspect=%+v err=%v", before, err)
	}
	prepared, err := Prepare(ctx, media, backup, PrepareRequest{
		RelativePath: "book.cbz", OperationID: "operation-abcdef", ExpectedSHA256: before.SHA256,
		NewComicInfo: []byte(`<ComicInfo><Title>New</Title></ComicInfo>`), ChangedElements: []string{"Title"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a non-cooperating writer after Prepare. Commit must refuse and
	// must not silently restore over that writer's version.
	changed := append(bytes.Clone(original), byte('x'))
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := Commit(ctx, media, backup, prepared); !errors.Is(err, ErrConflict) || result.FileCommitted {
		t.Fatalf("commit result=%+v err=%v", result, err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, changed) {
		t.Fatal("conflict overwrote external version")
	}
	if err := Abort(media, prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := media.Lstat(prepared.TemporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("abort left temp")
	}
	if got, err := backup.ReadFile(prepared.BackupPath); err != nil || !bytes.Equal(got, original) {
		t.Fatal("abort discarded the original backup")
	}
}

func TestRefuseUnsafeArchiveXMLAndSymlink(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml"})
	before, err := Inspect(ctx, media, "book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		xml  string
		keys []string
	}{
		{"unlisted field change", `<ComicInfo><Title>New</Title><PageCount>2</PageCount><Pages><Page Image="0" Type="FrontCover"></Page></Pages></ComicInfo>`, []string{"Title"}},
		{"pages change", `<ComicInfo><Title>New</Title><PageCount>1</PageCount><Pages><Page Image="0" Type="BackCover"></Page></Pages></ComicInfo>`, []string{"Title"}},
		{"unknown extension", `<ComicInfo><Title>New</Title><PageCount>1</PageCount><Pages><Page Image="0" Type="FrontCover"></Page></Pages><Private>loss</Private></ComicInfo>`, []string{"Title"}},
		{"xml comment", `<ComicInfo><!-- private --><Title>New</Title><PageCount>1</PageCount><Pages><Page Image="0" Type="FrontCover"></Page></Pages></ComicInfo>`, []string{"Title"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Prepare(ctx, media, backup, PrepareRequest{RelativePath: "book.cbz", OperationID: "operation-12345678", ExpectedSHA256: before.SHA256, NewComicInfo: []byte(tc.xml), ChangedElements: tc.keys})
			if !errors.Is(err, ErrInvalidXML) {
				t.Fatalf("expected XML rejection, got %v", err)
			}
		})
	}
	if err := os.Symlink(book, filepath.Join(mediaDir, "linked.cbz")); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, media, "linked.cbz", Limits{}); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("symlink accepted: %v", err)
	}
	if got, err := os.ReadFile(book); err != nil || digest(got) != before.SHA256 {
		t.Fatal("rejected edits changed source")
	}
}

func TestRefusePrefixAndDuplicateComicInfo(t *testing.T) {
	ctx := context.Background()
	media, _, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml", "sub/ComicInfo.xml"})
	if _, err := Inspect(ctx, media, "book.cbz", Limits{}); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("ambiguous XML accepted: %v", err)
	}
	original := fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml"})
	withPrefix := append([]byte("prefix"), original...)
	if err := os.WriteFile(book, withPrefix, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, media, "book.cbz", Limits{}); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("prefixed ZIP accepted: %v", err)
	}
}

func TestCanceledPrepareLeavesOriginal(t *testing.T) {
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	original := fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml"})
	before, err := Inspect(context.Background(), media, "book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Prepare(ctx, media, backup, PrepareRequest{RelativePath: "book.cbz", OperationID: "operation-12345678", ExpectedSHA256: before.SHA256, NewComicInfo: []byte(updatedXML), ChangedElements: []string{"Title"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	got, err := os.ReadFile(book)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("canceled prepare changed source")
	}
}

func TestBackupDoesNotLinkOriginal(t *testing.T) {
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml"})
	before, err := Inspect(context.Background(), media, "book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), media, backup, PrepareRequest{RelativePath: "book.cbz", OperationID: "operation-12345678", ExpectedSHA256: before.SHA256, NewComicInfo: []byte(updatedXML), ChangedElements: []string{"Title"}})
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(book)
	if err != nil {
		t.Fatal(err)
	}
	backupInfo, err := backup.Stat(prepared.BackupPath)
	if err != nil || os.SameFile(originalInfo, backupInfo) {
		t.Fatal("backup is a hard link to editable source")
	}
	if err := Abort(media, prepared); err != nil {
		t.Fatal(err)
	}
	file, err := backup.Open(prepared.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := io.Copy(io.Discard, file); err != nil {
		t.Fatal(err)
	}
}

func TestCommitRefusesTamperedBackupAndPreservesPermissions(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	original := fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml"})
	if err := os.Chmod(book, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := Inspect(ctx, media, "book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(ctx, media, backup, PrepareRequest{RelativePath: "book.cbz", OperationID: "operation-12345678", ExpectedSHA256: before.SHA256, NewComicInfo: []byte(updatedXML), ChangedElements: []string{"Title"}})
	if err != nil {
		t.Fatal(err)
	}
	// A previous Commit attempt can set source permissions on the temp and
	// then fail before rename. The prepared operation must remain retryable.
	if err := media.Chmod(prepared.TemporaryPath, 0644); err != nil {
		t.Fatal(err)
	}
	backupFile, err := backup.OpenFile(prepared.BackupPath, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backupFile.WriteAt([]byte("changed"), 0); err != nil {
		t.Fatal(err)
	}
	backupFile.Close()
	if result, err := Commit(ctx, media, backup, prepared); !errors.Is(err, ErrBackup) || result.FileCommitted {
		t.Fatalf("tampered backup accepted: result=%+v err=%v", result, err)
	}
	if got, err := os.ReadFile(book); err != nil || !bytes.Equal(got, original) {
		t.Fatal("backup failure altered original")
	}
	if err := backup.WriteFile(prepared.BackupPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Commit(ctx, media, backup, prepared)
	if err != nil || !result.FileCommitted {
		t.Fatalf("commit after repaired backup: result=%+v err=%v", result, err)
	}
	info, err := os.Stat(book)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("source permissions changed: mode=%v", info.Mode())
	}
}

func TestArchiveLimitsAndIntermediateSymlink(t *testing.T) {
	ctx := context.Background()
	media, _, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml"})
	if _, err := Inspect(ctx, media, "book.cbz", Limits{MaxEntries: 1}); !errors.Is(err, ErrLimit) {
		t.Fatalf("entry limit ignored: %v", err)
	}
	if _, err := Inspect(ctx, media, "book.cbz", Limits{MaxArchiveBytes: 20}); !errors.Is(err, ErrLimit) {
		t.Fatalf("archive limit ignored: %v", err)
	}
	if err := os.Symlink(mediaDir, filepath.Join(mediaDir, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, media, "linked/book.cbz", Limits{}); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("intermediate symlink accepted: %v", err)
	}
}

func TestExplicitPageCountCorrection(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	wrongCount := strings.Replace(originalXML, "<PageCount>1</PageCount>", "<PageCount>9</PageCount>", 1)
	fixtureCBZ(t, book, wrongCount, []string{"1.jpg", "ComicInfo.xml"})
	before, err := Inspect(ctx, media, "book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Prepare(ctx, media, backup, PrepareRequest{RelativePath: "book.cbz", OperationID: "operation-12345678", ExpectedSHA256: before.SHA256, NewComicInfo: []byte(updatedXML), ChangedElements: []string{"Title", "PageCount"}})
	if !errors.Is(err, ErrInvalidXML) {
		t.Fatalf("implicit PageCount correction accepted: %v", err)
	}
	prepared, err := Prepare(ctx, media, backup, PrepareRequest{RelativePath: "book.cbz", OperationID: "operation-12345678", ExpectedSHA256: before.SHA256, NewComicInfo: []byte(updatedXML), ChangedElements: []string{"Title", "PageCount"}, AllowPageCountCorrection: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := Abort(media, prepared); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptMemberCRCIsRejectedBeforeBackup(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	raw := fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml"})
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	offset, err := reader.File[0].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.Clone(raw)
	corrupt[offset] ^= 0xff
	if err := os.WriteFile(book, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, media, "book.cbz", Limits{}); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("corrupt image accepted: %v", err)
	}
	if entries, err := backup.ReadFile("operation-12345678/manifest.json"); err == nil || len(entries) > 0 {
		t.Fatal("inspection created a backup")
	}
}

func TestPreservesTimestampAndEntryComment(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	file, err := os.Create(book)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, name := range []string{"1.jpg", "ComicInfo.xml"} {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Comment: "entry comment"}
		header.SetModTime(time.Date(2024, time.March, 12, 13, 14, 16, 0, time.UTC))
		member, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		data := "page"
		if name == "ComicInfo.xml" {
			data = originalXML
		}
		if _, err := member.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := Inspect(ctx, media, "book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(ctx, media, backup, PrepareRequest{RelativePath: "book.cbz", OperationID: "operation-12345678", ExpectedSHA256: before.SHA256, NewComicInfo: []byte(updatedXML), ChangedElements: []string{"Title"}})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := Commit(ctx, media, backup, prepared); err != nil || !result.FileCommitted {
		t.Fatalf("commit result=%+v err=%v", result, err)
	}
}
