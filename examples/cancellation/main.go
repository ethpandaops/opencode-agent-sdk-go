// Demonstrates cancelling an in-flight opencode turn via
// [Session.Cancel]. The SDK sends session/cancel to opencode, which
// stops the running turn promptly; the pending Prompt call then
// returns an error that wraps [opencodesdk.ErrCancelled].
//
// See also [Client.CancelAll] for fanning cancel notifications across
// every live session on a Client — useful for coordinated shutdown
// when the caller no longer tracks individual Session handles.
//
//	go run ./examples/cancellation
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return fmt.Errorf("new session: %w", err)
		}

		// Background goroutine: print assistant deltas so we can see the
		// turn was genuinely in progress when we cancelled it.
		go func() {
			for n := range sess.Updates() {
				if n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.Content.Text != nil {
					fmt.Print(n.Update.AgentMessageChunk.Content.Text.Text)
				}
			}
		}()

		// Fire a long-running prompt, then cancel it after 500ms.
		// Demonstrates Client.CancelAll — an equivalent effect that
		// fans session/cancel across every live session. Swap in
		// sess.Cancel(ctx) when you only want to abort one session.
		go func() {
			time.Sleep(500 * time.Millisecond)
			fmt.Println("\n\n[client] cancelling…")

			if cancelErr := c.CancelAll(context.Background()); cancelErr != nil {
				fmt.Fprintf(os.Stderr, "c.CancelAll: %v\n", cancelErr)
			}
		}()

		prompt := "Count slowly from one to fifty, one number per line, with a short observation between each."

		_, err = sess.Prompt(ctx, acp.TextBlock(prompt))

		switch {
		case errors.Is(err, opencodesdk.ErrCancelled):
			fmt.Printf("\n\n[client] Prompt returned ErrCancelled as expected: %v\n", err)

			return nil
		case err != nil:
			return fmt.Errorf("unexpected prompt error: %w", err)
		default:
			fmt.Println("\n\n[client] turn completed before cancel fired — try a longer prompt")

			return nil
		}
	},
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
