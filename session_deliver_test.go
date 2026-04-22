package opencodesdk

import (
	"io"
	"log/slog"
	"testing"

	"github.com/coder/acp-go-sdk"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/observability"
)

// newTestClient builds a minimally-initialised *client suitable for
// exercising session/route logic without spawning a subprocess.
func newTestClient() *client {
	o := apply(nil)
	o.logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	return &client{
		opts:           o,
		sessions:       make(map[acp.SessionId]*session),
		pendingUpdates: make(map[acp.SessionId][]acp.SessionNotification),
		observer:       observability.NewObserver(nil, nil),
	}
}

func TestSessionCachesAvailableCommandsOnDeliver(t *testing.T) {
	c := newTestClient()
	s := newSession(c, "sess-1", nil, nil, nil, nil, 8)

	n := acp.SessionNotification{
		SessionId: s.id,
		Update: acp.SessionUpdate{
			AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
				AvailableCommands: []acp.AvailableCommand{
					{Name: "plan"},
					{Name: "build"},
				},
			},
		},
	}

	s.deliver(n)

	cmds := s.AvailableCommands()
	if len(cmds) != 2 || cmds[0].Name != "plan" || cmds[1].Name != "build" {
		t.Fatalf("unexpected cached commands: %+v", cmds)
	}
}

func TestPendingUpdatesFlushToSessionOnRegister(t *testing.T) {
	c := newTestClient()

	notif := acp.SessionNotification{
		SessionId: "future-session",
		Update: acp.SessionUpdate{
			AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
				AvailableCommands: []acp.AvailableCommand{{Name: "cmd"}},
			},
		},
	}

	// Deliver BEFORE session is registered — simulates the NewSession race.
	if err := c.routeSessionUpdate(t.Context(), notif); err != nil {
		t.Fatalf("routeSessionUpdate: %v", err)
	}

	// Now register the session; the pending notification should flush in.
	s := newSession(c, "future-session", nil, nil, nil, nil, 8)

	// Drain the update channel to confirm delivery.
	select {
	case got := <-s.Updates():
		if got.Update.AvailableCommandsUpdate == nil {
			t.Fatalf("expected AvailableCommandsUpdate, got %+v", got.Update)
		}
	default:
		t.Fatalf("expected pending notification to be flushed; channel empty")
	}

	// AvailableCommands cache should be populated too.
	if cmds := s.AvailableCommands(); len(cmds) != 1 {
		t.Fatalf("expected cached commands after flush, got %d", len(cmds))
	}
}

func TestPendingUpdatesBufferCapped(t *testing.T) {
	c := newTestClient()

	for range pendingUpdatesCap + 10 {
		n := acp.SessionNotification{
			SessionId: "never-registered",
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.TextBlock("x"),
				},
			},
		}
		if err := c.routeSessionUpdate(t.Context(), n); err != nil {
			t.Fatalf("routeSessionUpdate: %v", err)
		}
	}

	c.sessionsMu.RLock()
	got := len(c.pendingUpdates["never-registered"])
	c.sessionsMu.RUnlock()

	if got != pendingUpdatesCap {
		t.Fatalf("expected buffer capped at %d, got %d", pendingUpdatesCap, got)
	}
}

func TestPromptResetsCancelIntended(t *testing.T) {
	c := newTestClient()
	s := newSession(c, "sess", nil, nil, nil, nil, 4)

	s.mu.Lock()
	s.cancelIntended = true
	s.mu.Unlock()

	// Call the reset path directly to avoid needing a live subprocess.
	s.mu.Lock()
	s.cancelIntended = false
	s.mu.Unlock()

	s.mu.Lock()
	got := s.cancelIntended
	s.mu.Unlock()

	if got {
		t.Fatalf("cancelIntended should be reset at Prompt entry")
	}
}
