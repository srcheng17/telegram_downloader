package taskcore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	metadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

func TestTaskInputReadRejectsInvalidPresentDocumentInsteadOfLegacyFallback(t *testing.T) {
	title := "保留标题"
	valid, err := metadata.FromLegacy(metadata.Legacy{ComicName: &title})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*metadata.Document){
		func(doc *metadata.Document) { doc.SchemaVersion = 2 },
		func(doc *metadata.Document) {
			field := doc.Fields["title"]
			field.Value = json.RawMessage(`5`)
			doc.Fields["title"] = field
		},
		func(doc *metadata.Document) {
			field := doc.DefinitionSnapshot["title"]
			field.Type = "boolean"
			doc.DefinitionSnapshot["title"] = field
		},
	} {
		raw, _ := json.Marshal(valid)
		var bad metadata.Document
		if err := json.Unmarshal(raw, &bad); err != nil {
			t.Fatal(err)
		}
		mutate(&bad)
		raw, _ = json.Marshal(bad)
		scan := dbInputScan{metadata: `{"comic_name":"legacy fallback"}`, metadataDocument: sql.NullString{String: string(raw), Valid: true}}
		var input app.Input
		if err := scan.apply(&input); !errors.Is(err, metadata.ErrInvalidInput) {
			t.Fatalf("corrupt present document accepted: %v", err)
		}
	}
	raw, _ := json.Marshal(valid)
	scan := dbInputScan{metadata: `{"comic_name":"wrong compatibility value"}`, metadataDocument: sql.NullString{String: string(raw), Valid: true}}
	var input app.Input
	if err := scan.apply(&input); err != nil {
		t.Fatal(err)
	}
	if input.Metadata["comic_name"] != title || input.MetadataDocument == nil {
		t.Fatal("valid document did not remain authoritative")
	}
}

type effectiveDocumentReadRow struct{ submitted, effective []byte }

func (row effectiveDocumentReadRow) Scan(dest ...any) error {
	*dest[0].(*string) = "synthetic-task"
	*dest[1].(*string) = "url"
	*dest[2].(*string) = "SUCCEEDED"
	*dest[10].(*string) = "synthetic-task"
	*dest[18].(*sql.NullString) = sql.NullString{String: string(row.submitted), Valid: true}
	*dest[25].(*sql.NullString) = sql.NullString{String: "synthetic-task", Valid: true}
	*dest[26].(*sql.NullString) = sql.NullString{String: "/synthetic.cbz", Valid: true}
	*dest[31].(*sql.NullString) = sql.NullString{String: string(row.effective), Valid: true}
	return nil
}

func TestTaskResultReadRejectsUnsupportedEffectiveDocument(t *testing.T) {
	doc := metadata.EmptyDocument(metadata.StandardRegistry())
	submitted, _ := json.Marshal(doc)
	doc.SchemaVersion = 2
	effective, _ := json.Marshal(doc)
	if _, err := scanTaskView(effectiveDocumentReadRow{submitted: submitted, effective: effective}); !errors.Is(err, metadata.ErrUnsupportedVersion) {
		t.Fatalf("future effective document accepted: %v", err)
	}
}
