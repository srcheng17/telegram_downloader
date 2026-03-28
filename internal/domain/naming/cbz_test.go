package naming

import (
	"regexp"
	"testing"
)

func TestBuildCBZFileNameIncludesAuthorSeriesTitleAndTimestamp(t *testing.T) {
	t.Parallel()

	got := BuildCBZFileName(CBZFileNameInput{
		Author:    "作者A",
		Series:    "系列B",
		Title:     "漫画C",
		Timestamp: 1700000000,
	})
	if matched := regexp.MustCompile(`^作者A_系列B_漫画C_1700000000\.cbz$`).MatchString(got); !matched {
		t.Fatalf("unexpected cbz file name %q", got)
	}
}

func TestBuildCBZFileNameOmitsSeriesWhenEmpty(t *testing.T) {
	t.Parallel()

	got := BuildCBZFileName(CBZFileNameInput{
		Author:    "作者A",
		Series:    "",
		Title:     "漫画C",
		Timestamp: 1700000000,
	})
	if got != "作者A_漫画C_1700000000.cbz" {
		t.Fatalf("unexpected cbz file name %q", got)
	}
}

