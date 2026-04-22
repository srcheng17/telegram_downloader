package taskcore

import "strings"

type Phase string

type Unit string

const (
	PhaseUploading   Phase = "uploading"
	PhasePreparing   Phase = "preparing"
	PhaseDownloading Phase = "downloading"
	PhasePackaging   Phase = "packaging"
	PhaseCopying     Phase = "copying"
	PhaseDone        Phase = "done"
)

const (
	UnitNone   Unit = "none"
	UnitBytes  Unit = "bytes"
	UnitImages Unit = "images"
	UnitFiles  Unit = "files"
	UnitSteps  Unit = "steps"
)

type Progress struct {
	Phase   Phase
	Current int64
	Total   int64
	Unit    Unit
	Message string
}

func NewProgress(phase Phase, current int64, total int64, unit Unit, message string) Progress {
	if current < 0 {
		current = 0
	}
	if total < 0 {
		total = 0
	}
	return Progress{
		Phase:   phase,
		Current: current,
		Total:   total,
		Unit:    unit,
		Message: strings.TrimSpace(message),
	}
}

func (p Phase) Label() string {
	switch p {
	case PhaseUploading:
		return "上传中"
	case PhasePreparing:
		return "准备中"
	case PhaseDownloading:
		return "下载中"
	case PhasePackaging:
		return "打包中"
	case PhaseCopying:
		return "复制中"
	case PhaseDone:
		return "已完成"
	default:
		return "处理中"
	}
}
