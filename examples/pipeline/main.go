// Demonstrates a multi-step LLM pipeline with Go-side gating. This
// shape pops up constantly in production agent flows:
//
//  1. generate — ask the model for an initial draft
//  2. evaluate — ask the model to score the draft 1-10
//  3. gate (Go) — parse the score; decide whether to refine
//  4. refine — if needed, ask the model to tighten the draft
//
// All four calls share one opencode session so later prompts see the
// earlier turns as conversation history. That keeps token usage
// low and ensures the model is evaluating/refining the same draft it
// wrote a moment ago.
//
//	go run ./examples/pipeline
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

const scoreThreshold = 7

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return fmt.Errorf("new session: %w", err)
		}

		// Step 1: generate
		draft, err := ask(ctx, sess,
			"Write one paragraph of marketing copy for a new set of noise-cancelling headphones. "+
				"Two to three sentences. No preamble.",
		)
		if err != nil {
			return fmt.Errorf("generate: %w", err)
		}

		fmt.Println("== draft ==")
		fmt.Println(draft)

		// Step 2: evaluate
		eval, err := ask(ctx, sess,
			"Rate the copy you just wrote from 1-10 on how concise and compelling it is. "+
				"Reply with ONLY the integer score on its own line, no explanation.",
		)
		if err != nil {
			return fmt.Errorf("evaluate: %w", err)
		}

		fmt.Printf("\n== score == %s\n", strings.TrimSpace(eval))

		// Step 3: gate (pure Go — no LLM call)
		score := parseScore(eval)

		if score >= scoreThreshold {
			fmt.Printf("\n[gate] score %d ≥ %d — shipping as-is\n", score, scoreThreshold)

			return nil
		}

		fmt.Printf("\n[gate] score %d < %d — refining\n", score, scoreThreshold)

		// Step 4: refine
		refined, err := ask(ctx, sess,
			"Rewrite the copy to be tighter and punchier. Keep it to two sentences max. "+
				"Reply with ONLY the rewritten copy.",
		)
		if err != nil {
			return fmt.Errorf("refine: %w", err)
		}

		fmt.Println("\n== refined ==")
		fmt.Println(refined)

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

// ask submits a prompt to the session and aggregates the agent's text
// response by draining the Updates channel during the turn.
func ask(ctx context.Context, sess opencodesdk.Session, prompt string) (string, error) {
	done := make(chan struct{})

	var text strings.Builder

	go func() {
		defer close(done)

		for n := range sess.Updates() {
			if n.Update.AgentMessageChunk == nil {
				continue
			}

			if n.Update.AgentMessageChunk.Content.Text != nil {
				text.WriteString(n.Update.AgentMessageChunk.Content.Text.Text)
			}
		}
	}()

	_, err := sess.Prompt(ctx, acp.TextBlock(prompt))
	// Give the drain goroutine a moment to catch the final chunks.
	time.Sleep(100 * time.Millisecond)
	// The drain goroutine only exits on channel close (happens on
	// Client.Close); we read the accumulated text without waiting.
	_ = done

	return strings.TrimSpace(text.String()), err
}

// parseScore tolerates models that preface the number with junk; it
// returns the first integer found, or 0 if none.
func parseScore(s string) int {
	var digits strings.Builder

	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)

			continue
		}

		if digits.Len() > 0 {
			break
		}
	}

	if digits.Len() == 0 {
		return 0
	}

	n, err := strconv.Atoi(digits.String())
	if err != nil {
		return 0
	}

	return n
}
