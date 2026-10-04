package downloader

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ryancheng/telegram-downloader/internal/archive"
	"github.com/ryancheng/telegram-downloader/internal/comicinfo"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/domain"
)

type metadataPackHooks struct {
	wrapWriter     func(io.Writer) io.Writer
	closeFile      func(*os.File) error
	beforeValidate func(string) error
}

// MetadataLocalImages shares the download pipeline's existing URL/content-type
// extension decision; no private source URL is used as an archive entry name.
func MetadataLocalImages(images []domain.DownloadedImage) []LocalImage {
	local := make([]LocalImage, 0, len(images))
	for index, image := range images {
		local = append(local, LocalImage{Name: fmt.Sprintf("%04d%s", index+1, imageArchiveExtension(image.URL, image.ContentType)), Data: image.Data})
	}
	return local
}

func PackageMetadataCBZ(ctx context.Context, input MetadataPackInput) (MetadataPackResult, error) {
	return packageMetadataCBZ(ctx, input, metadataPackHooks{})
}
func packageMetadataCBZ(ctx context.Context, input MetadataPackInput, hooks metadataPackHooks) (MetadataPackResult, error) {
	result := MetadataPackResult{Profile: comicinfo.Profile}
	manifest, err := archive.RetainMetadataBundle(ctx, input.Bundle, input.PrivateDir)
	result.RetentionManifest = manifest
	if err != nil {
		return result, err
	}
	images := input.Images
	var raw []byte
	var mapping []int
	if input.Bundle != nil {
		bundle := input.Bundle
		result.Warnings = append(result.Warnings, bundle.Warnings...)
		if len(images) == 0 {
			for _, image := range bundle.Images {
				images = append(images, LocalImage{Name: image.Name, Data: image.Data})
			}
		}
		if len(bundle.Images) > 0 {
			if len(images) != len(bundle.Images) {
				return result, errors.New("archive image count changed before packaging")
			}
			for i, image := range images {
				if !bytes.Equal(image.Data, bundle.Images[i].Data) {
					return result, errors.New("archive image bytes changed before packaging")
				}
			}
		}
		count := 0
		for _, entry := range bundle.RawMetadata {
			if entry.Kind == "comicinfo" {
				count++
				raw = entry.Data
			}
		}
		if count > 1 {
			return result, archive.ErrMultipleComicInfo
		}
		if bundle.PageOrderKnown && len(bundle.Pages) == len(images) {
			mapping = make([]int, len(images))
			for _, page := range bundle.Pages {
				if page.SourceIndex < 0 || page.SourceIndex >= len(images) || page.OutputIndex < 0 || page.OutputIndex >= len(images) {
					return result, errors.New("invalid source page identity")
				}
				mapping[page.SourceIndex] = page.OutputIndex
			}
		}
	}
	if len(images) == 0 || len(images) > config.MaxImagesPerTask {
		return result, errors.New("invalid packaging image count")
	}
	var total int64
	for _, image := range images {
		if len(image.Data) == 0 || int64(len(image.Data)) > config.MaxBytesPerImage {
			return result, errors.New("packaging image size limit exceeded")
		}
		total += int64(len(image.Data))
		if total > config.MaxBytesPerTask {
			return result, errors.New("packaging total size limit exceeded")
		}
	}
	merged, err := comicinfo.Merge(raw, input.Document, input.Registry, mapping, len(images))
	if err != nil {
		return result, err
	}
	result.EffectiveDocument = merged.EffectiveDocument
	result.Warnings = append(result.Warnings, merged.Warnings...)
	result.PreservedStandard = merged.PreservedStandard
	privateData, err := json.Marshal(struct {
		Profile           string   `json:"profile"`
		Document          any      `json:"document"`
		Warnings          any      `json:"warnings"`
		PreservedStandard []string `json:"preserved_standard"`
	}{merged.Profile, merged.EffectiveDocument, result.Warnings, result.PreservedStandard})
	if err != nil {
		return result, err
	}
	manifest, err = archive.RetainEffectiveMetadata(ctx, input.PrivateDir, privateData, manifest)
	result.RetentionManifest = manifest
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(input.OutputPath) == "" {
		return result, errors.New("cbz output path is required")
	}
	if _, err = os.Lstat(input.OutputPath); !errors.Is(err, os.ErrNotExist) {
		return result, errors.New("refusing to replace an existing artifact")
	}
	directory := filepath.Dir(input.OutputPath)
	if err = os.MkdirAll(directory, 0755); err != nil {
		return result, err
	}
	file, err := os.CreateTemp(directory, ".metadata-cbz-*.tmp")
	if err != nil {
		return result, err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	var writer io.Writer = file
	if hooks.wrapWriter != nil {
		writer = hooks.wrapWriter(writer)
	}
	archiveWriter := zip.NewWriter(writer)
	if input.Bundle != nil && input.Bundle.OutputComment != "" {
		if err = archiveWriter.SetComment(input.Bundle.OutputComment); err != nil {
			file.Close()
			return result, err
		}
	}
	write := func(name string, data []byte) error {
		member, err := archiveWriter.Create(name)
		if err != nil {
			return err
		}
		_, err = io.Copy(member, metadataContextReader{ctx, bytes.NewReader(data)})
		return err
	}
	if err = write("ComicInfo.xml", merged.XML); err != nil {
		archiveWriter.Close()
		file.Close()
		return result, err
	}
	names := make([]string, len(images))
	for i, image := range images {
		names[i] = metadataPageName(image.Name, i)
		if err = write(names[i], image.Data); err != nil {
			archiveWriter.Close()
			file.Close()
			return result, err
		}
	}
	if err = archiveWriter.Close(); err != nil {
		file.Close()
		return result, err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return result, err
	}
	if hooks.closeFile != nil {
		err = hooks.closeFile(file)
	} else {
		err = file.Close()
	}
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if hooks.beforeValidate != nil {
		if err = hooks.beforeValidate(temporary); err != nil {
			return result, err
		}
	}
	if err = validateMetadataCBZ(ctx, temporary, merged.XML, images, names, input.Bundle); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	// Same-directory hard-link publication is atomic and, unlike plain Rename,
	// refuses to overwrite any artifact published by another execution.
	if err = os.Link(temporary, input.OutputPath); err != nil {
		return result, err
	}
	publishedDir, err := os.Open(directory)
	if err == nil {
		err = publishedDir.Sync()
		closeErr := publishedDir.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		_ = os.Remove(input.OutputPath)
		return result, err
	}
	return result, nil
}

type metadataContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r metadataContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
func metadataPageName(name string, index int) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".avif":
	default:
		ext = ".jpg"
	}
	return fmt.Sprintf("%04d%s", index+1, ext)
}
func validateMetadataCBZ(ctx context.Context, path string, expectedXML []byte, images []LocalImage, names []string, bundle *archive.MetadataBundle) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	if len(reader.File) != len(images)+1 {
		return errors.New("packaged entry count mismatch")
	}
	comment := ""
	if bundle != nil {
		comment = bundle.OutputComment
	}
	if reader.Comment != comment {
		return errors.New("packaged comment mismatch")
	}
	seen := map[string]bool{}
	for i, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if seen[file.Name] {
			return errors.New("duplicate packaged entry")
		}
		seen[file.Name] = true
		expected := expectedXML
		name := "ComicInfo.xml"
		if i > 0 {
			expected = images[i-1].Data
			name = names[i-1]
		}
		if file.Name != name || file.UncompressedSize64 != uint64(len(expected)) {
			return errors.New("packaged page identity mismatch")
		}
		member, err := file.Open()
		if err != nil {
			return err
		}
		hash := sha256.New()
		size, readErr := io.Copy(hash, io.LimitReader(metadataContextReader{ctx, member}, int64(len(expected))+1))
		closeErr := member.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		expectedHash := sha256.Sum256(expected)
		if size != int64(len(expected)) || !bytes.Equal(hash.Sum(nil), expectedHash[:]) {
			return errors.New("packaged page hash mismatch")
		}
	}
	parsed, err := comicinfo.Parse(expectedXML)
	if err != nil || len(parsed.Warnings) > 0 {
		return errors.New("packaged ComicInfo profile readback failed")
	}
	return nil
}
