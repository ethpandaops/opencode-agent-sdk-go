// Demonstrates WithEffort: opencode encodes reasoning depth as a
// `/<variant>` suffix on a model id (e.g.
// "anthropic/claude-opus-4-7/high"). WithEffort maps an abstract
// level (None/Low/Medium/High/Max) to whatever variant the chosen
// model exposes, with sensible fallback when the requested level is
// unavailable.
//
//	go run ./examples/effort
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cwd, _ := os.Getwd()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		// Apply WithEffort at session creation. The SDK probes the
		// session's current model for available variants and re-applies
		// session/set_model with the chosen `<base>/<variant>` suffix.
		sess, err := c.NewSession(ctx, opencodesdk.WithEffort(opencodesdk.EffortHigh))
		if err != nil {
			return fmt.Errorf("new session: %w", err)
		}

		variant := sess.CurrentVariant()
		if variant != nil {
			fmt.Printf("base model:        %s\n", variant.ModelId)
			fmt.Printf("current variant:   %q\n", variant.Variant)
			fmt.Printf("available variants: %s\n", strings.Join(variant.AvailableVariants, ", "))
		} else {
			fmt.Println("session did not advertise variant info; the configured model")
			fmt.Println("probably has no variants — WithEffort is a documented no-op for")
			fmt.Println("chat-only / embedding / TTS models.")
		}

		go func() {
			for range sess.Updates() {
			}
		}()

		_, err = sess.Prompt(ctx, acp.TextBlock("Reply with just: ok."))
		if err != nil {
			return fmt.Errorf("prompt: %w", err)
		}

		fmt.Println("\nprompt completed successfully under selected effort.")

		return nil
	},
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WithClient: %v\n", err)
		os.Exit(1)
	}
}
