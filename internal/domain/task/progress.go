package task

func IsPreparingURLProgress(status string, totalImages int) bool {
	return NormalizeStatus(status) == StatusRunning && totalImages <= 0
}

