// Demonstrates intra-session mutations: Session.SetModel and
// Session.SetMode. Both go through opencode's stable
// session/set_config_option path and fire SessionConfigOptionUpdate +
// CurrentModeUpdate notifications that typed subscribers can observe.
//
//	go run ./examples/session_mutations
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		exitf("Start: %v", startErr)
	}

	sess, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	// Dump the baseline negotiated at session/new.
	fmt.Printf("== baseline ==\n")

	if models := sess.InitialModels(); models != nil {
		fmt.Printf("initial model: %s\n", models.CurrentModelId)
		fmt.Printf("available models: %d\n", len(models.AvailableModels))
	}

	if modes := sess.InitialModes(); modes != nil {
		fmt.Printf("initial mode:  %s\n", modes.CurrentModeId)
	}

	fmt.Printf("available modes:\n")

	for _, m := range sess.AvailableModes() {
		fmt.Printf("  - %s (%s)\n", m.Id, m.Name)
	}

	// Typed subscribers so we can watch the mutations fire the right
	// session/update notifications.
	sess.Subscribe(opencodesdk.UpdateHandlers{
		ConfigOption: func(_ context.Context, upd *acp.SessionConfigOptionUpdate) {
			fmt.Printf("  [update] config_options (%d entries)\n", len(upd.ConfigOptions))
		},
		CurrentMode: func(_ context.Context, upd *acp.SessionCurrentModeUpdate) {
			fmt.Printf("  [update] current_mode = %s\n", upd.CurrentModeId)
		},
	})

	go func() {
		for range sess.Updates() {
		}
	}()

	// Pick a second model different from the initial one, if available.
	if models := sess.InitialModels(); models != nil && len(models.AvailableModels) >= 2 {
		alt := pickAlternateModel(models.AvailableModels, string(models.CurrentModelId))
		if alt != "" {
			fmt.Printf("\n== SetModel %s ==\n", alt)

			if mErr := sess.SetModel(ctx, alt); mErr != nil {
				fmt.Printf("SetModel failed (non-fatal): %v\n", mErr)
			}
		}
	}

	// Switch mode: build -> plan. Many default installs have both.
	fmt.Printf("\n== SetMode %s ==\n", opencodesdk.ModePlan)

	if mErr := sess.SetMode(ctx, opencodesdk.ModePlan); mErr != nil {
		fmt.Printf("SetMode failed (non-fatal): %v\n", mErr)
	}

	// Fire a small prompt so the turn runs under the new config.
	fmt.Printf("\n== prompt under new config ==\n")

	res, err := sess.Prompt(ctx, acp.TextBlock(
		"Reply with one short sentence naming the agent mode you are in.",
	))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	fmt.Printf("\nstop: %s\n", res.StopReason)
}

// pickAlternateModel returns a model id that differs from current. Tries
// to find one from the same provider first for a less drastic switch.
func pickAlternateModel(models []acp.ModelInfo, current string) string {
	var fallback string

	for _, m := range models {
		id := string(m.ModelId)
		if id == current {
			continue
		}

		if fallback == "" {
			fallback = id
		}

		// Prefer a model whose id shares the provider prefix of current.
		if prefix := providerPrefix(current); prefix != "" && strings.HasPrefix(id, prefix) {
			return id
		}
	}

	return fallback
}

func providerPrefix(modelID string) string {
	if i := strings.Index(modelID, "/"); i > 0 {
		return modelID[:i+1]
	}

	return ""
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
