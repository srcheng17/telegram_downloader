// Package comicinfo owns the fixed ComicInfo protocol and its field mapping.
// It emits a strict profile, retaining unsupported source data in private
// archive sidecars through the caller rather than inventing XML extensions.
package comicinfo

import (
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const Profile = "comicinfo-2.1-draft@99e1453a163c777b4b5320a68732f6f133ac7918"
const MaxXMLBytes = 1 << 20

type elementSpec struct{ Name, Key, Kind string }

// This is the serialization order, checked against the pinned XSD in tests.
var elements = []elementSpec{
	{"Title", "title", "text"}, {"Series", "series", "text"}, {"Number", "number", "text"}, {"Count", "count", "int"}, {"Volume", "volume", "int"},
	{"AlternateSeries", "", "text"}, {"AlternateNumber", "", "text"}, {"AlternateCount", "", "int"}, {"Summary", "summary", "text"}, {"Notes", "", "text"},
	{"Year", "", "int"}, {"Month", "", "int"}, {"Day", "", "int"},
	{"Writer", "creators.writer", "list"}, {"Penciller", "creators.penciller", "list"}, {"Inker", "creators.inker", "list"}, {"Colorist", "creators.colorist", "list"}, {"Letterer", "creators.letterer", "list"}, {"CoverArtist", "creators.cover_artist", "list"}, {"Editor", "creators.editor", "list"}, {"Translator", "creators.translator", "list"},
	{"Publisher", "publisher", "text"}, {"Imprint", "imprint", "text"}, {"Genre", "genres", "list"}, {"Tags", "tags", "list"}, {"Web", "web", "text"}, {"PageCount", "page_count", "int"}, {"LanguageISO", "language", "text"}, {"Format", "format", "text"}, {"BlackAndWhite", "", "yesno"}, {"Manga", "", "manga"},
	{"Characters", "", "text"}, {"Teams", "", "text"}, {"Locations", "", "text"}, {"ScanInformation", "", "text"}, {"StoryArc", "", "text"}, {"StoryArcNumber", "", "text"}, {"SeriesGroup", "", "text"}, {"AgeRating", "age_rating", "age"}, {"Pages", "", "pages"}, {"CommunityRating", "", "rating"}, {"MainCharacterOrTeam", "", "text"}, {"Review", "", "text"}, {"GTIN", "", "text"},
}
var ageRatings = []string{"Unknown", "Adults Only 18+", "Early Childhood", "Everyone", "Everyone 10+", "G", "Kids to Adults", "M", "MA15+", "Mature 17+", "PG", "R18+", "Rating Pending", "Teen", "X18+"}
var decimalRating = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$`)

func specFor(name string) (elementSpec, bool) {
	for _, e := range elements {
		if e.Name == name {
			return e, true
		}
	}
	return elementSpec{}, false
}
func validScalar(spec elementSpec, value string) bool {
	switch spec.Kind {
	case "int":
		v, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
		return err == nil && v >= 0
	case "yesno":
		return slices.Contains([]string{"Unknown", "No", "Yes"}, value)
	case "manga":
		return slices.Contains([]string{"Unknown", "No", "Yes", "YesAndRightToLeft"}, value)
	case "age":
		return slices.Contains(ageRatings, value)
	case "rating":
		value = strings.TrimSpace(value)
		if !decimalRating.MatchString(value) {
			return false
		}
		rating, ok := new(big.Rat).SetString(value)
		if !ok || rating.Sign() < 0 || rating.Cmp(big.NewRat(5, 1)) > 0 {
			return false
		}
		return new(big.Rat).Mul(rating, big.NewRat(10, 1)).IsInt()
	default:
		return true
	}
}
