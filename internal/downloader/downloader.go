package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/domain"
)

const (
	defaultWorkerCount = 4
	defaultHTTPTimeout = 30 * time.Second
)

type Service struct {
	HTTPClient              *http.Client
	DownloadRetries         int
	ImageConcurrency        int
	MaxImages               int
	MaxImageBytes           int64
	MaxTotalBytes           int64
	OnTotalImagesDiscovered func(total int)
	OnImageDownloaded       func(downloaded, total int)
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

type imageSlot struct {
	index      int
	candidates []string
}

type downloadHTTPStatusError struct {
	url        string
	statusCode int
}

func (e *downloadHTTPStatusError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("download image %s: unexpected status %d", e.url, e.statusCode)
}

func (s Service) Download(ctx context.Context, pageURL string) (domain.DownloadResult, error) {
	client := s.httpClient()

	pageHTML, err := fetchPageHTML(ctx, client, pageURL)
	if err != nil {
		return domain.DownloadResult{}, err
	}

	imageSlots, err := extractImageCandidateSets(pageURL, pageHTML)
	if err != nil {
		return domain.DownloadResult{}, err
	}

	result := domain.DownloadResult{
		Images:      make([]domain.DownloadedImage, 0, len(imageSlots)),
		TotalImages: len(imageSlots),
	}
	if s.OnTotalImagesDiscovered != nil {
		s.OnTotalImagesDiscovered(len(imageSlots))
	}

	if len(imageSlots) == 0 {
		return result, errors.New("no images found on page")
	}
	if s.MaxImages > 0 && len(imageSlots) > s.MaxImages {
		return result, &LimitExceededError{
			Kind:   LimitKindImageCount,
			Limit:  int64(s.MaxImages),
			Actual: int64(len(imageSlots)),
		}
	}

	workerCount := s.imageWorkerCount(len(imageSlots))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan imageSlot, len(imageSlots))
	for index := range imageSlots {
		jobs <- imageSlot{
			index:      index,
			candidates: imageSlots[index],
		}
	}
	close(jobs)

	results := make(chan imageResult, len(imageSlots))
	var wg sync.WaitGroup
	var totalBytes int64
	var totalBytesMu sync.Mutex

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for slot := range jobs {
				if err := ctx.Err(); err != nil {
					return
				}

				imageURL := ""
				if len(slot.candidates) > 0 {
					imageURL = slot.candidates[0]
				}
				image, downloadErr := s.downloadImageSlot(
					ctx,
					client,
					slot.candidates,
					&totalBytes,
					&totalBytesMu,
				)
				if downloadErr != nil {
					if isLimitExceeded(downloadErr) {
						cancel()
					}
					if isContextCancellation(downloadErr) {
						cancel()
					}
					results <- imageResult{
						index: slot.index,
						url:   imageURL,
						err:   downloadErr,
					}
					continue
				}
				results <- imageResult{
					index: slot.index,
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

	successByIndex := make([]domain.DownloadedImage, len(imageSlots))
	successMarker := make([]bool, len(imageSlots))
	failures := make([]ImageFailure, 0)
	var limitErr error
	var cancelErr error

	for item := range results {
		if item.err != nil {
			failures = append(failures, ImageFailure{URL: item.url, Err: item.err})
			if limitErr == nil && isLimitExceeded(item.err) {
				limitErr = item.err
			}
			if cancelErr == nil && isContextCancellation(item.err) {
				cancelErr = item.err
			}
			continue
		}

		successByIndex[item.index] = item.image
		successMarker[item.index] = true
		if s.OnImageDownloaded != nil {
			completed := result.DownloadedImages + 1
			s.OnImageDownloaded(completed, len(imageSlots))
		}
		result.DownloadedImages++
	}

	for i := range successMarker {
		if !successMarker[i] {
			continue
		}
		result.Images = append(result.Images, successByIndex[i])
	}
	if result.DownloadedImages == 0 {
		result.DownloadedImages = len(result.Images)
	}
	result.TotalBytes = totalBytes

	if limitErr != nil {
		return result, limitErr
	}
	if cancelErr != nil {
		return result, cancelErr
	}
	if ctxErr := ctx.Err(); isContextCancellation(ctxErr) {
		return result, ctxErr
	}
	if len(failures) > 0 {
		return result, &PartialFailureError{
			Total:      len(imageSlots),
			Successful: len(result.Images),
			Failures:   failures,
		}
	}

	return result, nil
}

func (s Service) PackageCBZ(images []domain.DownloadedImage, metadata TaskMetadata, outputPath string) error {
	return s.PackageCBZContext(context.Background(), images, metadata, outputPath)
}

func (s Service) PackageCBZContext(ctx context.Context, images []domain.DownloadedImage, metadata TaskMetadata, outputPath string) error {
	localImages := make([]LocalImage, 0, len(images))
	for index, image := range images {
		localImages = append(localImages, LocalImage{
			Name: fmt.Sprintf("%d%s", index+1, imageArchiveExtension(image.URL, image.ContentType)),
			Data: image.Data,
		})
	}

	comicInfo, err := WriteComicInfoXML(metadata)
	if err != nil {
		return err
	}

	return PackCBZContext(ctx, localImages, comicInfo, outputPath)
}

func TaskMetadataFromTask(task domain.TaskLog) TaskMetadata {
	return TaskMetadata{
		Writer:  valueOrEmpty(task.Author),
		Series:  valueOrEmpty(task.SeriesName),
		Title:   valueOrEmpty(task.ComicName),
		Summary: valueOrEmpty(task.Summary),
		Tags: firstNonEmpty(
			valueOrEmpty(task.TagsNormalized),
			valueOrEmpty(task.TagsRaw),
		),
		Genre: firstNonEmpty(
			valueOrEmpty(task.GenresNormalized),
			valueOrEmpty(task.GenresRaw),
		),
	}
}

func (s Service) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

func (s Service) downloadImageSlot(
	ctx context.Context,
	client *http.Client,
	candidates []string,
	totalBytes *int64,
	totalBytesMu *sync.Mutex,
) (domain.DownloadedImage, error) {
	var lastErr error
	retries := s.downloadRetries()
	for _, candidateURL := range candidates {
		image, err := s.downloadImageWithRetries(ctx, client, candidateURL, retries, totalBytes, totalBytesMu)
		if err == nil {
			return image, nil
		}
		if isLimitExceeded(err) || isContextCancellation(err) {
			return domain.DownloadedImage{}, err
		}
		lastErr = err
	}
	if lastErr != nil {
		return domain.DownloadedImage{}, fmt.Errorf("all candidate urls failed: %w", lastErr)
	}
	return domain.DownloadedImage{}, errors.New("no candidate urls available for image")
}

func (s Service) imageWorkerCount(totalImages int) int {
	workerCount := s.ImageConcurrency
	if workerCount <= 0 {
		workerCount = defaultWorkerCount
	}
	if totalImages > 0 && totalImages < workerCount {
		workerCount = totalImages
	}
	if workerCount <= 0 {
		return 1
	}
	return workerCount
}

func (s Service) downloadRetries() int {
	if s.DownloadRetries > 0 {
		return s.DownloadRetries
	}
	return 0
}

func (s Service) downloadImageWithRetries(
	ctx context.Context,
	client *http.Client,
	imageURL string,
	retries int,
	totalBytes *int64,
	totalBytesMu *sync.Mutex,
) (domain.DownloadedImage, error) {
	attempts := retries + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		image, err := s.downloadImage(ctx, client, imageURL, totalBytes, totalBytesMu)
		if err == nil {
			return image, nil
		}
		if isLimitExceeded(err) || isContextCancellation(err) {
			return domain.DownloadedImage{}, err
		}
		lastErr = err
		if !shouldRetryDownloadError(err) {
			break
		}
	}
	if lastErr != nil {
		return domain.DownloadedImage{}, lastErr
	}
	return domain.DownloadedImage{}, errors.New("download image failed")
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
		return domain.DownloadedImage{}, &downloadHTTPStatusError{
			url:        imageURL,
			statusCode: resp.StatusCode,
		}
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
			if ctx.Err() != nil {
				return domain.DownloadedImage{}, ctx.Err()
			}
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
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
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
	pictureBlockPattern = regexp.MustCompile(`(?is)<picture\b[^>]*>.*?</picture>`)
	imgTagPattern       = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	sourceTagPattern    = regexp.MustCompile(`(?is)<source\b[^>]*>`)
	urlAttrPattern      = regexp.MustCompile(`(?is)\b(src|data-src|data-original|data-lazy-src|data-url)\b\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	srcsetAttrPattern   = regexp.MustCompile(`(?is)\b(srcset|data-srcset)\b\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

var imageArchiveExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".webp": {},
	".gif":  {},
	".bmp":  {},
	".avif": {},
}

var contentTypeToArchiveExtension = map[string]string{
	"image/jpeg":     ".jpg",
	"image/jpg":      ".jpg",
	"image/pjpeg":    ".jpg",
	"image/png":      ".png",
	"image/webp":     ".webp",
	"image/gif":      ".gif",
	"image/bmp":      ".bmp",
	"image/x-ms-bmp": ".bmp",
	"image/avif":     ".avif",
}

func imageArchiveExtension(rawURL, contentType string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err == nil && parsed != nil {
		ext := strings.ToLower(path.Ext(parsed.Path))
		if _, ok := imageArchiveExtensions[ext]; ok {
			return ext
		}
	}

	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err == nil {
		if ext, ok := contentTypeToArchiveExtension[strings.ToLower(mediaType)]; ok {
			return ext
		}
	}

	if ext, ok := contentTypeToArchiveExtension[strings.ToLower(strings.TrimSpace(contentType))]; ok {
		return ext
	}

	return ".jpg"
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func extractImageCandidateSets(pageURL, pageHTML string) ([][]string, error) {
	baseURL, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("parse page url %q: %w", pageURL, err)
	}

	candidateSets := make([][]string, 0)
	cursor := 0
	for _, match := range pictureBlockPattern.FindAllStringIndex(pageHTML, -1) {
		start := match[0]
		end := match[1]

		if start > cursor {
			segment := pageHTML[cursor:start]
			candidateSets = append(candidateSets, extractStandaloneImageCandidateSets(baseURL, segment)...)
		}

		pictureBlock := pageHTML[start:end]
		candidateSets = append(candidateSets, extractPictureBlockCandidateSets(baseURL, pictureBlock)...)
		cursor = end
	}
	if cursor < len(pageHTML) {
		candidateSets = append(candidateSets, extractStandaloneImageCandidateSets(baseURL, pageHTML[cursor:])...)
	}

	return candidateSets, nil
}

func extractStandaloneImageCandidateSets(baseURL *url.URL, htmlSegment string) [][]string {
	candidateSets := make([][]string, 0)
	for _, imgTag := range imgTagPattern.FindAllString(htmlSegment, -1) {
		candidates := normalizeCandidateSet(baseURL, rawCandidatesFromTag(
			imgTag,
			[]string{"src", "data-src", "data-original", "data-lazy-src", "data-url"},
			[]string{"srcset", "data-srcset"},
		))
		if len(candidates) == 0 {
			continue
		}
		candidateSets = append(candidateSets, candidates)
	}
	return candidateSets
}

func extractPictureBlockCandidateSets(baseURL *url.URL, pictureBlock string) [][]string {
	sourceCandidates := make([]string, 0)
	for _, sourceTag := range sourceTagPattern.FindAllString(pictureBlock, -1) {
		sourceCandidates = append(sourceCandidates, rawCandidatesFromTag(
			sourceTag,
			[]string{"src"},
			[]string{"srcset", "data-srcset"},
		)...)
	}

	candidateSets := make([][]string, 0)
	for _, imgTag := range imgTagPattern.FindAllString(pictureBlock, -1) {
		rawCandidates := rawCandidatesFromTag(
			imgTag,
			[]string{"src", "data-src", "data-original", "data-lazy-src", "data-url"},
			[]string{"srcset", "data-srcset"},
		)
		rawCandidates = append(rawCandidates, sourceCandidates...)

		candidates := normalizeCandidateSet(baseURL, rawCandidates)
		if len(candidates) == 0 {
			continue
		}
		candidateSets = append(candidateSets, candidates)
	}
	return candidateSets
}

func rawCandidatesFromTag(tag string, urlAttributeOrder, srcsetAttributeOrder []string) []string {
	urlAttributeValues := readAttributeValues(tag, urlAttrPattern)
	srcsetValues := readAttributeValues(tag, srcsetAttrPattern)

	rawCandidates := make([]string, 0)
	for _, attribute := range urlAttributeOrder {
		value := strings.TrimSpace(urlAttributeValues[attribute])
		if value == "" {
			continue
		}
		rawCandidates = append(rawCandidates, value)
	}

	for _, attribute := range srcsetAttributeOrder {
		rawSrcset := strings.TrimSpace(srcsetValues[attribute])
		if rawSrcset == "" {
			continue
		}
		rawCandidates = append(rawCandidates, parseSrcsetCandidates(rawSrcset)...)
	}
	return rawCandidates
}

func parseSrcsetCandidates(rawSrcset string) []string {
	items := strings.Split(rawSrcset, ",")
	candidates := make([]string, 0, len(items))
	for _, item := range items {
		fields := strings.Fields(strings.TrimSpace(item))
		if len(fields) == 0 {
			continue
		}
		if fields[0] != "" {
			candidates = append(candidates, fields[0])
		}
	}
	return candidates
}

func readAttributeValues(tag string, pattern *regexp.Regexp) map[string]string {
	values := make(map[string]string)
	for _, match := range pattern.FindAllStringSubmatch(tag, -1) {
		name := strings.ToLower(strings.TrimSpace(match[1]))
		if _, exists := values[name]; exists {
			continue
		}
		value := strings.TrimSpace(firstNonEmpty(match[2], match[3], match[4]))
		if value == "" {
			continue
		}
		values[name] = value
	}
	return values
}

func normalizeCandidateSet(baseURL *url.URL, rawCandidates []string) []string {
	candidates := make([]string, 0, len(rawCandidates))
	seen := make(map[string]struct{})
	for _, rawCandidate := range rawCandidates {
		normalizedURL, ok := normalizeImageURL(baseURL, rawCandidate)
		if !ok {
			continue
		}
		if _, exists := seen[normalizedURL]; exists {
			continue
		}
		seen[normalizedURL] = struct{}{}
		candidates = append(candidates, normalizedURL)
	}
	return candidates
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

func IsLimitExceededError(err error) bool {
	return isLimitExceeded(err)
}

func isContextCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func IsContextCancellationError(err error) bool {
	return isContextCancellation(err)
}

func shouldRetryDownloadError(err error) bool {
	if err == nil {
		return false
	}
	if isLimitExceeded(err) || isContextCancellation(err) {
		return false
	}

	var statusErr *downloadHTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.statusCode == http.StatusRequestTimeout ||
			statusErr.statusCode == http.StatusTooManyRequests ||
			statusErr.statusCode >= http.StatusInternalServerError
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

func ShouldRetryTaskError(err error) bool {
	return shouldRetryDownloadError(err)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
