package httpapi

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeLogQueryClampsAndNormalizes(t *testing.T) {
	query := normalizeLogQuery("0", "999", "unknown", "  abc  ")

	if query.Page != 1 {
		t.Fatalf("expected page to clamp to 1, got %d", query.Page)
	}
	if query.PerPage != 100 {
		t.Fatalf("expected per_page to clamp to 100, got %d", query.PerPage)
	}
	if query.Status != "" {
		t.Fatalf("expected unknown status to normalize to empty, got %q", query.Status)
	}
	if query.Keyword != "abc" {
		t.Fatalf("expected keyword to trim whitespace, got %q", query.Keyword)
	}
}

func TestNormalizeLogQueryKeepsUTF8WithinByteLimit(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"ascii", strings.Repeat("a", 121), strings.Repeat("a", 120)},
		{"chinese exact boundary", strings.Repeat("中", 41), strings.Repeat("中", 40)},
		{"partial chinese", strings.Repeat("a", 118) + "中文", strings.Repeat("a", 118)},
		{"partial emoji", strings.Repeat("a", 119) + "😀", strings.Repeat("a", 119)},
		{"emoji exact boundary", strings.Repeat("😀", 31), strings.Repeat("😀", 30)},
		{"malformed input", "中\xff文", "中文"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeLogQuery("1", "20", "", tc.input).Keyword
			if got != tc.want || len(got) > 120 || !utf8.ValidString(got) {
				t.Fatalf("keyword = %q (%d bytes, valid=%v), want %q", got, len(got), utf8.ValidString(got), tc.want)
			}
		})
	}
}
