package tasks

import (
	domainmetadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

func NormalizeMetadata(input MetadataInput) Metadata {
	return Metadata{
		Author:           domainmetadata.NormalizeAuthorListPtr(input.Author),
		SeriesName:       domainmetadata.NormalizeOptionalTextPtr(input.SeriesName),
		ComicName:        domainmetadata.NormalizeOptionalTextPtr(input.ComicName),
		Summary:          domainmetadata.NormalizeOptionalTextPtr(input.Summary),
		TagsRaw:          domainmetadata.NormalizeOptionalTextPtr(input.TagsRaw),
		TagsNormalized:   domainmetadata.NormalizeTagLikeListPtr(input.TagsRaw),
		GenresRaw:        domainmetadata.NormalizeOptionalTextPtr(input.GenresRaw),
		GenresNormalized: domainmetadata.NormalizeTagLikeListPtr(input.GenresRaw),
	}
}
