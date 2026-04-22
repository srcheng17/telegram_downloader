package taskcore

import "testing"

func TestTransitionAllowsApprovedLifecycle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		from  Status
		to    Status
		actor Actor
	}{
		{name: "upload source ready", from: StatusCreated, to: StatusReady, actor: ActorAPI},
		{name: "created cancel requested", from: StatusCreated, to: StatusCanceling, actor: ActorAPI},
		{name: "ready claimed", from: StatusReady, to: StatusRunning, actor: ActorWorker},
		{name: "ready failed by app", from: StatusReady, to: StatusFailed, actor: ActorAPI},
		{name: "running succeeded", from: StatusRunning, to: StatusSucceeded, actor: ActorWorker},
		{name: "running failed", from: StatusRunning, to: StatusFailed, actor: ActorWorker},
		{name: "running cancel requested", from: StatusRunning, to: StatusCanceling, actor: ActorAPI},
		{name: "cancel acknowledged", from: StatusCanceling, to: StatusCanceled, actor: ActorWorker},
		{name: "failed retry", from: StatusFailed, to: StatusReady, actor: ActorAPI},
		{name: "canceled retry", from: StatusCanceled, to: StatusReady, actor: ActorAPI},
		{name: "expired lease requeue", from: StatusRunning, to: StatusReady, actor: ActorRecovery},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTransition(tc.from, tc.to, tc.actor); err != nil {
				t.Fatalf("ValidateTransition(%s -> %s by %s) returned error: %v", tc.from, tc.to, tc.actor, err)
			}
		})
	}
}

func TestTransitionRejectsAmbiguousLifecycle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		from  Status
		to    Status
		actor Actor
	}{
		{name: "succeeded cannot retry", from: StatusSucceeded, to: StatusReady, actor: ActorAPI},
		{name: "failed cannot complete", from: StatusFailed, to: StatusSucceeded, actor: ActorWorker},
		{name: "worker cannot requeue running without recovery", from: StatusRunning, to: StatusReady, actor: ActorWorker},
		{name: "ready cannot directly succeed", from: StatusReady, to: StatusSucceeded, actor: ActorWorker},
		{name: "created cannot run", from: StatusCreated, to: StatusRunning, actor: ActorWorker},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTransition(tc.from, tc.to, tc.actor); err == nil {
				t.Fatalf("ValidateTransition(%s -> %s by %s) succeeded, want error", tc.from, tc.to, tc.actor)
			}
		})
	}
}

func TestActionsComeFromDomainStateAndArtifacts(t *testing.T) {
	t.Parallel()

	if !CanCancel(StatusReady) || !CanCancel(StatusRunning) || !CanCancel(StatusCreated) {
		t.Fatalf("created, ready, and running tasks must be cancelable")
	}
	if CanCancel(StatusSucceeded) || CanCancel(StatusFailed) || CanCancel(StatusCanceled) {
		t.Fatalf("terminal tasks must not be cancelable")
	}
	if !CanRetry(StatusFailed) || !CanRetry(StatusCanceled) {
		t.Fatalf("failed and canceled tasks must be retryable")
	}
	if CanRetry(StatusSucceeded) || CanRetry(StatusReady) {
		t.Fatalf("succeeded and active tasks must not be retryable")
	}
	if !CanAccessResult(StatusSucceeded, true) {
		t.Fatalf("succeeded task with result must allow result access")
	}
	if CanAccessResult(StatusSucceeded, false) || CanAccessResult(StatusFailed, true) {
		t.Fatalf("result access requires succeeded status and result")
	}
}
