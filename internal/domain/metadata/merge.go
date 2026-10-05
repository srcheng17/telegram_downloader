package metadata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

type MetadataCandidate struct {
	CandidateID          string                    `json:"candidate_id"`
	RequestID            string                    `json:"request_id"`
	Origin               string                    `json:"origin"`
	SchemaVersion        int                       `json:"schema_version"`
	DefinitionsVersion   string                    `json:"definitions_version"`
	BaseDocumentRevision uint64                    `json:"base_document_revision"`
	InputRevision        uint64                    `json:"input_revision"`
	ConfigRevision       *uint64                   `json:"config_revision,omitempty"`
	FieldRevisions       map[string]uint64         `json:"field_revisions"`
	Fields               map[string]CandidateField `json:"fields"`
	Warnings             []Warning                 `json:"warnings,omitempty"`
}
type CandidateField struct {
	State      string          `json:"state"`
	Value      json.RawMessage `json:"value,omitempty"`
	Provenance []Provenance    `json:"provenance"`
	Warnings   []Warning       `json:"warnings,omitempty"`
}

func (c *MetadataCandidate) UnmarshalJSON(data []byte) error {
	type plain MetadataCandidate
	var value plain
	if err := DecodeJSON(data, &value); err != nil {
		return err
	}
	if err := requireMembers(data, "candidate_id", "request_id", "origin", "schema_version", "definitions_version", "base_document_revision", "input_revision", "field_revisions", "fields"); err != nil {
		return err
	}
	var revisions struct {
		Fields map[string]json.RawMessage `json:"field_revisions"`
	}
	if err := json.Unmarshal(data, &revisions); err != nil {
		return invalid("candidate", "invalid_field_revision")
	}
	for key, raw := range revisions.Fields {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return invalid(key, "invalid_field_revision")
		}
	}
	*c = MetadataCandidate(value)
	return nil
}

type Operation struct {
	Op            string             `json:"op"`
	Key           string             `json:"key,omitempty"`
	Value         json.RawMessage    `json:"value,omitempty"`
	Candidate     *MetadataCandidate `json:"candidate,omitempty"`
	Keys          []string           `json:"keys,omitempty"`
	ConfirmLocked bool               `json:"confirm_locked,omitempty"`
}
type MergeContext struct {
	InputRevision  uint64
	ConfigRevision *uint64
	Now            time.Time
}

type ConflictError struct {
	Keys []string `json:"keys"`
}

func (e *ConflictError) Error() string { return "metadata fields changed since candidate creation" }
func (e *ConflictError) Unwrap() error { return ErrConflict }

func ValidateCandidate(candidate MetadataCandidate, r Registry) error {
	if candidate.SchemaVersion != SchemaVersion || candidate.DefinitionsVersion != r.DefinitionsVersion {
		return ErrUnsupportedVersion
	}
	if !sourceID.MatchString(candidate.CandidateID) || !sourceID.MatchString(candidate.RequestID) || !candidateKind(candidate.Origin) || candidate.BaseDocumentRevision > MaxRevision || candidate.InputRevision > MaxRevision || candidate.ConfigRevision != nil && *candidate.ConfigRevision > MaxRevision {
		return invalid("candidate", "invalid_context")
	}
	if len(candidate.Fields) == 0 || len(candidate.Fields) > len(r.Definitions) || len(candidate.FieldRevisions) != len(candidate.Fields) {
		return invalid("candidate", "invalid_fields")
	}
	data, err := json.Marshal(candidate)
	if err != nil || len(data) > MaxDocumentBytes {
		return invalid("candidate", "too_large")
	}
	if err := validateWarnings(candidate.Warnings); err != nil {
		return err
	}
	for _, key := range sortedKeys(candidate.Fields) {
		field := candidate.Fields[key]
		def, ok := r.Definitions[key]
		if !ok || !def.Enabled || !contains(def.Extractable, candidate.Origin) {
			return invalid(key, "source_not_allowed")
		}
		rev, ok := candidate.FieldRevisions[key]
		if !ok || rev > candidate.BaseDocumentRevision {
			return invalid(key, "invalid_field_revision")
		}
		if validateProvenance(field.Provenance) != nil {
			return invalid(key, "invalid_provenance")
		}
		for _, source := range field.Provenance {
			if source.Kind != candidate.Origin {
				return invalid(key, "candidate_source_mismatch")
			}
		}
		if err := validateWarnings(field.Warnings); err != nil {
			return err
		}
		switch field.State {
		case "cleared":
			if len(field.Value) != 0 {
				return invalid(key, "cleared_has_value")
			}
		case "value":
			if _, err := validateValue(key, field.Value, def); err != nil {
				return err
			}
		default:
			return invalid(key, "invalid_state")
		}
	}
	return nil
}

func validateWarnings(warnings []Warning) error {
	if len(warnings) > MaxListItems {
		return invalid("warnings", "too_many")
	}
	for _, w := range warnings {
		if !validText(w.Key, 128) || !sourceID.MatchString(w.Code) || !validText(w.Message, MaxListItemBytes) {
			return invalid("warnings", "invalid_warning")
		}
	}
	return nil
}

// ApplyPatch is a pure transformation, not a persistent draft CAS. A complete
// selection succeeds atomically; callers own checking response baselines.
func ApplyPatch(doc Document, r Registry, expectedRevision uint64, operations []Operation, ctx MergeContext) (Document, []Warning, error) {
	result, _, err := Validate(doc, r)
	if err != nil {
		return Document{}, nil, err
	}
	if doc.Revision != expectedRevision {
		return Document{}, nil, ErrConflict
	}
	if doc.Revision >= MaxRevision || ctx.InputRevision > MaxRevision || ctx.ConfigRevision != nil && *ctx.ConfigRevision > MaxRevision {
		return Document{}, nil, invalid("revision", "out_of_range")
	}
	if len(operations) == 0 || len(operations) > len(r.Definitions) {
		return Document{}, nil, invalid("operations", "invalid_count")
	}
	if ctx.Now.IsZero() {
		return Document{}, nil, invalid("operations", "adoption_time_required")
	}
	next := doc.Revision + 1
	changed := map[string]bool{}
	warnings := make([]Warning, 0)
	touch := func(key string) error {
		if changed[key] {
			return invalid(key, "duplicate_operation")
		}
		changed[key] = true
		return nil
	}
	for _, op := range operations {
		switch op.Op {
		case "set", "clear", "unlock":
			if op.Candidate != nil || len(op.Keys) > 0 || op.ConfirmLocked || op.Op != "set" && len(op.Value) != 0 {
				return Document{}, nil, invalid("operations", "invalid_shape")
			}
			def, ok := r.Definitions[op.Key]
			if !ok || !def.Editable || !def.Enabled {
				return Document{}, nil, invalid(op.Key, "not_editable")
			}
			if err := touch(op.Key); err != nil {
				return Document{}, nil, err
			}
			f := FieldState{State: "cleared", ManualLocked: true, Revision: next, Provenance: []Provenance{{Kind: "manual", SourceID: "user", AdoptedAt: ctx.Now.UTC().Format(time.RFC3339Nano)}}}
			if op.Op == "set" {
				value, err := validateValue(op.Key, op.Value, def)
				if err != nil {
					return Document{}, nil, err
				}
				f.State = "value"
				f.Value = value
			}
			if op.Op == "unlock" {
				old, ok := result.Fields[op.Key]
				if !ok {
					return Document{}, nil, invalid(op.Key, "absent_field")
				}
				f = old
				f.ManualLocked = false
				f.Revision = next
			}
			result.Fields[op.Key] = f
			result.DefinitionSnapshot[op.Key] = def
		case "adopt":
			if op.Candidate == nil || op.Key != "" || len(op.Value) != 0 || len(op.Keys) == 0 {
				return Document{}, nil, invalid("operations", "invalid_adoption")
			}
			c := *op.Candidate
			if err := ValidateCandidate(c, r); err != nil {
				return Document{}, nil, err
			}
			if c.BaseDocumentRevision > doc.Revision || c.InputRevision != ctx.InputRevision || !sameRevision(c.ConfigRevision, ctx.ConfigRevision) {
				return Document{}, nil, ErrConflict
			}
			conflicts := make([]string, 0)
			for _, key := range op.Keys {
				if _, ok := c.Fields[key]; !ok {
					return Document{}, nil, invalid(key, "missing_candidate_field")
				}
				if err := touch(key); err != nil {
					return Document{}, nil, err
				}
				current := doc.Fields[key]
				if current.Revision != c.FieldRevisions[key] || current.ManualLocked && !op.ConfirmLocked {
					conflicts = append(conflicts, key)
				}
			}
			if len(conflicts) > 0 {
				return Document{}, nil, &ConflictError{Keys: conflicts}
			}
			for _, key := range op.Keys {
				proposed := c.Fields[key]
				provenance := append([]Provenance(nil), proposed.Provenance...)
				if len(provenance) == 0 {
					provenance = []Provenance{{Kind: c.Origin, SourceID: c.Origin}}
				}
				for i := range provenance {
					provenance[i].AdoptedAt = ctx.Now.UTC().Format(time.RFC3339Nano)
				}
				// Explicit confirmation may replace a locked value but never unlock it.
				result.Fields[key] = FieldState{State: proposed.State, Value: append(json.RawMessage(nil), proposed.Value...), Revision: next, ManualLocked: doc.Fields[key].ManualLocked, Provenance: provenance}
				result.DefinitionSnapshot[key] = r.Definitions[key]
				warnings = append(warnings, proposed.Warnings...)
			}
			warnings = append(warnings, c.Warnings...)
		default:
			return Document{}, nil, invalid("operations", "unknown_operation")
		}
	}
	result.Revision = next
	validated, validationWarnings, err := Validate(result, r)
	if err != nil {
		return Document{}, nil, err
	}
	warnings = append(warnings, validationWarnings...)
	return validated, warnings, nil
}

func ApplyCandidate(doc Document, r Registry, expectedRevision uint64, candidate MetadataCandidate, keys []string, confirmLocked bool, ctx MergeContext) (Document, []Warning, error) {
	return ApplyPatch(doc, r, expectedRevision, []Operation{{Op: "adopt", Candidate: &candidate, Keys: keys, ConfirmLocked: confirmLocked}}, ctx)
}
func sameRevision(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// CandidateFromDocument turns a historical snapshot into an explicit selection;
// it never copies historical locks or overwrites the target draft wholesale.
func CandidateFromDocument(source, target Document, requestID string, inputRevision uint64) (MetadataCandidate, error) {
	if source.DefinitionsVersion != target.DefinitionsVersion {
		return MetadataCandidate{}, fmt.Errorf("%w: history definition version differs", ErrUnsupportedVersion)
	}
	candidate := MetadataCandidate{CandidateID: requestID, RequestID: requestID, Origin: "legacy", SchemaVersion: SchemaVersion, DefinitionsVersion: target.DefinitionsVersion, BaseDocumentRevision: target.Revision, InputRevision: inputRevision, FieldRevisions: map[string]uint64{}, Fields: map[string]CandidateField{}}
	for key, f := range source.Fields {
		candidate.FieldRevisions[key] = target.Fields[key].Revision
		candidate.Fields[key] = CandidateField{State: f.State, Value: append(json.RawMessage(nil), f.Value...), Provenance: []Provenance{{Kind: "legacy", SourceID: "history"}}}
	}
	return candidate, nil
}
