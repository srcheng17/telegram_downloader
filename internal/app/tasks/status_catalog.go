package tasks

func Catalog() map[string]StatusMeta {
	return map[string]StatusMeta{
		StatusQueued: {
			Code:      StatusQueued,
			Label:     "排队中",
			CanCancel: true,
		},
		StatusRunning: {
			Code:      StatusRunning,
			Label:     "进行中",
			CanCancel: true,
		},
		StatusCancelRequested: {
			Code:      StatusCancelRequested,
			Label:     "取消中",
			CanCancel: true,
		},
		StatusSuccess: {
			Code:        StatusSuccess,
			Label:       "已完成",
			CanDownload: true,
		},
		StatusFailed: {
			Code:  StatusFailed,
			Label: "失败",
		},
		StatusCanceled: {
			Code:  StatusCanceled,
			Label: "已取消",
		},
	}
}
