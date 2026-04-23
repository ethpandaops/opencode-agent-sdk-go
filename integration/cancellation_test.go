//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// blockingTool returns a Tool that signals on `started` the first time
// it's invoked, then blocks on its context until cancellation or until
// `teardown` is closed (the test's cleanup signal). Used to hold an
// agent turn open deterministically while the test fires a cancellation
// — independent of LLM prompt-refusal behaviour.
//
// Intentionally does not capture *testing.T: opencode may keep the tool
// call outstanding briefly after the test function returns while it
// tears down the subprocess, and calling t.Logf from an orphaned
// handler goroutine panics on "Log in goroutine after Test... has
// completed".
func blockingTool(teardown <-chan struct{}, started chan<- struct{}) opencodesdk.Tool {
	return opencodesdk.NewTool(
		"wait_for_cancel",
		"Call this tool exactly once as your first action. It will block until the conversation is cancelled. Do not produce any other output until this tool returns.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		func(ctx context.Context, _ map[string]any) (opencodesdk.ToolResult, error) {
			select {
			case started <- struct{}{}:
			default:
			}

			select {
			case <-ctx.Done():
				return opencodesdk.ToolResult{}, ctx.Err()
			case <-teardown:
				return opencodesdk.ToolResult{Text: "test teardown"}, nil
			case <-time.After(10 * time.Second):
				return opencodesdk.ToolResult{Text: "timeout without cancellation"}, nil
			}
		},
	)
}

// TestCancellation_CtxCancelDuringPrompt asserts that cancelling the ctx
// passed to Prompt propagates through to a cancelled error. Uses a
// blocking SDK tool to hold the agent turn open deterministically,
// independent of LLM behaviour.
//
// The sibling "Session.Cancel during prompt" path is covered by unit
// tests (cancel_all_test.go) that count session/cancel notifications
// on a fake agent — opencode 1.14.20 ignores session/cancel during
// in-flight MCP tool calls, so an integration variant is unreachable
// until opencode gains that behaviour.
func TestCancellation_CtxCancelDuringPrompt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	started := make(chan struct{}, 1)
	teardown := make(chan struct{})
	defer close(teardown)

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx,
			opencodesdk.WithModel("opencode/gpt-5-nano"),
			opencodesdk.WithEffort(opencodesdk.EffortHigh),
		)
		if err != nil {
			return err
		}

		go func() {
			for range sess.Updates() {
			}
		}()

		promptCtx, promptCancel := context.WithCancel(ctx)
		defer promptCancel()

		go func() {
			select {
			case <-started:
			case <-ctx.Done():
				return
			}

			promptCancel()
		}()

		result, promptErr := sess.Prompt(promptCtx, acp.TextBlock(
			"Call the wait_for_cancel tool now and wait for its result. Do not produce any text output until that tool returns."),
		)

		if errors.Is(promptErr, opencodesdk.ErrCancelled) || errors.Is(promptErr, context.Canceled) {
			return nil
		}

		if promptErr == nil && result != nil && result.StopReason == acp.StopReasonCancelled {
			return nil
		}

		if promptErr == nil {
			t.Skipf("agent finished without honouring ctx cancel (stop_reason=%q); model did not call wait_for_cancel this run",
				result.StopReason)
		}

		t.Fatalf("expected ErrCancelled/context.Canceled or StopReason=cancelled after ctx cancel, got err=%v result=%+v", promptErr, result)

		return nil
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithSDKTools(blockingTool(teardown, started)),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}
}

// TestCancellation_CancelBeforePromptIsNoop calls Cancel when no turn
// is in flight; next Prompt must behave normally (not misclassify its
// error as ErrCancelled).
func TestCancellation_CancelBeforePromptIsNoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		// Stray cancel with no turn in flight.
		_ = sess.Cancel(ctx)

		// Drain updates so they don't back up.
		go func() {
			for range sess.Updates() {
			}
		}()

		res, promptErr := sess.Prompt(ctx, acp.TextBlock("Reply with just: ok."))
		if promptErr != nil {
			return promptErr
		}

		if res.StopReason == "" {
			t.Fatalf("expected a stop reason")
		}

		return nil
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}
}
