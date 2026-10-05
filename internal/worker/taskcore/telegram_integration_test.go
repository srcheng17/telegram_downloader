package taskcore

import (
	"bytes"
	"context"
	"errors"
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/app/telegram"
	taskarchive "github.com/ryancheng/telegram-downloader/internal/archive"
	"github.com/ryancheng/telegram-downloader/internal/comicinfo"
	metadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/downloader"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type archiveTelegram struct {
	archive []byte
	path    string
}

func TestTelegramMetadataFailureRetainsOriginalAndCleansTemporarySource(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries map[string]string
		want    error
	}{
		{"extraction_rejects_multiple_metadata", map[string]string{"1.jpg": "page-bytes", "ComicInfo.xml": "<ComicInfo/>", "nested/ComicInfo.xml": "<ComicInfo/>"}, taskarchive.ErrMultipleComicInfo},
		{"packaging_rejects_malformed_metadata", map[string]string{"1.jpg": "page-bytes", "ComicInfo.xml": "<ComicInfo><Title>broken"}, comicinfo.ErrInvalidXML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			fixture := filepath.Join(root, "fixture.zip")
			writeTestZip(t, fixture, tc.entries)
			data, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			bridge := &archiveTelegram{archive: data}
			doc := metadata.EmptyDocument(metadata.StandardRegistry())
			source := telegram.Input{MessageURL: "https://t.me/example_channel/1", AccountRevision: 1, AccountIdentity: strings.Repeat("a", 64)}
			task := app.Task{ID: "telegram-metadata-failure", Kind: domain.KindTelegram, Status: domain.StatusRunning, Attempt: 1, Generation: 1, LeaseOwner: "worker"}
			privateSource := filepath.Join(root, "private", task.ID, "1", "source-archive.bin")
			registered := false
			d := NewTaskDownloader(TaskDownloaderConfig{Tasks: &fakeTaskViewService{view: &app.TaskView{Task: task, Input: app.Input{Telegram: &source, MetadataDocument: &doc}}}, Telegram: bridge, Service: &downloader.Service{}, Registry: fixedRegistry{}, DownloadRoot: filepath.Join(root, "output"), RetentionRoot: filepath.Join(root, "private"), RecordRetention: func(_ context.Context, got app.Task, manifest domain.RetentionManifest) error {
				preserved, readErr := os.ReadFile(privateSource)
				if readErr != nil || !bytes.Equal(preserved, data) || !manifest.SourceRetained || got.Generation != task.Generation || got.LeaseOwner != task.LeaseOwner {
					t.Fatal("retention registration preceded source preservation or lost execution identity", readErr)
				}
				registered = true
				return nil
			}})
			result, err := d.ExecuteWithMetadata(context.Background(), task)
			if !errors.Is(err, tc.want) || result.Path != "" || !registered {
				t.Fatal("metadata failure published an artifact or lost retained source", err)
			}
			if _, err = os.Stat(filepath.Dir(bridge.path)); !os.IsNotExist(err) {
				t.Fatal("metadata failure left temporary source after durable retention", err)
			}
			preserved, err := os.ReadFile(privateSource)
			if err != nil || !bytes.Equal(preserved, data) {
				t.Fatal("metadata failure removed or changed retained original", err)
			}
			artifacts, err := filepath.Glob(filepath.Join(root, "output", task.ID, "1", "*.cbz"))
			if err != nil || len(artifacts) != 0 {
				t.Fatal("metadata failure left a published CBZ", err)
			}
		})
	}
}

func TestTelegramRetentionRegistrationFailurePreservesTemporarySource(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{{"stale_generation", app.ErrConflict}, {"repository_failure", errors.New("retention repository unavailable")}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			fixture := filepath.Join(root, "fixture.zip")
			writeTestZip(t, fixture, map[string]string{"1.jpg": "page-bytes"})
			data, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			bridge := &archiveTelegram{archive: data}
			doc := metadata.EmptyDocument(metadata.StandardRegistry())
			source := telegram.Input{MessageURL: "https://t.me/example_channel/1", AccountRevision: 1, AccountIdentity: strings.Repeat("a", 64)}
			task := app.Task{ID: "telegram-retention-failure", Kind: domain.KindTelegram, Status: domain.StatusRunning, Attempt: 1, Generation: 1, LeaseOwner: "worker"}
			called := false
			d := NewTaskDownloader(TaskDownloaderConfig{Tasks: &fakeTaskViewService{view: &app.TaskView{Task: task, Input: app.Input{Telegram: &source, MetadataDocument: &doc}}}, Telegram: bridge, Service: &downloader.Service{}, Registry: fixedRegistry{}, DownloadRoot: filepath.Join(root, "output"), RetentionRoot: filepath.Join(root, "private"), RecordRetention: func(_ context.Context, got app.Task, manifest domain.RetentionManifest) error {
				called = true
				if !manifest.SourceRetained || got.Generation != task.Generation {
					t.Fatal("retention record lacks preserved source or generation")
				}
				return tc.err
			}})
			result, err := d.ExecuteWithMetadata(context.Background(), task)
			if !errors.Is(err, tc.err) || result.Path != "" || !called {
				t.Fatal("retention registration error ignored", err)
			}
			remaining, err := os.ReadFile(bridge.path)
			if err != nil || !bytes.Equal(remaining, data) {
				t.Fatal("temporary original deleted before fenced retention registration", err)
			}
			preserved, err := os.ReadFile(filepath.Join(root, "private", task.ID, "1", "source-archive.bin"))
			if err != nil || !bytes.Equal(preserved, data) {
				t.Fatal("unregistered private copy corrupted or removed", err)
			}
			artifacts, err := filepath.Glob(filepath.Join(root, "output", task.ID, "1", "*.cbz"))
			if err != nil || len(artifacts) != 0 {
				t.Fatal("retention registration failure published CBZ", err)
			}
		})
	}
}

func (f *archiveTelegram) Download(_ context.Context, _ telegram.Input, dir string) (telegram.Source, error) {
	if err := os.Mkdir(dir, 0700); err != nil {
		return telegram.Source{}, err
	}
	f.path = filepath.Join(dir, "source.zip")
	err := os.WriteFile(f.path, f.archive, 0600)
	return telegram.Source{Path: f.path, Size: int64(len(f.archive)), Extension: ".zip"}, err
}
func TestTelegramTemporaryArchiveReleasedOnlyAfterDurableRetention(t *testing.T) {
	for _, cancelAfterRetention := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "cancelled"}[cancelAfterRetention], func(t *testing.T) {
			root := t.TempDir()
			fixture := filepath.Join(root, "fixture.zip")
			writeTestZip(t, fixture, map[string]string{"1.jpg": "page-bytes"})
			data, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			bridge := &archiveTelegram{archive: data}
			doc := metadata.EmptyDocument(metadata.StandardRegistry())
			source := telegram.Input{MessageURL: "https://t.me/example_channel/1", AccountRevision: 1, AccountIdentity: strings.Repeat("a", 64)}
			task := app.Task{ID: "telegram-worker", Kind: domain.KindTelegram, Status: domain.StatusRunning, Attempt: 1, Generation: 1, LeaseOwner: "worker"}
			tasks := &fakeTaskViewService{view: &app.TaskView{Task: task, Input: app.Input{Telegram: &source, MetadataDocument: &doc}}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			retained := false
			d := NewTaskDownloader(TaskDownloaderConfig{Tasks: tasks, Telegram: bridge, Service: &downloader.Service{}, Registry: fixedRegistry{}, DownloadRoot: filepath.Join(root, "output"), RetentionRoot: filepath.Join(root, "private"), RecordRetention: func(_ context.Context, _ app.Task, m domain.RetentionManifest) error {
				retained = m.SourceRetained
				if cancelAfterRetention {
					cancel()
				}
				return nil
			}})
			result, err := d.ExecuteWithMetadata(ctx, task)
			if cancelAfterRetention {
				if err == nil || result.Path != "" {
					t.Fatal("cancelled source published")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !retained {
				t.Fatal("source not recorded")
			}
			if _, err = os.Stat(bridge.path); !os.IsNotExist(err) {
				t.Fatal("large temporary source remained after durable retention", err)
			}
			preserved, err := os.ReadFile(filepath.Join(root, "private", task.ID, "1", "source-archive.bin"))
			if err != nil || string(preserved) != string(data) {
				t.Fatal("source retention not intact", err)
			}
		})
	}
}
