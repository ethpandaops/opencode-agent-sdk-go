// Example: ResilientQuery applies exponential backoff + jitter for
// retryable errors (rate limit, overload, transient connection).
// Authentication, capability, and CLI errors surface immediately.
// Run with `go run ./examples/resilient_query`.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cwd, _ := os.Getwd()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := opencodesdk.ResilientQuery(ctx,
		"Give me a one-sentence Go tip.",
		opencodesdk.ResilientQueryOptions{
			RetryPolicy: opencodesdk.RetryPolicy{
				MaxRetries:  3,
				BaseDelay:   time.Second,
				MaxDelay:    10 * time.Second,
				JitterRatio: 0.3,
			},
			Logger: logger,
			OnRetry: func(_ context.Context, attempt int, decision opencodesdk.RetryDecision, cause error) {
				fmt.Fprintf(os.Stderr, "[retry %d] class=%s delay=%s cause=%v\n",
					attempt, decision.Class, decision.RecommendedDelay, cause)
			},
		},
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		classification := opencodesdk.ClassifyError(err)
		fmt.Fprintf(os.Stderr, "query failed: class=%s action=%s err=%v\n",
			classification.Class, classification.RecoveryAction, err)

		os.Exit(1)
	}

	fmt.Println(result.AssistantText)
}
