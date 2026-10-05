package taskcore

import (
	"bytes"
	"context"
	"errors"
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	taskarchive "github.com/ryancheng/telegram-downloader/internal/archive"
	metadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/downloader"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fixedRegistry struct{}

func (fixedRegistry) Get(context.Context, string) (metadata.Registry, error) {
	return metadata.StandardRegistry(), nil
}

func TestWorkerRetainsSourceAndPublishesDerivedMetadata(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.cbz")
	writeTestZip(t, source, map[string]string{"10.jpg": "tenth", "2.jpg": "second", "ComicInfo.xml": "<ComicInfo><Title>Original</Title><Publisher>Original publisher</Publisher><Notes>Keep unedited</Notes></ComicInfo>"})
	title := "Updated"
	doc, err := metadata.FromLegacy(metadata.Legacy{ComicName: &title})
	if err != nil {
		t.Fatal(err)
	}
	task := app.Task{ID: "metadata-worker", Kind: domain.KindUpload, Status: domain.StatusRunning, Attempt: 1, Generation: 1, LeaseOwner: "worker"}
	tasks := &fakeTaskViewService{view: &app.TaskView{Task: task, Input: app.Input{SourceArchivePath: source, MetadataDocument: &doc}}}
	var retained domain.RetentionManifest
	d := NewTaskDownloader(TaskDownloaderConfig{Tasks: tasks, Service: &downloader.Service{}, Registry: fixedRegistry{}, DownloadRoot: filepath.Join(root, "public"), RetentionRoot: filepath.Join(root, "private"), RecordRetention: func(_ context.Context, got app.Task, m domain.RetentionManifest) error {
		if got.Generation != 1 {
			t.Fatal("unfenced retention")
		}
		retained = m
		return nil
	}})
	result, err := d.ExecuteWithMetadata(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if result.EffectiveMetadataDocument == nil || result.RetentionManifest == nil || !retained.SourceRetained {
		t.Fatal("effective metadata or retention missing")
	}
	if string(result.EffectiveMetadataDocument.Fields["title"].Value) != `"Updated"` || string(result.EffectiveMetadataDocument.Fields["page_count"].Value) != "2" {
		t.Fatal("wrong effective fields")
	}
	if string(readCBZEntry(t, result.Path, "0001.jpg")) != "second" || string(readCBZEntry(t, result.Path, "0002.jpg")) != "tenth" {
		t.Fatal("image order/bytes changed")
	}
	if _, err = os.Stat(filepath.Join(root, "private", task.ID, "1", "source-archive.bin")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(source); err != nil {
		t.Fatal("original removed before committed success")
	}
	if doc.Fields["page_count"].State != "" {
		t.Fatal("immutable submitted document changed")
	}
}

func TestWorkerMalformedMetadataRetainsSourceAndDoesNotPublish(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.zip")
	writeTestZip(t, source, map[string]string{"1.jpg": "bytes", "ComicInfo.xml": "<ComicInfo><Title>broken"})
	doc := metadata.EmptyDocument(metadata.StandardRegistry())
	task := app.Task{ID: "malformed-worker", Kind: domain.KindUpload, Status: domain.StatusRunning, Attempt: 1, Generation: 1, LeaseOwner: "worker"}
	retained := false
	d := NewTaskDownloader(TaskDownloaderConfig{Tasks: &fakeTaskViewService{view: &app.TaskView{Task: task, Input: app.Input{SourceArchivePath: source, MetadataDocument: &doc}}}, Service: &downloader.Service{}, Registry: fixedRegistry{}, DownloadRoot: filepath.Join(root, "public"), RetentionRoot: filepath.Join(root, "private"), RecordRetention: func(_ context.Context, _ app.Task, m domain.RetentionManifest) error {
		retained = m.SourceRetained
		return nil
	}})
	result, err := d.ExecuteWithMetadata(context.Background(), task)
	if err == nil || result.Path != "" || !retained {
		t.Fatal("malformed metadata published or lost source", err)
	}
}

func TestWorkerExistingArtifactRefusalDoesNotDeletePreviousOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.cbz")
	writeTestZip(t, source, map[string]string{"1.jpg": "new-image"})
	doc := metadata.EmptyDocument(metadata.StandardRegistry())
	task := app.Task{ID: "existing-artifact", Kind: domain.KindUpload, Status: domain.StatusRunning, Attempt: 1, Generation: 1, LeaseOwner: "worker"}
	view := &app.TaskView{Task: task, Input: app.Input{SourceArchivePath: source, MetadataDocument: &doc}}
	stamp := time.Unix(1700000000, 0)
	output := filepath.Join(root, "public", task.ID, "1", buildTaskCoreDownloadFilename(metadataFromInput(view.Input), stamp.Unix()))
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		t.Fatal(err)
	}
	previous := []byte("previous successful artifact bytes")
	if err := os.WriteFile(output, previous, 0600); err != nil {
		t.Fatal(err)
	}
	d := NewTaskDownloader(TaskDownloaderConfig{Tasks: &fakeTaskViewService{view: view}, Service: &downloader.Service{}, Registry: fixedRegistry{}, DownloadRoot: filepath.Join(root, "public"), RetentionRoot: filepath.Join(root, "private"), Now: func() time.Time { return stamp }, RecordRetention: func(context.Context, app.Task, domain.RetentionManifest) error { return nil }})
	result, err := d.ExecuteWithMetadata(context.Background(), task)
	if err == nil || result.Path != "" {
		t.Fatal("existing output accepted")
	}
	actual, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(actual, previous) {
		t.Fatal("existing artifact deleted or replaced", err)
	}
	if _, err = os.Stat(source); err != nil {
		t.Fatal("source removed on publication refusal")
	}
}

type cancelAfterMetadataExtraction struct{ cancel context.CancelFunc }

func (e cancelAfterMetadataExtraction) Extract(ctx context.Context, path string) ([]taskarchive.ExtractedImage, error) {
	bundle, err := e.ExtractWithMetadata(ctx, path)
	if bundle == nil {
		return nil, err
	}
	return bundle.Images, err
}
func (e cancelAfterMetadataExtraction) ExtractWithMetadata(ctx context.Context, path string) (*taskarchive.MetadataBundle, error) {
	bundle, err := taskarchive.NewExtractor(taskarchive.ExtractorConfig{}).ExtractWithMetadata(ctx, path)
	if err != nil {
		return bundle, err
	}
	e.cancel()
	return bundle, context.Canceled
}

func TestWorkerCanceledUploadRetainsOriginalWithBoundedIndependentContext(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.cbz")
	writeTestZip(t, source, map[string]string{"1.jpg": "image", "ComicInfo.xml": "<ComicInfo/>"})
	original, _ := os.ReadFile(source)
	doc := metadata.EmptyDocument(metadata.StandardRegistry())
	task := app.Task{ID: "cancel-retention", Kind: domain.KindUpload, Status: domain.StatusRunning, Attempt: 1, Generation: 1, LeaseOwner: "worker"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registered := false
	d := NewTaskDownloader(TaskDownloaderConfig{Tasks: &fakeTaskViewService{view: &app.TaskView{Task: task, Input: app.Input{SourceArchivePath: source, MetadataDocument: &doc}}}, Service: &downloader.Service{}, Registry: fixedRegistry{}, Extractor: cancelAfterMetadataExtraction{cancel}, DownloadRoot: filepath.Join(root, "public"), RetentionRoot: filepath.Join(root, "private"), RecordRetention: func(retainCtx context.Context, got app.Task, m domain.RetentionManifest) error {
		if ctx.Err() == nil || retainCtx.Err() != nil {
			t.Fatal("retention reused cancelled execution context")
		}
		deadline, ok := retainCtx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > time.Minute {
			t.Fatal("retention lacks finite one-minute bound")
		}
		if got.Generation != task.Generation || !m.SourceRetained {
			t.Fatal("source retention registration lost execution identity")
		}
		registered = true
		return nil
	}})
	result, err := d.ExecuteWithMetadata(ctx, task)
	if !errors.Is(err, context.Canceled) || result.Path != "" || !registered {
		t.Fatal("cancel or retained source registration failed", err)
	}
	retained, err := os.ReadFile(filepath.Join(root, "private", task.ID, "1", "source-archive.bin"))
	if err != nil || !bytes.Equal(retained, original) {
		t.Fatal("cancelled original not durably preserved", err)
	}
	if _, err = os.Stat(source); err != nil {
		t.Fatal("cancelled source removed")
	}
}
