package cbzedit

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func preparedFixture(t *testing.T) (*os.Root, *os.Root, string, []byte, Prepared) {
	t.Helper()
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	original := fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml", "2.png"})
	before, err := Inspect(context.Background(), media, "book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), media, backup, PrepareRequest{
		RelativePath: "book.cbz", OperationID: "operation-12345678", ExpectedSHA256: before.SHA256,
		NewComicInfo: []byte(updatedXML), ChangedElements: []string{"Title"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return media, backup, book, original, prepared
}

func TestProbeRecoversPreparedDescriptorAndCommitAcrossCrashWindow(t *testing.T) {
	ctx := context.Background()
	media, backup, _, _, prepared := preparedFixture(t)
	result, err := Probe(ctx, media, backup, prepared.RelativePath, prepared.OperationID, prepared.OriginalSHA256)
	if err != nil || result.State != ProbePrepared || !result.BackupValid || !result.TemporaryExists || result.Prepared == nil || result.Prepared.NewSHA256 != prepared.NewSHA256 {
		t.Fatalf("prepared probe: %+v, %v", result, err)
	}
	if _, err := Commit(ctx, media, backup, *result.Prepared); err != nil {
		t.Fatal(err)
	}
	result, err = Probe(ctx, media, backup, prepared.RelativePath, prepared.OperationID, prepared.OriginalSHA256)
	if err != nil || result.State != ProbeCommitted || result.TemporaryExists || result.CurrentSHA256 != prepared.NewSHA256 {
		t.Fatalf("post-rename probe: %+v, %v", result, err)
	}
}

func TestProbeIncompleteAndCorruptPrivateEvidence(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml"})
	before, err := Inspect(ctx, media, "book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	missing, err := Probe(ctx, media, backup, "book.cbz", "operation-12345678", before.SHA256)
	if err != nil || missing.State != ProbeMissing {
		t.Fatalf("missing probe: %+v, %v", missing, err)
	}
	if _, err := copyBackup(ctx, media, backup, "book.cbz", "operation-12345678", before.SHA256, before.Size); err != nil {
		t.Fatal(err)
	}
	incomplete, err := Probe(ctx, media, backup, "book.cbz", "operation-12345678", before.SHA256)
	if err != nil || incomplete.State != ProbeIncomplete || !incomplete.BackupValid || incomplete.Prepared != nil || incomplete.CurrentSHA256 != before.SHA256 {
		t.Fatalf("incomplete probe: %+v, %v", incomplete, err)
	}
	file, err := backup.OpenFile("operation-12345678/"+backupFileName, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("damaged"), 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	corrupt, err := Probe(ctx, media, backup, "book.cbz", "operation-12345678", before.SHA256)
	if !errors.Is(err, ErrBackup) || !corrupt.BackupExists || corrupt.BackupValid {
		t.Fatalf("corrupt evidence accepted: %+v, %v", corrupt, err)
	}
}

func TestProbePrivateDirectoryCreatedBeforeBackupManifest(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml"})
	before, err := Inspect(ctx, media, "book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	operationRoot, err := privateOperationRoot(backup, "operation-12345678", true)
	if err != nil {
		t.Fatal(err)
	}
	operationRoot.Close()
	probe, err := Probe(ctx, media, backup, "book.cbz", "operation-12345678", before.SHA256)
	if err != nil || probe.State != ProbeIncomplete || probe.BackupExists || probe.CurrentSHA256 != before.SHA256 {
		t.Fatalf("early-crash probe: %+v, %v", probe, err)
	}
	if err := AbandonIncomplete(ctx, media, backup, "book.cbz", "operation-12345678", before.SHA256); err != nil {
		t.Fatalf("early-crash abandonment: %v", err)
	}
}

func TestRestorePreservesEditedVersionAndRequiresExactCurrentHash(t *testing.T) {
	ctx := context.Background()
	media, backup, book, original, prepared := preparedFixture(t)
	if _, err := Commit(ctx, media, backup, prepared); err != nil {
		t.Fatal(err)
	}
	edited, err := os.ReadFile(book)
	if err != nil || digest(edited) != prepared.NewSHA256 {
		t.Fatal("edited archive missing")
	}
	result, err := Restore(ctx, media, backup, prepared)
	if err != nil || !result.FileRestored || result.SHA256 != prepared.OriginalSHA256 || result.PreservedRef != prepared.OperationID+"/"+preservedFileName {
		t.Fatalf("restore result: %+v, %v", result, err)
	}
	current, err := os.ReadFile(book)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("restored bytes differ from original")
	}
	preserved, err := backup.ReadFile(result.PreservedRef)
	if err != nil || !bytes.Equal(preserved, edited) {
		t.Fatal("pre-restore version was not retained")
	}
	currentInfo, err := os.Stat(book)
	if err != nil {
		t.Fatal(err)
	}
	preservedInfo, err := backup.Stat(result.PreservedRef)
	if err != nil || os.SameFile(currentInfo, preservedInfo) {
		t.Fatal("preserved version is a hard link to the current archive")
	}
	probe, err := Probe(ctx, media, backup, prepared.RelativePath, prepared.OperationID, prepared.OriginalSHA256)
	if err != nil || probe.State != ProbeRestored || !probe.PreservedCurrent {
		t.Fatalf("restored probe: %+v, %v", probe, err)
	}
	if _, err := Restore(ctx, media, backup, prepared); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeat restore should refuse changed current version: %v", err)
	}
}

func TestRestoreRejectsExternalWriterAndTamperedBackup(t *testing.T) {
	ctx := context.Background()
	media, backup, book, _, prepared := preparedFixture(t)
	if _, err := Commit(ctx, media, backup, prepared); err != nil {
		t.Fatal(err)
	}
	edited, err := os.ReadFile(book)
	if err != nil {
		t.Fatal(err)
	}
	newer := fixtureCBZ(t, book, `<ComicInfo><Title>Third</Title><PageCount>1</PageCount><Pages><Page Image="0" Type="FrontCover"></Page></Pages></ComicInfo>`, []string{"1.jpg", "ComicInfo.xml", "2.png"})
	if _, err := Restore(ctx, media, backup, prepared); !errors.Is(err, ErrConflict) {
		t.Fatalf("external writer overwritten: %v", err)
	}
	if got, err := os.ReadFile(book); err != nil || !bytes.Equal(got, newer) {
		t.Fatal("external version was changed")
	}
	probe, err := Probe(ctx, media, backup, prepared.RelativePath, prepared.OperationID, prepared.OriginalSHA256)
	if err != nil || probe.State != ProbeConflict {
		t.Fatalf("external-write probe: %+v, %v", probe, err)
	}
	if err := os.WriteFile(book, edited, 0600); err != nil {
		t.Fatal(err)
	}
	backupFile, err := backup.OpenFile(prepared.BackupPath, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backupFile.WriteAt([]byte("damaged"), 0); err != nil {
		t.Fatal(err)
	}
	if err := backupFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBackup(ctx, backup, prepared); !errors.Is(err, ErrBackup) {
		t.Fatalf("corrupt backup accepted: %v", err)
	}
	if _, err := Restore(ctx, media, backup, prepared); !errors.Is(err, ErrBackup) {
		t.Fatalf("restore accepted corrupt backup: %v", err)
	}
	if got, err := os.ReadFile(book); err != nil || !bytes.Equal(got, edited) {
		t.Fatal("corrupt backup restore changed target")
	}
}

func TestProbeRejectsTamperedPreparedManifestAndRestorationEvidence(t *testing.T) {
	ctx := context.Background()
	media, backup, _, _, prepared := preparedFixture(t)
	manifestPath := prepared.OperationID + "/" + preparedManifestName
	if err := backup.WriteFile(manifestPath, []byte(`{"version":1,"prepared":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Probe(ctx, media, backup, prepared.RelativePath, prepared.OperationID, prepared.OriginalSHA256); !errors.Is(err, ErrBackup) {
		t.Fatalf("tampered descriptor accepted: %v", err)
	}
}

func TestProbeRefusesTamperedPreparedTemporary(t *testing.T) {
	ctx := context.Background()
	media, backup, _, _, prepared := preparedFixture(t)
	temporary, err := media.OpenFile(prepared.TemporaryPath, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := temporary.WriteAt([]byte("damaged"), 0); err != nil {
		t.Fatal(err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := Probe(ctx, media, backup, prepared.RelativePath, prepared.OperationID, prepared.OriginalSHA256)
	if err != nil || result.State != ProbeConflict || !result.TemporaryExists || !result.BackupValid {
		t.Fatalf("tampered temporary accepted: %+v, %v", result, err)
	}
	if err := AbandonIncomplete(ctx, media, backup, prepared.RelativePath, prepared.OperationID, prepared.OriginalSHA256); !errors.Is(err, ErrConflict) {
		t.Fatalf("tampered prepared descriptor should require manual handling: %v", err)
	}
}

func TestAbandonIncompleteRemovesOnlyOwnOrphans(t *testing.T) {
	ctx := context.Background()
	media, backup, mediaDir := roots(t)
	book := filepath.Join(mediaDir, "book.cbz")
	fixtureCBZ(t, book, originalXML, []string{"1.jpg", "ComicInfo.xml"})
	before, err := Inspect(ctx, media, "book.cbz", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := copyBackup(ctx, media, backup, "book.cbz", "operation-12345678", before.SHA256, before.Size); err != nil {
		t.Fatal(err)
	}
	ownName, ownFile, err := newTemporary(media, "operation-12345678")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ownFile.WriteString("interrupted write"); err != nil {
		t.Fatal(err)
	}
	if err := ownFile.Close(); err != nil {
		t.Fatal(err)
	}
	otherName, otherFile, err := newTemporary(media, "different-12345678")
	if err != nil {
		t.Fatal(err)
	}
	otherFile.Close()
	if err := AbandonIncomplete(ctx, media, backup, "book.cbz", "operation-12345678", before.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := media.Lstat(ownName); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("operation orphan was not removed")
	}
	if _, err := media.Lstat(otherName); err != nil {
		t.Fatal("another operation's temp was removed")
	}
	if err := VerifyOriginalBackup(ctx, backup, "operation-12345678", before.SHA256, before.Size); err != nil {
		t.Fatal("abandon discarded the original backup")
	}
	if current, err := Inspect(ctx, media, "book.cbz", Limits{}); err != nil || current.SHA256 != before.SHA256 {
		t.Fatal("abandon changed the source")
	}
}

func TestAbandonIncompleteRefusesPreparedAndConflictedSource(t *testing.T) {
	ctx := context.Background()
	media, backup, book, _, prepared := preparedFixture(t)
	if err := AbandonIncomplete(ctx, media, backup, prepared.RelativePath, prepared.OperationID, prepared.OriginalSHA256); !errors.Is(err, ErrConflict) {
		t.Fatalf("valid prepared temp abandoned: %v", err)
	}
	if _, err := media.Lstat(prepared.TemporaryPath); err != nil {
		t.Fatal("valid prepared temp disappeared")
	}
	if err := os.WriteFile(book, []byte("external data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AbandonIncomplete(ctx, media, backup, prepared.RelativePath, prepared.OperationID, prepared.OriginalSHA256); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed target should block cleanup: %v", err)
	}
}
