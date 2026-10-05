package comicinfo

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

var ErrInvalidXML = errors.New("原 ComicInfo 无法安全解析，请修正归档后重新提交")

type Page struct{ Attributes []xml.Attr }
type Parsed struct {
	Values   map[string]string
	Pages    []Page
	Warnings []metadata.Warning
}
type node struct {
	name     xml.Name
	attrs    []xml.Attr
	text     strings.Builder
	children []*node
}

func warning(key, code, message string) metadata.Warning {
	return metadata.Warning{Key: key, Code: code, Message: message}
}
func warn(list *[]metadata.Warning, key, code, message string) {
	for _, existing := range *list {
		if existing.Key == key && existing.Code == code {
			return
		}
	}
	if len(*list) < metadata.MaxListItems {
		*list = append(*list, warning(key, code, message))
	}
}

func Parse(data []byte) (Parsed, error) {
	result := Parsed{Values: map[string]string{}, Warnings: []metadata.Warning{}}
	if len(data) == 0 {
		return result, nil
	}
	if len(data) > MaxXMLBytes || !utf8.Valid(data) {
		return result, ErrInvalidXML
	}
	decoder := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	decoder.Strict = true
	var root *node
	stack := []*node{}
	closed := false
	declaration := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, ErrInvalidXML
		}
		switch v := token.(type) {
		case xml.Directive:
			return result, ErrInvalidXML
		case xml.ProcInst:
			if v.Target != "xml" || root != nil || declaration {
				return result, ErrInvalidXML
			}
			declaration = true
		case xml.StartElement:
			if closed || len(stack) >= 64 {
				return result, ErrInvalidXML
			}
			attributes := map[xml.Name]bool{}
			for _, attribute := range v.Attr {
				if attributes[attribute.Name] {
					return result, ErrInvalidXML
				}
				attributes[attribute.Name] = true
			}
			n := &node{name: v.Name, attrs: append([]xml.Attr(nil), v.Attr...)}
			if len(stack) == 0 {
				if root != nil || v.Name.Local != "ComicInfo" || v.Name.Space != "" {
					return result, ErrInvalidXML
				}
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return result, ErrInvalidXML
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				closed = true
			}
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(v)) != "" {
					return result, ErrInvalidXML
				}
			} else {
				stack[len(stack)-1].text.Write(v)
			}
		case xml.Comment:
			warn(&result.Warnings, "", "xml_comment_retained", "原 XML 注释仅保留在私密元数据副本中。")
		}
	}
	if root == nil || !closed || len(stack) > 0 || strings.TrimSpace(root.text.String()) != "" {
		return result, ErrInvalidXML
	}
	for _, attr := range root.attrs {
		if attr.Name.Space != "xmlns" && attr.Name.Local != "xmlns" {
			warn(&result.Warnings, "", "xml_attribute_retained", "原 XML 扩展属性仅保留在私密副本中。")
		}
	}
	seen := map[string]bool{}
	for _, child := range root.children {
		spec, known := specFor(child.name.Local)
		if !known || child.name.Space != "" {
			warn(&result.Warnings, "", "xml_extension_retained", "原 XML 扩展元素仅保留在私密副本中。")
			continue
		}
		if seen[spec.Name] {
			return result, ErrInvalidXML
		}
		seen[spec.Name] = true
		if len(child.attrs) > 0 {
			warn(&result.Warnings, spec.Key, "xml_attribute_retained", "原字段的扩展属性仅保留在私密副本中。")
		}
		if spec.Kind == "pages" {
			parsePages(child, &result)
			continue
		}
		if len(child.children) > 0 || !validScalar(spec, child.text.String()) {
			warn(&result.Warnings, spec.Key, "unsupported_xml_value", "原字段的结构或取值不符合导出格式，已保留副本。")
			continue
		}
		result.Values[spec.Name] = child.text.String()
	}
	return result, nil
}
func parsePages(parent *node, result *Parsed) {
	if strings.TrimSpace(parent.text.String()) != "" {
		warn(&result.Warnings, "", "pages_unmapped", "原页面信息无法可靠映射，已保留副本。")
		return
	}
	for _, child := range parent.children {
		if child.name.Local != "Page" || child.name.Space != "" || len(child.children) > 0 || strings.TrimSpace(child.text.String()) != "" {
			warn(&result.Warnings, "", "pages_unmapped", "原页面扩展仅保留在私密副本中。")
			continue
		}
		page := Page{}
		valid := true
		hasImage := false
		seen := map[string]bool{}
		for _, attr := range child.attrs {
			name := attr.Name.Local
			if seen[name] {
				valid = false
				break
			}
			seen[name] = true
			if attr.Name.Space != "" {
				warn(&result.Warnings, "", "page_attribute_retained", "原页面扩展属性仅保留在私密副本中。")
				continue
			}
			switch name {
			case "Image":
				hasImage = true
				v, err := strconv.ParseInt(attr.Value, 10, 32)
				valid = err == nil && v >= 0
			case "ImageSize":
				v, err := strconv.ParseInt(attr.Value, 10, 64)
				valid = err == nil && v >= 0
			case "ImageWidth", "ImageHeight":
				_, err := strconv.ParseInt(attr.Value, 10, 32)
				valid = err == nil
			case "DoublePage":
				valid = slices.Contains([]string{"true", "false", "0", "1"}, attr.Value)
			case "Type":
				for _, value := range strings.Fields(attr.Value) {
					if !slices.Contains([]string{"FrontCover", "InnerCover", "Roundup", "Story", "Advertisement", "Editorial", "Letters", "Preview", "BackCover", "Other", "Deleted"}, value) {
						valid = false
					}
				}
			case "Key", "Bookmark":
			default:
				warn(&result.Warnings, "", "page_attribute_retained", "原页面扩展属性仅保留在私密副本中。")
				continue
			}
			if !valid {
				break
			}
			page.Attributes = append(page.Attributes, attr)
		}
		if valid && hasImage {
			result.Pages = append(result.Pages, page)
		} else {
			warn(&result.Warnings, "", "pages_unmapped", "原页面索引或属性无效，已保留副本。")
		}
	}
}
