package downloader

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	taskarchive "github.com/ryancheng/telegram-downloader/internal/archive"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

func metadataPackFixture(t *testing.T, xmlText string) MetadataPackInput {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.cbz")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	writer.SetComment(`{"ComicBookInfo/1.0":{"title":"old"}}`)
	for _, entry := range [][2]string{{"chapter/10.jpg", "ten-image"}, {"chapter/2.jpg", "two-image"}, {"chapter/1.jpg", "one-image"}, {"ComicInfo.xml", xmlText}, {"MetronInfo.xml", "<MetronInfo/>"}, {"other.txt", "attachment-original-only"}} {
		member, err := writer.Create(entry[0])
		if err != nil {
			t.Fatal(err)
		}
		member.Write([]byte(entry[1]))
	}
	writer.Close()
	file.Close()
	bundle, err := taskarchive.NewExtractor(taskarchive.ExtractorConfig{}).ExtractWithMetadata(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := metadata.NewRegistry(metadata.StandardRegistry(), []metadata.FieldDefinition{{Key: "custom.user.note", Label: "内部备注", Type: "string", MaxBytes: 100, Enabled: true, Editable: true, ExportStatus: "internal_only"}})
	if err != nil {
		t.Fatal(err)
	}
	doc := metadata.EmptyDocument(registry)
	for key, value := range map[string]string{"custom.user.note": "internal-confirmed", "title": "New title"} {
		raw, _ := json.Marshal(value)
		doc.Fields[key] = metadata.FieldState{State: "value", Value: raw, ManualLocked: true, Provenance: []metadata.Provenance{{Kind: "manual", SourceID: "user"}}}
		doc.DefinitionSnapshot[key] = registry.Definitions[key]
	}
	return MetadataPackInput{Bundle: bundle, Document: doc, Registry: registry, PrivateDir: filepath.Join(dir, "private"), OutputPath: filepath.Join(dir, "output", "result.cbz")}
}
func TestMetadataPackActualBytesMetadataAndPrivateEvidence(t *testing.T) {
	input := metadataPackFixture(t, `<ComicInfo><Title>Old title</Title><Publisher>Keep Publisher</Publisher><Notes>Keep notes</Notes><Pages><Page Image="0" Type="FrontCover"/></Pages><Unknown value="original"/></ComicInfo>`)
	before, _ := os.ReadFile(input.Bundle.SourcePath)
	result, err := PackageMetadataCBZ(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if err = taskcore.ValidateRetentionManifest(result.RetentionManifest); err != nil {
		t.Fatal(err)
	}
	if !result.RetentionManifest.SourceRetained || len(result.RetentionManifest.Files) != 5 {
		t.Fatalf("manifest=%+v", result.RetentionManifest)
	}
	if string(result.EffectiveDocument.Fields["publisher"].Value) != `"Keep Publisher"` || string(result.EffectiveDocument.Fields["page_count"].Value) != "3" || !result.EffectiveDocument.Fields["title"].ManualLocked {
		t.Fatal("effective document merge lost fields/locks")
	}
	reader, err := zip.OpenReader(input.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if reader.Comment != "" || len(reader.File) != 4 {
		t.Fatal("competing metadata exported")
	}
	want := []string{"ComicInfo.xml", "0001.jpg", "0002.jpg", "0003.jpg"}
	bytesWant := [][]byte{nil, []byte("one-image"), []byte("two-image"), []byte("ten-image")}
	for i, file := range reader.File {
		if file.Name != want[i] {
			t.Fatalf("wrong page identity %s", file.Name)
		}
		member, _ := file.Open()
		data, err := io.ReadAll(member)
		member.Close()
		if err != nil {
			t.Fatal("CRC readback failed", err)
		}
		if i == 0 {
			for _, text := range []string{"<Title>New title</Title>", "<Publisher>Keep Publisher</Publisher>", "<Notes>Keep notes</Notes>", "<PageCount>3</PageCount>"} {
				if !bytes.Contains(data, []byte(text)) {
					t.Fatalf("missing %s", text)
				}
			}
			for _, text := range []string{"Unknown", "<Pages>", "internal-confirmed", "attachment-original-only", "source_id"} {
				if bytes.Contains(data, []byte(text)) {
					t.Fatalf("unexpected export %s", text)
				}
			}
		} else if sha256.Sum256(data) != sha256.Sum256(bytesWant[i]) {
			t.Fatal("page bytes/order changed")
		}
	}
	after, _ := os.ReadFile(input.Bundle.SourcePath)
	retained, _ := os.ReadFile(filepath.Join(input.PrivateDir, "source-archive.bin"))
	if !bytes.Equal(before, after) || !bytes.Equal(before, retained) {
		t.Fatal("original changed or retention incomplete")
	}
	private, _ := os.ReadFile(filepath.Join(input.PrivateDir, "effective-metadata.json"))
	if !bytes.Contains(private, []byte("internal-confirmed")) {
		t.Fatal("unmapped custom metadata lost")
	}
	if len(result.Warnings) == 0 {
		t.Fatal("unknown/pages warnings missing")
	}
	if _, err = PackageMetadataCBZ(context.Background(), input); err == nil {
		t.Fatal("existing artifact overwritten")
	}
}

type alwaysFailWriter struct{}

func (alwaysFailWriter) Write([]byte) (int, error) { return 0, errors.New("injected disk failure") }
func TestMetadataPackFailuresNeverPublishAndKeepSource(t *testing.T) {
	for _, failure := range []string{"xml", "write", "close", "validation", "cancel", "retention"} {
		t.Run(failure, func(t *testing.T) {
			xmlText := `<ComicInfo><Publisher>Original</Publisher></ComicInfo>`
			if failure == "xml" {
				xmlText = `<!DOCTYPE ComicInfo><ComicInfo/>`
			}
			input := metadataPackFixture(t, xmlText)
			old := filepath.Join(filepath.Dir(input.OutputPath), "previous-success.cbz")
			os.MkdirAll(filepath.Dir(old), 0755)
			os.WriteFile(old, []byte("previous-success"), 0600)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hooks := metadataPackHooks{}
			switch failure {
			case "write":
				hooks.wrapWriter = func(io.Writer) io.Writer { return alwaysFailWriter{} }
			case "close":
				hooks.closeFile = func(file *os.File) error { file.Close(); return errors.New("injected close failure") }
			case "validation":
				hooks.beforeValidate = func(path string) error { return os.WriteFile(path, []byte("corrupted"), 0600) }
			case "cancel":
				hooks.beforeValidate = func(string) error { cancel(); return nil }
			case "retention":
				os.MkdirAll(input.PrivateDir, 0700)
				os.WriteFile(filepath.Join(input.PrivateDir, "source-archive.bin"), []byte("conflicting evidence"), 0600)
			}
			result, err := packageMetadataCBZ(ctx, input, hooks)
			if err == nil {
				t.Fatal("injected failure accepted")
			}
			if _, err = os.Stat(input.OutputPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("partial output published")
			}
			if _, err = os.Stat(input.Bundle.SourcePath); err != nil {
				t.Fatal("original deleted")
			}
			data, _ := os.ReadFile(old)
			if string(data) != "previous-success" {
				t.Fatal("previous success damaged")
			}
			if failure != "retention" && !result.RetentionManifest.SourceRetained {
				t.Fatal("failed generation lost retention reference")
			}
			files, _ := filepath.Glob(filepath.Join(filepath.Dir(input.OutputPath), ".metadata-cbz-*"))
			if len(files) != 0 {
				t.Fatal("temporary cbz leaked")
			}
		})
	}
}
func TestMetadataPackNoPrivateDocumentEntersArchive(t *testing.T) {
	input := metadataPackFixture(t, `<ComicInfo/>`)
	input.Document.Fields["title"] = metadata.FieldState{State: "cleared", ManualLocked: true, Provenance: []metadata.Provenance{{Kind: "manual", SourceID: "user"}}}
	result, err := PackageMetadataCBZ(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	reader, _ := zip.OpenReader(input.OutputPath)
	defer reader.Close()
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, ".json") {
			t.Fatal("private sidecar included in archive")
		}
	}
	if result.EffectiveDocument.Fields["title"].State != "cleared" {
		t.Fatal("explicit clear not preserved")
	}
}
