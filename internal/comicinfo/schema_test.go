package comicinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

func TestPinnedSchemaAndActualXSDValidation(t *testing.T) {
	for path, want := range map[string]string{"testdata/schema/v2.0/ComicInfo.xsd": "3d9109effff705014f5f6076d92b0ad8c8dad90d993f2592d744c10bad45311c", "testdata/schema/v2.1-draft/ComicInfo.xsd": "c7a925b75a5297ec66b19898f59597077326a6b812d5ce77b3035fc179d75ed9"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(raw)
		if hex.EncodeToString(hash[:]) != want {
			t.Fatal("pinned schema changed")
		}
	}
	raw, _ := os.ReadFile("testdata/schema/v2.1-draft/ComicInfo.xsd")
	var schema struct {
		Types []struct {
			Name     string `xml:"name,attr"`
			Sequence struct {
				Elements []struct {
					Name string `xml:"name,attr"`
				} `xml:"element"`
			} `xml:"sequence"`
		} `xml:"complexType"`
	}
	if err := xml.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	for _, kind := range schema.Types {
		if kind.Name == "ComicInfo" {
			if len(kind.Sequence.Elements) != len(elements) {
				t.Fatal("profile field count drift")
			}
			for i, item := range kind.Sequence.Elements {
				if elements[i].Name != item.Name {
					t.Fatal("profile sequence drift")
				}
			}
		}
	}
	r := metadata.StandardRegistry()
	doc := metadata.EmptyDocument(r)
	set := func(key string, value any) {
		raw, _ := json.Marshal(value)
		doc.Fields[key] = metadata.FieldState{State: "value", Value: raw, Provenance: []metadata.Provenance{}}
		doc.DefinitionSnapshot[key] = r.Definitions[key]
	}
	set("title", "中文 & <书名>")
	set("tags", []string{"BL", "原样标签"})
	set("creators.translator", []string{"译者"})
	set("identifiers", []metadata.Identifier{{Scheme: "isbn", Value: "978-4-08-874784-2"}})
	set("age_rating", "Mature 17+")
	set("manga", "yes")
	set("reading_direction", "rtl")
	result, err := Merge([]byte(`<ComicInfo><AlternateSeries>Alternate</AlternateSeries><BlackAndWhite>Yes</BlackAndWhite><CommunityRating>4.5</CommunityRating><Review>Preserved</Review></ComicInfo>`), doc, r, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := exec.LookPath("xmllint")
	if err != nil {
		t.Fatal("xmllint is required to verify the pinned offline XSD")
	}
	file := filepath.Join(t.TempDir(), "ComicInfo.xml")
	if err = os.WriteFile(file, result.XML, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(binary, "--nonet", "--noout", "--schema", "testdata/schema/v2.1-draft/ComicInfo.xsd", file).CombinedOutput(); err != nil {
		t.Fatalf("2.1 XSD: %v %s", err, output)
	}
	if _, err = exec.Command(binary, "--nonet", "--noout", "--schema", "testdata/schema/v2.0/ComicInfo.xsd", file).CombinedOutput(); err == nil {
		t.Fatal("2.0 unexpectedly accepted Tags/Translator/GTIN")
	}
	core, err := Merge(nil, metadata.EmptyDocument(r), r, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(file, core.XML, 0600)
	if output, err := exec.Command(binary, "--nonet", "--noout", "--schema", "testdata/schema/v2.0/ComicInfo.xsd", file).CombinedOutput(); err != nil {
		t.Fatalf("2.0 core comparison failed: %s", output)
	}
}
func TestUnknownFieldsCommaListsIdentifiersAndMangaConflict(t *testing.T) {
	r := metadata.StandardRegistry()
	doc := metadata.EmptyDocument(r)
	set := func(key string, value any) {
		raw, _ := json.Marshal(value)
		doc.Fields[key] = metadata.FieldState{State: "value", Value: raw, Provenance: []metadata.Provenance{}}
		doc.DefinitionSnapshot[key] = r.Definitions[key]
	}
	set("tags", []string{"single, indivisible"})
	set("manga", "no")
	set("reading_direction", "rtl")
	set("identifiers", []metadata.Identifier{{Scheme: "isbn", Value: "9784088747842"}, {Scheme: "isbn", Value: "9784088700335"}, {Scheme: "bangumi", Value: "123"}})
	result, err := Merge([]byte(`<ComicInfo private="saved"><Unknown secret="x"/><Count>72</Count><Number>1.5</Number><Volume>2024</Volume><Manga>Yes</Manga><Pages><Page Image="99"/></Pages></ComicInfo>`), doc, r, []int{0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"<Tags>", "<Manga>", "<GTIN>", "<Pages>", "Unknown", "private"} {
		if strings.Contains(string(result.XML), forbidden) {
			t.Fatalf("unexpected %s", forbidden)
		}
	}
	for _, expected := range []string{"<Count>72</Count>", "<Number>1.5</Number>", "<Volume>2024</Volume>"} {
		if !strings.Contains(string(result.XML), expected) {
			t.Fatal("count/volume/number semantics lost")
		}
	}
	if len(result.Warnings) < 5 {
		t.Fatal("loss warnings missing")
	}
	deep := "<ComicInfo>" + strings.Repeat("<x>", 64) + strings.Repeat("</x>", 64) + "</ComicInfo>"
	if _, err = Parse([]byte(deep)); err == nil {
		t.Fatal("depth64 limit not enforced")
	}
}

func TestXMLLexicalPreservationAndIdentifierSchemes(t *testing.T) {
	data := append([]byte{0xef, 0xbb, 0xbf}, []byte(`<ComicInfo><CommunityRating>4.500</CommunityRating></ComicInfo>`)...)
	parsed, err := Parse(data)
	if err != nil || parsed.Values["CommunityRating"] != "4.500" {
		t.Fatal("valid XML BOM/decimal lost")
	}
	if _, err = Parse([]byte(`<ComicInfo a="1" a="2"/>`)); err == nil {
		t.Fatal("duplicate XML attribute accepted")
	}
	for _, id := range []metadata.Identifier{{Scheme: "isbn", Value: "12345670"}, {Scheme: "isbn13", Value: "4006381333931"}, {Scheme: "gtin", Value: "4088747844"}, {Scheme: "bangumi", Value: "9784088747842"}} {
		if _, ok := exportIdentifier(id); ok {
			t.Fatal("incorrect identifier scheme exported")
		}
	}
	if _, ok := exportIdentifier(metadata.Identifier{Scheme: "isbn", Value: "9784088747842"}); !ok {
		t.Fatal("valid ISBN rejected")
	}
}
