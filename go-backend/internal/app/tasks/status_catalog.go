package tasks

func Catalog() map[string]StatusMeta {
	return map[string]StatusMeta{
		"QUEUED": {
			Code:      "QUEUED",
			Label:     "排队中",
			CanCancel: true,
		},
		"RUNNING": {
			Code:      "RUNNING",
			Label:     "进行中",
			CanCancel: true,
		},
		"CANCEL_REQUESTED": {
			Code:      "CANCEL_REQUESTED",
			Label:     "取消中",
			CanCancel: true,
		},
		"SUCCESS": {
			Code:        "SUCCESS",
			Label:       "已完成",
			CanDownload: true,
		},
		"FAILED": {
			Code:  "FAILED",
			Label: "失败",
		},
		"CANCELED": {
			Code:  "CANCELED",
			Label: "已取消",
		},
	}
}
