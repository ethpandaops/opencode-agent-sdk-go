//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestLifecycle_StartClose covers the happy path: NewClient → Start →
// Close with no sessions. Verifies capabilities and agent info are
// populated after Start.
func TestLifecycle_StartClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if err := c.Start(ctx); err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Start: %v", err)
	}

	if c.AgentInfo().Name == "" {
		t.Fatalf("AgentInfo.Name empty after Start; expected opencode to self-identify")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestLifecycle_DoubleCloseIsNoop — Close must be idempotent.
func TestLifecycle_DoubleCloseIsNoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := c.Start(ctx); err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Start: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("second Close must be a no-op, got: %v", err)
	}
}

// TestLifecycle_CloseBeforeStart — Close before Start must not error.
func TestLifecycle_CloseBeforeStart(t *testing.T) {
	c, err := opencodesdk.NewClient(opencodesdk.WithLogger(testLogger(t)))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close before Start should succeed; got: %v", err)
	}
}

// TestLifecycle_MethodsBeforeStart — ensureStarted gate returns ErrClientNotStarted.
func TestLifecycle_MethodsBeforeStart(t *testing.T) {
	c, err := opencodesdk.NewClient(opencodesdk.WithLogger(testLogger(t)))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	_, err = c.NewSession(context.Background())
	if !errors.Is(err, opencodesdk.ErrClientNotStarted) {
		t.Fatalf("NewSession before Start: want ErrClientNotStarted, got %v", err)
	}
}

// TestLifecycle_MethodsAfterClose — methods must return ErrClientClosed.
func TestLifecycle_MethodsAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	if closeErr := c.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	_, err = c.NewSession(ctx)
	if !errors.Is(err, opencodesdk.ErrClientClosed) {
		t.Fatalf("NewSession after Close: want ErrClientClosed, got %v", err)
	}
}

// TestLifecycle_StartWithBogusCLIPath — ErrCLINotFound surfaces when
// the pinned binary does not exist.
func TestLifecycle_StartWithBogusCLIPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCLIPath("/definitely/not/a/real/opencode-binary"),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	err = c.Start(ctx)
	if !errors.Is(err, opencodesdk.ErrCLINotFound) {
		t.Fatalf("Start with bogus path: want ErrCLINotFound, got %v", err)
	}
}

// TestLifecycle_DoubleStart_ReturnsErrClientAlreadyConnected — Start
// must refuse to run twice on the same Client.
func TestLifecycle_DoubleStart_ReturnsErrClientAlreadyConnected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if err := c.Start(ctx); err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("first Start: %v", err)
	}

	err = c.Start(ctx)
	if !errors.Is(err, opencodesdk.ErrClientAlreadyConnected) {
		t.Fatalf("second Start: want ErrClientAlreadyConnected, got %v", err)
	}
}

// TestLifecycle_CancelAll_NoSessions_ReturnsNil — CancelAll on a
// freshly started client with no sessions is a no-op.
func TestLifecycle_CancelAll_NoSessions_ReturnsNil(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if err := c.Start(ctx); err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Start: %v", err)
	}

	if err := c.CancelAll(ctx); err != nil {
		t.Fatalf("CancelAll: %v", err)
	}
}

// TestLifecycle_AvailableModes_ReturnsOpencodeDefaults — a freshly
// created session should expose at least the built-in ModeBuild /
// ModePlan modes via Session.AvailableModes.
func TestLifecycle_AvailableModes_ReturnsOpencodeDefaults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if err := c.Start(ctx); err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Start: %v", err)
	}

	sess, err := c.NewSession(ctx)
	if err != nil {
		skipIfAuthRequired(t, err)
		t.Fatalf("NewSession: %v", err)
	}

	modes := sess.AvailableModes()
	if len(modes) == 0 {
		t.Fatalf("AvailableModes empty; expected opencode to advertise at least build+plan")
	}

	ids := make(map[string]bool, len(modes))
	for _, m := range modes {
		ids[string(m.Id)] = true
	}

	if !ids[opencodesdk.ModeBuild] {
		t.Errorf("AvailableModes missing %q; got %+v", opencodesdk.ModeBuild, modes)
	}

	if !ids[opencodesdk.ModePlan] {
		t.Errorf("AvailableModes missing %q; got %+v", opencodesdk.ModePlan, modes)
	}
}

// TestLifecycle_RapidStartCloseCycle — repeatedly spin up and tear
// down a client. Catches leaks or races in the shutdown path.
func TestLifecycle_RapidStartCloseCycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cwd := tempCwd(t)

	for i := range 3 {
		c, err := opencodesdk.NewClient(
			opencodesdk.WithLogger(testLogger(t)),
			opencodesdk.WithCwd(cwd),
		)
		if err != nil {
			t.Fatalf("iteration %d: NewClient: %v", i, err)
		}

		if err := c.Start(ctx); err != nil {
			skipIfCLIUnavailable(t, err)
			skipIfAuthRequired(t, err)
			t.Fatalf("iteration %d: Start: %v", i, err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("iteration %d: Close: %v", i, err)
		}
	}
}
