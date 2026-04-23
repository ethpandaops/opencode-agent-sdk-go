package opencodesdk

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/coder/acp-go-sdk"
)

func TestSubscribeDispatchesByVariant(t *testing.T) {
	c := newTestClient()
	s := newSession(c, "sess-sub", nil, nil, nil, nil, 16)

	var (
		agentMessages atomic.Int32
		plans         atomic.Int32
		toolCalls     atomic.Int32
	)

	unsub := s.Subscribe(UpdateHandlers{
		AgentMessage: func(_ context.Context, _ *acp.SessionUpdateAgentMessageChunk) {
			agentMessages.Add(1)
		},
		Plan: func(_ context.Context, _ *acp.SessionUpdatePlan) {
			plans.Add(1)
		},
		ToolCall: func(_ context.Context, _ *acp.SessionUpdateToolCall) {
			toolCalls.Add(1)
		},
	})
	defer unsub()

	s.deliver(acp.SessionNotification{
		SessionId: s.id,
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Content: acp.TextBlock("hi")},
		},
	})

	s.deliver(acp.SessionNotification{
		SessionId: s.id,
		Update:    acp.SessionUpdate{Plan: &acp.SessionUpdatePlan{}},
	})

	s.deliver(acp.SessionNotification{
		SessionId: s.id,
		Update:    acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{Title: "edit"}},
	})

	if got := agentMessages.Load(); got != 1 {
		t.Fatalf("AgentMessage handler fired %d times, want 1", got)
	}

	if got := plans.Load(); got != 1 {
		t.Fatalf("Plan handler fired %d times, want 1", got)
	}

	if got := toolCalls.Load(); got != 1 {
		t.Fatalf("ToolCall handler fired %d times, want 1", got)
	}
}

func TestSubscribeUnsubscribeStopsDispatch(t *testing.T) {
	c := newTestClient()
	s := newSession(c, "sess-unsub", nil, nil, nil, nil, 8)

	var calls atomic.Int32

	unsub := s.Subscribe(UpdateHandlers{
		AgentMessage: func(_ context.Context, _ *acp.SessionUpdateAgentMessageChunk) {
			calls.Add(1)
		},
	})

	s.deliver(chunkNotification(string(s.id), "one"))

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 call before unsubscribe, got %d", got)
	}

	unsub()

	s.deliver(chunkNotification(string(s.id), "two"))

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected no further calls after unsubscribe, got %d", got)
	}
}

func TestDeliverDropBumpsCounterAndFiresCallback(t *testing.T) {
	c := newTestClient()

	var (
		dropped atomic.Int64
		sid     atomic.Pointer[string]
	)

	c.opts.onUpdateDropped = func(_ context.Context, id string, count int64) {
		dropped.Store(count)
		sid.Store(&id)
	}

	// Use a buffer of 1 so the second deliver overflows.
	s := newSession(c, "sess-drop", nil, nil, nil, nil, 1)

	n := chunkNotification(string(s.id), "x")

	s.deliver(n)
	s.deliver(n)

	if got := s.DroppedUpdates(); got != 1 {
		t.Fatalf("DroppedUpdates = %d, want 1", got)
	}

	if got := dropped.Load(); got != 1 {
		t.Fatalf("callback drop count = %d, want 1", got)
	}

	ptr := sid.Load()
	if ptr == nil || *ptr != string(s.id) {
		t.Fatalf("callback session id not captured; got %v", ptr)
	}
}
