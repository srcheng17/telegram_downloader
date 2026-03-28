package worker

import (
	domainnaming "github.com/ryancheng/telegram-downloader/internal/domain/naming"
	"github.com/ryancheng/telegram-downloader/internal/downloader"
)

func buildDownloadFilename(metadata downloader.TaskMetadata, unixTimestamp int64) string {
	return domainnaming.BuildCBZFileName(domainnaming.CBZFileNameInput{
		Author:    metadata.Writer,
		Series:    metadata.Series,
		Title:     metadata.Title,
		Timestamp: unixTimestamp,
	})
}
