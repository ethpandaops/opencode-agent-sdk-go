// Demonstrates WithMaxTurns: a client-side cap on the number of
// assistant messages observed per session. opencode has no
// protocol-level turn limit, so the SDK counts distinct assistant
// message ids and calls Session.Cancel once the cap is crossed —
// useful as a backstop against runaway agent loops.
//
//	go run ./examples/max_turns
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx, opencodesdk.WithMaxTurns(2))
		if err != nil {
			return fmt.Errorf("new session: %w", err)
		}

		go func() {
			for n := range sess.Updates() {
				if n.Update.AgentMessageChunk != nil &&
					n.Update.AgentMessageChunk.Content.Text != nil {
					fmt.Print(n.Update.AgentMessageChunk.Content.Text.Text)
				}
			}
		}()

		// A multi-step prompt that would otherwise produce many
		// assistant messages. With WithMaxTurns(2) the SDK cancels the
		// in-flight turn shortly after the second assistant message.
		prompt := "Plan and execute a 5-step research project on the history of Go. " +
			"Provide each step as a separate detailed message."

		_, promptErr := sess.Prompt(ctx, acp.TextBlock(prompt))

		fmt.Println()

		switch {
		case errors.Is(promptErr, opencodesdk.ErrCancelled):
			fmt.Println("\nturn cancelled by WithMaxTurns(2) — expected.")
		case promptErr != nil:
			return fmt.Errorf("prompt: %w", promptErr)
		default:
			fmt.Println("\nprompt completed before the turn cap was reached.")
		}

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
