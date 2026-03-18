package domain

const (
	StatusUploading       = "UPLOADING"
	StatusPending         = "PENDING"
	StatusInProgress      = "IN_PROGRESS"
	StatusCancelRequested = "CANCEL_REQUESTED"
	StatusCanceled        = "CANCELED"
	StatusSuccess         = "SUCCESS"
	StatusFailed          = "FAILED"
)

var knownStatuses = map[string]struct{}{
	StatusUploading:       {},
	StatusPending:         {},
	StatusInProgress:      {},
	StatusCancelRequested: {},
	StatusCanceled:        {},
	StatusSuccess:         {},
	StatusFailed:          {},
}

var ActiveTaskStatuses = []string{
	StatusUploading,
	StatusPending,
	StatusInProgress,
	StatusCancelRequested,
}

var StatusCatalog = map[string]StatusMeta{
	StatusUploading: {
		Label:       "上传中",
		CanCancel:   true,
		CanDownload: false,
		Terminal:    false,
	},
	StatusPending: {
		Label:       "等待中",
		CanCancel:   true,
		CanDownload: false,
		Terminal:    false,
	},
	StatusInProgress: {
		Label:       "下载中",
		CanCancel:   true,
		CanDownload: false,
		Terminal:    false,
	},
	StatusCancelRequested: {
		Label:       "取消中",
		CanCancel:   false,
		CanDownload: false,
		Terminal:    false,
	},
	StatusCanceled: {
		Label:       "已取消",
		CanCancel:   false,
		CanDownload: false,
		Terminal:    true,
	},
	StatusSuccess: {
		Label:       "成功",
		CanCancel:   false,
		CanDownload: true,
		Terminal:    true,
	},
	StatusFailed: {
		Label:       "失败",
		CanCancel:   false,
		CanDownload: false,
		Terminal:    true,
	},
}

const (
	DefaultLogsPerPage = 25
	MaxLogsPerPage     = 100
)

func IsKnownStatus(status string) bool {
	_, ok := knownStatuses[status]
	return ok
}

func CopyStatusCatalog() map[string]StatusMeta {
	catalog := make(map[string]StatusMeta, len(StatusCatalog))
	for code, meta := range StatusCatalog {
		catalog[code] = meta
	}
	return catalog
}
