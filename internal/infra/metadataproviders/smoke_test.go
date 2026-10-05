package metadataproviders

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

// Opt-in read-only proof against neutral public records; never uses stored keys.
func TestPublicNeutralSmoke(t *testing.T) {
	if os.Getenv("METADATA_PROVIDER_SMOKE") != "1" {
		t.Skip("public smoke is opt-in")
	}
	c := New()
	for _, record := range []struct{ provider, id string }{{"mangabaka", "270"}, {"mangaupdates", "17360452316"}, {"bangumi", "26595"}} {
		t.Run(record.provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			rows, err := c.Search(ctx, config(record.provider), credentials.NewSecret(""), "Naruto")
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) == 0 || len(rows) > 20 {
				t.Fatal("unexpected neutral search count")
			}
			resolved, err := c.Resolve(ctx, config(record.provider), credentials.NewSecret(""), record.id)
			if err != nil {
				t.Fatal(err)
			}
			candidate := metadata.MetadataCandidate{CandidateID: "smoke", RequestID: "smoke", Origin: "provider", SchemaVersion: 1, DefinitionsVersion: metadata.StandardDefinitionsVersion, Fields: map[string]metadata.CandidateField{}, FieldRevisions: map[string]uint64{}}
			for key, field := range resolved.Fields {
				candidate.Fields[key] = metadata.CandidateField{State: "value", Value: field.Value, Provenance: []metadata.Provenance{{Kind: "provider", SourceID: record.provider, RecordID: resolved.ID, SourceField: field.Path, RetrievedAt: resolved.RetrievedAt, PublicURL: resolved.PublicURL}}}
				candidate.FieldRevisions[key] = 0
			}
			if err := metadata.ValidateCandidate(candidate, metadata.StandardRegistry()); err != nil {
				t.Fatal(err)
			}
			t.Logf("anonymous search=%d bounded results; fixed neutral detail=%s; validated fields=%d", len(rows), record.id, len(candidate.Fields))
		})
	}
}
