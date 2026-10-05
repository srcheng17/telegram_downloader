package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	SchemaVersion              = 1
	StandardDefinitionsVersion = "standard-v1"
	MaxDocumentBytes           = 256 * 1024
	MaxCustomFields            = 64
	MaxStringBytes             = 4096
	MaxSummaryBytes            = 16384
	MaxListItems               = 64
	MaxListItemBytes           = 1024
	MaxProvenance              = 8
	MaxRevision                = uint64(9007199254740991) // JSON consumers must compare revisions exactly.
)

type Limits struct {
	DocumentBytes      int `json:"document_bytes"`
	CustomFields       int `json:"custom_fields"`
	StringBytes        int `json:"string_bytes"`
	SummaryBytes       int `json:"summary_bytes"`
	ListItems          int `json:"list_items"`
	ListItemBytes      int `json:"list_item_bytes"`
	ProvenancePerField int `json:"provenance_per_field"`
}

type FieldDefinition struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	Type          string   `json:"type"`
	MaxBytes      int      `json:"max_bytes,omitempty"`
	MaxItems      int      `json:"max_items,omitempty"`
	ItemMaxBytes  int      `json:"item_max_bytes,omitempty"`
	Minimum       *int64   `json:"minimum,omitempty"`
	Maximum       *int64   `json:"maximum,omitempty"`
	Enum          []string `json:"enum,omitempty"`
	Extractable   []string `json:"extractable"`
	Editable      bool     `json:"editable"`
	Enabled       bool     `json:"enabled"`
	ExportStatus  string   `json:"export_status"`
	ExportMapping string   `json:"export_mapping,omitempty"`
}

type Registry struct {
	SchemaVersion      int                        `json:"schema_version"`
	DefinitionsVersion string                     `json:"definitions_version"`
	Definitions        map[string]FieldDefinition `json:"definitions"`
	Limits             Limits                     `json:"limits"`
}

func StandardRegistry() Registry {
	r := Registry{SchemaVersion: SchemaVersion, DefinitionsVersion: StandardDefinitionsVersion, Definitions: map[string]FieldDefinition{}, Limits: Limits{MaxDocumentBytes, MaxCustomFields, MaxStringBytes, MaxSummaryBytes, MaxListItems, MaxListItemBytes, MaxProvenance}}
	add := func(key, label, kind, mapping string) {
		d := FieldDefinition{Key: key, Label: label, Type: kind, Editable: true, Enabled: true, Extractable: []string{"archive", "ocr", "rule", "ai", "provider", "legacy"}, ExportStatus: "mapped", ExportMapping: mapping}
		if mapping == "" {
			d.ExportStatus = "internal_only"
		}
		switch kind {
		case "string":
			d.MaxBytes = MaxStringBytes
		case "string[]", "identifiers":
			d.MaxItems = MaxListItems
			d.ItemMaxBytes = MaxListItemBytes
		case "integer":
			min, max := int64(1), int64(2147483647)
			d.Minimum = &min
			d.Maximum = &max
		}
		r.Definitions[key] = d
	}
	for _, field := range [][3]string{{"title", "标题", "Title"}, {"series", "系列", "Series"}, {"number", "本册编号", "Number"}, {"summary", "简介", "Summary"}, {"publisher", "出版社", "Publisher"}, {"imprint", "出版品牌", "Imprint"}, {"language", "语言", "LanguageISO"}, {"format", "出版形式", "Format"}, {"web", "公开链接", "Web"}, {"reading_direction", "阅读方向", "Manga"}, {"manga", "漫画标记", "Manga"}, {"age_rating", "年龄分级", "AgeRating"}} {
		add(field[0], field[1], "string", field[2])
	}
	d := r.Definitions["summary"]
	d.MaxBytes = MaxSummaryBytes
	r.Definitions[d.Key] = d
	for _, field := range [][3]string{{"aliases", "作品别名", ""}, {"tags", "标签", "Tags"}, {"genres", "题材", "Genre"}, {"creators.writer", "作者／原作", "Writer"}, {"creators.penciller", "作画", "Penciller"}, {"creators.inker", "勾线", "Inker"}, {"creators.colorist", "上色", "Colorist"}, {"creators.letterer", "嵌字", "Letterer"}, {"creators.cover_artist", "封面绘制", "CoverArtist"}, {"creators.editor", "编辑", "Editor"}, {"creators.translator", "翻译", "Translator"}} {
		add(field[0], field[1], "string[]", field[2])
	}
	add("count", "系列总册／期数", "integer", "Count")
	add("volume", "系列轮次／卷系", "integer", "Volume")
	add("page_count", "页数", "integer", "PageCount")
	d = r.Definitions["page_count"]
	zero := int64(0)
	d.Minimum = &zero
	d.Extractable = []string{"archive", "legacy"}
	r.Definitions[d.Key] = d
	add("publication_date", "出版日期", "date", "Year/Month/Day")
	add("identifiers", "标识符", "identifiers", "GTIN")
	for key, values := range map[string][]string{"reading_direction": {"ltr", "rtl", "unknown"}, "manga": {"yes", "no", "unknown"}, "age_rating": {"unknown", "Unknown", "Adults Only 18+", "Early Childhood", "Everyone", "Everyone 10+", "G", "Kids to Adults", "M", "MA15+", "Mature 17+", "PG", "R18+", "Rating Pending", "Teen", "X18+"}} {
		d = r.Definitions[key]
		d.Enum = values
		r.Definitions[key] = d
	}
	return r
}

var customKey = regexp.MustCompile(`^custom\.user\.[a-z][a-z0-9_]{0,31}$`)

// NewRegistry publishes a new immutable definition set. Existing keys must remain;
// disabling a field preserves old documents and prevents new values under this version.
func NewRegistry(previous Registry, custom []FieldDefinition) (Registry, error) {
	if len(custom) > MaxCustomFields {
		return Registry{}, invalid("definitions", "too_many_fields")
	}
	result := StandardRegistry()
	seen := map[string]bool{}
	for _, d := range custom {
		if !customKey.MatchString(d.Key) || seen[d.Key] {
			return Registry{}, invalid("definitions", "invalid_or_duplicate_key")
		}
		seen[d.Key] = true
		if err := validateCustomDefinition(d); err != nil {
			return Registry{}, err
		}
		if old, ok := previous.Definitions[d.Key]; ok && old.Type != d.Type {
			return Registry{}, invalid(d.Key, "immutable_type")
		}
		result.Definitions[d.Key] = d
	}
	for key := range previous.Definitions {
		if customKey.MatchString(key) && !seen[key] {
			return Registry{}, invalid(key, "disable_instead_of_delete")
		}
	}
	if reflect.DeepEqual(result.Definitions, previous.Definitions) {
		return previous, nil
	}
	if len(custom) == 0 {
		return result, nil
	}
	data, _ := json.Marshal(result.Definitions)
	hash := sha256.Sum256(data)
	result.DefinitionsVersion = "custom-" + hex.EncodeToString(hash[:])
	return result, nil
}

func validateCustomDefinition(d FieldDefinition) error {
	if !validText(d.Label, 128) || strings.TrimSpace(d.Label) == "" || !strings.ContainsFunc(d.Label, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
		return invalid(d.Key, "chinese_label_required")
	}
	if d.ExportStatus != "internal_only" || d.ExportMapping != "" || len(d.Enum) > 0 {
		return invalid(d.Key, "custom_export_or_enum_not_supported")
	}
	if len(d.Extractable) > 6 {
		return invalid(d.Key, "too_many_extractable_kinds")
	}
	seenKinds := map[string]bool{}
	for _, kind := range d.Extractable {
		if !candidateKind(kind) || seenKinds[kind] {
			return invalid(d.Key, "invalid_extractable_kind")
		}
		seenKinds[kind] = true
	}
	switch d.Type {
	case "string":
		if d.MaxBytes < 1 || d.MaxBytes > MaxStringBytes || d.MaxItems != 0 || d.ItemMaxBytes != 0 || d.Minimum != nil || d.Maximum != nil {
			return invalid(d.Key, "invalid_bounds")
		}
	case "string[]":
		if d.MaxItems < 1 || d.MaxItems > MaxListItems || d.ItemMaxBytes < 1 || d.ItemMaxBytes > MaxListItemBytes || d.MaxBytes != 0 || d.Minimum != nil || d.Maximum != nil {
			return invalid(d.Key, "invalid_bounds")
		}
	case "integer":
		if d.Minimum == nil || d.Maximum == nil || *d.Minimum < -2147483648 || *d.Maximum > 2147483647 || *d.Minimum > *d.Maximum || d.MaxBytes != 0 || d.MaxItems != 0 || d.ItemMaxBytes != 0 {
			return invalid(d.Key, "invalid_bounds")
		}
	case "boolean":
		if d.MaxBytes != 0 || d.MaxItems != 0 || d.ItemMaxBytes != 0 || d.Minimum != nil || d.Maximum != nil {
			return invalid(d.Key, "invalid_bounds")
		}
	default:
		return invalid(d.Key, "unsupported_type")
	}
	return nil
}

func (r Registry) CustomDefinitions() []FieldDefinition {
	defs := make([]FieldDefinition, 0)
	for key, d := range r.Definitions {
		if customKey.MatchString(key) {
			defs = append(defs, d)
		}
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Key < defs[j].Key })
	return defs
}

// ValidateRegistry checks persisted data against its content version and built-in
// contract. It never replaces an old registry with the current settings.
func ValidateRegistry(r Registry) error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: registry schema", ErrUnsupportedVersion)
	}
	rebuilt, err := NewRegistry(StandardRegistry(), r.CustomDefinitions())
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(r, rebuilt) {
		return invalid("definitions", "invalid_registry_snapshot")
	}
	return nil
}
