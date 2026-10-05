package komgaedit

import (
	"context"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

type catalogGateway interface {
	Libraries(context.Context) ([]komga.Library, error)
	Library(context.Context, string) (komga.Library, error)
	Books(context.Context, komga.ListBooksOptions) (komga.BookPage, error)
	Book(context.Context, string) (komga.Book, error)
}

type gatewayFactory func(string, credentials.Secret) (catalogGateway, error)

type CatalogService struct {
	connection *ConnectionService
	mappings   map[string]config.KomgaLibraryMapping
	allowed    map[string]bool
	factory    gatewayFactory
}

func NewCatalogService(connection *ConnectionService, mappings []config.KomgaLibraryMapping, readOnlyLibraries []string) *CatalogService {
	indexed := make(map[string]config.KomgaLibraryMapping, len(mappings))
	allowed := make(map[string]bool, len(mappings)+len(readOnlyLibraries))
	for _, mapping := range mappings {
		indexed[mapping.LibraryID] = mapping
		allowed[mapping.LibraryID] = true
	}
	for _, id := range readOnlyLibraries {
		allowed[id] = true
	}
	return &CatalogService{connection: connection, mappings: indexed, allowed: allowed, factory: func(base string, key credentials.Secret) (catalogGateway, error) {
		return komga.NewClient(base, key)
	}}
}

type LibraryView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Unavailable bool   `json:"unavailable"`
	Writable    bool   `json:"writable"`
}

type BookView struct {
	ID          string             `json:"id"`
	LibraryID   string             `json:"library_id"`
	SeriesID    string             `json:"series_id"`
	SeriesTitle string             `json:"series_title"`
	Title       string             `json:"title"`
	FileName    string             `json:"file_name"`
	FileType    string             `json:"file_type"`
	MediaStatus string             `json:"media_status"`
	Editable    bool               `json:"editable"`
	ReadOnly    string             `json:"read_only_reason,omitempty"`
	Metadata    komga.BookMetadata `json:"metadata"`
}

type BooksView struct {
	Books         []BookView `json:"books"`
	Page          int        `json:"page"`
	Size          int        `json:"size"`
	TotalPages    int        `json:"total_pages"`
	TotalElements int64      `json:"total_elements"`
}

func (s *CatalogService) Libraries(ctx context.Context) ([]LibraryView, error) {
	client, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	libraries, err := client.Libraries(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]LibraryView, 0, len(libraries))
	for _, library := range libraries {
		if !s.allowed[library.ID] {
			continue
		}
		mapping, ok := s.mappings[library.ID]
		views = append(views, LibraryView{ID: library.ID, Name: library.Name, Unavailable: library.Unavailable, Writable: ok && mapping.KomgaRoot == library.Root && library.ImportComicInfoBook && !library.Unavailable})
	}
	return views, nil
}

func (s *CatalogService) Books(ctx context.Context, libraryID, query string, page, size int) (BooksView, error) {
	if page < 0 || page > 100000 || size < 1 || size > 50 || len(query) > 256 || !s.allowed[libraryID] {
		return BooksView{}, ErrInvalid
	}
	client, err := s.connect(ctx)
	if err != nil {
		return BooksView{}, err
	}
	result, err := client.Books(ctx, komga.ListBooksOptions{LibraryID: libraryID, Query: query, Page: page, Size: size})
	if err != nil {
		return BooksView{}, err
	}
	libraries, err := client.Libraries(ctx)
	if err != nil {
		return BooksView{}, err
	}
	byID := make(map[string]komga.Library, len(libraries))
	for _, library := range libraries {
		byID[library.ID] = library
	}
	views := make([]BookView, 0, len(result.Content))
	for _, book := range result.Content {
		library, exists := byID[book.LibraryID]
		view := s.project(book, &library)
		if !exists {
			view.Editable = false
			view.ReadOnly = "library_unavailable"
		} else if view.Editable {
			root, _, err := s.resolveFile(book, library)
			if err != nil {
				view.Editable = false
				view.ReadOnly = "file_unavailable"
			} else {
				_ = root.Close()
			}
		}
		views = append(views, view)
	}
	return BooksView{Books: views, Page: result.Number, Size: result.Size, TotalPages: result.TotalPages, TotalElements: result.TotalElements}, nil
}

func (s *CatalogService) Book(ctx context.Context, id string) (BookView, error) {
	client, err := s.connect(ctx)
	if err != nil {
		return BookView{}, err
	}
	book, err := client.Book(ctx, id)
	if err != nil {
		return BookView{}, err
	}
	if !s.allowed[book.LibraryID] {
		return BookView{}, ErrNotFound
	}
	library, err := client.Library(ctx, book.LibraryID)
	if err != nil {
		return BookView{}, err
	}
	view := s.project(book, &library)
	if view.Editable {
		root, _, resolveErr := s.resolveFile(book, library)
		if resolveErr == nil {
			_ = root.Close()
		}
		if resolveErr != nil {
			view.Editable = false
			view.ReadOnly = "file_unavailable"
		}
	}
	return view, nil
}

func (s *CatalogService) connect(ctx context.Context) (catalogGateway, error) {
	if s == nil || s.connection == nil || s.factory == nil {
		return nil, ErrDisabled
	}
	base, key, err := s.connection.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	return s.factory(base, key)
}

func (s *CatalogService) project(book komga.Book, library *komga.Library) BookView {
	name := ""
	if full, err := komgaArchivePath(book.URL); err == nil {
		name = path.Base(full)
	}
	fileType := strings.ToLower(path.Ext(name))
	view := BookView{ID: book.ID, LibraryID: book.LibraryID, SeriesID: book.SeriesID, SeriesTitle: book.SeriesTitle, Title: book.Name, FileName: name, FileType: fileType, MediaStatus: book.Media.Status, Metadata: book.Metadata}
	view.Metadata.Links = nil
	for _, link := range book.Metadata.Links {
		u, err := url.Parse(link.URL)
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil {
			view.Metadata.Links = append(view.Metadata.Links, link)
		}
	}
	if fileType != ".cbz" {
		view.ReadOnly = "unsupported_format"
		return view
	}
	mapping, ok := s.mappings[book.LibraryID]
	if !ok {
		view.ReadOnly = "unmapped_library"
		return view
	}
	if library != nil {
		if mapping.KomgaRoot != library.Root || library.Unavailable {
			view.ReadOnly = "library_unavailable"
			return view
		}
		if !library.ImportComicInfoBook {
			view.ReadOnly = "comicinfo_import_disabled"
			return view
		}
	}
	view.Editable = true
	return view
}

// resolveFile returns only server-side values. Callers must never serialize
// the root or relative path directly into an API response.
func (s *CatalogService) resolveFile(book komga.Book, library komga.Library) (*os.Root, string, error) {
	mapping, ok := s.mappings[library.ID]
	if !ok || mapping.KomgaRoot != library.Root || book.LibraryID != library.ID {
		return nil, "", ErrUnsafe
	}
	full, err := komgaArchivePath(book.URL)
	if err != nil {
		return nil, "", ErrUnsafe
	}
	base := mapping.KomgaRoot
	if full == base || !strings.HasPrefix(full, base+"/") {
		return nil, "", ErrUnsafe
	}
	relative := strings.TrimPrefix(full, base+"/")
	if strings.ToLower(path.Ext(relative)) != ".cbz" {
		return nil, "", ErrUnsafe
	}
	info, err := os.Lstat(mapping.LocalRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, "", ErrUnsafe
	}
	root, err := os.OpenRoot(mapping.LocalRoot)
	if err != nil {
		return nil, "", ErrUnsafe
	}
	segments := strings.Split(filepath.FromSlash(relative), string(filepath.Separator))
	for i := range segments {
		if segments[i] == "" || segments[i] == "." || segments[i] == ".." {
			root.Close()
			return nil, "", ErrUnsafe
		}
		partial := filepath.Join(segments[:i+1]...)
		entry, err := root.Lstat(partial)
		if err != nil || entry.Mode()&os.ModeSymlink != 0 || (i < len(segments)-1 && !entry.IsDir()) || (i == len(segments)-1 && !entry.Mode().IsRegular()) {
			root.Close()
			return nil, "", ErrUnsafe
		}
	}
	return root, filepath.Join(segments...), nil
}

// Komga 1.28.1 returns a raw absolute filesystem path in BookDto.url.
// Some installations return a file URI, so accept both without treating a
// raw path's percent characters as URI escapes.
func komgaArchivePath(raw string) (string, error) {
	var full string
	if strings.HasPrefix(raw, "/") {
		full = raw
	} else {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "file" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
			(u.Host != "" && !strings.EqualFold(u.Host, "localhost")) {
			return "", ErrUnsafe
		}
		full = u.Path
	}
	if full == "" || !path.IsAbs(full) || path.Clean(full) != full || strings.ContainsAny(full, "\\\x00\r\n") {
		return "", ErrUnsafe
	}
	return full, nil
}
