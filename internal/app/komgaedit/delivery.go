package komgaedit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/ryancheng/telegram-downloader/internal/archive/cbzedit"
	"github.com/ryancheng/telegram-downloader/internal/comicinfo"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

type deliveryGateway interface {
	catalogGateway
	ScanLibrary(context.Context, string) error
	AnalyzeBook(context.Context, string) error
}

type DeliveryService struct {
	catalog *CatalogService
	root    string
}

func NewDeliveryService(catalog *CatalogService, root string) *DeliveryService {
	return &DeliveryService{catalog: catalog, root: root}
}

// Deliver resumes by inspecting exact destination contents, so retrying or
// restarting the API never generates another task or overwrites another book.
func (s *DeliveryService) Deliver(ctx context.Context, source, fileName, series string) (tasks.KomgaDeliveryResult, error) {
	result := tasks.KomgaDeliveryResult{Status: "pending", Indexed: "pending"}
	if s == nil || s.catalog == nil || !strings.EqualFold(filepath.Ext(fileName), ".cbz") {
		return result, ErrUnsafe
	}
	copier := tasks.NewKomgaCopier(tasks.KomgaCopyConfig{Root: s.root})
	target, err := copier.TargetPath(fileName, series)
	if err != nil {
		return result, ErrUnsafe
	}
	mapping, relative, ok := s.targetMapping(target)
	if !ok {
		return result, ErrUnsafe
	}
	// All upstream work is bounded below the ordinary HTTP write deadline.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client, err := s.catalog.connect(ctx)
	if err != nil {
		return result, err
	}
	gateway, ok := client.(deliveryGateway)
	if !ok {
		return result, ErrDisabled
	}
	library, err := gateway.Library(ctx, mapping.LibraryID)
	if err != nil {
		return result, err
	}
	if library.Root != mapping.KomgaRoot || library.Unavailable || !library.ImportComicInfoBook {
		return result, ErrUnsafe
	}
	sourceFile, err := os.Open(source)
	if err != nil {
		return result, err
	}
	hash := sha256.New()
	_, hashErr := io.Copy(hash, sourceFile)
	_ = sourceFile.Close()
	if hashErr != nil {
		return result, hashErr
	}
	expectedSHA := hex.EncodeToString(hash.Sum(nil))
	if _, err := copier.CopyFromPath(source, fileName, series); err != nil {
		return result, err
	}
	result.TargetPath, result.Copied, result.LibraryID = target, true, mapping.LibraryID
	expectedPath := path.Join(mapping.KomgaRoot, filepath.ToSlash(relative))
	// First read allows a completed retry to return without scheduling another
	// scan. Presence alone, filename matching and READY alone are insufficient.
	if book, found := s.findDeliveredBook(ctx, gateway, mapping.LibraryID, expectedPath, fileName); found {
		if s.deliveryVisible(ctx, gateway, book.ID, library, target, expectedSHA) {
			result.Status, result.Indexed, result.BookID = "indexed", "verified", book.ID
			return result, nil
		}
	}
	if err := gateway.ScanLibrary(ctx, mapping.LibraryID); err != nil {
		result.Reason = "scan_failed"
		return result, nil
	}
	// One short readback window. Larger libraries can continue from this exact
	// stage via the same copy action, without ever claiming accepted == indexed.
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(400 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				result.Reason = "readback_pending"
				return result, nil
			case <-timer.C:
			}
		}
		book, found := s.findDeliveredBook(ctx, gateway, mapping.LibraryID, expectedPath, fileName)
		if !found {
			continue
		}
		if attempt == 0 {
			_ = gateway.AnalyzeBook(ctx, book.ID)
		}
		if s.deliveryVisible(ctx, gateway, book.ID, library, target, expectedSHA) {
			result.Status, result.Indexed, result.BookID = "indexed", "verified", book.ID
			return result, nil
		}
	}
	result.Reason = "readback_pending"
	return result, nil
}

func (s *DeliveryService) targetMapping(target string) (config.KomgaLibraryMapping, string, bool) {
	var found config.KomgaLibraryMapping
	var relative string
	for _, mapping := range s.catalog.mappings {
		rel, err := filepath.Rel(mapping.LocalRoot, target)
		if err == nil && filepath.IsLocal(rel) && rel != "." {
			if found.LibraryID != "" {
				return found, "", false
			}
			found, relative = mapping, rel
		}
	}
	return found, relative, found.LibraryID != ""
}

func (s *DeliveryService) findDeliveredBook(ctx context.Context, client deliveryGateway, libraryID, expected, fileName string) (komga.Book, bool) {
	// Query narrows normal libraries; bounded unfiltered fallback handles titles
	// imported from ComicInfo that differ from the archive name.
	for _, query := range []string{strings.TrimSuffix(fileName, filepath.Ext(fileName)), ""} {
		for page := 0; page < 10; page++ {
			items, err := client.Books(ctx, komga.ListBooksOptions{LibraryID: libraryID, Query: query, Page: page, Size: 100})
			if err != nil {
				return komga.Book{}, false
			}
			for _, book := range items.Content {
				actual, err := komgaArchivePath(book.URL)
				if err == nil && actual == expected && book.LibraryID == libraryID {
					return book, true
				}
			}
			if page+1 >= items.TotalPages {
				break
			}
		}
	}
	return komga.Book{}, false
}

func (s *DeliveryService) deliveryVisible(ctx context.Context, client deliveryGateway, id string, library komga.Library, expectedTarget, expectedSHA string) bool {
	book, err := client.Book(ctx, id)
	if err != nil || book.ID == "" || book.LibraryID != library.ID || book.Media.Status != "READY" {
		return false
	}
	root, relative, err := s.catalog.resolveFile(book, library)
	if err != nil {
		return false
	}
	defer root.Close()
	mapping := s.catalog.mappings[library.ID]
	if filepath.Join(mapping.LocalRoot, relative) != expectedTarget {
		return false
	}
	inspection, err := cbzedit.Inspect(ctx, root, relative, cbzedit.Limits{})
	if err != nil || !inspection.HasComicInfo || inspection.SHA256 != expectedSHA || inspection.Size != book.SizeBytes || inspection.PageCount != book.Media.PagesCount {
		return false
	}
	parsed, err := comicinfo.Parse(inspection.ComicInfo)
	if err != nil {
		return false
	}
	changes := []FieldChange{}
	for element, key := range map[string]string{"Title": "title", "Number": "number", "Summary": "summary", "Tags": "tags", "Web": "web", "GTIN": "identifiers", "Year": "publication_date"} {
		if parsed.Values[element] != "" {
			changes = append(changes, FieldChange{Key: key})
		}
	}
	for element := range authorElements {
		if parsed.Values[element] != "" {
			changes = append(changes, FieldChange{Key: "creators.writer"})
			break
		}
	}
	// Require at least one concrete metadata value in addition to path and media
	// identity; an empty catalog record cannot prove metadata import.
	return len(changes) > 0 && projectionMatches(book, inspection.ComicInfo, changes)
}
