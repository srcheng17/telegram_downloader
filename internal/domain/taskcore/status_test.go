package taskcore

import "testing"

func TestNormalizeStatusTrimsAndUppercases(t *testing.T) {
	t.Parallel()

	if got := NormalizeStatus("  succeeded \n"); got != StatusSucceeded {
		t.Fatalf("NormalizeStatus() = %q, want %q", got, StatusSucceeded)
	}
}

func TestNormalizeKindTrimsAndLowercases(t *testing.T) {
	t.Parallel()

	if got := NormalizeKind("  UPLOAD \t"); got != KindUpload {
		t.Fatalf("NormalizeKind() = %q, want %q", got, KindUpload)
	}
}

func TestIsTerminal(t *testing.T) {
	t.Parallel()

	terminal := []Status{StatusSucceeded, StatusFailed, StatusCanceled}
	for _, status := range terminal {
		if !IsTerminal(status) {
			t.Fatalf("IsTerminal(%q) = false, want true", status)
		}
	}

	nonTerminal := []Status{StatusCreated, StatusReady, StatusRunning, StatusCanceling}
	for _, status := range nonTerminal {
		if IsTerminal(status) {
			t.Fatalf("IsTerminal(%q) = true, want false", status)
		}
	}
}

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
		{name: "expired lease failure", from: StatusRunning, to: StatusFailed, actor: ActorRecovery},
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

	cancelable := []Status{StatusCreated, StatusReady, StatusRunning}
	for _, status := range cancelable {
		if !CanCancel(status) {
			t.Fatalf("CanCancel(%q) = false, want true", status)
		}
	}
	for _, status := range []Status{StatusSucceeded, StatusFailed, StatusCanceled} {
		if CanCancel(status) {
			t.Fatalf("CanCancel(%q) = true, want false", status)
		}
	}

	if !CanRetry(StatusFailed, true) || !CanRetry(StatusCanceled, true) {
		t.Fatalf("failed and canceled tasks must be retryable")
	}
	if CanRetry(StatusSucceeded, true) || CanRetry(StatusReady, true) || CanRetry(StatusRunning, true) || CanRetry(StatusCanceling, true) || CanRetry(StatusCreated, true) {
		t.Fatalf("only failed and canceled tasks must be retryable")
	}

	actions := AvailableActions(StatusSucceeded, true, true, true)
	want := []Action{ActionDownload, ActionCopyToKomga}
	if len(actions) != len(want) {
		t.Fatalf("AvailableActions(succeeded, hasResult=true, komgaConfigured=true) len = %d, want %d", len(actions), len(want))
	}
	for i, action := range want {
		if actions[i] != action {
			t.Fatalf("AvailableActions(succeeded, hasResult=true, komgaConfigured=true)[%d] = %q, want %q", i, actions[i], action)
		}
	}

	actions = AvailableActions(StatusSucceeded, true, true, false)
	want = []Action{ActionDownload}
	if len(actions) != len(want) {
		t.Fatalf("AvailableActions(succeeded, hasResult=true, komgaConfigured=false) len = %d, want %d", len(actions), len(want))
	}
	for i, action := range want {
		if actions[i] != action {
			t.Fatalf("AvailableActions(succeeded, hasResult=true, komgaConfigured=false)[%d] = %q, want %q", i, actions[i], action)
		}
	}

	if got := AvailableActions(StatusSucceeded, true, false, true); len(got) != 0 {
		t.Fatalf("AvailableActions(succeeded, hasResult=false, komgaConfigured=true) = %v, want no result actions", got)
	}

	if got := AvailableActions(StatusReady, false, false, true); len(got) != 1 || got[0] != ActionCancel {
		t.Fatalf("AvailableActions(ready, hasResult=false, komgaConfigured=true) = %v, want [cancel]", got)
	}
}

func TestRetryRequiresSource(t *testing.T) {
	t.Parallel()
	for _, status := range []Status{StatusFailed, StatusCanceled} {
		if CanRetry(status, false) {
			t.Fatalf("CanRetry(%s, false) = true", status)
		}
		if got := AvailableActions(status, false, false, false); len(got) != 0 {
			t.Fatalf("AvailableActions(%s, false, false, false) = %v, want none", status, got)
		}
	}
}
