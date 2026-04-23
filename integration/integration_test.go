//go:build integration

// Package integration contains end-to-end tests that spawn the real
// `opencode acp` subprocess. Run with:
//
//	go test -tags=integration -timeout=30m ./integration/...
//
// Each test that drives an actual LLM turn can take 2–5 minutes,
// so the default 10m go-test timeout is too tight when running the
// full suite. Use -run to target subsets, e.g.:
//
//	go test -tags=integration -run TestLifecycle ./integration/...
//	go test -tags=integration -run TestQuery_OneShot ./integration/...
//
// Tests skip themselves when:
//   - the opencode CLI is not on $PATH
//   - the CLI is present but reports ErrAuthRequired (user has not
//     run `opencode auth login`)
//
// Other failures are treated as real test failures.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// skipIfCLIUnavailable skips t when err indicates the opencode binary
// is not on PATH or is unsupported. Call immediately after Start /
// Query / NewSession returns an error.
func skipIfCLIUnavailable(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		return
	}

	switch {
	case errors.Is(err, opencodesdk.ErrCLINotFound):
		t.Skip("opencode CLI not installed; skipping integration test")
	case errors.Is(err, opencodesdk.ErrUnsupportedCLIVersion):
		t.Skip("opencode CLI version too old; skipping integration test")
	}
}

// skipIfAuthRequired skips t when err indicates the user hasn't run
// `opencode auth login`. The integration suite is not a good place to
// block on interactive auth flows.
func skipIfAuthRequired(t *testing.T, err error) {
	t.Helper()

	if errors.Is(err, opencodesdk.ErrAuthRequired) {
		t.Skip("opencode auth not configured; run `opencode auth login` before running integration tests")
	}
}

// testLogger returns a slog.Logger for integration tests. On verbose
// runs it writes to stderr at WARN level — surfacing errors and
// warnings without the INFO-level subprocess-lifecycle chatter
// (`opencode CLI discovered`, `starting opencode acp`, `connection
// closed`) that otherwise floods `go test -v` output. Silent on
// non-verbose runs.
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()

	if !testing.Verbose() {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// tempCwd returns an absolute path to a fresh temporary directory for
// the test. The directory is automatically cleaned up on test exit.
func tempCwd(t *testing.T) string {
	t.Helper()

	abs, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	return abs
}

// tempCwdWithOpencodeConfig is tempCwd plus a project-local opencode.json
// written at the root. Lets tests force opencode's permission / tooling
// routing into specific modes (e.g. permission.edit="ask") regardless of
// the user's global opencode config.
func tempCwdWithOpencodeConfig(t *testing.T, config map[string]any) string {
	t.Helper()

	cwd := tempCwd(t)

	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("marshal opencode.json: %v", err)
	}

	if err := os.WriteFile(filepath.Join(cwd, "opencode.json"), payload, 0o600); err != nil {
		t.Fatalf("write opencode.json: %v", err)
	}

	return cwd
}

// collectText accumulates AgentMessageChunk text from updates until
// ctx is done or the channel closes.
func collectText(ctx context.Context, updates <-chan acp.SessionNotification) string {
	var sb strings.Builder

	for {
		select {
		case <-ctx.Done():
			return sb.String()
		case n, ok := <-updates:
			if !ok {
				return sb.String()
			}

			if n.Update.AgentMessageChunk == nil {
				continue
			}

			if n.Update.AgentMessageChunk.Content.Text != nil {
				sb.WriteString(n.Update.AgentMessageChunk.Content.Text.Text)
			}
		}
	}
}
