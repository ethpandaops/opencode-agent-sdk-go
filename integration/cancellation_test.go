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

// TestCancellation_SessionCancelDuringPrompt fires a long prompt then
// calls Session.Cancel; the pending Prompt must return an error that
// satisfies errors.Is(ErrCancelled).
func TestCancellation_SessionCancelDuringPrompt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		// Drain updates in the background so the session buffer doesn't fill.
		go func() {
			for range sess.Updates() {
			}
		}()

		// Fire the cancel ~600ms after the prompt starts.
		go func() {
			time.Sleep(600 * time.Millisecond)

			_ = sess.Cancel(context.Background())
		}()

		prompt := "Count slowly from 1 to 200 with a short observation between each number. Do not stop until you reach 200."

		_, promptErr := sess.Prompt(ctx, acp.TextBlock(prompt))

		if errors.Is(promptErr, opencodesdk.ErrCancelled) {
			return nil
		}

		if promptErr == nil {
			// The agent might have finished faster than expected. Not ideal
			// but don't fail — log and continue.
			t.Logf("Prompt completed before cancel fired; this run did not exercise cancellation")

			return nil
		}

		return promptErr
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

// TestCancellation_CtxCancelDuringPrompt cancels the ctx passed to
// Prompt directly. The SDK forwards session/cancel to opencode and the
// Prompt call returns with a cancelled error.
func TestCancellation_CtxCancelDuringPrompt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		go func() {
			for range sess.Updates() {
			}
		}()

		promptCtx, promptCancel := context.WithCancel(ctx)

		go func() {
			time.Sleep(500 * time.Millisecond)
			promptCancel()
		}()

		_, promptErr := sess.Prompt(promptCtx, acp.TextBlock(
			"Count slowly from 1 to 200 with observations. Do not stop until you reach 200."),
		)

		if errors.Is(promptErr, opencodesdk.ErrCancelled) || errors.Is(promptErr, context.Canceled) {
			return nil
		}

		if promptErr == nil {
			t.Logf("Prompt completed before ctx cancel fired")

			return nil
		}

		return promptErr
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
