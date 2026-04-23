// Demonstrates Session.Subscribe — typed per-variant callbacks for
// session/update notifications. Use this instead of (or alongside)
// the Session.Updates() channel when you want a focused handler per
// update kind without a giant switch statement.
//
//	go run ./examples/typed_subscribers
package main

import (
	"context"
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

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithModel("opencode/big-pickle"),
		opencodesdk.WithOnTurnComplete(func(_ context.Context, sid string, res *opencodesdk.PromptResult, err error) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "\n[turn-complete %s] error: %v\n", sid, err)

				return
			}

			fmt.Fprintf(os.Stderr, "\n[turn-complete %s] stop=%s\n", sid, res.StopReason)
		}),
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		exitf("Start: %v", startErr)
	}

	sess, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	unsub := sess.Subscribe(opencodesdk.UpdateHandlers{
		AgentMessage: func(_ context.Context, chunk *acp.SessionUpdateAgentMessageChunk) {
			if chunk.Content.Text != nil {
				fmt.Print(chunk.Content.Text.Text)
			}
		},
		ToolCall: func(_ context.Context, tc *acp.SessionUpdateToolCall) {
			fmt.Printf("\n[tool %s] %s\n", tc.Kind, tc.Title)
		},
		Plan: func(_ context.Context, plan *acp.SessionUpdatePlan) {
			fmt.Printf("\n[plan] %d entries\n", len(plan.Entries))
		},
		Usage: func(_ context.Context, upd *acp.SessionUsageUpdate) {
			if upd.Cost != nil {
				fmt.Printf("\n[usage] cost=%v %s\n", upd.Cost.Amount, upd.Cost.Currency)
			}
		},
	})
	defer unsub()

	res, err := sess.Prompt(ctx, opencodesdk.TextBlock("In one paragraph, describe the Agent Client Protocol."))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	fmt.Printf("\n\nstop=%s, dropped_updates=%d\n", res.StopReason, sess.DroppedUpdates())
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
