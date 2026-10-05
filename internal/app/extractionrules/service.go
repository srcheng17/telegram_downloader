// Package extractionrules owns versioned, finite local extraction rules.
package extractionrules

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

var (
	ErrInvalid  = errors.New("invalid extraction rules")
	ErrConflict = errors.New("extraction rules changed")
	ErrNotFound = errors.New("extraction rules not found")
)

const MaxBytes = 64 << 10

type Options struct {
	Separators      []string `json:"separators"`
	CaseInsensitive *bool    `json:"case_insensitive"`
	TrimSpace       *bool    `json:"trim_space"`
}
type Rule struct {
	ID        string   `json:"id"`
	TargetKey string   `json:"target_key"`
	Labels    []string `json:"labels"`
	Mode      string   `json:"mode"`
	Options   *Options `json:"label_value_options,omitempty"`
}

func (r *Rule) UnmarshalJSON(data []byte) error {
	type plain Rule
	var value plain
	if domain.DecodeJSON(data, &value) != nil {
		return ErrInvalid
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(data, &members) != nil {
		return ErrInvalid
	}
	if raw, exists := members["label_value_options"]; exists && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ErrInvalid
	}
	*r = Rule(value)
	return nil
}

type RuleSet struct {
	RulesVersion       uint64           `json:"rules_version"`
	DefinitionsVersion string           `json:"definitions_version"`
	Rules              []Rule           `json:"rules"`
	Warnings           []domain.Warning `json:"warnings,omitempty"`
}
type Update struct {
	ExpectedVersion    *uint64 `json:"expected_version"`
	DefinitionsVersion string  `json:"definitions_version"`
	Rules              []Rule  `json:"rules"`
}
type Repository interface {
	Get(context.Context) (RuleSet, error)
	Save(context.Context, RuleSet, uint64) (RuleSet, error)
}
type Schema interface {
	Schema(context.Context) (domain.Registry, error)
}
type Service struct {
	repo   Repository
	schema Schema
}

func NewService(repo Repository, schema Schema) *Service { return &Service{repo, schema} }

func (s *Service) Get(ctx context.Context) (RuleSet, error) {
	r, err := s.schema.Schema(ctx)
	if err != nil {
		return RuleSet{}, err
	}
	set, err := s.repo.Get(ctx)
	if errors.Is(err, ErrNotFound) {
		return RuleSet{DefinitionsVersion: r.DefinitionsVersion, Rules: []Rule{}}, nil
	}
	if err != nil {
		return RuleSet{}, err
	}
	if set.DefinitionsVersion != r.DefinitionsVersion || Validate(set.Rules, r) != nil {
		set.Warnings = []domain.Warning{{Key: "rules", Code: "definitions_changed", Message: "字段定义已变化，请重新核对并保存规则；旧规则暂不执行。"}}
	}
	return set, nil
}
func (s *Service) Save(ctx context.Context, input Update) (RuleSet, error) {
	if input.ExpectedVersion == nil || *input.ExpectedVersion >= domain.MaxRevision {
		return RuleSet{}, ErrInvalid
	}
	r, err := s.schema.Schema(ctx)
	if err != nil {
		return RuleSet{}, err
	}
	if input.DefinitionsVersion != r.DefinitionsVersion {
		return RuleSet{}, ErrConflict
	}
	if err = Validate(input.Rules, r); err != nil {
		return RuleSet{}, err
	}
	return s.repo.Save(ctx, RuleSet{DefinitionsVersion: r.DefinitionsVersion, Rules: input.Rules}, *input.ExpectedVersion)
}

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func normalizeLabel(label string) string {
	// Match the browser's finite normalization: Unicode White_Space and A-Z.
	// Full Unicode lowercasing differs across Go/ECMAScript (e.g. U+0130, sigma).
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, strings.TrimSpace(label))
}

func Validate(rules []Rule, registry domain.Registry) error {
	data, err := json.Marshal(rules)
	if err != nil || len(data) > MaxBytes || rules == nil || len(rules) > 128 {
		return ErrInvalid
	}
	ids := map[string]bool{}
	labels := map[string]string{}
	for _, rule := range rules {
		if !validID.MatchString(rule.ID) || ids[rule.ID] || len(rule.Labels) < 1 || len(rule.Labels) > 8 {
			return ErrInvalid
		}
		ids[rule.ID] = true
		d, ok := registry.Definitions[rule.TargetKey]
		if !ok || !d.Enabled || !slices.Contains(d.Extractable, "rule") || rule.TargetKey == "page_count" {
			return ErrInvalid
		}
		switch rule.Mode {
		case "continuation":
			if d.Type != "string" {
				return ErrInvalid
			}
		case "hashtag_list":
			if d.Type != "string[]" {
				return ErrInvalid
			}
		case "label_value":
			if !slices.Contains([]string{"string", "string[]", "integer", "boolean", "date", "identifiers"}, d.Type) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		ownLabels := map[string]bool{}
		for _, label := range rule.Labels {
			normalized := normalizeLabel(label)
			if normalized == "" || len(label) > 128 || !utf8.ValidString(label) || strings.IndexFunc(label, unicode.IsControl) >= 0 || strings.ContainsAny(label, ":：") {
				return ErrInvalid
			}
			if ownLabels[normalized] {
				return ErrInvalid
			}
			ownLabels[normalized] = true
			if previous, exists := labels[normalized]; exists && previous != rule.TargetKey {
				return ErrInvalid
			}
			labels[normalized] = rule.TargetKey
		}
		if o := rule.Options; o != nil {
			if o.CaseInsensitive == nil || o.TrimSpace == nil || o.Separators == nil || len(o.Separators) > 4 {
				return ErrInvalid
			}
			seen := map[string]bool{}
			for _, sep := range o.Separators {
				if !slices.Contains([]string{"comma", "chinese_comma", "semicolon", "newline"}, sep) || seen[sep] {
					return ErrInvalid
				}
				seen[sep] = true
			}
		}
	}
	return nil
}
