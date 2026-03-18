package tasks

import "testing"

func TestNormalizeMetadataTrimsAndNormalizesChineseComma(t *testing.T) {
	got := NormalizeMetadata(MetadataInput{
		TagsRaw:   strPtr("a， b "),
		GenresRaw: strPtr("冒险， 科幻"),
	})
	if got.TagsNormalized == nil || *got.TagsNormalized != "a,b" {
		t.Fatalf("unexpected tags normalization: %#v", got.TagsNormalized)
	}
	if got.GenresNormalized == nil || *got.GenresNormalized != "冒险,科幻" {
		t.Fatalf("unexpected genres normalization: %#v", got.GenresNormalized)
	}
}

func TestNormalizeMetadataSupportsHashAndWhitespaceSeparators(t *testing.T) {
	got := NormalizeMetadata(MetadataInput{
		Author:    strPtr(" 作者A # 作者B ＃ 作者A "),
		TagsRaw:   strPtr("剧情 # 动作， 热血"),
		GenresRaw: strPtr("青年 ＃ 悬疑"),
	})

	if got.Author == nil || *got.Author != "作者A,作者B" {
		t.Fatalf("unexpected author normalization: %#v", got.Author)
	}
	if got.TagsNormalized == nil || *got.TagsNormalized != "剧情,动作,热血" {
		t.Fatalf("unexpected tags normalization: %#v", got.TagsNormalized)
	}
	if got.GenresNormalized == nil || *got.GenresNormalized != "青年,悬疑" {
		t.Fatalf("unexpected genres normalization: %#v", got.GenresNormalized)
	}
}

func strPtr(value string) *string {
	return &value
}
