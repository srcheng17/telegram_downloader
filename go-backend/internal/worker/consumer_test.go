package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
)

func TestConsumerClaimsAndAcksMessageOnSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream := &fakeStreamClient{
		messages: []Message{{
			ID:           "1-0",
			TaskID:       "task-1",
			EnqueueToken: "token-1",
		}},
	}
	store := &fakeTaskStore{defaultTransition: true}

	consumer := NewConsumer(ConsumerConfig{
		Stream:   stream,
		Store:    store,
		Group:    "go-workers",
		Consumer: "worker-1",
		Block:    time.Millisecond,
		Handler: func(_ context.Context, msg Message) error {
			if msg.TaskID != "task-1" {
				t.Fatalf("unexpected task id passed to handler: %q", msg.TaskID)
			}
			cancel()
			return nil
		},
	})

	err := consumer.Run(ctx)
	if err != nil {
		t.Fatalf("consumer run: %v", err)
	}

	if len(store.claimCalls) != 1 {
		t.Fatalf("expected 1 claim call, got %d", len(store.claimCalls))
	}
	claim := store.claimCalls[0]
	if claim.taskID != "task-1" {
		t.Fatalf("expected claimed task_id task-1, got %q", claim.taskID)
	}
	if claim.token != "token-1" {
		t.Fatalf("expected claimed token token-1, got %q", claim.token)
	}
	if claim.worker != "worker-1" {
		t.Fatalf("expected claimed worker worker-1, got %q", claim.worker)
	}

	if len(stream.ackCalls) != 1 {
		t.Fatalf("expected 1 ack call, got %d", len(stream.ackCalls))
	}
	ack := stream.ackCalls[0]
	if ack.group != "go-workers" {
		t.Fatalf("expected ack group go-workers, got %q", ack.group)
	}
	if len(ack.ids) != 1 || ack.ids[0] != "1-0" {
		t.Fatalf("expected ack id 1-0, got %#v", ack.ids)
	}
}

func TestConsumerHandlerErrorAttemptsFailTransitionAndAckThenContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream := &fakeStreamClient{
		messages: []Message{
			{ID: "1-0", TaskID: "task-1", EnqueueToken: "token-1"},
			{ID: "2-0", TaskID: "task-2", EnqueueToken: "token-2"},
		},
	}
	store := &fakeTaskStore{defaultTransition: true}

	handlerCalls := 0
	consumer := NewConsumer(ConsumerConfig{
		Stream:   stream,
		Store:    store,
		Group:    "go-workers",
		Consumer: "worker-1",
		Block:    time.Millisecond,
		Handler: func(_ context.Context, msg Message) error {
			handlerCalls++
			if msg.ID == "1-0" {
				return errors.New("handler boom")
			}
			cancel()
			return nil
		},
	})

	err := consumer.Run(ctx)
	if err != nil {
		t.Fatalf("consumer run: %v", err)
	}

	if handlerCalls != 2 {
		t.Fatalf("expected handler to run for both messages, got %d calls", handlerCalls)
	}
	if len(store.terminalCalls) != 1 {
		t.Fatalf("expected one terminal transition attempt, got %d", len(store.terminalCalls))
	}
	terminal := store.terminalCalls[0]
	if terminal.TaskID != "task-1" {
		t.Fatalf("expected task-1 failed transition, got %q", terminal.TaskID)
	}
	if terminal.Worker != "worker-1" {
		t.Fatalf("expected worker-1 in terminal transition, got %q", terminal.Worker)
	}
	if terminal.Status != domain.StatusFailed {
		t.Fatalf("expected status FAILED, got %q", terminal.Status)
	}
	if terminal.Error == nil || !strings.Contains(*terminal.Error, "handler boom") {
		t.Fatalf("expected failure error to contain handler boom, got %#v", terminal.Error)
	}

	if len(stream.ackCalls) != 2 {
		t.Fatalf("expected 2 ack calls, got %d", len(stream.ackCalls))
	}
	if stream.ackCalls[0].ids[0] != "1-0" {
		t.Fatalf("expected first ack for 1-0, got %#v", stream.ackCalls[0].ids)
	}
	if stream.ackCalls[1].ids[0] != "2-0" {
		t.Fatalf("expected second ack for 2-0, got %#v", stream.ackCalls[1].ids)
	}

	if len(store.claimCalls) != 2 {
		t.Fatalf("expected claim loop to continue, got %d claim calls", len(store.claimCalls))
	}
}

func TestConsumerAcksUnclaimedMessageAndContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream := &fakeStreamClient{
		messages: []Message{
			{ID: "1-0", TaskID: "task-unclaimed", EnqueueToken: "token-1"},
			{ID: "2-0", TaskID: "task-claimed", EnqueueToken: "token-2"},
		},
	}
	store := &fakeTaskStore{
		transitionResults: []claimResult{
			{claimed: false},
			{claimed: true},
		},
	}

	handlerCalls := 0
	consumer := NewConsumer(ConsumerConfig{
		Stream:   stream,
		Store:    store,
		Group:    "go-workers",
		Consumer: "worker-1",
		Block:    time.Millisecond,
		Handler: func(_ context.Context, msg Message) error {
			handlerCalls++
			if msg.TaskID != "task-claimed" {
				t.Fatalf("handler should run only for claimed message, got %q", msg.TaskID)
			}
			cancel()
			return nil
		},
	})

	err := consumer.Run(ctx)
	if err != nil {
		t.Fatalf("consumer run: %v", err)
	}

	if handlerCalls != 1 {
		t.Fatalf("expected handler to run once, got %d", handlerCalls)
	}
	if len(store.claimCalls) != 2 {
		t.Fatalf("expected 2 claim calls, got %d", len(store.claimCalls))
	}
	if len(store.terminalCalls) != 0 {
		t.Fatalf("expected no terminal transition for unclaimed path, got %d", len(store.terminalCalls))
	}

	if len(stream.ackCalls) != 2 {
		t.Fatalf("expected ack on both messages, got %d", len(stream.ackCalls))
	}
	if stream.ackCalls[0].ids[0] != "1-0" {
		t.Fatalf("expected first ack for unclaimed 1-0, got %#v", stream.ackCalls[0].ids)
	}
	if stream.ackCalls[1].ids[0] != "2-0" {
		t.Fatalf("expected second ack for claimed 2-0, got %#v", stream.ackCalls[1].ids)
	}
}

func TestConsumerClaimErrorMarksFailedAndAcksThenContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream := &fakeStreamClient{
		messages: []Message{
			{ID: "1-0", TaskID: "task-claim-error", EnqueueToken: "token-1"},
			{ID: "2-0", TaskID: "task-claimed", EnqueueToken: "token-2"},
		},
	}
	store := &fakeTaskStore{
		transitionResults: []claimResult{
			{err: errors.New("claim db timeout")},
			{claimed: true},
		},
	}

	handlerCalls := 0
	consumer := NewConsumer(ConsumerConfig{
		Stream:   stream,
		Store:    store,
		Group:    "go-workers",
		Consumer: "worker-1",
		Block:    time.Millisecond,
		Handler: func(_ context.Context, msg Message) error {
			handlerCalls++
			if msg.TaskID != "task-claimed" {
				t.Fatalf("handler should run only for claim-success message, got %q", msg.TaskID)
			}
			cancel()
			return nil
		},
	})

	err := consumer.Run(ctx)
	if err != nil {
		t.Fatalf("consumer run: %v", err)
	}

	if len(store.markFailedCalls) != 1 {
		t.Fatalf("expected one mark-failed call, got %d", len(store.markFailedCalls))
	}
	markFailed := store.markFailedCalls[0]
	if markFailed.taskID != "task-claim-error" {
		t.Fatalf("expected mark-failed task task-claim-error, got %q", markFailed.taskID)
	}
	if !strings.Contains(markFailed.message, "claim db timeout") {
		t.Fatalf("expected mark-failed message to include claim error, got %q", markFailed.message)
	}

	if len(stream.ackCalls) != 2 {
		t.Fatalf("expected two ack calls, got %d", len(stream.ackCalls))
	}
	if stream.ackCalls[0].ids[0] != "1-0" {
		t.Fatalf("expected first ack id 1-0, got %#v", stream.ackCalls[0].ids)
	}
	if stream.ackCalls[1].ids[0] != "2-0" {
		t.Fatalf("expected second ack id 2-0, got %#v", stream.ackCalls[1].ids)
	}
	if handlerCalls != 1 {
		t.Fatalf("expected handler to run once for second message, got %d", handlerCalls)
	}
}

func TestConsumerHandlerErrorWithFailTransitionErrorDoesNotAckFailedMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream := &fakeStreamClient{
		messages: []Message{
			{ID: "1-0", TaskID: "task-fail-transition", EnqueueToken: "token-1"},
			{ID: "2-0", TaskID: "task-success", EnqueueToken: "token-2"},
		},
	}
	store := &fakeTaskStore{
		transitionResults: []claimResult{
			{claimed: true},
			{claimed: true},
		},
		terminalErr: errors.New("transition update failed"),
	}

	handlerCalls := 0
	consumer := NewConsumer(ConsumerConfig{
		Stream:   stream,
		Store:    store,
		Group:    "go-workers",
		Consumer: "worker-1",
		Block:    time.Millisecond,
		Handler: func(_ context.Context, msg Message) error {
			handlerCalls++
			if msg.TaskID == "task-fail-transition" {
				return errors.New("handler boom")
			}
			cancel()
			return nil
		},
	})

	err := consumer.Run(ctx)
	if err != nil {
		t.Fatalf("consumer run: %v", err)
	}

	if len(store.terminalCalls) != 1 {
		t.Fatalf("expected one terminal transition call, got %d", len(store.terminalCalls))
	}
	if store.terminalCalls[0].TaskID != "task-fail-transition" {
		t.Fatalf("expected fail transition for task-fail-transition, got %q", store.terminalCalls[0].TaskID)
	}
	if len(stream.ackCalls) != 1 {
		t.Fatalf("expected only success message ack, got %d", len(stream.ackCalls))
	}
	if stream.ackCalls[0].ids[0] != "2-0" {
		t.Fatalf("expected ack only for 2-0, got %#v", stream.ackCalls[0].ids)
	}
	if handlerCalls != 2 {
		t.Fatalf("expected handler to continue to second message, got %d calls", handlerCalls)
	}
}

type fakeTaskStore struct {
	transitionResults []claimResult
	defaultTransition bool
	claimCalls        []claimCall
	terminalCalls     []postgres.TransitionTerminalInput
	terminalErr       error
	markFailedCalls   []markFailedCall
	markFailedErr     error
}

type claimResult struct {
	claimed bool
	err     error
}

type claimCall struct {
	taskID string
	token  string
	worker string
}

type markFailedCall struct {
	taskID  string
	message string
}

func (f *fakeTaskStore) TransitionPendingToInProgress(_ context.Context, taskID, token, worker string) (bool, error) {
	f.claimCalls = append(f.claimCalls, claimCall{taskID: taskID, token: token, worker: worker})
	if len(f.transitionResults) > 0 {
		result := f.transitionResults[0]
		f.transitionResults = f.transitionResults[1:]
		return result.claimed, result.err
	}
	return f.defaultTransition, nil
}

func (f *fakeTaskStore) TransitionToTerminal(_ context.Context, input postgres.TransitionTerminalInput) error {
	f.terminalCalls = append(f.terminalCalls, input)
	return f.terminalErr
}

func (f *fakeTaskStore) MarkTaskFailed(_ context.Context, taskID, message string) error {
	f.markFailedCalls = append(f.markFailedCalls, markFailedCall{
		taskID:  taskID,
		message: message,
	})
	return f.markFailedErr
}

type fakeStreamClient struct {
	messages []Message
	ackCalls []ackCall
	reads    int
}

type ackCall struct {
	group string
	ids   []string
}

func (f *fakeStreamClient) ReadGroup(ctx context.Context, _ string, _ string, _ int64, _ time.Duration) ([]Message, error) {
	if f.reads < len(f.messages) {
		msg := f.messages[f.reads]
		f.reads++
		return []Message{msg}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeStreamClient) Ack(_ context.Context, group string, ids ...string) error {
	copied := append([]string(nil), ids...)
	f.ackCalls = append(f.ackCalls, ackCall{group: group, ids: copied})
	return nil
}
