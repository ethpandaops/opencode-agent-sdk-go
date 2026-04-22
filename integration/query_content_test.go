//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestQueryContent_TextOnly proves QueryContent accepts a plain text
// block and runs end-to-end against opencode just like the legacy
// string-based Query.
func TestQueryContent_TextOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	blocks := opencodesdk.Text("Reply with only the digit: 7.")

	res, err := opencodesdk.QueryContent(ctx, blocks,
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("QueryContent: %v", err)
	}

	if res.SessionID == "" {
		t.Fatalf("SessionID empty")
	}

	if res.StopReason == "" {
		t.Fatalf("StopReason empty; expected end_turn")
	}
}

// TestQueryStreamContent_IteratorVariant drives the iterator-backed
// QueryStreamContent over three dynamically produced prompts via
// PromptsFromSlice, proving symmetry with the string-based QueryStream
// and that the iterator plumbing works live.
func TestQueryStreamContent_IteratorVariant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	prompts := [][]acp.ContentBlock{
		{opencodesdk.TextBlock("Reply with just the digit: 1.")},
		{opencodesdk.TextBlock("Reply with just the digit: 2.")},
	}

	var seen []string

	for res, err := range opencodesdk.QueryStreamContent(ctx,
		opencodesdk.PromptsFromSlice(prompts),
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	) {
		if err != nil {
			skipIfCLIUnavailable(t, err)
			skipIfAuthRequired(t, err)
			t.Fatalf("QueryStreamContent: %v", err)
		}

		seen = append(seen, res.SessionID)
	}

	if len(seen) != len(prompts) {
		t.Fatalf("expected %d results, got %d", len(prompts), len(seen))
	}

	for i, sid := range seen {
		if sid != seen[0] {
			t.Fatalf("prompt %d: sessionID diverged; first=%q here=%q", i, seen[0], sid)
		}
	}
}

// TestWithAddDirs_CapabilityAdvertised checks whether opencode's live
// server exposes the ACP unstable SessionCapabilities.AdditionalDirectories
// capability. Both outcomes are legitimate: the SDK must accept
// WithAddDirs and drop it silently when the capability is missing.
//
// When opencode advertises the capability, we additionally run a
// one-shot Query with WithAddDirs to prove the extra directory is
// accepted on the wire.
func TestWithAddDirs_CapabilityAdvertised(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if startErr := c.Start(ctx); startErr != nil {
		skipIfCLIUnavailable(t, startErr)
		skipIfAuthRequired(t, startErr)
		t.Fatalf("Start: %v", startErr)
	}

	defer c.Close()

	caps := c.Capabilities()

	if caps.SessionCapabilities.AdditionalDirectories == nil {
		t.Logf("opencode %s does not advertise SessionCapabilities.AdditionalDirectories; WithAddDirs will no-op",
			c.AgentInfo().Version)

		return
	}

	t.Logf("opencode %s advertises additionalDirectories; running WithAddDirs probe", c.AgentInfo().Version)

	// Capability advertised — prove the field reaches the wire without
	// error. tempCwd(t) is absolute and exists, so it's a safe extra dir.
	_, qerr := opencodesdk.Query(ctx, "Reply with just: ok",
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithAddDirs(tempCwd(t)),
	)
	if qerr != nil {
		t.Fatalf("Query with WithAddDirs: %v", qerr)
	}
}

// TestWithPure_StartsCleanly asserts that spawning `opencode acp --pure`
// doesn't regress the default lifecycle — purely a smoke test; the CLI
// itself honours --pure regardless of SDK.
func TestWithPure_StartsCleanly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithPure(),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if startErr := c.Start(ctx); startErr != nil {
		skipIfCLIUnavailable(t, startErr)
		skipIfAuthRequired(t, startErr)
		t.Fatalf("Start: %v", startErr)
	}

	if closeErr := c.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
}
