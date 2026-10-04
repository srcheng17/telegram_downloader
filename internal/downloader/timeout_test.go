package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadRequestTimeouts(t *testing.T) {
	for _, bodyTimeout := range []bool{false, true} {
		for _, mode := range []string{"retry recovers", "fallback recovers", "exhausted keeps other images"} {
			t.Run(fmt.Sprintf("body=%t/%s", bodyTimeout, mode), func(t *testing.T) {
				var attempts atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/page":
						if mode == "fallback recovers" {
							_, _ = fmt.Fprint(w, `<img src="/slow.jpg" data-src="/fallback.jpg"><img src="/other.jpg">`)
						} else {
							_, _ = fmt.Fprint(w, `<img src="/slow.jpg"><img src="/other.jpg">`)
						}
					case "/slow.jpg":
						attempt := attempts.Add(1)
						if mode == "retry recovers" && attempt == 3 {
							_, _ = fmt.Fprint(w, "recovered")
							return
						}
						if bodyTimeout {
							_, _ = fmt.Fprint(w, "partial")
							w.(http.Flusher).Flush()
						}
						<-r.Context().Done()
					case "/fallback.jpg", "/other.jpg":
						_, _ = fmt.Fprint(w, r.URL.Path)
					default:
						http.NotFound(w, r)
					}
				}))
				t.Cleanup(server.Close)
				client := server.Client()
				client.Timeout = 50 * time.Millisecond
				service := Service{HTTPClient: client, DownloadRetries: 2, ImageConcurrency: 1}
				result, err := service.Download(context.Background(), server.URL+"/page")
				if attempts.Load() != 3 {
					t.Errorf("request attempts = %d, want 1 + 2 retries", attempts.Load())
				}
				if mode == "exhausted keeps other images" {
					var partial *PartialFailureError
					if !errors.As(err, &partial) || partial.Successful != 1 || len(partial.Failures) != 1 {
						t.Fatalf("want one exhausted image and one success, got result=%+v err=%v", result, err)
					}
				} else if err != nil || result.DownloadedImages != 2 {
					t.Fatalf("want two downloaded images, got result=%+v err=%v", result, err)
				}
				if len(result.Images) == 0 || string(result.Images[len(result.Images)-1].Data) != "/other.jpg" {
					t.Fatalf("request timeout canceled unrelated image: %+v", result)
				}
			})
		}
	}
}

func TestDownloadParentCancellationStopsRetriesAndFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var imageAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/page" {
			_, _ = fmt.Fprint(w, `<img src="/slow.jpg" data-src="/fallback.jpg"><img src="/other.jpg">`)
			return
		}
		imageAttempts.Add(1)
		cancel()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	service := Service{HTTPClient: server.Client(), DownloadRetries: 5, ImageConcurrency: 1}
	_, err := service.Download(ctx, server.URL+"/page")
	if !errors.Is(err, context.Canceled) || imageAttempts.Load() != 1 {
		t.Fatalf("parent cancellation: err=%v attempts=%d", err, imageAttempts.Load())
	}
}
