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
// the model exposes one. Pins the model to opencode/gpt-5-nano (a free
// variant-capable model) so the assertion path is reachable without
// relying on the user's default model.
func TestEffort_VariantApplied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx,
			opencodesdk.WithModel("opencode/gpt-5-nano"),
			opencodesdk.WithEffort(opencodesdk.EffortHigh),
		)
		if err != nil {
			return err
		}

		variant := sess.CurrentVariant()
		if variant == nil {
			t.Fatal("CurrentVariant returned nil after WithModel + WithEffort")
		}

		if len(variant.AvailableVariants) == 0 {
			t.Fatalf("opencode/gpt-5-nano advertised no variants: %+v", variant)
		}

		if variant.Variant == "" {
			t.Fatalf("WithEffort(High) did not resolve to a variant: %+v", variant)
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

		// Pick a safe command to invoke. `compact` is an opencode
		// built-in that compacts the current session — safe in this
		// throwaway session and always present. `help` / `models` are
		// accepted too for forward-compat in case future opencode
		// versions advertise them.
		safe := ""

		for _, cmd := range commands {
			lc := strings.ToLower(cmd.Name)
			if lc == "compact" || lc == "help" || lc == "models" || lc == "model" {
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
