// Example: per-session cost tracking via CostTracker, persisted to
// $XDG_DATA_HOME/opencode/sdk/session-costs/<id>.json. Run with
// `go run ./examples/cost_tracker`.
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

	tracker := opencodesdk.NewCostTracker()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
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

	// Subscribe the tracker to every UsageUpdate for this session.
	unsub := sess.Subscribe(opencodesdk.UpdateHandlers{
		Usage: tracker.ObserveUsage(sess.ID()),
	})

	defer unsub()

	result, err := sess.Prompt(ctx, acp.TextBlock("Explain what opencode is in one sentence."))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	tracker.ObservePromptResult(sess.ID(), result)

	snap := tracker.Snapshot()
	fmt.Printf("sessions: %d   cost: $%.4f USD   tokens: in=%d out=%d\n",
		snap.Sessions, snap.TotalCostUSD, snap.InputTokens, snap.OutputTokens)

	if err := tracker.SaveSessionCost(sess.ID(), opencodesdk.SessionCostOptions{}); err != nil {
		fmt.Fprintf(os.Stderr, "SaveSessionCost: %v\n", err)
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
