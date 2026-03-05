package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
)

const defaultWorkerCount = 4

type Service struct {
	HTTPClient    *http.Client
	MaxImages     int
	MaxImageBytes int64
	MaxTotalBytes int64
}

type LimitKind string

const (
	LimitKindImageCount LimitKind = "image_count"
	LimitKindImageBytes LimitKind = "image_bytes"
	LimitKindTotalBytes LimitKind = "total_bytes"
)

type LimitExceededError struct {
	Kind   LimitKind
	Limit  int64
	Actual int64
}

func (e *LimitExceededError) Error() string {
	if e == nil {
		return ""
	}

	switch e.Kind {
	case LimitKindImageCount:
		return fmt.Sprintf("image count limit exceeded: found %d images, max allowed is %d", e.Actual, e.Limit)
	case LimitKindImageBytes:
		return fmt.Sprintf("single image size limit exceeded: downloaded %d bytes, max allowed is %d", e.Actual, e.Limit)
	case LimitKindTotalBytes:
		return fmt.Sprintf("total download size limit exceeded: downloaded %d bytes, max allowed is %d", e.Actual, e.Limit)
	default:
		return fmt.Sprintf("download limit exceeded: actual=%d limit=%d", e.Actual, e.Limit)
	}
}

type ImageFailure struct {
	URL string
	Err error
}

type PartialFailureError struct {
	Total      int
	Successful int
	Failures   []ImageFailure
}

func (e *PartialFailureError) Error() string {
	if e == nil {
		return ""
	}
	failed := e.Total - e.Successful
	sample := ""
	if len(e.Failures) > 0 && e.Failures[0].Err != nil {
		sample = e.Failures[0].Err.Error()
	}
	if sample == "" {
		return fmt.Sprintf(
			"partial download failed: %d/%d images downloaded, %d failed",
			e.Successful,
			e.Total,
			failed,
		)
	}
	return fmt.Sprintf(
		"partial download failed: %d/%d images downloaded, %d failed. example error: %s",
		e.Successful,
		e.Total,
		failed,
		sample,
	)
}

type imageResult struct {
	index int
	url   string
	image domain.DownloadedImage
	err   error
}

func (s Service) Download(ctx context.Context, pageURL string) (domain.DownloadResult, error) {
	client := s.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	pageHTML, err := fetchPageHTML(ctx, client, pageURL)
	if err != nil {
		return domain.DownloadResult{}, err
	}

	imageURLs, err := extractImageURLs(pageURL, pageHTML)
	if err != nil {
		return domain.DownloadResult{}, err
	}

	result := domain.DownloadResult{
		Images:      make([]domain.DownloadedImage, 0, len(imageURLs)),
		TotalImages: len(imageURLs),
	}

	if len(imageURLs) == 0 {
		return result, errors.New("no images found on page")
	}
	if s.MaxImages > 0 && len(imageURLs) > s.MaxImages {
		return result, &LimitExceededError{
			Kind:   LimitKindImageCount,
			Limit:  int64(s.MaxImages),
			Actual: int64(len(imageURLs)),
		}
	}

	workerCount := defaultWorkerCount
	if len(imageURLs) < workerCount {
		workerCount = len(imageURLs)
	}
	if workerCount <= 0 {
		workerCount = 1
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int, len(imageURLs))
	for index := range imageURLs {
		jobs <- index
	}
	close(jobs)

	results := make(chan imageResult, len(imageURLs))
	var wg sync.WaitGroup
	var totalBytes int64
	var totalBytesMu sync.Mutex

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					return
				}

				imageURL := imageURLs[index]
				image, downloadErr := s.downloadImage(ctx, client, imageURL, &totalBytes, &totalBytesMu)
				if downloadErr != nil {
					if isLimitExceeded(downloadErr) {
						cancel()
					}
					results <- imageResult{
						index: index,
						url:   imageURL,
						err:   downloadErr,
					}
					continue
				}
				results <- imageResult{
					index: index,
					url:   imageURL,
					image: image,
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	successByIndex := make([]domain.DownloadedImage, len(imageURLs))
	successMarker := make([]bool, len(imageURLs))
	failures := make([]ImageFailure, 0)
	var limitErr error

	for item := range results {
		if item.err != nil {
			failures = append(failures, ImageFailure{URL: item.url, Err: item.err})
			if limitErr == nil && isLimitExceeded(item.err) {
				limitErr = item.err
			}
			continue
		}

		successByIndex[item.index] = item.image
		successMarker[item.index] = true
	}

	for i := range successMarker {
		if !successMarker[i] {
			continue
		}
		result.Images = append(result.Images, successByIndex[i])
	}
	result.DownloadedImages = len(result.Images)
	result.TotalBytes = totalBytes

	if limitErr != nil {
		return result, limitErr
	}
	if len(failures) > 0 {
		return result, &PartialFailureError{
			Total:      len(imageURLs),
			Successful: len(result.Images),
			Failures:   failures,
		}
	}

	return result, nil
}

func (s Service) downloadImage(
	ctx context.Context,
	client *http.Client,
	imageURL string,
	totalBytes *int64,
	totalBytesMu *sync.Mutex,
) (domain.DownloadedImage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return domain.DownloadedImage{}, fmt.Errorf("build image request for %s: %w", imageURL, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return domain.DownloadedImage{}, ctx.Err()
		}
		return domain.DownloadedImage{}, fmt.Errorf("download image %s: %w", imageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return domain.DownloadedImage{}, fmt.Errorf("download image %s: unexpected status %d", imageURL, resp.StatusCode)
	}

	var imageBytes int64
	var buf bytes.Buffer
	chunk := make([]byte, 32*1024)

	for {
		n, readErr := resp.Body.Read(chunk)
		if n > 0 {
			imageBytes += int64(n)
			if s.MaxImageBytes > 0 && imageBytes > s.MaxImageBytes {
				return domain.DownloadedImage{}, &LimitExceededError{
					Kind:   LimitKindImageBytes,
					Limit:  s.MaxImageBytes,
					Actual: imageBytes,
				}
			}

			totalBytesMu.Lock()
			nextTotalBytes := *totalBytes + int64(n)
			if s.MaxTotalBytes > 0 && nextTotalBytes > s.MaxTotalBytes {
				totalBytesMu.Unlock()
				return domain.DownloadedImage{}, &LimitExceededError{
					Kind:   LimitKindTotalBytes,
					Limit:  s.MaxTotalBytes,
					Actual: nextTotalBytes,
				}
			}
			*totalBytes = nextTotalBytes
			totalBytesMu.Unlock()

			if _, err := buf.Write(chunk[:n]); err != nil {
				return domain.DownloadedImage{}, fmt.Errorf("buffer image %s: %w", imageURL, err)
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return domain.DownloadedImage{}, fmt.Errorf("read image body %s: %w", imageURL, readErr)
		}
	}

	return domain.DownloadedImage{
		URL:         imageURL,
		ContentType: resp.Header.Get("Content-Type"),
		Data:        buf.Bytes(),
	}, nil
}

func fetchPageHTML(ctx context.Context, client *http.Client, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("build page request for %s: %w", pageURL, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch page %s: %w", pageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("fetch page %s: unexpected status %d", pageURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read page %s body: %w", pageURL, err)
	}

	return string(body), nil
}

var (
	imgTagPattern        = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	imgCandidatePattern  = regexp.MustCompile(`(?is)(src|data-src|data-original|data-lazy-src|data-url)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	imgSrcsetAttrPattern = regexp.MustCompile(`(?is)(srcset|data-srcset)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

func extractImageURLs(pageURL, pageHTML string) ([]string, error) {
	baseURL, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("parse page url %q: %w", pageURL, err)
	}

	imageURLs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, imgTag := range imgTagPattern.FindAllString(pageHTML, -1) {
		candidate := extractImageCandidate(imgTag)
		if candidate == "" {
			continue
		}

		normalizedURL, ok := normalizeImageURL(baseURL, candidate)
		if !ok {
			continue
		}
		if _, exists := seen[normalizedURL]; exists {
			continue
		}

		seen[normalizedURL] = struct{}{}
		imageURLs = append(imageURLs, normalizedURL)
	}

	return imageURLs, nil
}

func extractImageCandidate(imgTag string) string {
	attributeValues := make(map[string]string)
	for _, match := range imgCandidatePattern.FindAllStringSubmatch(imgTag, -1) {
		name := strings.ToLower(strings.TrimSpace(match[1]))
		if _, exists := attributeValues[name]; exists {
			continue
		}
		value := firstNonEmpty(match[2], match[3], match[4])
		if value == "" {
			continue
		}
		attributeValues[name] = value
	}

	for _, attribute := range []string{"src", "data-src", "data-original", "data-lazy-src", "data-url"} {
		value := strings.TrimSpace(attributeValues[attribute])
		if value != "" {
			return value
		}
	}

	for _, match := range imgSrcsetAttrPattern.FindAllStringSubmatch(imgTag, -1) {
		rawSrcset := firstNonEmpty(match[2], match[3], match[4])
		if rawSrcset == "" {
			continue
		}
		if candidate := parseFirstSrcsetCandidate(rawSrcset); candidate != "" {
			return candidate
		}
	}

	return ""
}

func parseFirstSrcsetCandidate(rawSrcset string) string {
	items := strings.Split(rawSrcset, ",")
	for _, item := range items {
		fields := strings.Fields(strings.TrimSpace(item))
		if len(fields) == 0 {
			continue
		}
		if fields[0] != "" {
			return fields[0]
		}
	}
	return ""
}

func normalizeImageURL(baseURL *url.URL, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}

	resolved, err := baseURL.Parse(trimmed)
	if err != nil {
		return "", false
	}
	scheme := strings.ToLower(strings.TrimSpace(resolved.Scheme))
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return resolved.String(), true
}

func isLimitExceeded(err error) bool {
	var limitErr *LimitExceededError
	return errors.As(err, &limitErr)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
