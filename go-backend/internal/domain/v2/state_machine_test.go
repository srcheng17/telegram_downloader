package v2

import "testing"

func TestTransitionAllowsQueuedToRunning(t *testing.T) {
	if !CanTransition(StatusQueued, StatusRunning) {
		t.Fatalf("expected queued -> running to be allowed")
	}
}

func TestTransitionRejectsRunningToQueued(t *testing.T) {
	if CanTransition(StatusRunning, StatusQueued) {
		t.Fatalf("expected running -> queued to be rejected")
	}
}
