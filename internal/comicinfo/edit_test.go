package comicinfo

import (
	"errors"
	"strings"
	"testing"
)

func TestEditExistingPreservesUnchangedFieldsAndPages(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?><ComicInfo><Title>Old</Title><Summary>Keep</Summary><PageCount>2</PageCount><Pages><Page Image="0" Type="FrontCover"/><Page Image="1"/></Pages></ComicInfo>`)
	title := "New"
	result, err := EditExisting(raw, []ElementChange{{Name: "Title", Value: &title}}, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(result.XML)
	if err != nil || parsed.Values["Title"] != "New" || parsed.Values["Summary"] != "Keep" || parsed.Values["PageCount"] != "2" || len(parsed.Pages) != 2 {
		t.Fatalf("unexpected edited XML: %#v, %v", parsed, err)
	}
	if len(result.ChangedElements) != 1 || result.ChangedElements[0] != "Title" {
		t.Fatalf("changed elements: %#v", result.ChangedElements)
	}
}

func TestEditExistingRejectsUnknownXMLAndImplicitPageCountChange(t *testing.T) {
	for _, raw := range []string{
		`<ComicInfo><Title>Old</Title><Custom>keep me</Custom></ComicInfo>`,
		`<ComicInfo><!-- retain --><Title>Old</Title></ComicInfo>`,
		`<ComicInfo><Title>Old</Title><PageCount>9</PageCount></ComicInfo>`,
	} {
		value := "New"
		_, err := EditExisting([]byte(raw), []ElementChange{{Name: "Title", Value: &value}}, 1, false)
		if !errors.Is(err, ErrUnsafeExisting) {
			t.Fatalf("unsafe XML accepted: %s, %v", raw, err)
		}
	}
	corrected, err := EditExisting([]byte(`<ComicInfo><Title>Old</Title><PageCount>9</PageCount></ComicInfo>`), []ElementChange{{Name: "Title", Value: nil}}, 1, true)
	if err != nil || !strings.Contains(string(corrected.XML), "<PageCount>1</PageCount>") {
		t.Fatalf("explicit correction rejected: %v", err)
	}
}
