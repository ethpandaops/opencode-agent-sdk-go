package opencodesdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

// runCommandHarness wires a promptCapturingAgent into a pipe-backed
// Client and returns the started Session ready for RunCommand calls.
// The capture slice and mutex are returned so callers can read the
// last-seen prompt blocks safely.
func runCommandHarness(t *testing.T) (Client, Session, func() []acp.ContentBlock, context.Context, func()) {
	t.Helper()

	var (
		mu       sync.Mutex
		captured []acp.ContentBlock
	)

	agent := &promptCapturingAgent{
		fakeAgent: &fakeAgent{},
		onPrompt: func(blocks []acp.ContentBlock) {
			mu.Lock()
			defer mu.Unlock()

			captured = append(captured[:0], blocks...)
		},
	}

	c, cleanup := startPipeClient(t, agent)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

	sess, err := c.NewSession(ctx)
	if err != nil {
		cancel()
		cleanup()
		t.Fatalf("NewSession: %v", err)
	}

	go func() {
		for range sess.Updates() {
		}
	}()

	last := func() []acp.ContentBlock {
		mu.Lock()
		defer mu.Unlock()

		out := make([]acp.ContentBlock, len(captured))
		copy(out, captured)

		return out
	}

	return c, sess, last, ctx, func() {
		cancel()
		cleanup()
	}
}

func TestSession_RunCommand_SendsSlashTextBlock(t *testing.T) {
	_, sess, last, ctx, done := runCommandHarness(t)
	defer done()

	if _, err := sess.RunCommand(ctx, "init"); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}

	blocks := last()
	if len(blocks) != 1 || blocks[0].Text == nil {
		t.Fatalf("expected single text block, got %+v", blocks)
	}

	if got := blocks[0].Text.Text; got != "/init" {
		t.Errorf("RunCommand text = %q, want %q", got, "/init")
	}
}

func TestSession_RunCommand_WithArgs(t *testing.T) {
	_, sess, last, ctx, done := runCommandHarness(t)
	defer done()

	if _, err := sess.RunCommand(ctx, "share", "public"); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}

	if got := last()[0].Text.Text; got != "/share public" {
		t.Errorf("RunCommand text = %q, want %q", got, "/share public")
	}
}

func TestSession_RunCommand_AcceptsLeadingSlash(t *testing.T) {
	_, sess, last, ctx, done := runCommandHarness(t)
	defer done()

	// Caller passes the leading slash themselves; the SDK should not
	// double-prefix.
	if _, err := sess.RunCommand(ctx, "/init"); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}

	if got := last()[0].Text.Text; got != "/init" {
		t.Errorf("RunCommand text = %q, want %q", got, "/init")
	}
}

func TestSession_RunCommand_RejectsEmptyName(t *testing.T) {
	_, sess, _, ctx, done := runCommandHarness(t)
	defer done()

	if _, err := sess.RunCommand(ctx, ""); err == nil {
		t.Fatalf("expected error on empty command name")
	}
}
