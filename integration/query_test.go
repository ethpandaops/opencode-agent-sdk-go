//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestQuery_OneShot drives the top-level Query helper end-to-end:
// spawn, session, one prompt, teardown.
func TestQuery_OneShot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := opencodesdk.Query(ctx,
		"Reply with only the digit: 7.",
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Query: %v", err)
	}

	if res.SessionID == "" {
		t.Fatalf("SessionID empty")
	}

	if res.StopReason == "" {
		t.Fatalf("StopReason empty; expected end_turn")
	}

	if len(res.Notifications) == 0 {
		t.Fatalf("Notifications empty; expected at least one session/update")
	}
}

// TestQueryStream_MultiPrompt drives QueryStream over three prompts
// and verifies each yields a non-nil QueryResult with matching
// SessionID (single shared session).
func TestQueryStream_MultiPrompt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	prompts := []string{
		"Reply with just the digit: 1.",
		"Reply with just the digit: 2.",
		"Reply with just the digit: 3.",
	}

	var seen []string

	for res, err := range opencodesdk.QueryStream(ctx, prompts,
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	) {
		if err != nil {
			skipIfCLIUnavailable(t, err)
			skipIfAuthRequired(t, err)
			t.Fatalf("QueryStream: %v", err)
		}

		seen = append(seen, res.SessionID)
	}

	if len(seen) != len(prompts) {
		t.Fatalf("expected %d results, got %d", len(prompts), len(seen))
	}

	// All results should share the same session.
	for i, sid := range seen {
		if sid != seen[0] {
			t.Fatalf("prompt %d: sessionID diverged; first=%q here=%q", i, seen[0], sid)
		}
	}
}
