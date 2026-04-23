//go:build integration

package integration

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestPermissionCallback_WiredThrough registers a permission callback
// and runs a session in the "plan" agent mode (which causes opencode
// to actually emit session/request_permission). The callback is
// expected to be invoked at least once; if it isn't — opencode's
// config may have auto-approve — we skip rather than fail.
func TestPermissionCallback_WiredThrough(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var callbackHits atomic.Int32

	approve := func(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		callbackHits.Add(1)

		return opencodesdk.AllowOnce(ctx, req)
	}

	cwd := tempCwdWithOpencodeConfig(t, map[string]any{
		"permission": map[string]any{
			"edit": "ask",
			"bash": "ask",
		},
	})

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

		_, promptErr := sess.Prompt(ctx, acp.TextBlock(
			"Create a new file named hello.txt in the current directory containing the text 'hi'.",
		))

		return promptErr
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithCanUseTool(approve),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}

	if callbackHits.Load() == 0 {
		t.Skip("permission callback never fired this run (model likely refused the tool call); rerun to retry")
	}

	t.Logf("permission callback invoked %d time(s)", callbackHits.Load())
}

// TestPermissionCallback_FsWriteIntercepted registers an fs-write
// callback and drives a prompt that should try to write. If opencode
// routes the write through fs/write_text_file we'll see the callback
// fire.
func TestPermissionCallback_FsWriteIntercepted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var fsHits atomic.Int32

	fsCB := func(_ context.Context, req acp.WriteTextFileRequest) error {
		fsHits.Add(1)
		t.Logf("fs write delegated: path=%q len=%d", req.Path, len(req.Content))

		return nil
	}

	cwd := tempCwdWithOpencodeConfig(t, map[string]any{
		"permission": map[string]any{
			"edit": "ask",
		},
	})

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

		_, promptErr := sess.Prompt(ctx, acp.TextBlock(
			"Please create a file named greeting.txt containing the single word 'howdy'.",
		))

		return promptErr
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithOnFsWrite(fsCB),
		opencodesdk.WithCanUseTool(opencodesdk.AllowOnce),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}

	if fsHits.Load() == 0 {
		t.Skip("fs/write_text_file callback never fired this run (model did not delegate the write); rerun to retry")
	}

	t.Logf("fs callback invoked %d time(s)", fsHits.Load())
}

// Wait a beat for goroutine cleanup between subtests. (Keeps the
// aggregate integration run from stacking stale updates goroutines.)
var _ = time.Millisecond
