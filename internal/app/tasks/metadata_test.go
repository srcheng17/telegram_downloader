package tasks

import "testing"

func TestNormalizeMetadataTrimsAndNormalizesChineseComma(t *testing.T) {
	got := NormalizeMetadata(MetadataInput{
		TagsRaw:   strPtr("a， b "),
		GenresRaw: strPtr("冒险， 科幻"),
	})
	if got.TagsNormalized == nil || *got.TagsNormalized != "a, b" {
		t.Fatalf("unexpected tags normalization: %#v", got.TagsNormalized)
	}
	if got.GenresNormalized == nil || *got.GenresNormalized != "冒险, 科幻" {
		t.Fatalf("unexpected genres normalization: %#v", got.GenresNormalized)
	}
}

func strPtr(value string) *string {
	return &value
}
