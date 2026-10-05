package main

import (
	"context"
	"encoding/json"
	"reflect"

	appmetadata "github.com/ryancheng/telegram-downloader/internal/app/metadata"
	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type modelTestContract struct{ metadata *appmetadata.Service }

const modelFixtureVersion = "bibliography-test-v1"

func modelFixtureKeys() []string {
	return []string{"title", "creators.writer", "publisher", "language", "publication_date", "aliases", "tags", "number", "count"}
}

var modelTestValues = map[string]any{
	"title": "雨后书店", "creators.writer": []any{"林青"}, "publisher": "示例出版社",
	"language": "zh", "publication_date": map[string]any{"year": float64(2024)},
	"aliases": []any{"雨后的小书店"}, "tags": []any{"日常"}, "number": "1", "count": float64(2),
}

func (c modelTestContract) Fixture(ctx context.Context) (sourcesettings.InferenceFixture, error) {
	registry, err := c.metadata.Schema(ctx)
	if err != nil {
		return sourcesettings.InferenceFixture{}, err
	}
	keys := modelFixtureKeys()
	properties := map[string]any{}
	for _, key := range keys {
		def, ok := registry.Definitions[key]
		if !ok || !def.Enabled {
			return sourcesettings.InferenceFixture{}, domain.ErrInvalidInput
		}
		switch def.Type {
		case "string":
			properties[key] = map[string]any{"type": "string"}
		case "string[]":
			properties[key] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		case "integer":
			properties[key] = map[string]any{"type": "integer"}
		case "date":
			properties[key] = map[string]any{"type": "object", "properties": map[string]any{"year": map[string]any{"type": "integer"}}, "required": []string{"year"}, "additionalProperties": false}
		default:
			return sourcesettings.InferenceFixture{}, domain.ErrInvalidInput
		}
	}
	schema, err := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": keys, "additionalProperties": false})
	if err != nil {
		return sourcesettings.InferenceFixture{}, err
	}
	return sourcesettings.InferenceFixture{SchemaVersion: registry.SchemaVersion, DefinitionsVersion: registry.DefinitionsVersion, FieldKeys: keys, FixtureVersion: modelFixtureVersion, Schema: schema, Text: "提取以下虚构书目信息，字段保持原文：标题：雨后书店；作者：林青；出版社：示例出版社；语言代码：zh；出版年份：2024；别名：雨后的小书店；标签：日常；本册编号：1；同系列总册数：2。"}, nil
}

func (c modelTestContract) Validate(ctx context.Context, fixture sourcesettings.InferenceFixture, output json.RawMessage) error {
	registry, err := c.metadata.Schema(ctx)
	if err != nil {
		return err
	}
	if registry.DefinitionsVersion != fixture.DefinitionsVersion || registry.SchemaVersion != fixture.SchemaVersion ||
		fixture.FixtureVersion != modelFixtureVersion || !reflect.DeepEqual(fixture.FieldKeys, modelFixtureKeys()) {
		return domain.ErrConflict
	}
	var values map[string]json.RawMessage
	if err := domain.DecodeJSON(output, &values); err != nil {
		return err
	}
	if len(values) != len(modelTestValues) {
		return domain.ErrInvalidInput
	}
	candidate := domain.MetadataCandidate{
		CandidateID: "fixed-model-test", RequestID: "fixed-model-test", Origin: "ai",
		SchemaVersion: registry.SchemaVersion, DefinitionsVersion: registry.DefinitionsVersion,
		FieldRevisions: map[string]uint64{}, Fields: map[string]domain.CandidateField{},
	}
	for key, want := range modelTestValues {
		var got any
		if err := json.Unmarshal(values[key], &got); err != nil || !reflect.DeepEqual(got, want) {
			return domain.ErrInvalidInput
		}
		candidate.Fields[key] = domain.CandidateField{State: "value", Value: values[key], Provenance: []domain.Provenance{{Kind: "ai", SourceID: "fixed-model-test"}}}
		candidate.FieldRevisions[key] = 0
	}
	return domain.ValidateCandidate(candidate, registry)
}
