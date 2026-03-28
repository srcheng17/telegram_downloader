package task

import "testing"

func TestIsTerminalStatusRecognizesFinishedStatuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status string
		want   bool
	}{
		{name: "success", status: StatusSuccess, want: true},
		{name: "failed", status: StatusFailed, want: true},
		{name: "canceled", status: StatusCanceled, want: true},
		{name: "running", status: StatusRunning, want: false},
		{name: "uploading", status: StatusUploading, want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsTerminalStatus(tc.status); got != tc.want {
				t.Fatalf("IsTerminalStatus(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

