package metadata

import "testing"

func TestNormalizeAuthorListSupportsCommaAndHashDelimiters(t *testing.T) {
	t.Parallel()

	got := NormalizeAuthorListPtr(stringPtr(" 作者A # 作者B，作者A ＃ 作者C "))
	if got == nil || *got != "作者A,作者B,作者C" {
		t.Fatalf("expected normalized author list, got %#v", got)
	}
}

func TestNormalizeTagLikeListSupportsWhitespaceCommaAndHashWithDedupe(t *testing.T) {
	t.Parallel()

	got := NormalizeTagLikeListPtr(stringPtr(" tag1  tag2,#tag3，tag2   # tag4,,tag1  "))
	if got == nil || *got != "tag1,tag2,tag3,tag4" {
		t.Fatalf("expected normalized tag-like list, got %#v", got)
	}
}

func TestNormalizeOptionalTextTrimsBlankValuesToNil(t *testing.T) {
	t.Parallel()

	if got := NormalizeOptionalTextPtr(stringPtr("   ")); got != nil {
		t.Fatalf("expected blank input to normalize to nil, got %#v", got)
	}
}

func stringPtr(value string) *string {
	return &value
}

