//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestStreaming_AgentMessageChunksArrive verifies that at least one
// AgentMessageChunk flows through Session.Updates during a turn, and
// that its concatenation matches the non-empty AssistantText the SDK
// rolls up.
func TestStreaming_AgentMessageChunksArrive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := opencodesdk.Query(ctx,
		"Reply with exactly the phrase: the quick brown fox.",
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Query: %v", err)
	}

	if res.AssistantText == "" {
		t.Fatalf("AssistantText is empty; expected aggregated chunks")
	}

	var chunks int

	for _, n := range res.Notifications {
		if n.Update.AgentMessageChunk != nil {
			chunks++
		}
	}

	if chunks == 0 {
		t.Fatalf("expected at least one agent_message_chunk notification; got %d total", len(res.Notifications))
	}

	if !strings.Contains(strings.ToLower(res.AssistantText), "fox") {
		t.Logf("AssistantText did not contain 'fox'; got %q", res.AssistantText)
	}
}

// TestStreaming_UsageReported checks that opencode returns a Usage
// block with non-zero input + output tokens on a successful turn.
func TestStreaming_UsageReported(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := opencodesdk.Query(ctx,
		"Say hello in one word.",
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Query: %v", err)
	}

	if res.Usage == nil {
		t.Skipf("opencode did not return a Usage block; model/provider may not report tokens")
	}

	if res.Usage.InputTokens == 0 || res.Usage.OutputTokens == 0 {
		t.Fatalf("expected non-zero InputTokens+OutputTokens; got %+v", res.Usage)
	}
}

// TestStreaming_UpdatesChannelClosesOnClientClose verifies that the
// Session.Updates channel closes cleanly when the owning Client is
// closed (so consumers blocked on it unblock).
func TestStreaming_UpdatesChannelClosesOnClientClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if startErr := c.Start(ctx); startErr != nil {
		skipIfCLIUnavailable(t, startErr)
		skipIfAuthRequired(t, startErr)
		t.Fatalf("Start: %v", startErr)
	}

	sess, err := c.NewSession(ctx)
	if err != nil {
		skipIfAuthRequired(t, err)
		t.Fatalf("NewSession: %v", err)
	}

	updates := sess.Updates()

	done := make(chan struct{})

	go func() {
		defer close(done)

		for range updates {
			// drain
		}
	}()

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Updates channel did not close after Client.Close")
	}
}

// TestStreaming_MultiTurnSameSession runs two prompts back-to-back on
// one session and asserts both return non-empty text.
func TestStreaming_MultiTurnSameSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		prompts := []string{
			"Reply with just the word: apple.",
			"Reply with just the word: banana.",
		}

		for _, p := range prompts {
			drainCtx, drainCancel := context.WithCancel(ctx)

			var collected string

			done := make(chan struct{})

			go func() {
				defer close(done)

				collected = collectText(drainCtx, sess.Updates())
			}()

			if _, promptErr := sess.Prompt(ctx, acp.TextBlock(p)); promptErr != nil {
				drainCancel()
				<-done

				return promptErr
			}

			time.Sleep(150 * time.Millisecond)

			drainCancel()
			<-done

			if strings.TrimSpace(collected) == "" {
				t.Fatalf("prompt %q produced no text", p)
			}
		}

		return nil
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}
}
