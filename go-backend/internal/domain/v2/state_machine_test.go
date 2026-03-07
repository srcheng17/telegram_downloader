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

func TestTransitionAllowsRunningToCancelRequested(t *testing.T) {
	if !CanTransition(StatusRunning, StatusCancelRequested) {
		t.Fatalf("expected running -> cancel_requested to be allowed")
	}
}

func TestTransitionAllowsCancelRequestedToCanceled(t *testing.T) {
	if !CanTransition(StatusCancelRequested, StatusCanceled) {
		t.Fatalf("expected cancel_requested -> canceled to be allowed")
	}
}
