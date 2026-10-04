package postgres

import (
	"encoding/json"
	"errors"
	"testing"

	metadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type documentHistoryReadRow struct{ submitted, effective []byte }

func (row documentHistoryReadRow) Scan(dest ...any) error {
	*dest[0].(*string) = "url"
	*dest[10].(*[]byte) = row.submitted
	*dest[12].(*[]byte) = row.effective
	return nil
}

func TestHistoryReadRejectsUnsupportedSubmittedAndEffectiveDocuments(t *testing.T) {
	title := "合法历史标题"
	doc, err := metadata.FromLegacy(metadata.Legacy{ComicName: &title})
	if err != nil {
		t.Fatal(err)
	}
	valid, _ := json.Marshal(doc)
	doc.SchemaVersion = 2
	future, _ := json.Marshal(doc)
	for _, row := range []documentHistoryReadRow{{submitted: future}, {submitted: valid, effective: future}} {
		if _, err := scanMetadataHistoryEntry(row); !errors.Is(err, metadata.ErrUnsupportedVersion) {
			t.Fatalf("future history snapshot accepted: %v", err)
		}
	}
	entry, err := scanMetadataHistoryEntry(documentHistoryReadRow{submitted: valid, effective: valid})
	if err != nil {
		t.Fatal(err)
	}
	if entry.MetadataDocument == nil || entry.EffectiveMetadataDocument == nil || entry.ComicName == nil || *entry.ComicName != title {
		t.Fatal("valid historical document lost")
	}
}
