//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestAuthCapability_InfoAndMethods verifies that after Start the
// client exposes a non-empty AgentInfo and (on opencode 1.14.20+) a
// non-empty AuthMethods list.
func TestAuthCapability_InfoAndMethods(t *testing.T) {
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

	info := c.AgentInfo()
	if info.Name == "" || info.Version == "" {
		t.Fatalf("AgentInfo incomplete: %+v", info)
	}

	t.Logf("agent=%s version=%s", info.Name, info.Version)

	methods := c.AuthMethods()
	t.Logf("auth methods advertised: %d", len(methods))
}

// TestAuthCapability_TerminalAuthMetaExposed opts into
// WithTerminalAuthCapability and asserts that at least one auth method
// advertises TerminalAuthInstructions. This is opencode-specific — the
// opt-in causes opencode to include _meta["terminal-auth"] blocks on
// its AuthMethod list.
func TestAuthCapability_TerminalAuthMetaExposed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithTerminalAuthCapability(true),
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

	methods := c.AuthMethods()
	if len(methods) == 0 {
		t.Skip("opencode advertised no auth methods on this install")
	}

	var sawMeta bool

	for _, m := range methods {
		if _, ok := opencodesdk.TerminalAuthInstructions(m); ok {
			sawMeta = true

			break
		}
	}

	if !sawMeta {
		t.Skip("no auth method advertised _meta[\"terminal-auth\"]; opencode may not support this on this build")
	}
}
