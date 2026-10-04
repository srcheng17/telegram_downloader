package comicinfo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

func TestPreserveStandardMissingClearAndPageMapping(t *testing.T) {
	r := metadata.StandardRegistry()
	doc := metadata.EmptyDocument(r)
	doc.Fields["title"] = metadata.FieldState{State: "cleared", Provenance: []metadata.Provenance{}}
	doc.DefinitionSnapshot["title"] = r.Definitions["title"]
	doc.Fields["tags"] = metadata.FieldState{State: "value", Value: json.RawMessage(`["中文","A&B"]`), Provenance: []metadata.Provenance{}}
	doc.DefinitionSnapshot["tags"] = r.Definitions["tags"]
	input := []byte(`<ComicInfo><Title>Old</Title><Series>Keep</Series><Notes>Untouched &amp; exact</Notes><Writer>Writer</Writer><Pages><Page Image="0" Type="FrontCover"/><Page Image="1" Bookmark="Second"/></Pages><Extension flag="x">private original</Extension></ComicInfo>`)
	result, err := Merge(input, doc, r, []int{1, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	output := string(result.XML)
	for _, text := range []string{"<Series>Keep</Series>", "<Notes>Untouched &amp; exact</Notes>", "<Tags>中文, A&amp;B</Tags>", "<PageCount>2</PageCount>", `Image="1" Type="FrontCover"`} {
		if !strings.Contains(output, text) {
			t.Fatalf("missing %q in %s", text, output)
		}
	}
	if strings.Contains(output, "<Title>") || strings.Contains(output, "Extension") {
		t.Fatal("clear ignored or extension emitted")
	}
	if len(result.Warnings) == 0 || result.EffectiveDocument.Fields["title"].State != "cleared" || string(result.EffectiveDocument.Fields["series"].Value) != `"Keep"` {
		t.Fatal("effective clear/baseline lost")
	}
	if _, _, err := metadata.Validate(result.EffectiveDocument, r); err != nil {
		t.Fatal(err)
	}
}
func TestXMLBoundsMalformedAndDTD(t *testing.T) {
	for _, raw := range []string{`<!DOCTYPE ComicInfo [<!ENTITY x SYSTEM "file:///etc/passwd">]><ComicInfo><Title>&x;</Title></ComicInfo>`, `<ComicInfo><Title>broken</ComicInfo>`, `<ComicInfo/><ComicInfo/>`, strings.Repeat("<x>", 65) + strings.Repeat("</x>", 65), strings.Repeat(" ", MaxXMLBytes+1)} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatal("unsafe XML accepted")
		}
	}
}
