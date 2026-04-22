package taskcore

import "testing"

func TestProgressLabelsAreStableForFrontend(t *testing.T) {
	t.Parallel()

	cases := []struct {
		phase Phase
		want  string
	}{
		{phase: PhaseUploading, want: "上传中"},
		{phase: PhasePreparing, want: "准备中"},
		{phase: PhaseDownloading, want: "下载中"},
		{phase: PhasePackaging, want: "打包中"},
		{phase: PhaseCopying, want: "复制中"},
		{phase: PhaseDone, want: "已完成"},
	}

	for _, tc := range cases {
		if got := tc.phase.Label(); got != tc.want {
			t.Fatalf("phase %s label = %q, want %q", tc.phase, got, tc.want)
		}
	}
}

func TestProgressSnapshotNormalizesNegativeNumbers(t *testing.T) {
	t.Parallel()

	snapshot := NewProgress(PhaseDownloading, -1, -9, UnitImages, "  preparing  ")
	if snapshot.Current != 0 || snapshot.Total != 0 {
		t.Fatalf("negative progress should normalize to zero, got %d/%d", snapshot.Current, snapshot.Total)
	}
	if snapshot.Message != "preparing" {
		t.Fatalf("message should be trimmed, got %q", snapshot.Message)
	}
}
