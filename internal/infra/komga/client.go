// Package komga contains the narrow HTTP protocol client for Komga 1.28.1.
// It does not decide which libraries are allowed, resolve file URLs, or write CBZs.
package komga

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ryancheng/telegram-downloader/internal/credentials"
)

const (
	maxResponseBytes = 4 << 20
	requestTimeout   = 15 * time.Second
)

// The application resolves encrypted settings per request, so Client values may
// be short lived. Reuse only the credential-free transport and its bounded pool.
var transport = &http.Transport{
	Proxy:                 nil,
	DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          20,
	MaxConnsPerHost:       4,
	IdleConnTimeout:       30 * time.Second,
	TLSHandshakeTimeout:   5 * time.Second,
	ResponseHeaderTimeout: 10 * time.Second,
}

// Error contains a safe machine code. It never includes the target URL,
// credentials, response body, book metadata, or an upstream error string.
type Error struct{ Code string }

func (e *Error) Error() string { return "komga: " + e.Code }
func fail(code string) error   { return &Error{Code: code} }

type Client struct {
	baseURL string
	apiKey  credentials.Secret
	http    *http.Client
}

// NewClient accepts an API key and a fixed instance URL. Plain HTTP is limited
// to loopback and Docker Desktop's host gateway; remote instances need HTTPS.
// Redirects are refused so credentials cannot be forwarded to a new target.
func NewClient(baseURL string, apiKey credentials.Secret) (*Client, error) {
	base, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	key := apiKey.Value()
	if len(key) == 0 || len(key) > 4096 || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return nil, fail("invalid_credential")
	}
	return &Client{
		baseURL: base,
		apiKey:  apiKey,
		http: &http.Client{
			Transport: transport,
			Timeout:   requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func normalizeBaseURL(raw string) (string, error) {
	if len(raw) == 0 || len(raw) > 2048 || raw != strings.TrimSpace(raw) {
		return "", fail("invalid_target")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Hostname() == "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		strings.ContainsAny(u.Path, "\\") {
		return "", fail("invalid_target")
	}
	if u.Path != "" {
		for _, part := range strings.Split(u.Path, "/") {
			if part == "." || part == ".." || strings.IndexFunc(part, unicode.IsControl) >= 0 {
				return "", fail("invalid_target")
			}
		}
	}
	if u.Scheme == "http" {
		host := strings.ToLower(u.Hostname())
		ip := net.ParseIP(host)
		if host != "localhost" && host != "host.docker.internal" && (ip == nil || !ip.IsLoopback()) {
			return "", fail("insecure_target")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/"), nil
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (c *Client) request(ctx context.Context, method, path string, body any, expectedStatus int, out any) error {
	if c == nil || c.http == nil {
		return fail("invalid_client")
	}
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fail("invalid_input")
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return fail("invalid_target")
	}
	req.Header.Set("X-API-Key", c.apiKey.Value())
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fail("timeout")
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return fail("timeout")
		}
		return fail("unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return fail("unauthorized")
		case http.StatusForbidden:
			return fail("forbidden")
		case http.StatusNotFound:
			if path == "/api/v1/libraries" || strings.HasPrefix(path, "/api/v1/books/list?") {
				return fail("unsupported_version")
			}
			return fail("not_found")
		case http.StatusTooManyRequests:
			return fail("rate_limited")
		case http.StatusBadRequest:
			return fail("invalid_request")
		case http.StatusMethodNotAllowed, http.StatusNotImplemented:
			return fail("unsupported_version")
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return fail("redirect_blocked")
		}
		if resp.StatusCode >= 500 {
			return fail("unavailable")
		}
		return fail("unexpected_status")
	}
	if out == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fail("invalid_response")
	}
	if len(data) == 0 || len(data) > maxResponseBytes || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		if len(data) > maxResponseBytes {
			return fail("response_too_large")
		}
		return fail("invalid_response")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fail("invalid_response")
	}
	return nil
}

func (c *Client) Libraries(ctx context.Context) ([]Library, error) {
	var libraries []Library
	if err := c.request(ctx, http.MethodGet, "/api/v1/libraries", nil, http.StatusOK, &libraries); err != nil {
		return nil, err
	}
	if libraries == nil {
		return nil, fail("invalid_response")
	}
	for _, library := range libraries {
		if !validID(library.ID) || library.Root == "" {
			return nil, fail("invalid_response")
		}
	}
	return libraries, nil
}

func (c *Client) Library(ctx context.Context, id string) (Library, error) {
	if !validID(id) {
		return Library{}, fail("invalid_input")
	}
	var library Library
	if err := c.request(ctx, http.MethodGet, "/api/v1/libraries/"+id, nil, http.StatusOK, &library); err != nil {
		return Library{}, err
	}
	if library.ID != id || library.Root == "" {
		return Library{}, fail("invalid_response")
	}
	return library, nil
}

func (c *Client) Books(ctx context.Context, opts ListBooksOptions) (BookPage, error) {
	if !validID(opts.LibraryID) || opts.Page < 0 || opts.Page > 1_000_000 || opts.Size < 1 || opts.Size > 100 ||
		len(opts.Query) > 256 || !utf8.ValidString(opts.Query) || strings.IndexFunc(opts.Query, unicode.IsControl) >= 0 {
		return BookPage{}, fail("invalid_input")
	}
	search := map[string]any{
		"condition": map[string]any{"allOf": []any{
			map[string]any{"libraryId": map[string]any{"operator": "is", "value": opts.LibraryID}},
			map[string]any{"deleted": map[string]any{"operator": "isFalse"}},
		}},
	}
	if query := strings.TrimSpace(opts.Query); query != "" {
		search["fullTextSearch"] = query
	}
	path := "/api/v1/books/list?page=" + strconv.Itoa(opts.Page) + "&size=" + strconv.Itoa(opts.Size)
	var page BookPage
	if err := c.request(ctx, http.MethodPost, path, search, http.StatusOK, &page); err != nil {
		return BookPage{}, err
	}
	if page.Content == nil || page.Number != opts.Page || page.Size <= 0 || page.Size > opts.Size || page.TotalPages < 0 || page.TotalElements < 0 || len(page.Content) > opts.Size {
		return BookPage{}, fail("invalid_response")
	}
	for _, book := range page.Content {
		if !validID(book.ID) || book.LibraryID != opts.LibraryID {
			return BookPage{}, fail("invalid_response")
		}
	}
	return page, nil
}

func (c *Client) Book(ctx context.Context, id string) (Book, error) {
	if !validID(id) {
		return Book{}, fail("invalid_input")
	}
	var book Book
	if err := c.request(ctx, http.MethodGet, "/api/v1/books/"+id, nil, http.StatusOK, &book); err != nil {
		return Book{}, err
	}
	if book.ID != id || !validID(book.LibraryID) || book.URL == "" {
		return Book{}, fail("invalid_response")
	}
	return book, nil
}

// AnalyzeBook queues analysis; its 202 response is not proof that Komga has
// reread ComicInfo or updated the book's metadata.
func (c *Client) AnalyzeBook(ctx context.Context, id string) error {
	if !validID(id) {
		return fail("invalid_input")
	}
	return c.request(ctx, http.MethodPost, "/api/v1/books/"+id+"/analyze", nil, http.StatusAccepted, nil)
}

// ClearBookMetadata sends only supported book-level clear fields, omitting all
// lock fields. Callers must first commit ComicInfo, check library settings and
// lock state, and reread Komga after this non-transactional projection update.
func (c *Client) ClearBookMetadata(ctx context.Context, id string, fields ...ClearField) error {
	if !validID(id) || len(fields) == 0 {
		return fail("invalid_input")
	}
	patch := make(map[string]any, len(fields))
	for _, field := range fields {
		if _, exists := patch[string(field)]; exists {
			return fail("invalid_input")
		}
		switch field {
		case ClearSummary, ClearISBN:
			patch[string(field)] = ""
		case ClearReleaseDate:
			patch[string(field)] = nil
		case ClearAuthors, ClearTags, ClearLinks:
			patch[string(field)] = []any{}
		default:
			return fail("invalid_input")
		}
	}
	return c.request(ctx, http.MethodPatch, "/api/v1/books/"+id+"/metadata", patch, http.StatusNoContent, nil)
}
