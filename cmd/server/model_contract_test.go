package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	appmetadata "github.com/ryancheng/telegram-downloader/internal/app/metadata"
	metadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/metadatadoc"
)

func TestModelContractExtendedFixtureAndStrictOutput(t *testing.T) {
	pool := workspaceTestPool(t)
	ctx := context.Background()
	service := appmetadata.NewService(metadatadoc.NewStore(pool))
	contract := modelTestContract{metadata: service}
	fixture, err := contract.Fixture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := service.Schema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.DefinitionsVersion != reg.DefinitionsVersion || fixture.SchemaVersion != reg.SchemaVersion || len(fixture.FieldKeys) <= 7 {
		t.Fatal("fixture not bound to extended registry")
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
		Additional bool                       `json:"additionalProperties"`
	}
	if err := json.Unmarshal(fixture.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Additional || !reflect.DeepEqual(schema.Required, fixture.FieldKeys) || len(schema.Properties) != len(fixture.FieldKeys) {
		t.Fatal("fixture permits unselected fields")
	}
	for _, key := range fixture.FieldKeys {
		def, ok := reg.Definitions[key]
		if !ok || !def.Enabled {
			t.Fatalf("fixture references unregistered field %s", key)
		}
	}
	valid, _ := json.Marshal(modelTestValues)
	if err := contract.Validate(ctx, fixture, valid); err != nil {
		t.Fatal("fixed extended output rejected", err)
	}
	invalid := []string{
		strings.TrimSuffix(string(valid), "}") + `,"page_count":99}`,
		`{"title":"雨后书店"}`,
		strings.Replace(string(valid), `"count":2`, `"count":0`, 1),
		strings.Replace(string(valid), `"aliases":["雨后的小书店"]`, `"aliases":"雨后的小书店"`, 1),
		strings.TrimSuffix(string(valid), "}") + `,"title":"雨后书店"}`,
		string(valid) + ` {}`,
	}
	for _, output := range invalid {
		if err := contract.Validate(ctx, fixture, json.RawMessage(output)); !errors.Is(err, metadata.ErrInvalidInput) {
			t.Fatalf("invalid output accepted: %v", err)
		}
	}
	custom := metadata.FieldDefinition{Key: "custom.user.note", Label: "测试备注", Type: "string", MaxBytes: 100, Editable: true, Enabled: true, Extractable: []string{"ai"}, ExportStatus: "internal_only"}
	if _, err := service.UpdateFields(ctx, appmetadata.UpdateFieldsInput{ExpectedDefinitionsVersion: reg.DefinitionsVersion, Definitions: []metadata.FieldDefinition{custom}}); err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(ctx, fixture, valid); !errors.Is(err, metadata.ErrConflict) {
		t.Fatalf("old definition version accepted: %v", err)
	}
}

func TestModelContractRejectsChangedFixtureIdentity(t *testing.T) {
	pool := workspaceTestPool(t)
	ctx := context.Background()
	contract := modelTestContract{metadata: appmetadata.NewService(metadatadoc.NewStore(pool))}
	original, err := contract.Fixture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	output, _ := json.Marshal(modelTestValues)
	cases := []struct {
		name   string
		mutate func()
	}{
		{"schema version", func() { original.SchemaVersion++ }},
		{"fixture version", func() { original.FixtureVersion = "different-test" }},
		{"selected keys", func() { original.FieldKeys = []string{"title"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original, err = contract.Fixture(ctx)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate()
			if err := contract.Validate(ctx, original, output); err == nil {
				t.Fatal("changed fixture identity accepted")
			}
		})
	}
}
