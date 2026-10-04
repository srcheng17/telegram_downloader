package comicinfo

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type Result struct {
	XML               []byte
	EffectiveDocument metadata.Document
	Profile           string
	Warnings          []metadata.Warning
	PreservedStandard []string
}

// Merge retains absent fields and applies explicit tombstones. Page mapping is
// indexed by the reliably known source image index; nil means unknown order.
func Merge(raw []byte, submitted metadata.Document, registry metadata.Registry, pageMapping []int, pageCount int) (Result, error) {
	result := Result{Profile: Profile, Warnings: []metadata.Warning{}, PreservedStandard: []string{}}
	if pageCount < 1 || pageCount > 300 {
		return result, errors.New("invalid actual page count")
	}
	doc, _, err := metadata.Validate(submitted, registry)
	if err != nil {
		return result, err
	}
	parsed, err := Parse(raw)
	if err != nil {
		return result, err
	}
	result.Warnings = append(result.Warnings, parsed.Warnings...)
	values := parsed.Values
	baseline := map[string]json.RawMessage{}
	putBaseline := func(key string, value any) { raw, _ := json.Marshal(value); baseline[key] = raw }
	for _, spec := range elements {
		value, present := values[spec.Name]
		if !present {
			continue
		}
		if spec.Key == "" {
			if !slices.Contains([]string{"Year", "Month", "Day", "Manga", "GTIN", "Pages"}, spec.Name) {
				result.PreservedStandard = append(result.PreservedStandard, spec.Name)
			}
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		switch spec.Kind {
		case "list":
			items := []string{}
			for _, item := range strings.Split(value, ",") {
				if item = strings.TrimSpace(item); item != "" {
					items = append(items, item)
				}
			}
			putBaseline(spec.Key, items)
		case "int":
			n, e := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if e == nil {
				putBaseline(spec.Key, n)
			}
		default:
			putBaseline(spec.Key, value)
		}
	}
	if year, e := strconv.Atoi(values["Year"]); e == nil && year > 0 {
		date := metadata.PublicationDate{Year: year}
		if month, e := strconv.Atoi(values["Month"]); e == nil {
			date.Month = &month
		}
		if day, e := strconv.Atoi(values["Day"]); e == nil {
			date.Day = &day
		}
		putBaseline("publication_date", date)
	}
	if value, ok := values["Manga"]; ok {
		switch value {
		case "YesAndRightToLeft":
			putBaseline("manga", "yes")
			putBaseline("reading_direction", "rtl")
		case "Yes":
			putBaseline("manga", "yes")
		case "No":
			putBaseline("manga", "no")
		default:
			putBaseline("manga", "unknown")
		}
	}
	if value, ok := values["GTIN"]; ok {
		if normalized, valid := validIdentifier(value); valid {
			scheme := "gtin"
			if len(normalized) == 10 || strings.HasPrefix(normalized, "978") || strings.HasPrefix(normalized, "979") {
				scheme = "isbn"
			}
			putBaseline("identifiers", []metadata.Identifier{{Scheme: scheme, Value: normalized}})
		} else {
			delete(values, "GTIN")
			warn(&result.Warnings, "identifiers", "invalid_identifier", "原 GTIN 不能通过标识符校验，仅保留在私密副本中。")
		}
	}
	candidate := metadata.MetadataCandidate{CandidateID: "archive-merge", RequestID: "archive-merge", Origin: "archive", SchemaVersion: metadata.SchemaVersion, DefinitionsVersion: registry.DefinitionsVersion, BaseDocumentRevision: doc.Revision, Fields: map[string]metadata.CandidateField{}, FieldRevisions: map[string]uint64{}}
	add := func(key string, value json.RawMessage, sourceField string) {
		def, ok := registry.Definitions[key]
		if !ok || !def.Enabled || !slices.Contains(def.Extractable, "archive") {
			return
		}
		field := metadata.CandidateField{State: "value", Value: value, Provenance: []metadata.Provenance{{Kind: "archive", SourceID: "comicinfo", SourceField: sourceField}}}
		probe := candidate
		probe.Fields = map[string]metadata.CandidateField{key: field}
		probe.FieldRevisions = map[string]uint64{key: doc.Fields[key].Revision}
		if metadata.ValidateCandidate(probe, registry) != nil {
			warn(&result.Warnings, key, "archive_field_not_imported", "原字段不符合当前文档约束；原 XML 已保留，未作为草稿字段补入。")
			if key == "web" {
				delete(values, "Web")
			}
			return
		}
		candidate.Fields[key] = field
		candidate.FieldRevisions[key] = doc.Fields[key].Revision
	}
	for key, value := range baseline {
		if _, provided := doc.Fields[key]; !provided {
			add(key, value, sourceXMLField(key))
		}
	}
	countRaw, _ := json.Marshal(pageCount)
	add("page_count", countRaw, "actual_images")
	keys := make([]string, 0, len(candidate.Fields))
	for key := range candidate.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		doc, _, err = metadata.ApplyCandidate(doc, registry, doc.Revision, candidate, keys, true, metadata.MergeContext{Now: time.Now()})
		if err != nil {
			return result, err
		}
	}
	result.EffectiveDocument = doc
	for _, spec := range elements {
		if spec.Key == "" {
			continue
		}
		field, provided := submitted.Fields[spec.Key]
		if !provided {
			continue
		}
		delete(values, spec.Name)
		if field.State == "cleared" {
			continue
		}
		var value string
		switch spec.Kind {
		case "list":
			var items []string
			_ = json.Unmarshal(field.Value, &items)
			ambiguous := false
			for _, item := range items {
				if strings.Contains(item, ",") {
					ambiguous = true
				}
			}
			if ambiguous {
				warn(&result.Warnings, spec.Key, "list_separator_loss", "列表项包含逗号，无法无损导出；完整值保留在应用及私密副本中。")
				continue
			}
			if len(items) == 0 {
				continue
			}
			value = strings.Join(items, ", ")
		case "int":
			var n int64
			_ = json.Unmarshal(field.Value, &n)
			value = strconv.FormatInt(n, 10)
		default:
			_ = json.Unmarshal(field.Value, &value)
		}
		if value == "unknown" || value == "Unknown" {
			continue
		}
		if validScalar(spec, value) {
			values[spec.Name] = value
		} else {
			warn(&result.Warnings, spec.Key, "not_exported", "该字段无法表示为目标 ComicInfo 取值，完整值保留在应用中。")
		}
	}
	values["PageCount"] = strconv.Itoa(pageCount)
	if f, present := submitted.Fields["publication_date"]; present {
		for _, name := range []string{"Year", "Month", "Day"} {
			delete(values, name)
		}
		if f.State == "value" {
			var date metadata.PublicationDate
			_ = json.Unmarshal(f.Value, &date)
			values["Year"] = strconv.Itoa(date.Year)
			if date.Month != nil {
				values["Month"] = strconv.Itoa(*date.Month)
			}
			if date.Day != nil {
				values["Day"] = strconv.Itoa(*date.Day)
			}
		}
	}
	if _, m := submitted.Fields["manga"]; m {
		exportManga(values, doc, &result.Warnings)
	} else if _, d := submitted.Fields["reading_direction"]; d {
		exportManga(values, doc, &result.Warnings)
	}
	if f, present := submitted.Fields["identifiers"]; present {
		delete(values, "GTIN")
		if f.State == "value" {
			var ids []metadata.Identifier
			_ = json.Unmarshal(f.Value, &ids)
			valid := map[string]bool{}
			for _, id := range ids {
				if value, ok := exportIdentifier(id); ok {
					valid[value] = true
				}
			}
			if len(valid) == 1 {
				for value := range valid {
					values["GTIN"] = value
				}
			} else if len(ids) > 0 {
				warn(&result.Warnings, "identifiers", "identifier_selection_required", "标识符未能唯一确定有效 GTIN/ISBN，未写入 XML；请核对标识符。")
			}
		}
	}
	for key, field := range doc.Fields {
		if field.State == "value" && (strings.HasPrefix(key, "custom.user.") || key == "aliases") {
			warn(&result.Warnings, key, "internal_only", "此字段没有 ComicInfo 标准映射，保留在应用及私密副本中。")
		}
	}
	pages := remapPages(parsed.Pages, pageMapping, pageCount, &result.Warnings)
	result.XML, err = serialize(values, pages)
	if err != nil {
		return result, err
	}
	if len(result.XML) > MaxXMLBytes {
		return result, errors.New("generated ComicInfo exceeds limit")
	}
	if check, e := Parse(result.XML); e != nil || len(check.Warnings) > 0 {
		return result, errors.New("generated ComicInfo failed profile readback")
	}
	return result, nil
}

func sourceXMLField(key string) string {
	for _, spec := range elements {
		if spec.Key == key {
			return spec.Name
		}
	}
	switch key {
	case "publication_date":
		return "Year/Month/Day"
	case "identifiers":
		return "GTIN"
	case "manga", "reading_direction":
		return "Manga"
	}
	return "ComicInfo"
}
func exportManga(values map[string]string, doc metadata.Document, warnings *[]metadata.Warning) {
	delete(values, "Manga")
	m, d := doc.Fields["manga"], doc.Fields["reading_direction"]
	if m.State != "value" {
		if d.State == "value" {
			warn(warnings, "reading_direction", "reading_direction_unmapped", "阅读方向需要明确漫画标记才能映射为 Manga；已保留完整内部值。")
		}
		return
	}
	var manga, direction string
	_ = json.Unmarshal(m.Value, &manga)
	if d.State == "value" {
		_ = json.Unmarshal(d.Value, &direction)
	}
	if manga == "no" && direction == "rtl" {
		warn(warnings, "manga", "manga_direction_conflict", "明确非漫画与从右向左阅读不能同时映射为 Manga，未导出该项。")
		return
	}
	switch manga {
	case "no":
		values["Manga"] = "No"
	case "yes":
		values["Manga"] = "Yes"
		if direction == "rtl" {
			values["Manga"] = "YesAndRightToLeft"
		}
	default:
		if direction == "rtl" {
			warn(warnings, "reading_direction", "reading_direction_unmapped", "漫画标记未知，无法单独导出从右向左阅读；已保留完整内部值。")
		}
	}
}

func exportIdentifier(id metadata.Identifier) (string, bool) {
	value, valid := validIdentifier(id.Value)
	if !valid {
		return "", false
	}
	switch strings.ToLower(id.Scheme) {
	case "isbn":
		return value, len(value) == 10 || len(value) == 13 && (strings.HasPrefix(value, "978") || strings.HasPrefix(value, "979"))
	case "isbn10":
		return value, len(value) == 10
	case "isbn13":
		return value, len(value) == 13 && (strings.HasPrefix(value, "978") || strings.HasPrefix(value, "979"))
	case "gtin":
		return value, len(value) != 10
	case "ean":
		return value, len(value) == 8 || len(value) == 13
	default:
		return "", false
	}
}
func remapPages(pages []Page, mapping []int, count int, warnings *[]metadata.Warning) []Page {
	if len(pages) == 0 {
		return nil
	}
	validMapping := len(mapping) == count
	used := map[int]bool{}
	for _, index := range mapping {
		if index < 0 || index >= count || used[index] {
			validMapping = false
		}
		used[index] = true
	}
	if !validMapping {
		warn(warnings, "", "pages_unmapped", "原页面枚举顺序无法确定，Pages 已保留到私密副本，未写入新包。")
		return nil
	}
	result := []Page{}
	seen := map[int]bool{}
	for _, page := range pages {
		for _, attr := range page.Attributes {
			if attr.Name.Local == "Image" {
				index, _ := strconv.Atoi(attr.Value)
				if index < 0 || index >= count || seen[index] {
					warn(warnings, "", "pages_unmapped", "原页面索引越界或重复，Pages 已保留到私密副本，未写入新包。")
					return nil
				}
				seen[index] = true
			}
		}
		copyPage := Page{Attributes: append([]xml.Attr(nil), page.Attributes...)}
		for i, attr := range copyPage.Attributes {
			if attr.Name.Local == "Image" {
				index, _ := strconv.Atoi(attr.Value)
				copyPage.Attributes[i].Value = strconv.Itoa(mapping[index])
			}
		}
		result = append(result, copyPage)
	}
	sort.SliceStable(result, func(i, j int) bool { return pageIndex(result[i]) < pageIndex(result[j]) })
	return result
}
func pageIndex(page Page) int {
	for _, attr := range page.Attributes {
		if attr.Name.Local == "Image" {
			value, _ := strconv.Atoi(attr.Value)
			return value
		}
	}
	return -1
}
func serialize(values map[string]string, pages []Page) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)
	encoder := xml.NewEncoder(&buffer)
	encoder.Indent("", "  ")
	root := xml.StartElement{Name: xml.Name{Local: "ComicInfo"}}
	if err := encoder.EncodeToken(root); err != nil {
		return nil, err
	}
	for _, spec := range elements {
		start := xml.StartElement{Name: xml.Name{Local: spec.Name}}
		if spec.Kind == "pages" {
			if len(pages) == 0 {
				continue
			}
			if err := encoder.EncodeToken(start); err != nil {
				return nil, err
			}
			for _, page := range pages {
				p := xml.StartElement{Name: xml.Name{Local: "Page"}, Attr: page.Attributes}
				if err := encoder.EncodeToken(p); err != nil {
					return nil, err
				}
				if err := encoder.EncodeToken(p.End()); err != nil {
					return nil, err
				}
			}
			if err := encoder.EncodeToken(start.End()); err != nil {
				return nil, err
			}
			continue
		}
		if value, ok := values[spec.Name]; ok {
			if err := encoder.EncodeElement(value, start); err != nil {
				return nil, err
			}
		}
	}
	if err := encoder.EncodeToken(root.End()); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
func validIdentifier(value string) (string, bool) {
	value = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(value)))
	if len(value) == 10 {
		sum := 0
		for i, c := range value {
			n := int(c - '0')
			if c == 'X' && i == 9 {
				n = 10
			}
			if n < 0 || n > 9 && !(n == 10 && i == 9) {
				return "", false
			}
			sum += (10 - i) * n
		}
		return value, sum%11 == 0
	}
	if !slices.Contains([]int{8, 12, 13, 14}, len(value)) {
		return "", false
	}
	sum := 0
	for i := len(value) - 1; i >= 0; i-- {
		c := value[i]
		if c < '0' || c > '9' {
			return "", false
		}
		weight := 1
		if (len(value)-1-i)%2 == 1 {
			weight = 3
		}
		sum += int(c-'0') * weight
	}
	return value, sum%10 == 0
}
