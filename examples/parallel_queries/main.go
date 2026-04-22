// Demonstrates running multiple opencode queries concurrently — each
// in its own subprocess + session — and aggregating the answers in Go.
//
// Because each [opencodesdk.Query] call spawns its own opencode CLI,
// these run fully in parallel on multi-core machines. For many small
// prompts the per-call subprocess startup cost will dominate; for
// expensive model turns the parallelism wins handily.
//
//	go run ./examples/parallel_queries
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

type result struct {
	style   string
	text    string
	err     error
	elapsed time.Duration
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	original := "The release ships on Friday. Deploy after review."

	styles := map[string]string{
		"formal":   "Rewrite this sentence in a formal business tone, no preamble: " + original,
		"casual":   "Rewrite this sentence casually for a team Slack, no preamble: " + original,
		"pirate":   "Rewrite this sentence in pirate-speak, no preamble: " + original,
		"haiku":    "Rewrite this sentence as a single English-language haiku (5-7-5 syllables), no preamble: " + original,
		"one-word": "Summarise this sentence in a single word, no preamble: " + original,
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []result
	)

	for style, prompt := range styles {
		wg.Add(1)

		go func() {
			defer wg.Done()

			started := time.Now()

			res, err := opencodesdk.Query(ctx, prompt,
				opencodesdk.WithLogger(logger),
				opencodesdk.WithCwd(cwd),
			)

			r := result{style: style, elapsed: time.Since(started), err: err}
			if res != nil {
				r.text = res.AssistantText
			}

			mu.Lock()

			results = append(results, r)

			mu.Unlock()
		}()
	}

	wg.Wait()

	fmt.Printf("original: %s\n\n", original)

	for _, r := range results {
		if r.err != nil {
			fmt.Printf("[%s] error after %s: %v\n", r.style, r.elapsed.Round(time.Millisecond), r.err)

			continue
		}

		fmt.Printf("[%s] (%s)\n%s\n\n", r.style, r.elapsed.Round(time.Millisecond), r.text)
	}
}
