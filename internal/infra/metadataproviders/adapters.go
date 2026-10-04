package metadataproviders

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	search "github.com/ryancheng/telegram-downloader/internal/app/metadatasearch"
	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type bakaRecord struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Native    string `json:"native_title"`
	Romanized string `json:"romanized_title"`
	Titles    []struct {
		Title string `json:"title"`
	} `json:"titles"`
	Authors     []string `json:"authors"`
	Artists     []string `json:"artists"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Language    string   `json:"original_language"`
	Status      string   `json:"status"`
	Rating      string   `json:"content_rating"`
	Published   struct {
		Start     string `json:"start_date"`
		Estimated bool   `json:"start_date_is_estimated"`
	} `json:"published"`
	Tags []struct {
		Name  string `json:"name"`
		Genre bool   `json:"is_genre"`
	} `json:"tags_v2"`
}
type updatesRecord struct {
	ID          int64  `json:"series_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Associated  []struct {
		Title string `json:"title"`
	} `json:"associated"`
	Authors []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"authors"`
	Genres []struct {
		Genre string `json:"genre"`
	} `json:"genres"`
	Categories []struct {
		Category string `json:"category"`
	} `json:"categories"`
	Publishers []struct {
		Name string `json:"publisher_name"`
		Type string `json:"type"`
	} `json:"publishers"`
}
type bangumiRecord struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Chinese  string `json:"name_cn"`
	Summary  string `json:"summary"`
	Type     int    `json:"type"`
	Platform string `json:"platform"`
	Date     string `json:"date"`
	Series   *bool  `json:"series"`
	NSFW     *bool  `json:"nsfw"`
	Volumes  int64  `json:"volumes"`
	Tags     []struct {
		Name string `json:"name"`
	} `json:"tags"`
	Infobox []struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	} `json:"infobox"`
}
type bangumiRelation struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Relation string `json:"relation"`
	Type     int    `json:"type"`
}

func (c *Catalog) Search(ctx context.Context, config sourcesettings.PublicSourceConfig, secret credentials.Secret, keyword string) ([]search.Record, error) {
	return c.cached(ctx, config, secret, "search", keyword, func() ([]search.Record, error) {
		result := []search.Record{}
		switch config.ProviderID {
		case "mangabaka":
			var data struct {
				Data *[]bakaRecord `json:"data"`
			}
			if err := c.request(ctx, config, secret, http.MethodGet, "/v1/series/search?q="+url.QueryEscape(keyword), nil, &data); err != nil {
				return nil, err
			}
			if data.Data == nil {
				return nil, invalidResponse()
			}
			for _, record := range *data.Data {
				if len(result) >= search.MaxResults {
					break
				}
				result = append(result, mapBaka(record))
			}
		case "mangaupdates":
			var data struct {
				Results *[]struct {
					Record   updatesRecord `json:"record"`
					HitTitle string        `json:"hit_title"`
				} `json:"results"`
			}
			if err := c.request(ctx, config, secret, http.MethodPost, "/v1/series/search", map[string]any{"search": keyword, "perpage": search.MaxResults}, &data); err != nil {
				return nil, err
			}
			if data.Results == nil {
				return nil, invalidResponse()
			}
			for _, hit := range *data.Results {
				if len(result) >= search.MaxResults {
					break
				}
				record := mapUpdates(hit.Record)
				record.Aliases = unique(append(record.Aliases, hit.HitTitle))
				result = append(result, record)
			}
		case "bangumi":
			var data struct {
				Data *[]bangumiRecord `json:"data"`
			}
			if err := c.request(ctx, config, secret, http.MethodPost, "/v0/search/subjects?limit=20", map[string]any{"keyword": keyword, "filter": map[string]any{"type": []int{1}}}, &data); err != nil {
				return nil, err
			}
			if data.Data == nil {
				return nil, invalidResponse()
			}
			for _, record := range *data.Data {
				if record.Type != 1 {
					continue
				}
				if len(result) >= search.MaxResults {
					break
				}
				result = append(result, mapBangumi(record))
			}
		default:
			return nil, &search.Error{Code: "unavailable"}
		}
		for _, r := range result {
			if !validRecord(r) {
				return nil, invalidResponse()
			}
		}
		return result, nil
	})
}
func (c *Catalog) Resolve(ctx context.Context, config sourcesettings.PublicSourceConfig, secret credentials.Secret, id string) (search.Record, error) {
	if !search.ValidRecordID(id) {
		return search.Record{}, invalidResponse()
	}
	rows, err := c.cached(ctx, config, secret, "resolve", id, func() ([]search.Record, error) {
		var record search.Record
		switch config.ProviderID {
		case "mangabaka":
			var data struct {
				Data bakaRecord `json:"data"`
			}
			if err := c.request(ctx, config, secret, http.MethodGet, "/v1/series/"+id, nil, &data); err != nil {
				return nil, err
			}
			record = mapBaka(data.Data)
		case "mangaupdates":
			var data updatesRecord
			if err := c.request(ctx, config, secret, http.MethodGet, "/v1/series/"+id, nil, &data); err != nil {
				return nil, err
			}
			record = mapUpdates(data)
		case "bangumi":
			var data bangumiRecord
			if err := c.request(ctx, config, secret, http.MethodGet, "/v0/subjects/"+id, nil, &data); err != nil {
				return nil, err
			}
			if data.Type != 1 {
				return nil, invalidResponse()
			}
			record = mapBangumi(data)
			var persons []bangumiRelation
			if err := c.request(ctx, config, secret, http.MethodGet, "/v0/subjects/"+id+"/persons", nil, &persons); err != nil {
				return nil, err
			}
			for _, p := range persons {
				key := ""
				switch p.Relation {
				case "作者", "原作", "脚本":
					key = "writer"
				case "作画":
					key = "penciller"
				case "翻译":
					key = "translator"
				}
				if key != "" {
					record.Creators[key] = append(record.Creators[key], p.Name)
				}
			}
			for key, names := range record.Creators {
				record.Creators[key] = unique(names)
				putList(&record, "creators."+key, "persons[relation]", names)
			}
			var relations []bangumiRelation
			if err := c.request(ctx, config, secret, http.MethodGet, "/v0/subjects/"+id+"/subjects", nil, &relations); err != nil {
				return nil, err
			}
			parents := []string{}
			for _, relation := range relations {
				if relation.Type == 1 && relation.Relation == "系列" {
					parents = append(parents, relation.Name)
				}
			}
			if len(parents) == 1 && (data.Series == nil || !*data.Series) {
				putText(&record, "series", "subjects[relation=系列].name", parents[0])
				record.Relationship = "volume_in_series"
			}
		default:
			return nil, &search.Error{Code: "unavailable"}
		}
		if record.ID != id || !validRecord(record) {
			return nil, invalidResponse()
		}
		return []search.Record{record}, nil
	})
	if err != nil {
		return search.Record{}, err
	}
	return rows[0], nil
}
func invalidResponse() error { return &search.Error{Code: "invalid_response"} }
func validRecord(r search.Record) bool {
	return search.ValidRecordID(r.ID) && strings.TrimSpace(r.Title) != "" && len(r.Title) <= metadata.MaxStringBytes
}
func newRecord(provider string, id int64, title, format string) search.Record {
	idText := strconv.FormatInt(id, 10)
	public := ""
	switch provider {
	case "mangabaka":
		public = "https://mangabaka.org/" + idText
	case "mangaupdates":
		public = "https://www.mangaupdates.com/series/" + strconv.FormatInt(id, 36)
	case "bangumi":
		public = "https://bgm.tv/subject/" + idText
	}
	r := search.Record{ID: idText, Title: strings.TrimSpace(title), Format: format, Relationship: "unknown", PublicURL: public, RetrievedAt: time.Now().UTC().Format(time.RFC3339), Fields: map[string]search.Field{}, Extra: map[string]search.Field{}, Creators: map[string][]string{}}
	putText(&r, "title", "title", r.Title)
	putText(&r, "format", "type", format)
	putText(&r, "web", "record_url", public)
	put(&r, "identifiers", "id", []metadata.Identifier{{Scheme: provider, Value: idText}})
	extra(&r, "source.type", "type", format)
	return r
}
func mapBaka(v bakaRecord) search.Record {
	r := newRecord("mangabaka", v.ID, v.Title, v.Type)
	putText(&r, "summary", "description", v.Description)
	putText(&r, "language", "original_language", v.Language)
	r.Aliases = []string{v.Native, v.Romanized}
	for _, t := range v.Titles {
		r.Aliases = append(r.Aliases, t.Title)
	}
	r.Aliases = unique(r.Aliases)
	putList(&r, "aliases", "titles[].title", r.Aliases)
	r.Creators["writer"] = unique(v.Authors)
	r.Creators["penciller"] = unique(v.Artists)
	putList(&r, "creators.writer", "authors", v.Authors)
	putList(&r, "creators.penciller", "artists", v.Artists)
	tags, genres := []string{}, []string{}
	for _, tag := range v.Tags {
		if tag.Genre {
			genres = append(genres, tag.Name)
		} else {
			tags = append(tags, tag.Name)
		}
	}
	putList(&r, "tags", "tags_v2[is_genre=false].name", tags)
	putList(&r, "genres", "tags_v2[is_genre=true].name", genres)
	extra(&r, "source.status", "status", v.Status)
	extra(&r, "source.content_rating", "content_rating", v.Rating)
	if !v.Published.Estimated {
		putDate(&r, "published.start_date", v.Published.Start)
	}
	// source.* and final_volume are deliberately not adopted. Third-party source
	// payloads have their own terms, and a final volume is not a current number.
	return r
}
func mapUpdates(v updatesRecord) search.Record {
	r := newRecord("mangaupdates", v.ID, v.Title, v.Type)
	putText(&r, "summary", "description", v.Description)
	for _, a := range v.Associated {
		r.Aliases = append(r.Aliases, a.Title)
	}
	r.Aliases = unique(r.Aliases)
	putList(&r, "aliases", "associated[].title", r.Aliases)
	for _, a := range v.Authors {
		key := ""
		switch a.Type {
		case "Author":
			key = "writer"
		case "Artist":
			key = "penciller"
		}
		if key != "" {
			r.Creators[key] = append(r.Creators[key], a.Name)
		}
	}
	for key, names := range r.Creators {
		r.Creators[key] = unique(names)
		putList(&r, "creators."+key, "authors[type].name", names)
	}
	tags, genres := []string{}, []string{}
	for _, g := range v.Genres {
		genres = append(genres, g.Genre)
	}
	for _, g := range v.Categories {
		tags = append(tags, g.Category)
	}
	putList(&r, "genres", "genres[].genre", genres)
	putList(&r, "tags", "categories[].category", tags)
	publishers := []string{}
	for _, p := range v.Publishers {
		if p.Type == "Original" {
			publishers = append(publishers, p.Name)
		}
	}
	publishers = unique(publishers)
	if len(publishers) == 1 {
		putText(&r, "publisher", "publishers[type=Original].publisher_name", publishers[0])
	}
	return r
}
func mapBangumi(v bangumiRecord) search.Record {
	title := v.Chinese
	titlePath := "name_cn"
	if strings.TrimSpace(title) == "" {
		title = v.Name
		titlePath = "name"
	}
	r := newRecord("bangumi", v.ID, title, v.Platform)
	putText(&r, "title", titlePath, title)
	putText(&r, "format", "platform", v.Platform)
	extra(&r, "source.type", "platform", v.Platform)
	r.Aliases = unique([]string{v.Name, v.Chinese})
	putList(&r, "aliases", "name/name_cn", r.Aliases)
	putText(&r, "summary", "summary", v.Summary)
	putDate(&r, "date", v.Date)
	if v.Series != nil {
		extra(&r, "source.series", "series", *v.Series)
		if *v.Series {
			r.Relationship = "series"
			putText(&r, "series", "name", v.Name)
			if v.Volumes > 0 {
				put(&r, "count", "volumes", v.Volumes)
			}
		} else {
			r.Relationship = "volume"
		}
	}
	if v.NSFW != nil {
		extra(&r, "source.nsfw", "nsfw", *v.NSFW)
	}
	tags := []string{}
	for _, tag := range v.Tags {
		tags = append(tags, tag.Name)
	}
	putList(&r, "tags", "tags[].name", tags)
	ids := []metadata.Identifier{{Scheme: "bangumi", Value: r.ID}}
	for _, box := range v.Infobox {
		var value string
		if json.Unmarshal(box.Value, &value) != nil {
			continue
		}
		switch box.Key {
		case "出版社":
			putText(&r, "publisher", "infobox.出版社", value)
		case "作者":
			r.Creators["writer"] = unique([]string{value})
			putList(&r, "creators.writer", "infobox.作者", r.Creators["writer"])
		case "ISBN", "ISBN-13", "ISBN-10":
			if strings.TrimSpace(value) != "" {
				ids = append(ids, metadata.Identifier{Scheme: "isbn", Value: strings.TrimSpace(value)})
			}
		}
	}
	put(&r, "identifiers", "id/infobox.ISBN", ids)
	return r
}
func unique(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] && len(v) <= metadata.MaxListItemBytes {
			seen[v] = true
			result = append(result, v)
			if len(result) == metadata.MaxListItems {
				break
			}
		}
	}
	return result
}
func put(r *search.Record, key, path string, value any) {
	raw, _ := json.Marshal(value)
	r.Fields[key] = search.Field{Value: raw, Path: path}
}
func putText(r *search.Record, key, path, value string) {
	if strings.TrimSpace(value) != "" {
		put(r, key, path, strings.TrimSpace(value))
	}
}
func putList(r *search.Record, key, path string, values []string) {
	values = unique(values)
	if len(values) > 0 {
		put(r, key, path, values)
	}
}
func extra(r *search.Record, key, path string, value any) {
	if s, ok := value.(string); ok && s == "" {
		return
	}
	raw, _ := json.Marshal(value)
	r.Extra[key] = search.Field{Value: raw, Path: path}
}
func putDate(r *search.Record, path, value string) {
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		month, day := int(parsed.Month()), parsed.Day()
		put(r, "publication_date", path, metadata.PublicationDate{Year: parsed.Year(), Month: &month, Day: &day})
	}
}
