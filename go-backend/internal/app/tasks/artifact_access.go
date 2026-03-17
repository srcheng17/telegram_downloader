package tasks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrArtifactUnavailable = errors.New("artifact unavailable")
	ErrArtifactPathInvalid = errors.New("artifact path is invalid")
)

type ArtifactAccessConfig struct {
	DownloadRoot string
}

type ArtifactAccess struct {
	downloadRoot string
}

type OpenedArtifact struct {
	File        *os.File
	FileName    string
	ContentType string
	ModTime     time.Time
}

func (a *OpenedArtifact) Close() error {
	if a == nil || a.File == nil {
		return nil
	}
	return a.File.Close()
}

func NewArtifactAccess(cfg ArtifactAccessConfig) *ArtifactAccess {
	return &ArtifactAccess{downloadRoot: strings.TrimSpace(cfg.DownloadRoot)}
}

func (a *ArtifactAccess) Open(resultZipPath string) (*OpenedArtifact, error) {
	rootAbs, safePath, err := a.resolveSafePath(resultZipPath)
	if err != nil {
		return nil, err
	}

	containmentRoot := rootAbs
	resolvedRoot, rootErr := filepath.EvalSymlinks(rootAbs)
	if rootErr == nil {
		containmentRoot = resolvedRoot
	}

	resolvedPath, err := filepath.EvalSymlinks(safePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrArtifactUnavailable
		}
		return nil, ErrArtifactPathInvalid
	}
	if !isWithinArtifactRoot(containmentRoot, resolvedPath) {
		return nil, ErrArtifactPathInvalid
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		return nil, ErrArtifactUnavailable
	}

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		_ = file.Close()
		return nil, ErrArtifactUnavailable
	}

	return &OpenedArtifact{
		File:        file,
		FileName:    filepath.Base(resolvedPath),
		ContentType: artifactContentType(resolvedPath),
		ModTime:     info.ModTime(),
	}, nil
}

func (a *ArtifactAccess) resolveSafePath(resultZipPath string) (string, string, error) {
	raw := strings.TrimSpace(resultZipPath)
	if raw == "" {
		return "", "", ErrArtifactUnavailable
	}
	if strings.ContainsRune(raw, rune(0)) {
		return "", "", ErrArtifactPathInvalid
	}

	rootAbs, err := filepath.Abs(a.downloadRootOrDefault())
	if err != nil {
		return "", "", ErrArtifactPathInvalid
	}

	candidatePath := filepath.Clean(raw)
	if candidatePath == "." {
		return "", "", ErrArtifactPathInvalid
	}
	if !filepath.IsAbs(candidatePath) {
		candidatePath = normalizeRootPrefixedRelativeArtifactPath(rootAbs, candidatePath)
		candidatePath = filepath.Join(rootAbs, candidatePath)
	}

	candidateAbs, err := filepath.Abs(candidatePath)
	if err != nil {
		return "", "", ErrArtifactPathInvalid
	}
	if !isWithinArtifactRoot(rootAbs, candidateAbs) {
		return "", "", ErrArtifactPathInvalid
	}

	return rootAbs, candidateAbs, nil
}

func normalizeRootPrefixedRelativeArtifactPath(rootAbs, candidatePath string) string {
	rootName := strings.TrimSpace(filepath.Base(rootAbs))
	if rootName == "" || rootName == "." || rootName == string(filepath.Separator) {
		return candidatePath
	}
	if candidatePath == rootName {
		return ""
	}
	prefix := rootName + string(filepath.Separator)
	if strings.HasPrefix(candidatePath, prefix) {
		return strings.TrimPrefix(candidatePath, prefix)
	}
	return candidatePath
}

func isWithinArtifactRoot(rootPath, candidatePath string) bool {
	rel, err := filepath.Rel(rootPath, candidatePath)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}

func (a *ArtifactAccess) downloadRootOrDefault() string {
	if strings.TrimSpace(a.downloadRoot) != "" {
		return strings.TrimSpace(a.downloadRoot)
	}
	if configured := strings.TrimSpace(os.Getenv("DOWNLOAD_PATH")); configured != "" {
		return configured
	}
	return "downloaded_images"
}

func artifactContentType(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".cbz":
		return "application/vnd.comicbook+zip"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}
