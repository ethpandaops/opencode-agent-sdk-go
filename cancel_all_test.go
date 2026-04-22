package opencodesdk

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

// countingCancelAgent is a fakeAgent variant that counts session/cancel
// notifications so TestClient_CancelAll_FansOut can verify fan-out.
type countingCancelAgent struct {
	fakeAgent
	cancels atomic.Int32
	counter atomic.Int32
}

func (a *countingCancelAgent) Cancel(_ context.Context, _ acp.CancelNotification) error {
	a.cancels.Add(1)

	return nil
}

func (a *countingCancelAgent) NewSession(_ context.Context, _ acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	n := a.counter.Add(1)

	return acp.NewSessionResponse{SessionId: acp.SessionId("ses_cancel_all_" + itoaSmall(int(n)))}, nil
}

func TestClient_CancelAll_NoSessions_ReturnsNil(t *testing.T) {
	c, cleanup := startPipeClient(t, &fakeAgent{})
	defer cleanup()

	if err := c.CancelAll(t.Context()); err != nil {
		t.Fatalf("CancelAll with zero sessions: %v", err)
	}
}

func TestClient_CancelAll_FansOutToEverySession(t *testing.T) {
	agent := &countingCancelAgent{}

	c, cleanup := startPipeClient(t, agent)
	defer cleanup()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	for range 3 {
		if _, err := c.NewSession(ctx); err != nil {
			t.Fatalf("NewSession: %v", err)
		}
	}

	if err := c.CancelAll(ctx); err != nil {
		t.Fatalf("CancelAll: %v", err)
	}

	// session/cancel is a notification (fire-and-forget); allow a
	// brief window for the agent-side handler to observe all 3.
	deadline := time.Now().Add(2 * time.Second)
	for agent.cancels.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if got := agent.cancels.Load(); got != 3 {
		t.Fatalf("agent observed %d cancels, want 3", got)
	}
}

func TestClient_Start_AlreadyConnected_ReturnsSentinel(t *testing.T) {
	c, cleanup := startPipeClient(t, &fakeAgent{})
	defer cleanup()

	err := c.Start(t.Context())
	if !errors.Is(err, ErrClientAlreadyConnected) {
		t.Fatalf("Start twice: expected ErrClientAlreadyConnected, got %v", err)
	}
}

// startPipeClient wires a pipe-backed Client against the supplied
// agent and returns it already Started. Cleanup closes the client.
func startPipeClient(t *testing.T, agent acp.Agent) (Client, func()) {
	t.Helper()

	factory := func(_ context.Context, handler acp.Client) (Transport, error) {
		return newPipeTransport(handler, agent), nil
	}

	c, err := NewClient(WithTransport(factory), WithSkipVersionCheck(true), WithCwd("/tmp"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err = c.Start(ctx)
	if err != nil {
		_ = c.Close()

		t.Fatalf("Start: %v", err)
	}

	return c, func() { _ = c.Close() }
}

// itoaSmall renders small non-negative ints without pulling in strconv
// (to keep this test file's imports minimal).
func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}

	var b [12]byte

	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}

	return string(b[i:])
}
