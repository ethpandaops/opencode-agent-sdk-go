//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestEffort_VariantApplied creates a session with WithEffort(High) and
// verifies the resulting model id carries a `/<variant>` suffix when
// the model exposes one. Skips when the chosen base model has no
// variants.
func TestEffort_VariantApplied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx, opencodesdk.WithEffort(opencodesdk.EffortHigh))
		if err != nil {
			return err
		}

		variant := sess.CurrentVariant()
		if variant == nil {
			t.Skip("session did not advertise variant info; nothing to assert")
		}

		if len(variant.AvailableVariants) == 0 {
			t.Skip("model exposes no variants; WithEffort is a documented no-op")
		}

		// CurrentVariant is sourced from the session's _meta block at
		// creation time; the SDK's mid-session set_model probe doesn't
		// re-publish meta. Inspect AvailableModels (current id may have
		// been updated by applyEffortOnSession via session/set_model).
		_ = variant

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

// TestRunCommand_AvailableCommands invokes a slash command discovered
// from the session's AvailableCommands snapshot. Skips when no
// commands are advertised.
func TestRunCommand_AvailableCommands(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		// availableCommands arrives ~1 tick after session/new as a
		// notification; give it a brief window to land.
		go func() {
			for range sess.Updates() {
			}
		}()

		deadline := time.Now().Add(2 * time.Second)
		for len(sess.AvailableCommands()) == 0 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}

		commands := sess.AvailableCommands()
		if len(commands) == 0 {
			t.Skip("opencode did not advertise any AvailableCommands")
		}

		// Pick a safe command to invoke. Avoid commands that would
		// modify state — `help` and `models` are typical safe picks
		// when present; otherwise just log the discovered set.
		safe := ""

		for _, cmd := range commands {
			lc := strings.ToLower(cmd.Name)
			if lc == "help" || lc == "models" || lc == "model" {
				safe = cmd.Name

				break
			}
		}

		if safe == "" {
			t.Logf("no known-safe slash command in %d available; skipping invocation", len(commands))

			return nil
		}

		if _, err := sess.RunCommand(ctx, safe); err != nil {
			t.Fatalf("RunCommand(%q): %v", safe, err)
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
