package extractionrules

import (
	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"testing"
)

func TestFiniteRuleValidation(t *testing.T) {
	r := domain.StandardRegistry()
	good := Rule{ID: "title", TargetKey: "title", Labels: []string{"标题"}, Mode: "label_value"}
	if err := Validate([]Rule{good}, r); err != nil {
		t.Fatal(err)
	}
	if err := Validate([]Rule{}, r); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		rule Rule
	}{
		{"no executable mode", Rule{ID: "x", TargetKey: "title", Labels: []string{"T"}, Mode: "regex"}},
		{"derived field", Rule{ID: "x", TargetKey: "page_count", Labels: []string{"Pages"}, Mode: "label_value"}},
		{"wrong type", Rule{ID: "x", TargetKey: "writers", Labels: []string{"Authors"}, Mode: "continuation"}},
		{"missing option bool", Rule{ID: "x", TargetKey: "title", Labels: []string{"T"}, Mode: "label_value", Options: &Options{Separators: []string{}}}},
		{"ambiguous label", Rule{ID: "other", TargetKey: "series", Labels: []string{" 标题 "}, Mode: "label_value"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if Validate([]Rule{good, test.rule}, r) == nil {
				t.Fatal("invalid rule accepted")
			}
		})
	}
	if Validate(nil, r) == nil {
		t.Fatal("null array accepted")
	}
}

func TestRuleLabelNormalizationAndExplicitNullParity(t *testing.T) {
	r := domain.StandardRegistry()
	duplicate := Rule{ID: "duplicate", TargetKey: "title", Labels: []string{" TITLE ", "title"}, Mode: "label_value"}
	if Validate([]Rule{duplicate}, r) == nil {
		t.Fatal("same-rule duplicate normalized label accepted")
	}
	// Non-ASCII case mappings differ between Go and JS. This finite parser only
	// folds A-Z; U+0130 remains distinct from i in both implementations.
	rules := []Rule{{ID: "one", TargetKey: "title", Labels: []string{"İ"}, Mode: "label_value"}, {ID: "two", TargetKey: "series", Labels: []string{"i"}, Mode: "label_value"}}
	if err := Validate(rules, r); err != nil {
		t.Fatal("ASCII-only normalization drift", err)
	}
	var rule Rule
	if domain.DecodeJSON([]byte(`{"id":"one","target_key":"title","labels":["Title"],"mode":"label_value","label_value_options":null}`), &rule) == nil {
		t.Fatal("explicit null options accepted")
	}
}
