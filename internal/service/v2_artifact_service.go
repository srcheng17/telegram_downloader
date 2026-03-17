package service

import (
	"errors"
	"os"
	"time"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
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
	access       *apptasks.ArtifactAccess
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
	return &V2ArtifactService{
		downloadRoot: cfg.DownloadRoot,
		access: apptasks.NewArtifactAccess(apptasks.ArtifactAccessConfig{
			DownloadRoot: cfg.DownloadRoot,
		}),
	}
}

func (s *V2ArtifactService) OpenArtifact(resultZipPath string) (*OpenedV2Artifact, error) {
	if s == nil || s.access == nil {
		return nil, ErrV2ArtifactPathInvalid
	}

	artifact, err := s.access.Open(resultZipPath)
	if err != nil {
		switch {
		case errors.Is(err, apptasks.ErrArtifactUnavailable):
			return nil, ErrV2ArtifactNotFound
		case errors.Is(err, apptasks.ErrArtifactPathInvalid):
			return nil, ErrV2ArtifactPathInvalid
		default:
			return nil, err
		}
	}

	return &OpenedV2Artifact{
		File:        artifact.File,
		FileName:    artifact.FileName,
		ContentType: artifact.ContentType,
		ModTime:     artifact.ModTime,
	}, nil
}
