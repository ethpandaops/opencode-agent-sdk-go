package opencodesdk

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

// TestWithMaxTurns_StoresLimit checks the option plumbing.
func TestWithMaxTurns_StoresLimit(t *testing.T) {
	o := apply([]Option{WithMaxTurns(3)})
	if o.maxTurns != 3 {
		t.Fatalf("WithMaxTurns(3): options.maxTurns = %d, want 3", o.maxTurns)
	}
}

// cancelCountingAgent embeds fakeAgent and counts session/cancel
// notifications. Used to verify attachMaxTurns triggers Session.Cancel.
type cancelCountingAgent struct {
	fakeAgent
	cancels atomic.Int64
}

func (a *cancelCountingAgent) Cancel(_ context.Context, _ acp.CancelNotification) error {
	a.cancels.Add(1)

	return nil
}

// TestAttachMaxTurns_FiresCancelOnNthMessage drives a session past the
// configured turn limit by delivering distinct-MessageId chunks and
// asserts Session.Cancel was invoked exactly once.
func TestAttachMaxTurns_FiresCancelOnNthMessage(t *testing.T) {
	agent := &cancelCountingAgent{}

	c, cleanup := startPipeClient(t, agent)
	defer cleanup()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sess, err := c.NewSession(ctx, WithMaxTurns(2))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	go func() {
		for range sess.Updates() {
		}
	}()

	deliver := func(msgID string) {
		impl, ok := sess.(*session)
		if !ok {
			t.Fatalf("expected *session, got %T", sess)
		}

		impl.deliver(acp.SessionNotification{
			SessionId: impl.id,
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content:   acp.TextBlock("ok"),
					MessageId: &msgID,
				},
			},
		})
	}

	deliver("m1") // turn 1
	deliver("m1") // duplicate, no count
	deliver("m1") // duplicate, no count

	if got := agent.cancels.Load(); got != 0 {
		t.Fatalf("cancel before limit: got %d cancels, want 0", got)
	}

	deliver("m2") // turn 2 → reaches limit, fires cancel

	deadline := time.Now().Add(2 * time.Second)
	for agent.cancels.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if got := agent.cancels.Load(); got != 1 {
		t.Fatalf("after limit: got %d cancels, want 1", got)
	}

	deliver("m3") // turn 3 → must NOT double-fire

	time.Sleep(50 * time.Millisecond)

	if got := agent.cancels.Load(); got != 1 {
		t.Fatalf("after subsequent message: got %d cancels, want 1 (no double-fire)", got)
	}
}

// TestAttachMaxTurns_ZeroLimitIsNoop verifies that limit==0 attaches no
// subscriber and never cancels.
func TestAttachMaxTurns_ZeroLimitIsNoop(t *testing.T) {
	agent := &cancelCountingAgent{}

	c, cleanup := startPipeClient(t, agent)
	defer cleanup()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sess, err := c.NewSession(ctx, WithMaxTurns(0))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	go func() {
		for range sess.Updates() {
		}
	}()

	impl, ok := sess.(*session)
	if !ok {
		t.Fatalf("expected *session, got %T", sess)
	}

	for i := range 5 {
		msgID := "m" + itoaSmall(i)

		impl.deliver(acp.SessionNotification{
			SessionId: impl.id,
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content:   acp.TextBlock("ok"),
					MessageId: &msgID,
				},
			},
		})
	}

	time.Sleep(50 * time.Millisecond)

	if got := agent.cancels.Load(); got != 0 {
		t.Fatalf("got %d cancels with limit=0, want 0", got)
	}
}
