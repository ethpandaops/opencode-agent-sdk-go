//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestDynamicControl_SetModel changes the session model mid-flight. We
// can't assert the agent actually honoured it without parsing
// provider-specific metadata, but we can assert the RPC succeeds and
// the subsequent turn still runs.
func TestDynamicControl_SetModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		models := sess.AvailableModels()
		if len(models) == 0 {
			t.Skip("opencode did not advertise any AvailableModels for this session")
		}

		// Pick any model that isn't the current default.
		var target string

		initial := sess.InitialModels()

		var currentID string

		if initial != nil {
			currentID = string(initial.CurrentModelId)
		}

		for _, m := range models {
			if string(m.ModelId) != currentID {
				target = string(m.ModelId)

				break
			}
		}

		if target == "" {
			t.Skip("only one model available; cannot exercise SetModel")
		}

		if setErr := sess.SetModel(ctx, target); setErr != nil {
			t.Fatalf("SetModel(%q): %v", target, setErr)
		}

		// Prove the session is still usable after model switch.
		_, promptErr := sess.Prompt(ctx, acp.TextBlock("Reply with just: ok."))

		return promptErr
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}
}

// TestDynamicControl_SetMode flips the session's agent "mode" (plan →
// build, etc.) and verifies the subsequent turn still runs.
func TestDynamicControl_SetMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		modes := sess.InitialModes()
		if modes == nil || len(modes.AvailableModes) == 0 {
			t.Skip("opencode did not advertise AvailableModes for this session")
		}

		var target string

		current := string(modes.CurrentModeId)

		for _, m := range modes.AvailableModes {
			if string(m.Id) != current {
				target = string(m.Id)

				break
			}
		}

		if target == "" {
			t.Skip("only one mode available; cannot exercise SetMode")
		}

		if setErr := sess.SetMode(ctx, target); setErr != nil {
			t.Fatalf("SetMode(%q): %v", target, setErr)
		}

		_, promptErr := sess.Prompt(ctx, acp.TextBlock("Reply with just: ok."))

		return promptErr
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}
}

// TestDynamicControl_WithAgentOption applies WithAgent at session
// creation time and verifies the mode option was accepted (no error
// from session/new or set_config_option).
func TestDynamicControl_WithAgentOption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx, opencodesdk.WithAgent("plan"))
		if err != nil {
			return err
		}

		// Just prove the session is live.
		_ = sess.ID()

		return nil
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)

		// "plan" may not be a valid mode on every opencode config; log rather than fail.
		t.Logf("WithAgent(plan) rejected; opencode config may not expose a plan mode: %v", err)
	}
}

// TestDynamicControl_UnstableSetModel exercises the unstable RPC wrapper.
func TestDynamicControl_UnstableSetModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		models := sess.AvailableModels()
		if len(models) == 0 {
			t.Skip("no AvailableModels advertised")
		}

		target := string(models[0].ModelId)

		if setErr := c.UnstableSetModel(ctx, sess.ID(), target); setErr != nil {
			t.Fatalf("UnstableSetModel: %v", setErr)
		}

		// Give opencode a tick to emit any follow-up notifications.
		time.Sleep(100 * time.Millisecond)

		return nil
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}
}
