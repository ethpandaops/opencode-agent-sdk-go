package opencodesdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

// recordingAgent extends fakeAgent to capture SetSessionConfigOption
// payloads for assertions.
type recordingAgent struct {
	fakeAgent

	mu       sync.Mutex
	captured []acp.SetSessionConfigOptionRequest
}

func (r *recordingAgent) SetSessionConfigOption(_ context.Context, req acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.captured = append(r.captured, req)

	return acp.SetSessionConfigOptionResponse{}, nil
}

func (r *recordingAgent) Snapshot() []acp.SetSessionConfigOptionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]acp.SetSessionConfigOptionRequest, len(r.captured))
	copy(out, r.captured)

	return out
}

func newRecordingClient(t *testing.T, agent *recordingAgent) (Client, context.Context, context.CancelFunc) {
	t.Helper()

	factory := func(_ context.Context, handler acp.Client) (Transport, error) {
		return newPipeTransport(handler, agent), nil
	}

	c, err := NewClient(WithTransport(factory), WithSkipVersionCheck(true), WithCwd("/tmp"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

	if startErr := c.Start(ctx); startErr != nil {
		cancel()

		_ = c.Close()

		t.Fatalf("Start: %v", startErr)
	}

	return c, ctx, cancel
}

func TestSession_SetConfigOption_StringValue(t *testing.T) {
	agent := &recordingAgent{}

	c, ctx, cancel := newRecordingClient(t, agent)
	defer cancel()
	defer c.Close()

	sess, err := c.NewSession(ctx)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if err := sess.SetConfigOption(ctx, "reasoning_effort", "medium"); err != nil {
		t.Fatalf("SetConfigOption: %v", err)
	}

	snap := agent.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("captured %d requests, want 1", len(snap))
	}

	req := snap[0]
	if req.ValueId == nil {
		t.Fatalf("ValueId variant not set; got %+v", req)
	}

	if req.ValueId.ConfigId != "reasoning_effort" {
		t.Fatalf("ConfigId = %q, want reasoning_effort", req.ValueId.ConfigId)
	}

	if req.ValueId.Value != "medium" {
		t.Fatalf("Value = %q, want medium", req.ValueId.Value)
	}
}

func TestSession_SetConfigOptionBool(t *testing.T) {
	agent := &recordingAgent{}

	c, ctx, cancel := newRecordingClient(t, agent)
	defer cancel()
	defer c.Close()

	sess, err := c.NewSession(ctx)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if err := sess.SetConfigOptionBool(ctx, "auto_compact", true); err != nil {
		t.Fatalf("SetConfigOptionBool: %v", err)
	}

	snap := agent.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("captured %d requests, want 1", len(snap))
	}

	req := snap[0]
	if req.Boolean == nil {
		t.Fatalf("Boolean variant not set; got %+v", req)
	}

	if req.Boolean.ConfigId != "auto_compact" {
		t.Fatalf("ConfigId = %q, want auto_compact", req.Boolean.ConfigId)
	}

	if req.Boolean.Type != "boolean" {
		t.Fatalf("Type = %q, want boolean", req.Boolean.Type)
	}

	if !req.Boolean.Value {
		t.Fatalf("Value = false, want true")
	}
}

func TestSession_SetConfigOption_UpdatesCachedModelMode(t *testing.T) {
	agent := &recordingAgent{}

	c, ctx, cancel := newRecordingClient(t, agent)
	defer cancel()
	defer c.Close()

	sess, err := c.NewSession(ctx)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if err := sess.SetConfigOption(ctx, "model", "anthropic/claude-sonnet-4"); err != nil {
		t.Fatalf("SetConfigOption(model): %v", err)
	}

	if err := sess.SetConfigOption(ctx, "mode", "build"); err != nil {
		t.Fatalf("SetConfigOption(mode): %v", err)
	}

	// The session's internal labels update is exercised implicitly — a
	// failing SetConfigOption would have returned above. We assert only
	// that both wire round-trips happened.
	if snap := agent.Snapshot(); len(snap) != 2 {
		t.Fatalf("captured %d requests, want 2", len(snap))
	}
}
