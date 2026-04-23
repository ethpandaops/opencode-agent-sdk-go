// Demonstrates opencodesdk.QueryStreamContent — the iterator-backed
// multimodal variant of QueryStream.
//
// Unlike QueryStream (which takes a []string pre-materialised), this
// variant drives opencode from any iter.Seq[[]ContentBlock] source.
// The SDK provides four helper constructors:
//
//   - opencodesdk.PromptsFromStrings  — []string  → iter.Seq
//   - opencodesdk.PromptsFromSlice    — [][]CB    → iter.Seq
//   - opencodesdk.PromptsFromChannel  — channel   → iter.Seq  (dynamic)
//   - opencodesdk.SinglePrompt        — single    → iter.Seq
//
// This example uses PromptsFromChannel to feed prompts generated on
// the fly: a goroutine produces three follow-up prompts and closes the
// channel to signal end-of-stream. Useful for interactive CLIs, chat
// bots, or queue consumers.
//
//	go run ./examples/query_stream_iter
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Produce prompts dynamically. In a real app this might be reading
	// from stdin, a message queue, or chained off previous results.
	ch := make(chan []acp.ContentBlock, 3)

	go func() {
		defer close(ch)

		for _, q := range []string{
			"Reply with just the digit: 2 + 2.",
			"Reply with just the digit: 10 * 4.",
			"Reply with just the digit: 100 - 58.",
		} {
			ch <- opencodesdk.Text(q)
		}
	}()

	start := time.Now()
	cursor := 0

	for res, err := range opencodesdk.QueryStreamContent(ctx,
		opencodesdk.PromptsFromChannel(ch),
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
	) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "prompt %d: %v\n", cursor, err)

			break
		}

		cursor++

		fmt.Printf("→ answer %d: %s\n", cursor, res.AssistantText)

		if res.Usage != nil {
			fmt.Printf("  tokens: in=%d out=%d total=%d\n",
				res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.TotalTokens)
		}

		fmt.Println()
	}

	fmt.Printf("elapsed: %s\n", time.Since(start).Round(time.Millisecond))
}
