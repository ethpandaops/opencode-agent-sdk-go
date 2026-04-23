// Demonstrates opencodesdk.QueryStream for running a series of prompts
// against a single, long-lived opencode session and iterating results
// as they arrive.
//
// Unlike [opencodesdk.Query] (one-shot) or [opencodesdk.WithClient]
// (manual session management), QueryStream reuses a single subprocess
// + session for the entire prompt list and yields one [QueryResult]
// per prompt via a Go 1.23 range-over-func iterator.
//
//	go run ./examples/query_stream
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	prompts := []string{
		"Reply with just the number: what is 2 + 2?",
		"Reply with just the number: what is 10 * 4?",
		"Reply with just the number: what is 100 - 58?",
	}

	start := time.Now()

	cursor := 0

	for res, err := range opencodesdk.QueryStream(ctx, prompts,
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithModel("opencode/big-pickle"),
	) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "QueryStream error at prompt %d: %v\n", cursor, err)

			break
		}

		fmt.Printf("→ prompt: %s\n", trimToOneLine(prompts[cursor]))
		fmt.Printf("  answer: %s\n", trimToOneLine(res.AssistantText))

		if res.Usage != nil {
			fmt.Printf("  tokens: in=%d out=%d total=%d\n",
				res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.TotalTokens)
		}

		fmt.Println()

		cursor++
	}

	fmt.Printf("elapsed: %s\n", time.Since(start).Round(time.Millisecond))
}

// trimToOneLine keeps printed output scannable regardless of what the
// model returned.
func trimToOneLine(s string) string {
	const limit = 120

	out := make([]rune, 0, limit)

	for _, r := range s {
		if r == '\n' || r == '\r' {
			break
		}

		out = append(out, r)

		if len(out) >= limit {
			break
		}
	}

	return string(out)
}
