package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrV2ArtifactNotFound    = errors.New("v2 artifact not found")
	ErrV2ArtifactPathInvalid = errors.New("v2 artifact path is invalid")
)

type V2ArtifactServiceConfig struct {
	DownloadRoot string
}

type V2ArtifactService struct {
	downloadRoot string
}

type OpenedV2Artifact struct {
	File        *os.File
	FileName    string
	ContentType string
	ModTime     time.Time
}

func (a *OpenedV2Artifact) Close() error {
	if a == nil || a.File == nil {
		return nil
	}
	return a.File.Close()
}

func NewV2ArtifactService(cfg V2ArtifactServiceConfig) *V2ArtifactService {
	return &V2ArtifactService{downloadRoot: strings.TrimSpace(cfg.DownloadRoot)}
}

func (s *V2ArtifactService) OpenArtifact(resultZipPath string) (*OpenedV2Artifact, error) {
	safePath, err := s.resolveSafePath(resultZipPath)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(safePath)
	if err != nil {
		return nil, ErrV2ArtifactNotFound
	}

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		_ = file.Close()
		return nil, ErrV2ArtifactNotFound
	}

	return &OpenedV2Artifact{
		File:        file,
		FileName:    filepath.Base(safePath),
		ContentType: artifactContentType(safePath),
		ModTime:     info.ModTime(),
	}, nil
}

func (s *V2ArtifactService) resolveSafePath(resultZipPath string) (string, error) {
	raw := strings.TrimSpace(resultZipPath)
	if raw == "" {
		return "", ErrV2ArtifactNotFound
	}
	if strings.ContainsRune(raw, rune(0)) {
		return "", ErrV2ArtifactPathInvalid
	}

	rootAbs, err := filepath.Abs(s.downloadRootOrDefault())
	if err != nil {
		return "", ErrV2ArtifactPathInvalid
	}

	candidatePath := filepath.Clean(raw)
	if candidatePath == "." {
		return "", ErrV2ArtifactPathInvalid
	}
	if !filepath.IsAbs(candidatePath) {
		candidatePath = filepath.Join(rootAbs, candidatePath)
	}

	candidateAbs, err := filepath.Abs(candidatePath)
	if err != nil {
		return "", ErrV2ArtifactPathInvalid
	}

	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return "", ErrV2ArtifactPathInvalid
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", ErrV2ArtifactPathInvalid
	}

	return candidateAbs, nil
}

func (s *V2ArtifactService) downloadRootOrDefault() string {
	if strings.TrimSpace(s.downloadRoot) != "" {
		return strings.TrimSpace(s.downloadRoot)
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
