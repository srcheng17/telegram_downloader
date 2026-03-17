package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractDownloadRequestFromForm(t *testing.T) {
	req := httptest.NewRequest(
		http.MethodPost,
		"/download",
		strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fabc&force=1&tags=a%EF%BC%8Cb"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rawURL, forceDownload, metadata, err := extractDownloadRequest(req)
	if err != nil {
		t.Fatalf("extract form request: %v", err)
	}
	if rawURL != "https://telegra.ph/abc" {
		t.Fatalf("expected rawURL to be parsed, got %q", rawURL)
	}
	if !forceDownload {
		t.Fatalf("expected force flag to be true")
	}
	if metadata.tagsNormalized == nil || *metadata.tagsNormalized != "a,b" {
		t.Fatalf("expected tags to normalize comma variants, got %#v", metadata.tagsNormalized)
	}
}

func TestExtractDownloadRequestFromJSON(t *testing.T) {
	req := httptest.NewRequest(
		http.MethodPost,
		"/download",
		strings.NewReader(`{"url":"https://telegra.ph/json","force":true,"author":"A，B","tags":"x y","genres":"g1#g2"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	rawURL, forceDownload, metadata, err := extractDownloadRequest(req)
	if err != nil {
		t.Fatalf("extract json request: %v", err)
	}
	if rawURL != "https://telegra.ph/json" {
		t.Fatalf("expected rawURL to be parsed, got %q", rawURL)
	}
	if !forceDownload {
		t.Fatalf("expected force flag to be true")
	}
	if metadata.author == nil || *metadata.author != "A,B" {
		t.Fatalf("expected author normalization, got %#v", metadata.author)
	}
	if metadata.tagsNormalized == nil || *metadata.tagsNormalized != "x,y" {
		t.Fatalf("expected tags normalization, got %#v", metadata.tagsNormalized)
	}
	if metadata.genresNormalized == nil || *metadata.genresNormalized != "g1,g2" {
		t.Fatalf("expected genres normalization, got %#v", metadata.genresNormalized)
	}
}
