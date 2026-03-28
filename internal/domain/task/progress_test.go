package task

import "testing"

func TestIsPreparingURLProgressBeforeDiscovery(t *testing.T) {
	t.Parallel()

	if !IsPreparingURLProgress(StatusRunning, 0) {
		t.Fatalf("expected running url task without totals to be preparing")
	}
	if IsPreparingURLProgress(StatusRunning, 3) {
		t.Fatalf("expected running url task with totals to stop preparing")
	}
	if IsPreparingURLProgress(StatusSuccess, 0) {
		t.Fatalf("expected non-running task to not be preparing")
	}
}

