package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloadFailsWhenImageCountExceedsLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(
				w,
				`<html><body><img src="/1.jpg"><img src="/2.jpg"><img src="/3.jpg"></body></html>`,
			)
		case "/1.jpg", "/2.jpg", "/3.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte{1, 2, 3})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	service := Service{
		HTTPClient:    server.Client(),
		MaxImages:     2,
		MaxImageBytes: 1024,
		MaxTotalBytes: 4096,
	}

	_, err := service.Download(context.Background(), server.URL+"/page")
	if err == nil {
		t.Fatal("expected image count guardrail error, got nil")
	}

	var limitErr *LimitExceededError
	if !errors.As(err, &limitErr) {
		t.Fatalf("expected limit error, got %T: %v", err, err)
	}
	if limitErr.Kind != LimitKindImageCount {
		t.Fatalf("expected image count limit kind, got %q", limitErr.Kind)
	}
	if limitErr.Limit != 2 {
		t.Fatalf("expected limit=2, got %d", limitErr.Limit)
	}
	if limitErr.Actual != 3 {
		t.Fatalf("expected actual=3, got %d", limitErr.Actual)
	}
}

func TestDownloadReturnsPartialFailureWhenAnyImageFails(t *testing.T) {
	var imageRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(w, `<html><body><img src="/ok.jpg"><img src="/missing.jpg"></body></html>`)
		case "/ok.jpg":
			imageRequests.Add(1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("ok"))
		case "/missing.jpg":
			imageRequests.Add(1)
			http.Error(w, "missing", http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	service := Service{
		HTTPClient:    server.Client(),
		MaxImages:     10,
		MaxImageBytes: 1024,
		MaxTotalBytes: 4096,
	}

	result, err := service.Download(context.Background(), server.URL+"/page")
	if err == nil {
		t.Fatal("expected partial download error, got nil")
	}

	var partialErr *PartialFailureError
	if !errors.As(err, &partialErr) {
		t.Fatalf("expected partial failure error, got %T: %v", err, err)
	}
	if partialErr.Total != 2 {
		t.Fatalf("expected total=2, got %d", partialErr.Total)
	}
	if partialErr.Successful != 1 {
		t.Fatalf("expected successful=1, got %d", partialErr.Successful)
	}
	if len(partialErr.Failures) != 1 {
		t.Fatalf("expected one failed image, got %d", len(partialErr.Failures))
	}
	if !strings.Contains(partialErr.Failures[0].Err.Error(), "404") {
		t.Fatalf("expected failure to contain status code, got %v", partialErr.Failures[0].Err)
	}
	if imageRequests.Load() != 2 {
		t.Fatalf("expected 2 image requests, got %d", imageRequests.Load())
	}

	if result.TotalImages != 2 {
		t.Fatalf("expected result total_images=2, got %d", result.TotalImages)
	}
	if result.DownloadedImages != 1 {
		t.Fatalf("expected result downloaded_images=1, got %d", result.DownloadedImages)
	}
	if len(result.Images) != 1 {
		t.Fatalf("expected one downloaded image, got %d", len(result.Images))
	}
	if result.Images[0].URL != server.URL+"/ok.jpg" {
		t.Fatalf("expected successful image URL %q, got %q", server.URL+"/ok.jpg", result.Images[0].URL)
	}
}
