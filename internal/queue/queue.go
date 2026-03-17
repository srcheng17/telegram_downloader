package queue

import "context"

type EnqueueMessage struct {
	TaskID       string
	EnqueueToken string
}

type DownloadQueue interface {
	EnqueueDownload(ctx context.Context, msg EnqueueMessage) error
}
