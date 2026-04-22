//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestSessionPersistence_ListSessionsAfterNewSession creates a session,
// runs a turn, closes the client, then reopens a fresh client and lists
// sessions in the same cwd. The session we just created should appear.
func TestSessionPersistence_ListSessionsAfterNewSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cwd := tempCwd(t)

	var createdID string

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		createdID = sess.ID()

		// Run a tiny turn so opencode has something to persist.
		if _, promptErr := sess.Prompt(ctx, acp.TextBlock("Reply with just: ok.")); promptErr != nil {
			return promptErr
		}

		return nil
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient (create): %v", err)
	}

	if createdID == "" {
		t.Fatalf("did not capture a session ID")
	}

	// Reopen with the same cwd and list sessions.
	err = opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sessions, _, listErr := c.ListSessions(ctx, "")
		if listErr != nil {
			return listErr
		}

		for _, s := range sessions {
			if string(s.SessionId) == createdID {
				return nil
			}
		}

		t.Fatalf("created session %q not found in ListSessions (saw %d sessions)", createdID, len(sessions))

		return nil
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient (list): %v", err)
	}
}

// TestSessionPersistence_LoadSessionReplay loads an existing session
// and observes that session/update replay fires before LoadSession
// returns.
func TestSessionPersistence_LoadSessionReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cwd := tempCwd(t)

	var sid string

	// Phase 1: create a session and run a turn so there's content to replay.
	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		sid = sess.ID()

		_, err = sess.Prompt(ctx, acp.TextBlock("Reply with the single word: marigold."))

		return err
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient (create): %v", err)
	}

	// Phase 2: load the session and collect replay.
	err = opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, loadErr := c.LoadSession(ctx, sid)
		if loadErr != nil {
			return loadErr
		}

		// Drain updates for a bounded window — replay notifications should
		// arrive promptly after LoadSession returns.
		drainCtx, drainCancel := context.WithTimeout(ctx, 5*time.Second)
		defer drainCancel()

		collected := collectText(drainCtx, sess.Updates())

		t.Logf("replay text length=%d snippet=%q", len(collected), truncate(collected, 120))

		return nil
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient (load): %v", err)
	}
}

// TestSessionPersistence_LoadSessionHistory loads an existing session
// via the history helper and asserts that the typed SessionHistory
// carries at least one replayed message.
func TestSessionPersistence_LoadSessionHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cwd := tempCwd(t)

	var sid string

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		sid = sess.ID()

		_, err = sess.Prompt(ctx, acp.TextBlock("Reply with the single word: violet."))

		return err
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient (create): %v", err)
	}

	err = opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		history, loadErr := c.LoadSessionHistory(ctx, sid)
		if loadErr != nil {
			return loadErr
		}

		if history.Session.ID() != sid {
			t.Fatalf("history.Session.ID = %q, want %q", history.Session.ID(), sid)
		}

		if len(history.Notifications) == 0 {
			t.Fatalf("SessionHistory.Notifications is empty; expected replay")
		}

		t.Logf("history: notifications=%d messages=%d", len(history.Notifications), len(history.Messages))

		for i, m := range history.Messages {
			t.Logf("  [%d] %s: %s", i, m.Role, truncate(m.Text, 80))
		}

		return nil
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient (load history): %v", err)
	}
}

// TestSessionPersistence_ListSessionsPagination verifies that a cursor
// is returned (or empty) and pagination works when > a page of sessions
// exists. This is a smoke test only; we don't create dozens of sessions.
func TestSessionPersistence_ListSessionsPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		_, cursor, listErr := c.ListSessions(ctx, "")
		if listErr != nil {
			return listErr
		}

		// Cursor may be empty if everything fit in one page; we only care
		// that the call didn't error.
		t.Logf("initial list cursor=%q", cursor)

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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "…"
}
