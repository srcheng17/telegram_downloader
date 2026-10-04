package komgaedit

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/archive/cbzedit"
	"github.com/ryancheng/telegram-downloader/internal/comicinfo"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

const previewLifetime = 10 * time.Minute

type EditService struct {
	catalog     *CatalogService
	operations  OperationRepository
	definitions RegistryProvider
	backupRoot  string
	tokenKey    [32]byte
	editFactory func(string, credentials.Secret) (editGateway, error)
}

func NewEditService(catalog *CatalogService, operations OperationRepository, definitions RegistryProvider, backupRoot string) (*EditService, error) {
	if catalog == nil || operations == nil || definitions == nil || backupRoot == "" {
		return nil, ErrInvalid
	}
	if err := rejectOverlappingMediaRoots(catalog); err != nil {
		return nil, err
	}
	if err := ensurePrivateBackupRoot(backupRoot); err != nil {
		return nil, err
	}
	service := &EditService{catalog: catalog, operations: operations, definitions: definitions, backupRoot: backupRoot,
		editFactory: func(base string, key credentials.Secret) (editGateway, error) { return komga.NewClient(base, key) }}
	if _, err := rand.Read(service.tokenKey[:]); err != nil {
		return nil, fmt.Errorf("initialize Komga edit preview tokens: %w", err)
	}
	return service, nil
}

type editableBook struct {
	book     komga.Book
	library  komga.Library
	view     BookView
	root     *os.Root
	relative string
}

func (s *EditService) loadBook(ctx context.Context, id string) (editableBook, error) {
	var result editableBook
	client, err := s.catalog.connect(ctx)
	if err != nil {
		return result, err
	}
	result.book, err = client.Book(ctx, id)
	if err != nil {
		return result, err
	}
	if !s.catalog.allowed[result.book.LibraryID] {
		return result, ErrNotFound
	}
	result.library, err = client.Library(ctx, result.book.LibraryID)
	if err != nil {
		return result, err
	}
	result.view = s.catalog.project(result.book, &result.library)
	result.root, result.relative, err = s.catalog.resolveFile(result.book, result.library)
	if err != nil && result.view.Editable {
		result.view.Editable = false
		result.view.ReadOnly = "file_unavailable"
	}
	return result, nil
}

func (s *EditService) Detail(ctx context.Context, id string) (EditDetail, error) {
	book, err := s.loadBook(ctx, id)
	if err != nil {
		return EditDetail{}, err
	}
	if book.root != nil {
		defer book.root.Close()
	}
	registry, err := s.definitions.Schema(ctx)
	if err != nil {
		return EditDetail{}, err
	}
	detail := EditDetail{Book: book.view, Schema: registry, Document: metadata.EmptyDocument(registry), Warnings: []metadata.Warning{}}
	editable := book.view.Editable && book.book.Media.Status == "READY"
	if !book.view.Editable {
		detail.BlockReason = book.view.ReadOnly
	} else if !editable {
		detail.BlockReason = "media_not_ready"
	}
	if book.root != nil {
		inspection, err := cbzedit.Inspect(ctx, book.root, book.relative, cbzedit.Limits{})
		if err != nil {
			detail.BlockReason = "unsafe_archive"
			editable = false
		} else {
			detail.SourceVersion = inspection.SHA256
			detail.HasComicInfo = inspection.HasComicInfo
			detail.PageCount = inspection.PageCount
			detail.Document, detail.Warnings, err = documentFromArchive(inspection.ComicInfo, inspection.PageCount, registry)
			if err != nil {
				detail.BlockReason = "unsafe_comicinfo"
				editable = false
			} else if pageCountMismatch(inspection) {
				detail.PageCountCorrectionNeeded = true
				detail.Warnings = append(detail.Warnings, metadata.Warning{Key: "page_count", Code: "page_count_mismatch", Message: "原 ComicInfo 页数与实际图片数不符；保存前须明确确认修正。"})
			}
		}
	}
	if editable && book.root != nil {
		if backup, err := openPrivateBackupRoot(s.backupRoot); err != nil {
			editable = false
			detail.BlockReason = "backup_unavailable"
		} else {
			_ = backup.Close()
		}
	}
	detail.CanSave = editable
	detail.Fields = editFields(registry, book.book, book.library, editable)
	return detail, nil
}

type previewState struct {
	book       editableBook
	inspection cbzedit.Inspection
	registry   metadata.Registry
	compiled   compiledEdit
}

func (s *EditService) compilePreview(ctx context.Context, id string, input PreviewRequest) (previewState, error) {
	var state previewState
	if !validSHA(input.SourceVersion) || len(input.Changes) > 32 || input.DefinitionsVersion == "" {
		return state, ErrInvalid
	}
	book, err := s.loadBook(ctx, id)
	if err != nil {
		return state, err
	}
	state.book = book
	if !book.view.Editable || book.root == nil || book.book.Media.Status != "READY" {
		return state, ErrUnsafe
	}
	if backup, err := openPrivateBackupRoot(s.backupRoot); err != nil {
		return state, ErrUnsafe
	} else {
		_ = backup.Close()
	}
	registry, err := s.definitions.Schema(ctx)
	if err != nil {
		return state, err
	}
	if registry.DefinitionsVersion != input.DefinitionsVersion {
		return state, ErrConflict
	}
	state.registry = registry
	inspection, err := cbzedit.Inspect(ctx, book.root, book.relative, cbzedit.Limits{})
	if err != nil {
		return state, fmt.Errorf("inspect existing CBZ: %w", err)
	}
	if inspection.SHA256 != input.SourceVersion {
		return state, ErrConflict
	}
	if pageCountMismatch(inspection) && !input.CorrectPageCount {
		return state, ErrUnsafe
	}
	state.inspection = inspection
	baseline, warnings, err := documentFromArchive(inspection.ComicInfo, inspection.PageCount, registry)
	if err != nil {
		return state, ErrUnsafe
	}
	state.compiled, err = compileEdit(inspection, baseline, registry, book.library, book.book, input.Changes, input.CorrectPageCount)
	if err != nil {
		return state, err
	}
	state.compiled.warnings = append(warnings, state.compiled.warnings...)
	return state, nil
}

func pageCountMismatch(inspection cbzedit.Inspection) bool {
	if !inspection.HasComicInfo {
		return false
	}
	parsed, err := comicinfo.Parse(inspection.ComicInfo)
	if err != nil {
		return false
	}
	old, exists := parsed.Values["PageCount"]
	if !exists {
		return false
	}
	count, err := strconv.Atoi(old)
	return err == nil && count != inspection.PageCount
}

func (s *EditService) Preview(ctx context.Context, id string, input PreviewRequest) (PreviewResult, error) {
	state, err := s.compilePreview(ctx, id, input)
	if state.book.root != nil {
		defer state.book.root.Close()
	}
	if err != nil {
		return PreviewResult{}, err
	}
	expires := time.Now().Add(previewLifetime).UTC().Truncate(time.Second)
	token, err := s.makePreviewToken(id, input, state.book.book, state.book.library, expires)
	if err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{PreviewToken: token, ExpiresAt: expires, FileChanged: state.compiled.fileChanged,
		Diffs: state.compiled.diffs, Warnings: state.compiled.warnings, CanSave: true}, nil
}

func (s *EditService) Save(ctx context.Context, id string, input SaveRequest) (OperationView, error) {
	if !validIdempotencyKey(input.IdempotencyKey) {
		return OperationView{}, ErrInvalid
	}
	digest, err := digestRequest(id, input.PreviewRequest)
	if err != nil {
		return OperationView{}, ErrInvalid
	}
	if existing, err := s.operations.GetByKey(ctx, input.IdempotencyKey); err == nil {
		if existing.BookID != id || existing.RequestDigest != digest {
			return OperationView{}, ErrConflict
		}
		return s.Operation(ctx, existing.ID)
	} else if !errors.Is(err, ErrNotFound) {
		return OperationView{}, err
	}
	state, err := s.compilePreview(ctx, id, input.PreviewRequest)
	if state.book.root != nil {
		defer state.book.root.Close()
	}
	if err != nil {
		return OperationView{}, err
	}
	if err := s.verifyPreviewToken(id, input.PreviewRequest, state.book.book, state.book.library, input.PreviewToken); err != nil {
		return OperationView{}, err
	}
	lockKey := state.book.book.LibraryID + "/" + state.book.relative
	var result OperationView
	err = s.operations.WithBookLock(ctx, lockKey, func(locked context.Context) error {
		if existing, err := s.operations.GetByKey(locked, input.IdempotencyKey); err == nil {
			if existing.BookID != id || existing.RequestDigest != digest {
				return ErrConflict
			}
			result, err = s.reconcileLocked(locked, existing)
			return err
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		latest, err := s.loadBook(locked, id)
		if err != nil {
			return err
		}
		if latest.root != nil {
			defer latest.root.Close()
		}
		if !latest.view.Editable || latest.root == nil || latest.book.Media.Status != "READY" || latest.book.LibraryID != state.book.book.LibraryID || latest.relative != state.book.relative || latest.book.URL != state.book.book.URL || bookRevision(latest.book, latest.library) != bookRevision(state.book.book, state.book.library) {
			return ErrConflict
		}
		for _, change := range input.Changes {
			if lockedField(change.Key, latest.book) || change.Key == "identifiers" && change.State == "cleared" && latest.library.ImportBarcodeIsbn {
				return ErrConflict
			}
		}
		fresh, err := cbzedit.Inspect(locked, latest.root, latest.relative, cbzedit.Limits{})
		if err != nil || fresh.SHA256 != state.inspection.SHA256 {
			return ErrConflict
		}
		operationID, err := newOperationID()
		if err != nil {
			return err
		}
		op := Operation{ID: operationID, IdempotencyKey: input.IdempotencyKey, RequestDigest: digest,
			BookID: id, LibraryID: state.book.book.LibraryID, RelativePath: state.book.relative,
			SourceSHA256: fresh.SHA256, TargetSHA256: fresh.SHA256,
			DesiredFields: input.Changes, State: StatePreparing}
		if !state.compiled.fileChanged {
			op.FileNoChange = true
			if projectionMatches(latest.book, fresh.ComicInfo, input.Changes) {
				op.State = StateCurrentValueConsistent
				op.ProjectionConsistent = true
			} else {
				op.State = StateSyncPending
			}
		}
		op, err = s.operations.Create(locked, op)
		if err != nil {
			return err
		}
		if state.compiled.fileChanged {
			backup, err := openPrivateBackupRoot(s.backupRoot)
			if err != nil {
				return ErrUnsafe
			}
			prepared, prepareErr := cbzedit.Prepare(locked, latest.root, backup, cbzedit.PrepareRequest{
				RelativePath: state.book.relative, OperationID: op.ID, ExpectedSHA256: op.SourceSHA256,
				NewComicInfo: state.compiled.newXML, ChangedElements: state.compiled.changedElements,
				AllowPageCountCorrection: state.compiled.correctPageCount})
			_ = backup.Close()
			if prepareErr != nil {
				op.LastErrorCode = errorCode(prepareErr)
				op.State = StateRestoreNeeded
				if updated, persistErr := s.persist(locked, op); persistErr == nil {
					op = updated
				}
				return prepareErr
			}
			op.Prepared = &prepared
			op.TargetSHA256 = prepared.NewSHA256
			op.BackupRef = prepared.BackupPath
			op.State = StatePrepared
			op, err = s.persist(locked, op)
			if err != nil {
				_ = cbzedit.Abort(latest.root, prepared)
				return err
			}
			commit, commitErr := s.commitPrepared(locked, latest.root, op)
			if commit.FileCommitted {
				op.FileCommitted = true
				op.State = StateFileCommitted
			}
			if commitErr != nil {
				op.LastErrorCode = errorCode(commitErr)
				if !commit.FileCommitted {
					op.State = StateRestoreNeeded
				}
			}
			op, err = s.persist(locked, op)
			if err != nil {
				return err
			}
			if commitErr != nil {
				result = operationView(op)
				return nil
			}
		}
		if op.ProjectionConsistent {
			result = operationView(op)
			return nil
		}
		op, err = s.syncLocked(locked, op)
		if err != nil {
			return err
		}
		result = operationView(op)
		return nil
	})
	return result, err
}

func (s *EditService) makePreviewToken(id string, input PreviewRequest, book komga.Book, library komga.Library, expires time.Time) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	timestamp := strconv.FormatInt(expires.Unix(), 10)
	revision := bookRevision(book, library)
	mac := hmac.New(sha256.New, s.tokenKey[:])
	_, _ = mac.Write([]byte(id + "\n" + timestamp + "\n" + revision + "\n"))
	_, _ = mac.Write(encoded)
	return timestamp + "." + revision + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func (s *EditService) verifyPreviewToken(id string, input PreviewRequest, book komga.Book, library komga.Library, token string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(parts[1]) != 64 || len(parts[2]) != 64 || parts[1] != bookRevision(book, library) {
		return ErrConflict
	}
	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > seconds || seconds > time.Now().Add(previewLifetime).Unix()+1 {
		return ErrConflict
	}
	expected, err := s.makePreviewToken(id, input, book, library, time.Unix(seconds, 0))
	if err != nil || !hmac.Equal([]byte(expected), []byte(token)) {
		return ErrConflict
	}
	return nil
}

func bookRevision(book komga.Book, library komga.Library) string {
	encoded, _ := json.Marshal(struct {
		BookID        string
		LibraryID     string
		URL           string
		FileHash      string
		FileModified  string
		LastModified  string
		Media         komga.Media
		Metadata      komga.BookMetadata
		ImportBook    bool
		ImportBarcode bool
	}{book.ID, book.LibraryID, book.URL, book.FileHash, book.FileLastModified, book.LastModified,
		book.Media, book.Metadata, library.ImportComicInfoBook, library.ImportBarcodeIsbn})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func digestRequest(id string, input PreviewRequest) (string, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte(id+"\n"), data...))
	return hex.EncodeToString(sum[:]), nil
}

func validSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}

func validIdempotencyKey(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r < 'a' || r > 'z' {
			if r < 'A' || r > 'Z' {
				if r < '0' || r > '9' {
					if r != '-' && r != '_' {
						return false
					}
				}
			}
		}
	}
	return true
}

func newOperationID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func openPrivateBackupRoot(name string) (*os.Root, error) {
	info, err := os.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, ErrUnsafe
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, ErrUnsafe
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, ErrUnsafe
	}
	return root, nil
}

func ensurePrivateBackupRoot(name string) error {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name || name == string(filepath.Separator) {
		return ErrInvalid
	}
	parts := strings.Split(strings.TrimPrefix(name, string(filepath.Separator)), string(filepath.Separator))
	current := string(filepath.Separator)
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return ErrInvalid
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("create private Komga edit backup directory: %w", err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafe
		}
		// Existing ancestors may be traversable, but must not be writable by
		// another user. The backup root itself must be fully private.
		if i == len(parts)-1 && info.Mode().Perm()&0077 != 0 || i < len(parts)-1 && info.Mode().Perm()&0022 != 0 {
			return ErrUnsafe
		}
	}
	root, err := openPrivateBackupRoot(name)
	if err != nil {
		return err
	}
	return root.Close()
}

func rejectOverlappingMediaRoots(catalog *CatalogService) error {
	roots := make([]string, 0, len(catalog.mappings))
	for _, mapping := range catalog.mappings {
		root := filepath.Clean(mapping.LocalRoot)
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		for _, previous := range roots {
			if root == previous || strings.HasPrefix(root, previous+string(filepath.Separator)) || strings.HasPrefix(previous, root+string(filepath.Separator)) {
				return ErrUnsafe
			}
		}
		roots = append(roots, root)
	}
	return nil
}

func operationView(op Operation) OperationView {
	view := OperationView{ID: op.ID, BookID: op.BookID, State: op.State,
		FileCommitted: op.FileCommitted, FileNoChange: op.FileNoChange, FileRestored: op.FileRestored,
		ProjectionApplicable: projectionApplicable(op.DesiredFields),
		ProjectionConsistent: op.ProjectionConsistent, AnalyzeVerified: op.AnalyzeVerified,
		LastErrorCode: op.LastErrorCode, AvailableActions: []string{}}
	if (op.FileCommitted || op.FileNoChange) && !op.ProjectionConsistent && op.State != StateRestoreNeeded && op.State != StateRestored {
		view.AvailableActions = append(view.AvailableActions, "retry_sync")
	}
	if op.FileCommitted && op.Prepared != nil && op.State != StateRestoreNeeded && op.State != StateRestored {
		view.AvailableActions = append(view.AvailableActions, "restore")
	}
	return view
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, cbzedit.ErrConflict), errors.Is(err, ErrConflict):
		return "source_changed"
	case errors.Is(err, cbzedit.ErrBackup):
		return "backup_unavailable"
	case errors.Is(err, cbzedit.ErrLimit):
		return "archive_limit"
	case errors.Is(err, cbzedit.ErrInvalidXML), errors.Is(err, cbzedit.ErrInvalidArchive):
		return "unsafe_archive"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		var upstream *komga.Error
		if errors.As(err, &upstream) {
			return upstream.Code
		}
		return "internal_error"
	}
}
