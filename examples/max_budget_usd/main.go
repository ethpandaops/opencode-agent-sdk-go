// Example: cap total session spend with WithMaxBudgetUSD. The SDK
// observes usage_update notifications, tracks cumulative cost, and
// calls Session.Cancel when the budget is exceeded.
//
// Run with `go run ./examples/max_budget_usd`.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	budget := flag.Float64("budget", 0.01, "max cumulative USD spend")

	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithMaxBudgetUSD(*budget),
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

	// Run a prompt that'll exercise usage accounting. With a very low
	// budget (default $0.01) the SDK will auto-cancel.
	_, promptErr := sess.Prompt(ctx, acp.TextBlock(
		"Write a 500-word essay on the history of the AT protocol.",
	))

	status := c.BudgetTracker().Status()

	fmt.Printf("cost: $%.4f USD / $%.4f cap  (ratio=%.2f  reason=%s)\n",
		status.Cost.TotalCostUSD, *status.MaxCostUSD, status.CompletionRatio, status.Reason)

	if promptErr != nil {
		switch {
		case errors.Is(promptErr, opencodesdk.ErrCancelled):
			fmt.Println("prompt cancelled (likely by budget enforcement)")
		default:
			fmt.Fprintf(os.Stderr, "Prompt: %v\n", promptErr)
		}
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
