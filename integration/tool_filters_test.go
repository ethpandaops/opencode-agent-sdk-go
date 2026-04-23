//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// writeAskConfig drops an opencode.json into cwd that routes edit/write/bash
// through session/request_permission. Without this file opencode's default
// ruleset auto-approves everything and the permission callback never fires.
func writeAskConfig(t *testing.T, cwd string) {
	t.Helper()

	path := filepath.Join(cwd, "opencode.json")
	body := `{
  "$schema": "https://opencode.ai/config.json",
  "permission": {
    "edit":  "ask",
    "write": "ask",
    "bash":  "ask"
  }
}`

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write opencode.json: %v", err)
	}
}

// TestAllowedTools_AutoApproved drives a file-edit prompt with WithAllowedTools
// containing the opencode tool name ("edit") and asserts the user canUseTool
// callback is bypassed.
func TestAllowedTools_AutoApproved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cwd := tempCwd(t)
	writeAskConfig(t, cwd)

	var userCallbackHits atomic.Int32

	userCB := func(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		userCallbackHits.Add(1)

		return opencodesdk.AllowOnce(ctx, req)
	}

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
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
		opencodesdk.WithCanUseTool(userCB),
		opencodesdk.WithAllowedTools("edit", "write"),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}

	if userCallbackHits.Load() != 0 {
		t.Fatalf("user canUseTool should be bypassed by WithAllowedTools; got %d hits",
			userCallbackHits.Load())
	}
}

// TestDisallowedTools_AutoRejected asks opencode to run a bash command while
// WithDisallowedTools includes "bash". Callback must NOT fire and opencode
// should see a reject_once outcome on any bash permission request.
func TestDisallowedTools_AutoRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cwd := tempCwd(t)
	writeAskConfig(t, cwd)

	var userCallbackHits atomic.Int32

	userCB := func(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		userCallbackHits.Add(1)

		return opencodesdk.AllowOnce(ctx, req)
	}

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		go func() {
			for range sess.Updates() {
			}
		}()

		_, _ = sess.Prompt(ctx, acp.TextBlock(
			"Run the bash command `echo hello`. Then stop.",
		))

		// A rejected tool call is not a prompt-level error; we don't
		// assert promptErr here. Success is "user callback not hit".
		return nil
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithCanUseTool(userCB),
		opencodesdk.WithDisallowedTools("bash"),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}

	if userCallbackHits.Load() != 0 {
		t.Fatalf("user canUseTool should be bypassed by WithDisallowedTools; got %d hits",
			userCallbackHits.Load())
	}
}
