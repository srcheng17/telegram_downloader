package httpapi

import "testing"

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
