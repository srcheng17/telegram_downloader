package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestDownloadUsesFallbackCandidateWhenPrimaryFails(t *testing.T) {
	var primaryRequests atomic.Int32
	var fallbackRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(
				w,
				`<html><body><picture><source srcset="/fallback.jpg 1x"><img src="/primary.webp"></picture></body></html>`,
			)
		case "/primary.webp":
			primaryRequests.Add(1)
			http.Error(w, "missing", http.StatusNotFound)
		case "/fallback.jpg":
			fallbackRequests.Add(1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("ok"))
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
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}

	if primaryRequests.Load() != 1 {
		t.Fatalf("expected one primary request, got %d", primaryRequests.Load())
	}
	if fallbackRequests.Load() != 1 {
		t.Fatalf("expected one fallback request, got %d", fallbackRequests.Load())
	}
	if result.TotalImages != 1 {
		t.Fatalf("expected total_images=1, got %d", result.TotalImages)
	}
	if result.DownloadedImages != 1 {
		t.Fatalf("expected downloaded_images=1, got %d", result.DownloadedImages)
	}
	if len(result.Images) != 1 {
		t.Fatalf("expected one downloaded image, got %d", len(result.Images))
	}
	if result.Images[0].URL != server.URL+"/fallback.jpg" {
		t.Fatalf("expected fallback image url %q, got %q", server.URL+"/fallback.jpg", result.Images[0].URL)
	}
}

func TestDownloadPropagatesDeadlineExceededInsteadOfPartialFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(w, `<html><body><img src="/fast.jpg"><img src="/slow.jpg"></body></html>`)
		case "/fast.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("ok"))
		case "/slow.jpg":
			time.Sleep(300 * time.Millisecond)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("slow"))
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

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	t.Cleanup(cancel)

	_, err := service.Download(ctx, server.URL+"/page")
	if err == nil {
		t.Fatal("expected deadline exceeded error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %T: %v", err, err)
	}

	var partialErr *PartialFailureError
	if errors.As(err, &partialErr) {
		t.Fatalf("expected context deadline error, got partial failure: %#v", partialErr)
	}
}

func TestDownloadPreservesRepeatedImageTags(t *testing.T) {
	var imageRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(w, `<html><body><img src="/same.jpg"><img src="/same.jpg"></body></html>`)
		case "/same.jpg":
			imageRequests.Add(1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("ok"))
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
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if imageRequests.Load() != 2 {
		t.Fatalf("expected 2 image requests, got %d", imageRequests.Load())
	}
	if result.TotalImages != 2 {
		t.Fatalf("expected total_images=2, got %d", result.TotalImages)
	}
	if result.DownloadedImages != 2 {
		t.Fatalf("expected downloaded_images=2, got %d", result.DownloadedImages)
	}
	if len(result.Images) != 2 {
		t.Fatalf("expected two output images, got %d", len(result.Images))
	}
	if result.Images[0].URL != server.URL+"/same.jpg" || result.Images[1].URL != server.URL+"/same.jpg" {
		t.Fatalf("expected repeated urls preserved, got %q and %q", result.Images[0].URL, result.Images[1].URL)
	}
}

func TestDownloadPropagatesContextErrorWhenWorkersExitWithoutResultErrors(t *testing.T) {
	var imageRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(w, `<html><body><img src="/image.jpg"></body></html>`)
		case "/image.jpg":
			imageRequests.Add(1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	client := server.Client()
	baseTransport := client.Transport
	client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := baseTransport.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if req.URL.Path == "/page" {
			resp.Body = &cancelOnEOFReadCloser{
				ReadCloser: resp.Body,
				cancel:     cancel,
			}
		}
		return resp, nil
	})

	service := Service{
		HTTPClient:    client,
		MaxImages:     10,
		MaxImageBytes: 1024,
		MaxTotalBytes: 4096,
	}

	_, err := service.Download(ctx, server.URL+"/page")
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %T: %v", err, err)
	}
	var partialErr *PartialFailureError
	if errors.As(err, &partialErr) {
		t.Fatalf("expected context cancellation, got partial failure: %#v", partialErr)
	}
	if imageRequests.Load() != 0 {
		t.Fatalf("expected no image request due early cancellation, got %d", imageRequests.Load())
	}
}

func TestDownloadFailsWhenSingleImageExceedsByteLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(w, `<html><body><img src="/big.jpg"></body></html>`)
		case "/big.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("0123456789"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	service := Service{
		HTTPClient:    server.Client(),
		MaxImages:     10,
		MaxImageBytes: 5,
		MaxTotalBytes: 4096,
	}

	_, err := service.Download(context.Background(), server.URL+"/page")
	if err == nil {
		t.Fatal("expected single-image byte limit error, got nil")
	}

	var limitErr *LimitExceededError
	if !errors.As(err, &limitErr) {
		t.Fatalf("expected limit error, got %T: %v", err, err)
	}
	if limitErr.Kind != LimitKindImageBytes {
		t.Fatalf("expected image bytes limit kind, got %q", limitErr.Kind)
	}
}

func TestDownloadFailsWhenTotalBytesExceedsLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(w, `<html><body><img src="/a.jpg"><img src="/b.jpg"></body></html>`)
		case "/a.jpg", "/b.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("1234"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	service := Service{
		HTTPClient:    server.Client(),
		MaxImages:     10,
		MaxImageBytes: 1024,
		MaxTotalBytes: 6,
	}

	_, err := service.Download(context.Background(), server.URL+"/page")
	if err == nil {
		t.Fatal("expected total-bytes limit error, got nil")
	}

	var limitErr *LimitExceededError
	if !errors.As(err, &limitErr) {
		t.Fatalf("expected limit error, got %T: %v", err, err)
	}
	if limitErr.Kind != LimitKindTotalBytes {
		t.Fatalf("expected total-bytes limit kind, got %q", limitErr.Kind)
	}
}

func TestDownloadRespectsConfiguredImageConcurrency(t *testing.T) {
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(w, `<html><body><img src="/a.jpg"><img src="/b.jpg"></body></html>`)
		case "/a.jpg", "/b.jpg":
			current := inFlight.Add(1)
			for {
				observed := maxInFlight.Load()
				if current <= observed || maxInFlight.CompareAndSwap(observed, current) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			inFlight.Add(-1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	service := Service{
		HTTPClient:       server.Client(),
		ImageConcurrency: 1,
		MaxImages:        10,
		MaxImageBytes:    1024,
		MaxTotalBytes:    4096,
	}

	result, err := service.Download(context.Background(), server.URL+"/page")
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if result.DownloadedImages != 2 {
		t.Fatalf("expected 2 downloaded images, got %d", result.DownloadedImages)
	}
	if maxInFlight.Load() != 1 {
		t.Fatalf("expected max inflight image downloads=1, got %d", maxInFlight.Load())
	}
}

func TestDownloadRetriesFailedImageRequests(t *testing.T) {
	var imageAttempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(w, `<html><body><img src="/retry.jpg"></body></html>`)
		case "/retry.jpg":
			attempt := imageAttempts.Add(1)
			if attempt < 3 {
				http.Error(w, "temporary", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	service := Service{
		HTTPClient:       server.Client(),
		DownloadRetries:  2,
		ImageConcurrency: 1,
		MaxImages:        10,
		MaxImageBytes:    1024,
		MaxTotalBytes:    4096,
	}

	result, err := service.Download(context.Background(), server.URL+"/page")
	if err != nil {
		t.Fatalf("expected retry to recover download, got error: %v", err)
	}
	if result.DownloadedImages != 1 {
		t.Fatalf("expected 1 downloaded image, got %d", result.DownloadedImages)
	}
	if imageAttempts.Load() != 3 {
		t.Fatalf("expected 3 attempts (1 + 2 retries), got %d", imageAttempts.Load())
	}
}

func TestDownloadFallbackDoesNotRetryNonRetryable404(t *testing.T) {
	var primaryAttempts atomic.Int32
	var fallbackAttempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = fmt.Fprint(
				w,
				`<html><body><picture><source srcset="/fallback.jpg 1x"><img src="/primary.jpg"></picture></body></html>`,
			)
		case "/primary.jpg":
			primaryAttempts.Add(1)
			http.Error(w, "missing", http.StatusNotFound)
		case "/fallback.jpg":
			fallbackAttempts.Add(1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	service := Service{
		HTTPClient:       server.Client(),
		DownloadRetries:  5,
		ImageConcurrency: 1,
		MaxImages:        10,
		MaxImageBytes:    1024,
		MaxTotalBytes:    4096,
	}

	result, err := service.Download(context.Background(), server.URL+"/page")
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if result.DownloadedImages != 1 {
		t.Fatalf("expected 1 downloaded image, got %d", result.DownloadedImages)
	}
	if primaryAttempts.Load() != 1 {
		t.Fatalf("expected primary 404 to be attempted once, got %d", primaryAttempts.Load())
	}
	if fallbackAttempts.Load() != 1 {
		t.Fatalf("expected fallback to be attempted once, got %d", fallbackAttempts.Load())
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type cancelOnEOFReadCloser struct {
	io.ReadCloser
	cancel func()
	fired  bool
}

func (r *cancelOnEOFReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if !r.fired && errors.Is(err, io.EOF) && r.cancel != nil {
		r.fired = true
		r.cancel()
	}
	return n, err
}
