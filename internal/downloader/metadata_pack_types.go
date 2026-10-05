package downloader

import (
	"github.com/ryancheng/telegram-downloader/internal/archive"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type MetadataPackInput struct {
	Images     []LocalImage
	Bundle     *archive.MetadataBundle
	Document   metadata.Document
	Registry   metadata.Registry
	OutputPath string
	PrivateDir string
}
type MetadataPackResult struct {
	EffectiveDocument metadata.Document         `json:"effective_metadata_document"`
	Profile           string                    `json:"profile"`
	Warnings          []metadata.Warning        `json:"warnings"`
	PreservedStandard []string                  `json:"preserved_standard,omitempty"`
	RetentionManifest archive.RetentionManifest `json:"retention_manifest"`
}
