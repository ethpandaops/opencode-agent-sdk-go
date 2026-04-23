// Demonstrates Client.LoadSession — rehydrate a previous opencode
// session by id. Unlike ResumeSession, LoadSession replays the full
// message history as session/update notifications before it returns;
// the SDK buffers those replay events and delivers them on the returned
// Session's Updates() channel. Use LoadSession when you want the
// client UI to re-render the prior transcript; use ResumeSession when
// you just want to keep prompting without the replay traffic.
//
//	go run ./examples/load_session
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
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		exitf("Start: %v", startErr)
	}

	// 1. Create a session and run one turn, then stop interacting with it.
	fmt.Println("== seeding a session with one turn ==")

	seed, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	go func() {
		for range seed.Updates() {
		}
	}()

	_, err = seed.Prompt(ctx, acp.TextBlock(
		"In one short sentence, name a color of the rainbow. Just the sentence, no extras.",
	))
	if err != nil {
		exitf("seed Prompt: %v", err)
	}

	id := seed.ID()
	fmt.Printf("seeded session: %s\n\n", id)

	// 2. Load it. opencode replays the transcript via session/update
	//    notifications; the SDK buffers those and hands them off on
	//    Updates() once LoadSession returns.
	fmt.Println("== loading it back ==")

	loaded, err := c.LoadSession(ctx, id)
	if err != nil {
		exitf("LoadSession: %v", err)
	}

	fmt.Printf("loaded session: %s\n", loaded.ID())

	// Drain the replay. Give opencode a short window to flush any
	// remaining notifications; the non-blocking default on the select
	// lets us exit cleanly even when the stream idles.
	var (
		userChunks  int
		agentChunks int
		otherUpds   int
	)

drain:
	for {
		select {
		case n, ok := <-loaded.Updates():
			if !ok {
				break drain
			}

			switch {
			case n.Update.UserMessageChunk != nil:
				userChunks++
			case n.Update.AgentMessageChunk != nil:
				agentChunks++
			default:
				otherUpds++
			}
		case <-time.After(500 * time.Millisecond):
			break drain
		}
	}

	fmt.Printf("replay events observed: user=%d agent=%d other=%d\n",
		userChunks, agentChunks, otherUpds)

	// 3. Continue the loaded session and confirm the memory is intact.
	fmt.Println("\n== prompting the loaded session ==")

	go func() {
		for range loaded.Updates() {
		}
	}()

	res, err := loaded.Prompt(ctx, acp.TextBlock(
		"What color did you name just now? Reply with just the single word.",
	))
	if err != nil {
		exitf("loaded Prompt: %v", err)
	}

	fmt.Printf("stop: %s\n", res.StopReason)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
