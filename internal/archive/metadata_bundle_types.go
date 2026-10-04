package archive

import (
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

const MaxMetadataEntryBytes int64 = 1 << 20
const MaxMetadataTotalBytes int64 = 8 << 20

// PageIdentity keeps duplicate paths distinct using the original entry index.
// SourceIndex is the original image enumeration, OutputIndex its natural order.
type PageIdentity struct {
	EntryName   string `json:"entry_name"`
	EntryIndex  int    `json:"entry_index"`
	SourceIndex int    `json:"source_index"`
	OutputIndex int    `json:"output_index"`
}
type MetadataEntry struct {
	Name       string
	EntryIndex int
	Kind       string
	Data       []byte
}
type MetadataBundle struct {
	SourcePath     string
	Images         []ExtractedImage
	Pages          []PageIdentity
	RawMetadata    []MetadataEntry
	ZIPComment     []byte
	OutputComment  string
	Warnings       []metadata.Warning
	RetainSource   bool
	PageOrderKnown bool
}
type RetainedFile = taskcore.RetainedFile

// Relative names only. The caller binds this manifest to task/generation under
// the separate private retention root, never a comic download route.
type RetentionManifest = taskcore.RetentionManifest
