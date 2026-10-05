package archive

import (
	"archive/tar"
	"archive/zip"
	"context"
	"errors"
	"io"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

var ErrMultipleComicInfo = errors.New("归档含多份 ComicInfo，请修正原件后重新提交")
var ErrMetadataLimit = errors.New("归档元数据超出保留上限，请修正原件后重新提交")

// ExtractWithMetadata returns the partial bundle even on failure so the caller
// can retain the original source before reporting the failed generation.
func (e *Extractor) ExtractWithMetadata(ctx context.Context, sourcePath string) (*MetadataBundle, error) {
	bundle := &MetadataBundle{SourcePath: sourcePath, RetainSource: true, PageOrderKnown: true, Warnings: []metadata.Warning{}}
	if e == nil {
		return bundle, errors.New("archive extractor is required")
	}
	if err := ctx.Err(); err != nil {
		return bundle, err
	}
	var err error
	switch strings.ToLower(filepath.Ext(sourcePath)) {
	case ".zip", ".cbz":
		err = readZipBundle(ctx, sourcePath, e.limits, bundle)
	case ".rar", ".7z":
		if external, ok := e.external.(interface {
			ExtractWithMetadata(context.Context, string) (*MetadataBundle, error)
		}); ok {
			return external.ExtractWithMetadata(ctx, sourcePath)
		}
		err = readExternalBundle(ctx, sourcePath, e.limits, bundle)
	default:
		err = errors.New("unsupported archive format")
	}
	if err != nil {
		return bundle, err
	}
	finishBundle(bundle)
	comicCount := 0
	for _, entry := range bundle.RawMetadata {
		if entry.Kind == "comicinfo" {
			comicCount++
		}
	}
	if comicCount > 1 {
		return bundle, ErrMultipleComicInfo
	}
	if len(bundle.Images) == 0 {
		return bundle, errors.New("no images found in archive")
	}
	return bundle, nil
}
func metadataKind(name string) string {
	base := strings.ToLower(path.Base(strings.ReplaceAll(name, "\\", "/")))
	switch base {
	case "comicinfo.xml":
		return "comicinfo"
	case "metroninfo.xml", "comicbookinfo.json", "comicinfo.json", "comet.xml", "metadata.json":
		return "competing_metadata"
	}
	return ""
}
func bundleWarning(bundle *MetadataBundle, code, message string) {
	for _, existing := range bundle.Warnings {
		if existing.Code == code {
			return
		}
	}
	if len(bundle.Warnings) < metadata.MaxListItems {
		bundle.Warnings = append(bundle.Warnings, metadata.Warning{Code: code, Message: message})
	}
}
func readZipBundle(ctx context.Context, sourcePath string, limits ExtractorConfig, bundle *MetadataBundle) error {
	reader, err := zip.OpenReader(sourcePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	bundle.ZIPComment = []byte(reader.Comment)
	classifyComment(bundle)
	var total uint64
	metaTotal := int64(len(bundle.ZIPComment))
	for index, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() {
			bundleWarning(bundle, "entry_not_exported", "非普通文件未写入新包；完整原归档已保留。")
			continue
		}
		if file.UncompressedSize64 > uint64(limits.MaxTotalBytes)-total {
			return errors.New("archive total size limit exceeded")
		}
		total += file.UncompressedSize64
		kind := metadataKind(file.Name)
		if kind == "" {
			continue
		}
		if len(bundle.RawMetadata) >= 256 || file.UncompressedSize64 > uint64(MaxMetadataEntryBytes) || int64(file.UncompressedSize64) > MaxMetadataTotalBytes-metaTotal {
			return ErrMetadataLimit
		}
		r, e := file.Open()
		if e != nil {
			return e
		}
		data, e := readArchiveImage(ctx, r, min(MaxMetadataEntryBytes, MaxMetadataTotalBytes-metaTotal))
		closeErr := r.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		metaTotal += int64(len(data))
		bundle.RawMetadata = append(bundle.RawMetadata, MetadataEntry{Name: file.Name, EntryIndex: index, Kind: kind, Data: data})
	}
	for index, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if file.FileInfo().IsDir() || !file.Mode().IsRegular() {
			continue
		}
		if !isImagePath(file.Name) {
			if metadataKind(file.Name) == "" {
				bundleWarning(bundle, "entry_not_exported", "非图片附件未写入新包；完整原归档已保留。")
			}
			continue
		}
		if len(bundle.Images) >= limits.MaxImages {
			return errors.New("archive image count limit exceeded")
		}
		if file.UncompressedSize64 > uint64(limits.MaxImageBytes) {
			return errors.New("archive image size limit exceeded")
		}
		r, e := file.Open()
		if e != nil {
			return e
		}
		data, e := readArchiveImage(ctx, r, limits.MaxImageBytes)
		closeErr := r.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		bundle.Pages = append(bundle.Pages, PageIdentity{EntryName: file.Name, EntryIndex: index, SourceIndex: len(bundle.Images)})
		bundle.Images = append(bundle.Images, ExtractedImage{Name: cleanEntry(file.Name), ContentType: contentTypeForPath(file.Name), Data: data})
	}
	return nil
}
func cleanEntry(name string) string { return path.Clean(strings.ReplaceAll(name, "\\", "/")) }
func readExternalBundle(ctx context.Context, sourcePath string, limits ExtractorConfig, bundle *MetadataBundle) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bsdtar", "-cf", "-", "--format=pax", "@"+sourcePath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	readErr := readTarBundle(ctx, stdout, limits, bundle)
	if readErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return readErr
	}
	return waitErr
}
func readTarBundle(ctx context.Context, stream io.Reader, limits ExtractorConfig, bundle *MetadataBundle) error {
	reader := tar.NewReader(contextReader{ctx: ctx, reader: stream})
	var total, metaTotal int64
	index := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		currentIndex := index
		index++
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			if header.Typeflag != tar.TypeDir {
				bundleWarning(bundle, "entry_not_exported", "非普通文件未写入新包；完整原归档已保留。")
			}
			continue
		}
		if header.Size < 0 || header.Size > limits.MaxTotalBytes-total {
			return errors.New("archive total size limit exceeded")
		}
		total += header.Size
		kind := metadataKind(header.Name)
		if kind != "" {
			if len(bundle.RawMetadata) >= 256 || header.Size > MaxMetadataEntryBytes || header.Size > MaxMetadataTotalBytes-metaTotal {
				return ErrMetadataLimit
			}
			data, err := readArchiveImage(ctx, reader, min(MaxMetadataEntryBytes, MaxMetadataTotalBytes-metaTotal))
			if err != nil {
				return err
			}
			metaTotal += int64(len(data))
			bundle.RawMetadata = append(bundle.RawMetadata, MetadataEntry{Name: header.Name, EntryIndex: currentIndex, Kind: kind, Data: data})
			continue
		}
		if !isImagePath(header.Name) {
			bundleWarning(bundle, "entry_not_exported", "非图片附件未写入新包；完整原归档已保留。")
			continue
		}
		if len(bundle.Images) >= limits.MaxImages {
			return errors.New("archive image count limit exceeded")
		}
		if header.Size > limits.MaxImageBytes {
			return errors.New("archive image size limit exceeded")
		}
		data, err := readArchiveImage(ctx, reader, limits.MaxImageBytes)
		if err != nil {
			return err
		}
		bundle.Pages = append(bundle.Pages, PageIdentity{EntryName: header.Name, EntryIndex: currentIndex, SourceIndex: len(bundle.Images)})
		bundle.Images = append(bundle.Images, ExtractedImage{Name: cleanEntry(header.Name), ContentType: contentTypeForPath(header.Name), Data: data})
	}
}
func finishBundle(bundle *MetadataBundle) {
	order := make([]int, len(bundle.Images))
	seen := map[string]bool{}
	for i, image := range bundle.Images {
		order[i] = i
		if seen[image.Name] {
			bundle.PageOrderKnown = false
		}
		seen[image.Name] = true
	}
	sort.SliceStable(order, func(i, j int) bool {
		return naturalNameLess(bundle.Images[order[i]].Name, bundle.Images[order[j]].Name)
	})
	images := make([]ExtractedImage, len(order))
	pages := make([]PageIdentity, len(order))
	for output, input := range order {
		images[output] = bundle.Images[input]
		pages[output] = bundle.Pages[input]
		pages[output].OutputIndex = output
		if output != input {
			bundle.PageOrderKnown = false
		}
	}
	bundle.Images, bundle.Pages = images, pages
	for _, entry := range bundle.RawMetadata {
		if entry.Kind == "comicinfo" && entry.Name != "ComicInfo.xml" {
			bundleWarning(bundle, "comicinfo_normalized", "原 ComicInfo 将规范化为根目录唯一文件。")
		}
		if entry.Kind == "competing_metadata" {
			bundleWarning(bundle, "competing_metadata_retained", "其他格式元数据仅保留在私密副本中，避免与新 ComicInfo 冲突。")
		}
	}
}
func classifyComment(bundle *MetadataBundle) {
	if len(bundle.ZIPComment) == 0 {
		return
	}
	text := strings.TrimSpace(string(bundle.ZIPComment))
	lower := strings.ToLower(text)
	ambiguous := !utf8.Valid(bundle.ZIPComment) || strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) || strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") || strings.HasPrefix(text, "<") || strings.Contains(lower, "comicinfo") || strings.Contains(lower, "comicbookinfo") || strings.Contains(lower, "metron")
	if ambiguous {
		bundleWarning(bundle, "zip_comment_retained", "原 ZIP 注释可能包含另一种元数据，仅保留在私密副本中。")
	} else {
		bundle.OutputComment = string(bundle.ZIPComment)
	}
}
