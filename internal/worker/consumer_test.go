package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/ryancheng/telegram-downloader/internal/domain"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
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

func TestNewConsumerDoesNotMutateProvidedExecutor(t *testing.T) {
	store := &fakeTaskStore{defaultTransition: true}
	executor := &Executor{}

	_ = NewConsumer(ConsumerConfig{
		Store:    store,
		Group:    "go-workers",
		Consumer: "worker-1",
		Executor: executor,
	})

	if executor.Store != nil {
		t.Fatalf("expected caller-provided executor to stay unchanged")
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

func TestConsumerUnclaimedRedeliveryWithPendingCanceledTerminalStatusAcks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream := &fakeStreamClient{
		messages: []Message{
			{ID: "1-0", TaskID: "task-pending-canceled", EnqueueToken: "token-1"},
			{ID: "2-0", TaskID: "task-claimed", EnqueueToken: "token-2"},
		},
	}
	store := &fakeTaskStore{
		transitionResults: []claimResult{
			{claimed: false},
			{claimed: true},
		},
		statusByTask: map[string]string{
			"task-pending-canceled": domain.StatusCanceled,
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

func TestConsumerUnclaimedRedeliveryWithNonTerminalStatusDoesNotAck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream := &fakeStreamClient{
		messages: []Message{
			{ID: "1-0", TaskID: "task-non-terminal", EnqueueToken: "token-1"},
			{ID: "2-0", TaskID: "task-claimed", EnqueueToken: "token-2"},
		},
	}
	store := &fakeTaskStore{
		transitionResults: []claimResult{
			{claimed: false},
			{claimed: true},
		},
		statusByTask: map[string]string{
			"task-non-terminal": domain.StatusInProgress,
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
	if len(stream.ackCalls) != 1 {
		t.Fatalf("expected only claimed message ack, got %d", len(stream.ackCalls))
	}
	if stream.ackCalls[0].ids[0] != "2-0" {
		t.Fatalf("expected ack only for 2-0, got %#v", stream.ackCalls[0].ids)
	}
	if status := store.taskStatus("task-non-terminal"); status != domain.StatusInProgress {
		t.Fatalf("expected non-terminal task to stay IN_PROGRESS, got %q", status)
	}
}

func TestConsumerUnclaimedCancelRequestedTransitionsToCanceledThenAcks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream := &fakeStreamClient{
		messages: []Message{
			{ID: "1-0", TaskID: "task-cancel", EnqueueToken: "token-1"},
			{ID: "2-0", TaskID: "task-claimed", EnqueueToken: "token-2"},
		},
	}
	store := &fakeTaskStore{
		transitionResults: []claimResult{
			{claimed: false},
			{claimed: true},
		},
		statusByTask: map[string]string{
			"task-cancel": domain.StatusCancelRequested,
		},
		ownerByTask: map[string]string{
			"task-cancel": "worker-1",
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
	if len(store.terminalCalls) != 1 {
		t.Fatalf("expected one cancel transition, got %d", len(store.terminalCalls))
	}
	if store.terminalCalls[0].TaskID != "task-cancel" || store.terminalCalls[0].Status != domain.StatusCanceled {
		t.Fatalf("expected CANCELED transition for task-cancel, got %#v", store.terminalCalls[0])
	}
	if status := store.taskStatus("task-cancel"); status != domain.StatusCanceled {
		t.Fatalf("expected task-cancel to become CANCELED, got %q", status)
	}
	if len(stream.ackCalls) != 2 {
		t.Fatalf("expected ack for cancel + claimed messages, got %d", len(stream.ackCalls))
	}
	if stream.ackCalls[0].ids[0] != "1-0" || stream.ackCalls[1].ids[0] != "2-0" {
		t.Fatalf("expected ack order [1-0,2-0], got %#v %#v", stream.ackCalls[0].ids, stream.ackCalls[1].ids)
	}
}

func TestConsumerUnclaimedWithoutStatusReaderDoesNotAck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream := &fakeStreamClient{
		messages: []Message{
			{ID: "1-0", TaskID: "task-unclaimed", EnqueueToken: "token-1"},
			{ID: "2-0", TaskID: "task-claimed", EnqueueToken: "token-2"},
		},
	}
	store := &fakeTaskStoreWithoutStatus{
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
	if len(stream.ackCalls) != 1 {
		t.Fatalf("expected only claimed message ack, got %d", len(stream.ackCalls))
	}
	if stream.ackCalls[0].ids[0] != "2-0" {
		t.Fatalf("expected ack only for 2-0, got %#v", stream.ackCalls[0].ids)
	}
}

func TestConsumerClaimErrorDoesNotAckAndContinues(t *testing.T) {
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

	if len(stream.ackCalls) != 1 {
		t.Fatalf("expected only success-message ack, got %d", len(stream.ackCalls))
	}
	if stream.ackCalls[0].ids[0] != "2-0" {
		t.Fatalf("expected ack only for 2-0, got %#v", stream.ackCalls[0].ids)
	}
	if handlerCalls != 1 {
		t.Fatalf("expected handler to run once for second message, got %d", handlerCalls)
	}
	if len(store.terminalCalls) != 0 {
		t.Fatalf("expected no fail transition on claim-error path, got %d", len(store.terminalCalls))
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

func TestConsumerHandlerErrorNoopFailTransitionLeavesNonTerminalDoesNotAck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream := &fakeStreamClient{
		messages: []Message{
			{ID: "1-0", TaskID: "task-non-terminal", EnqueueToken: "token-1"},
			{ID: "2-0", TaskID: "task-success", EnqueueToken: "token-2"},
		},
	}
	store := &fakeTaskStore{
		transitionResults: []claimResult{
			{claimed: true},
			{claimed: true},
		},
		beforeTerminalTransition: func(taskID string, input postgres.TransitionTerminalInput, current string) string {
			if taskID == "task-non-terminal" && input.Status == domain.StatusFailed {
				return domain.StatusCancelRequested
			}
			return current
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
			if msg.TaskID == "task-non-terminal" {
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
		t.Fatalf("expected one fail transition attempt, got %d", len(store.terminalCalls))
	}
	if len(stream.ackCalls) != 1 {
		t.Fatalf("expected only success message ack, got %d", len(stream.ackCalls))
	}
	if stream.ackCalls[0].ids[0] != "2-0" {
		t.Fatalf("expected ack only for 2-0, got %#v", stream.ackCalls[0].ids)
	}
	if status := store.taskStatus("task-non-terminal"); status != domain.StatusCancelRequested {
		t.Fatalf("expected non-terminal task to stay CANCEL_REQUESTED, got %q", status)
	}
	if handlerCalls != 2 {
		t.Fatalf("expected handler to continue to second message, got %d", handlerCalls)
	}
}

func TestRedisStreamReadGroupAlternatesNewAndPendingPriority(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	streamName := "download_tasks"
	group := "go-workers"
	consumer := "worker-stable"

	stream := NewRedisStream(client, streamName)
	if err := stream.CreateGroup(ctx, group); err != nil {
		t.Fatalf("create group: %v", err)
	}

	pendingFirstID, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		Values: map[string]any{
			"task_id":       "task-pending-1",
			"enqueue_token": "token-pending-1",
		},
	}).Result()
	if err != nil {
		t.Fatalf("xadd first: %v", err)
	}

	pendingSecondID, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		Values: map[string]any{
			"task_id":       "task-pending-2",
			"enqueue_token": "token-pending-2",
		},
	}).Result()
	if err != nil {
		t.Fatalf("xadd second pending: %v", err)
	}

	bootstrapRead1, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{streamName, ">"},
		Count:    1,
		Block:    time.Millisecond,
	}).Result()
	if err != nil {
		t.Fatalf("bootstrap read 1: %v", err)
	}
	if len(bootstrapRead1) != 1 || len(bootstrapRead1[0].Messages) != 1 || bootstrapRead1[0].Messages[0].ID != pendingFirstID {
		t.Fatalf("expected bootstrap pending id %s, got %#v", pendingFirstID, bootstrapRead1)
	}

	bootstrapRead2, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{streamName, ">"},
		Count:    1,
		Block:    time.Millisecond,
	}).Result()
	if err != nil {
		t.Fatalf("bootstrap read 2: %v", err)
	}
	if len(bootstrapRead2) != 1 || len(bootstrapRead2[0].Messages) != 1 || bootstrapRead2[0].Messages[0].ID != pendingSecondID {
		t.Fatalf("expected bootstrap pending id %s, got %#v", pendingSecondID, bootstrapRead2)
	}

	newFirstID, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		Values: map[string]any{
			"task_id":       "task-new-1",
			"enqueue_token": "token-new-1",
		},
	}).Result()
	if err != nil {
		t.Fatalf("xadd first new: %v", err)
	}

	newSecondID, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		Values: map[string]any{
			"task_id":       "task-new-2",
			"enqueue_token": "token-new-2",
		},
	}).Result()
	if err != nil {
		t.Fatalf("xadd second new: %v", err)
	}

	read1, err := stream.ReadGroup(ctx, group, consumer, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("read 1: %v", err)
	}
	if len(read1) != 1 || read1[0].ID != newFirstID {
		t.Fatalf("expected read 1 to prioritize new id %s, got %#v", newFirstID, read1)
	}
	if err := stream.Ack(ctx, group, read1[0].ID); err != nil {
		t.Fatalf("ack read 1: %v", err)
	}

	read2, err := stream.ReadGroup(ctx, group, consumer, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}
	if len(read2) == 0 || read2[0].ID != pendingFirstID {
		t.Fatalf("expected read 2 to prioritize pending id %s, got %#v", pendingFirstID, read2)
	}
	if err := stream.Ack(ctx, group, read2[0].ID); err != nil {
		t.Fatalf("ack read 2: %v", err)
	}

	read3, err := stream.ReadGroup(ctx, group, consumer, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("read 3: %v", err)
	}
	if len(read3) != 1 || read3[0].ID != newSecondID {
		t.Fatalf("expected read 3 to prioritize new id %s, got %#v", newSecondID, read3)
	}
	if err := stream.Ack(ctx, group, read3[0].ID); err != nil {
		t.Fatalf("ack read 3: %v", err)
	}

	read4, err := stream.ReadGroup(ctx, group, consumer, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("read 4: %v", err)
	}
	if len(read4) == 0 || read4[0].ID != pendingSecondID {
		t.Fatalf("expected read 4 to prioritize pending id %s, got %#v", pendingSecondID, read4)
	}
}

type fakeTaskStore struct {
	transitionResults        []claimResult
	defaultTransition        bool
	claimCalls               []claimCall
	terminalCalls            []postgres.TransitionTerminalInput
	terminalErr              error
	statusByTask             map[string]string
	ownerByTask              map[string]string
	beforeTerminalTransition func(taskID string, input postgres.TransitionTerminalInput, current string) string
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

func (f *fakeTaskStore) TransitionPendingToInProgress(_ context.Context, taskID, token, worker string) (bool, error) {
	f.claimCalls = append(f.claimCalls, claimCall{taskID: taskID, token: token, worker: worker})
	f.ensureStatusMap()
	f.ensureOwnerMap()
	if len(f.transitionResults) > 0 {
		result := f.transitionResults[0]
		f.transitionResults = f.transitionResults[1:]
		if result.claimed {
			f.statusByTask[taskID] = domain.StatusInProgress
			f.ownerByTask[taskID] = worker
		}
		return result.claimed, result.err
	}
	if f.defaultTransition {
		f.statusByTask[taskID] = domain.StatusInProgress
		f.ownerByTask[taskID] = worker
	}
	return f.defaultTransition, nil
}

func (f *fakeTaskStore) TransitionToTerminal(_ context.Context, input postgres.TransitionTerminalInput) error {
	f.terminalCalls = append(f.terminalCalls, input)
	if f.terminalErr != nil {
		return f.terminalErr
	}
	f.ensureStatusMap()
	f.ensureOwnerMap()

	current := f.taskStatus(input.TaskID)
	if f.beforeTerminalTransition != nil {
		current = f.beforeTerminalTransition(input.TaskID, input, current)
		f.statusByTask[input.TaskID] = current
	}

	owner := strings.TrimSpace(f.ownerByTask[input.TaskID])
	if !consumerFakeTransitionAllowed(current, input.Status, owner, input.Worker) {
		return nil
	}
	f.statusByTask[input.TaskID] = input.Status
	return nil
}

func (f *fakeTaskStore) GetTask(_ context.Context, taskID string) (*domain.TaskLog, error) {
	return &domain.TaskLog{
		ID:     taskID,
		Status: f.taskStatus(taskID),
	}, nil
}

func (f *fakeTaskStore) ensureStatusMap() {
	if f.statusByTask == nil {
		f.statusByTask = map[string]string{}
	}
}

func (f *fakeTaskStore) ensureOwnerMap() {
	if f.ownerByTask == nil {
		f.ownerByTask = map[string]string{}
	}
}

func (f *fakeTaskStore) taskStatus(taskID string) string {
	f.ensureStatusMap()
	status := strings.ToUpper(strings.TrimSpace(f.statusByTask[taskID]))
	if status == "" {
		return domain.StatusInProgress
	}
	return status
}

func consumerFakeTransitionAllowed(currentStatus, targetStatus, owner, worker string) bool {
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(owner) != strings.TrimSpace(worker) {
		return false
	}

	switch targetStatus {
	case domain.StatusCanceled:
		return currentStatus == domain.StatusInProgress || currentStatus == domain.StatusCancelRequested
	case domain.StatusSuccess, domain.StatusFailed:
		return currentStatus == domain.StatusInProgress
	default:
		return false
	}
}

type fakeTaskStoreWithoutStatus struct {
	transitionResults []claimResult
	defaultTransition bool
	claimCalls        []claimCall
	terminalCalls     []postgres.TransitionTerminalInput
	terminalErr       error
}

func (f *fakeTaskStoreWithoutStatus) TransitionPendingToInProgress(_ context.Context, taskID, token, worker string) (bool, error) {
	f.claimCalls = append(f.claimCalls, claimCall{taskID: taskID, token: token, worker: worker})
	if len(f.transitionResults) > 0 {
		result := f.transitionResults[0]
		f.transitionResults = f.transitionResults[1:]
		return result.claimed, result.err
	}
	return f.defaultTransition, nil
}

func (f *fakeTaskStoreWithoutStatus) TransitionToTerminal(_ context.Context, input postgres.TransitionTerminalInput) error {
	f.terminalCalls = append(f.terminalCalls, input)
	return f.terminalErr
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
