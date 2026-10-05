package metadata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
)

var (
	ErrInvalidInput       = errors.New("invalid metadata input")
	ErrConflict           = errors.New("metadata revision conflict")
	ErrUnsupportedVersion = fmt.Errorf("%w: unsupported metadata version", ErrInvalidInput)
)

type FieldError struct {
	Key  string `json:"key"`
	Code string `json:"code"`
}
type ValidationError struct {
	Fields []FieldError `json:"fields"`
}

func (e *ValidationError) Error() string {
	return "invalid metadata: " + e.Fields[0].Key + " (" + e.Fields[0].Code + ")"
}
func (e *ValidationError) Unwrap() error { return ErrInvalidInput }
func invalid(key, code string) error     { return &ValidationError{Fields: []FieldError{{key, code}}} }

type Warning struct {
	Key     string `json:"key"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Document struct {
	SchemaVersion      int                        `json:"schema_version"`
	DefinitionsVersion string                     `json:"definitions_version"`
	Revision           uint64                     `json:"revision"`
	Fields             map[string]FieldState      `json:"fields"`
	DefinitionSnapshot map[string]FieldDefinition `json:"definition_snapshot"`
}
type FieldState struct {
	State        string          `json:"state"`
	Value        json.RawMessage `json:"value,omitempty"`
	Revision     uint64          `json:"revision"`
	ManualLocked bool            `json:"manual_locked"`
	Provenance   []Provenance    `json:"provenance"`
}
type Provenance struct {
	Kind        string    `json:"kind"`
	SourceID    string    `json:"source_id"`
	RecordID    string    `json:"record_id,omitempty"`
	PublicURL   string    `json:"public_url,omitempty"`
	AdoptedAt   string    `json:"adopted_at,omitempty"`
	RetrievedAt string    `json:"retrieved_at,omitempty"`
	SourceField string    `json:"source_field,omitempty"`
	Attribution string    `json:"attribution,omitempty"`
	LicenseURL  string    `json:"license_url,omitempty"`
	Evidence    *Evidence `json:"evidence,omitempty"`
	Confidence  *float64  `json:"confidence,omitempty"`
}
type Evidence struct {
	ImageID string  `json:"image_id,omitempty"`
	Start   *uint64 `json:"start,omitempty"`
	End     *uint64 `json:"end,omitempty"`
}
type PublicationDate struct {
	Year  int  `json:"year"`
	Month *int `json:"month,omitempty"`
	Day   *int `json:"day,omitempty"`
}
type Identifier struct {
	Scheme string `json:"scheme"`
	Value  string `json:"value"`
}

// encoding/json otherwise accepts null for integer and boolean fields. These
// members carry conflict/lock semantics, so a missing or null member is invalid.
func (d *Document) UnmarshalJSON(data []byte) error {
	type plain Document
	var value plain
	if err := DecodeJSON(data, &value); err != nil {
		return err
	}
	if err := requireMembers(data, "schema_version", "definitions_version", "revision", "fields", "definition_snapshot"); err != nil {
		return err
	}
	*d = Document(value)
	return nil
}

func (f *FieldState) UnmarshalJSON(data []byte) error {
	type plain FieldState
	var value plain
	if err := DecodeJSON(data, &value); err != nil {
		return err
	}
	if err := requireMembers(data, "state", "revision", "manual_locked"); err != nil {
		return err
	}
	*f = FieldState(value)
	return nil
}

func requireMembers(data []byte, keys ...string) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return invalid("document", "invalid_json_shape")
	}
	for _, key := range keys {
		if raw, ok := members[key]; !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return invalid(key, "required_member")
		}
	}
	return nil
}

func EmptyDocument(r Registry) Document {
	return Document{SchemaVersion: SchemaVersion, DefinitionsVersion: r.DefinitionsVersion, Fields: map[string]FieldState{}, DefinitionSnapshot: map[string]FieldDefinition{}}
}

// DecodeJSON rejects duplicate/unknown object members, trailing data and invalid
// UTF-8. Callers should use it at protocol boundaries, including candidate input.
func DecodeJSON(data []byte, dst any) error {
	if !utf8.Valid(data) || len(data) == 0 {
		return invalid("document", "invalid_json")
	}
	probe := json.NewDecoder(bytes.NewReader(data))
	probe.UseNumber()
	if err := checkJSONValue(probe, 0); err != nil {
		return invalid("document", "invalid_json")
	}
	if _, err := probe.Token(); err != io.EOF {
		return invalid("document", "trailing_json")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return invalid("document", "invalid_json_shape")
	}
	return nil
}

func checkJSONValue(dec *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrInvalidInput
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return ErrInvalidInput
			}
			seen[key] = true
			if err := checkJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	case '[':
		for dec.More() {
			if err := checkJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	default:
		return ErrInvalidInput
	}
}

func Decode(data []byte, r Registry) (Document, error) {
	if len(data) > MaxDocumentBytes {
		return Document{}, invalid("document", "too_large")
	}
	var doc Document
	if err := DecodeJSON(data, &doc); err != nil {
		return Document{}, err
	}
	validated, _, err := Validate(doc, r)
	return validated, err
}

func Validate(doc Document, r Registry) (Document, []Warning, error) {
	if doc.SchemaVersion != SchemaVersion || r.SchemaVersion != SchemaVersion || doc.DefinitionsVersion != r.DefinitionsVersion {
		return Document{}, nil, ErrUnsupportedVersion
	}
	if doc.Revision > MaxRevision || doc.Fields == nil || doc.DefinitionSnapshot == nil || len(doc.Fields) > len(r.Definitions) || len(doc.Fields) != len(doc.DefinitionSnapshot) {
		return Document{}, nil, invalid("document", "invalid_structure")
	}
	encoded, err := json.Marshal(doc)
	if err != nil || len(encoded) > MaxDocumentBytes {
		return Document{}, nil, invalid("document", "too_large_or_invalid_json")
	}
	// Clone all maps, raw values and descriptors before normalizing: caller snapshots
	// remain immutable when validation or a later operation fails.
	var result Document
	if err := DecodeJSON(encoded, &result); err != nil {
		return Document{}, nil, err
	}
	warnings := make([]Warning, 0)
	for _, key := range sortedKeys(result.Fields) {
		field := result.Fields[key]
		def, ok := r.Definitions[key]
		if !ok {
			return Document{}, nil, invalid(key, "unregistered_field")
		}
		if snapshot, ok := result.DefinitionSnapshot[key]; !ok || !reflect.DeepEqual(snapshot, def) {
			return Document{}, nil, invalid(key, "definition_mismatch")
		}
		if field.Revision > doc.Revision {
			return Document{}, nil, invalid(key, "future_revision")
		}
		if err := validateProvenance(field.Provenance); err != nil {
			return Document{}, nil, invalid(key, "invalid_provenance")
		}
		if field.Provenance == nil {
			field.Provenance = []Provenance{}
		}
		switch field.State {
		case "cleared":
			if len(field.Value) != 0 {
				return Document{}, nil, invalid(key, "cleared_has_value")
			}
		case "value":
			value, err := validateValue(key, field.Value, def)
			if err != nil {
				return Document{}, nil, err
			}
			field.Value = value
			if def.ExportStatus == "internal_only" {
				warnings = append(warnings, Warning{key, "internal_only", "仅项目内保存，不导出到 ComicInfo"})
			}
			if key == "identifiers" {
				warnings = append(warnings, Warning{key, "identifier_export", "仅有效的 ISBN／GTIN 可映射到 ComicInfo，其余标识符仅项目内保存"})
			}
		default:
			return Document{}, nil, invalid(key, "invalid_state")
		}
		result.Fields[key] = field
	}
	if stringValue(result, "manga") == "no" && stringValue(result, "reading_direction") == "rtl" {
		warnings = append(warnings, Warning{"manga", "reading_direction_conflict", "漫画标记与右向左阅读方向需在导出前核对"})
	}
	encoded, err = json.Marshal(result)
	if err != nil || len(encoded) > MaxDocumentBytes {
		return Document{}, nil, invalid("document", "too_large")
	}
	return result, warnings, nil
}

func validateValue(key string, raw json.RawMessage, d FieldDefinition) (json.RawMessage, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, invalid(key, "value_required")
	}
	var value any
	switch d.Type {
	case "string":
		var s string
		if DecodeJSON(raw, &s) != nil || !validText(s, d.MaxBytes) || strings.TrimSpace(s) == "" {
			return nil, invalid(key, "invalid_string")
		}
		if len(d.Enum) > 0 && !contains(d.Enum, s) {
			return nil, invalid(key, "invalid_enum")
		}
		if key == "language" {
			if _, err := language.Parse(s); err != nil || !languageCode.MatchString(s) {
				return nil, invalid(key, "invalid_language")
			}
		}
		if key == "web" && !validPublicURL(s) {
			return nil, invalid(key, "invalid_public_url")
		}
		value = s
	case "string[]":
		var list []string
		if DecodeJSON(raw, &list) != nil || list == nil || len(list) > d.MaxItems {
			return nil, invalid(key, "invalid_list")
		}
		for _, s := range list {
			if !validText(s, d.ItemMaxBytes) || strings.TrimSpace(s) == "" {
				return nil, invalid(key, "invalid_list_item")
			}
		}
		value = list
	case "integer":
		var n int64
		if DecodeJSON(raw, &n) != nil || d.Minimum == nil || d.Maximum == nil || n < *d.Minimum || n > *d.Maximum {
			return nil, invalid(key, "invalid_integer")
		}
		value = n
	case "boolean":
		var b bool
		if DecodeJSON(raw, &b) != nil {
			return nil, invalid(key, "invalid_boolean")
		}
		value = b
	case "date":
		var date PublicationDate
		if DecodeJSON(raw, &date) != nil || date.Year < 1 || date.Year > 9999 || date.Day != nil && date.Month == nil {
			return nil, invalid(key, "invalid_date")
		}
		if date.Month != nil && (*date.Month < 1 || *date.Month > 12) {
			return nil, invalid(key, "invalid_date")
		}
		if date.Day != nil {
			actual := time.Date(date.Year, time.Month(*date.Month), *date.Day, 0, 0, 0, 0, time.UTC)
			if *date.Day < 1 || actual.Day() != *date.Day || actual.Month() != time.Month(*date.Month) {
				return nil, invalid(key, "invalid_date")
			}
		}
		value = date
	case "identifiers":
		var ids []Identifier
		if DecodeJSON(raw, &ids) != nil || ids == nil || len(ids) > d.MaxItems {
			return nil, invalid(key, "invalid_identifiers")
		}
		for _, id := range ids {
			if !identifierScheme.MatchString(id.Scheme) || !validText(id.Value, d.ItemMaxBytes) || strings.TrimSpace(id.Value) == "" {
				return nil, invalid(key, "invalid_identifier")
			}
		}
		value = ids
	default:
		return nil, invalid(key, "unsupported_type")
	}
	data, _ := json.Marshal(value)
	return data, nil
}

var (
	languageCode     = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$`)
	identifierScheme = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	sourceID         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
)

func validText(value string, max int) bool {
	if !utf8.ValidString(value) || len(value) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}
func validPublicURL(raw string) bool {
	if !validText(raw, MaxStringBytes) || strings.ContainsAny(raw, "\r\n\t ") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Opaque != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
			return false
		}
	} else if !strings.Contains(host, ".") {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key := range query {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") || lower == "key" || lower == "authorization" || lower == "signature" {
			return false
		}
	}
	return true
}
func candidateKind(kind string) bool {
	return contains([]string{"archive", "ocr", "rule", "ai", "provider", "legacy"}, kind)
}
func validateProvenance(sources []Provenance) error {
	if len(sources) > MaxProvenance {
		return ErrInvalidInput
	}
	for _, p := range sources {
		if p.Kind != "manual" && !candidateKind(p.Kind) || !sourceID.MatchString(p.SourceID) || !validText(p.RecordID, 256) {
			return ErrInvalidInput
		}
		if p.PublicURL != "" && !validPublicURL(p.PublicURL) {
			return ErrInvalidInput
		}
		if !validText(p.SourceField, 256) || !validText(p.Attribution, 1024) || p.LicenseURL != "" && !validPublicURL(p.LicenseURL) {
			return ErrInvalidInput
		}
		for _, stamp := range []string{p.AdoptedAt, p.RetrievedAt} {
			if stamp != "" {
				if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
					return ErrInvalidInput
				}
			}
		}
		if p.Confidence != nil && (math.IsNaN(*p.Confidence) || math.IsInf(*p.Confidence, 0) || *p.Confidence < 0 || *p.Confidence > 1) {
			return ErrInvalidInput
		}
		if p.Evidence != nil {
			e := p.Evidence
			if e.ImageID != "" && !sourceID.MatchString(e.ImageID) {
				return ErrInvalidInput
			}
			if (e.Start == nil) != (e.End == nil) || e.Start != nil && (*e.Start > *e.End || *e.End > MaxRevision) {
				return ErrInvalidInput
			}
		}
	}
	return nil
}
func contains(items []string, item string) bool {
	for _, v := range items {
		if v == item {
			return true
		}
	}
	return false
}
func sortedKeys[V any](items map[string]V) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func stringValue(doc Document, key string) string {
	var s string
	if f := doc.Fields[key]; f.State == "value" {
		_ = json.Unmarshal(f.Value, &s)
	}
	return s
}
