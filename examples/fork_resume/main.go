// Demonstrates Client.ForkSession and Client.ResumeSession.
//
//   - Fork creates a new session that inherits the parent's history up
//     to the fork point. The fork has a different SessionId but carries
//     any memory the parent had accumulated.
//   - Resume re-attaches to an existing session by id without replaying
//     its history on the wire (unlike LoadSession).
//
// Both wrap opencode's unstable session RPCs.
//
//	go run ./examples/fork_resume
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		exitf("Start: %v", startErr)
	}

	// 1. Seed session with a memorable fact.
	fmt.Println("== seeding parent session ==")

	parent, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	seedReply := captureText(parent)

	_, err = parent.Prompt(ctx, acp.TextBlock(
		"Remember this: my favorite number is 9137. Reply with exactly: REMEMBERED.",
	))
	if err != nil {
		exitf("seed Prompt: %v", err)
	}

	fmt.Printf("parent:   %s\n", short(parent.ID()))
	fmt.Printf("parent reply: %q\n\n", firstLine(seedReply()))

	// 2. Fork: new id, same memory.
	fmt.Println("== forking ==")

	forked, err := c.ForkSession(ctx, parent.ID())
	if err != nil {
		exitf("ForkSession: %v", err)
	}

	forkReply := captureText(forked)

	_, err = forked.Prompt(ctx, acp.TextBlock(
		"What was the favorite number I asked you to remember? Reply with just the digits.",
	))
	if err != nil {
		exitf("fork Prompt: %v", err)
	}

	fmt.Printf("forked:   %s  (parent=%s)\n", short(forked.ID()), short(parent.ID()))
	fmt.Printf("forked reply: %q\n\n", firstLine(forkReply()))

	// 3. Resume: same id, same memory, no history replay on the wire.
	fmt.Println("== resuming parent ==")

	resumed, err := c.ResumeSession(ctx, parent.ID())
	if err != nil {
		exitf("ResumeSession: %v", err)
	}

	resumeReply := captureText(resumed)

	_, err = resumed.Prompt(ctx, acp.TextBlock(
		"Recite the favorite number I told you earlier. Just the digits.",
	))
	if err != nil {
		exitf("resume Prompt: %v", err)
	}

	fmt.Printf("resumed:  %s  (same id as parent? %v)\n", short(resumed.ID()), resumed.ID() == parent.ID())
	fmt.Printf("resumed reply: %q\n", firstLine(resumeReply()))
}

// captureText subscribes to agent_message_chunk events and returns a
// getter for the accumulated text. Also drains Updates() so the
// dispatcher never blocks.
func captureText(s opencodesdk.Session) func() string {
	var (
		mu sync.Mutex
		sb strings.Builder
	)

	s.Subscribe(opencodesdk.UpdateHandlers{
		AgentMessage: func(_ context.Context, chunk *acp.SessionUpdateAgentMessageChunk) {
			if chunk.Content.Text == nil {
				return
			}

			mu.Lock()
			sb.WriteString(chunk.Content.Text.Text)
			mu.Unlock()
		},
	})

	go func() {
		for range s.Updates() {
		}
	}()

	return func() string {
		mu.Lock()
		defer mu.Unlock()

		return sb.String()
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}

	return s
}

func short(id string) string {
	if len(id) > 14 {
		return id[:14] + "…"
	}

	return id
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
