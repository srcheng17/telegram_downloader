package worker

import (
	"context"
	"testing"
	"time"
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
	store := &fakeTaskStore{transitioned: true}

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

type fakeTaskStore struct {
	transitioned bool
	claimCalls   []claimCall
}

type claimCall struct {
	taskID string
	token  string
	worker string
}

func (f *fakeTaskStore) TransitionPendingToInProgress(_ context.Context, taskID, token, worker string) (bool, error) {
	f.claimCalls = append(f.claimCalls, claimCall{taskID: taskID, token: token, worker: worker})
	return f.transitioned, nil
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
