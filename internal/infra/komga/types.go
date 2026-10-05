package komga

// Library contains server-side Komga details, including its container path.
// HTTP handlers must project this type before responding to a browser.
type Library struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Root                  string `json:"root"`
	ImportComicInfoBook   bool   `json:"importComicInfoBook"`
	ImportComicInfoSeries bool   `json:"importComicInfoSeries"`
	ImportBarcodeIsbn     bool   `json:"importBarcodeIsbn"`
	Unavailable           bool   `json:"unavailable"`
}

// Book includes Komga's file URL and is a server-side protocol DTO. Project
// only approved fields into browser or CLI responses.
type Book struct {
	ID               string       `json:"id"`
	LibraryID        string       `json:"libraryId"`
	SeriesID         string       `json:"seriesId"`
	SeriesTitle      string       `json:"seriesTitle"`
	Name             string       `json:"name"`
	URL              string       `json:"url"`
	FileHash         string       `json:"fileHash"`
	FileLastModified string       `json:"fileLastModified"`
	LastModified     string       `json:"lastModified"`
	SizeBytes        int64        `json:"sizeBytes"`
	Media            Media        `json:"media"`
	Metadata         BookMetadata `json:"metadata"`
}

type Media struct {
	Status       string `json:"status"`
	MediaType    string `json:"mediaType"`
	MediaProfile string `json:"mediaProfile"`
	PagesCount   int    `json:"pagesCount"`
}

type Author struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type WebLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type BookMetadata struct {
	Title           string    `json:"title"`
	TitleLock       bool      `json:"titleLock"`
	Summary         string    `json:"summary"`
	SummaryLock     bool      `json:"summaryLock"`
	Number          string    `json:"number"`
	NumberLock      bool      `json:"numberLock"`
	NumberSort      float64   `json:"numberSort"`
	NumberSortLock  bool      `json:"numberSortLock"`
	ReleaseDate     *string   `json:"releaseDate"`
	ReleaseDateLock bool      `json:"releaseDateLock"`
	Authors         []Author  `json:"authors"`
	AuthorsLock     bool      `json:"authorsLock"`
	Tags            []string  `json:"tags"`
	TagsLock        bool      `json:"tagsLock"`
	ISBN            string    `json:"isbn"`
	ISBNLock        bool      `json:"isbnLock"`
	Links           []WebLink `json:"links"`
	LinksLock       bool      `json:"linksLock"`
	LastModified    string    `json:"lastModified"`
}

type ListBooksOptions struct {
	LibraryID string
	Query     string
	Page      int // zero-based
	Size      int // 1..100
}

type BookPage struct {
	Content       []Book `json:"content"`
	Number        int    `json:"number"`
	Size          int    `json:"size"`
	TotalPages    int    `json:"totalPages"`
	TotalElements int64  `json:"totalElements"`
}

type ClearField string

const (
	ClearSummary     ClearField = "summary"
	ClearReleaseDate ClearField = "releaseDate"
	ClearAuthors     ClearField = "authors"
	ClearTags        ClearField = "tags"
	ClearISBN        ClearField = "isbn"
	ClearLinks       ClearField = "links"
)
