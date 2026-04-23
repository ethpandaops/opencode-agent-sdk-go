// Quick-start example for opencode-agent-sdk-go.
//
// Spawns `opencode acp`, starts a session, prompts it with a short
// question, streams the response, and tears down. Run `opencode auth
// login` before invoking.
//
//	go run ./examples/quick_start
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err = c.Start(ctx)
	if err != nil {
		exitf("Start: %v", err)
	}

	fmt.Printf("connected: %s %s\n\n", c.AgentInfo().Name, c.AgentInfo().Version)

	sess, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	go streamAssistantText(sess.Updates())

	res, err := sess.Prompt(ctx, acp.TextBlock("In three short sentences, explain what the Agent Client Protocol is."))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	fmt.Printf("\n\nstop reason: %s\n", res.StopReason)

	if res.Usage != nil {
		fmt.Printf("tokens: %d\n", res.Usage.TotalTokens)
	}
}

func streamAssistantText(ch <-chan acp.SessionNotification) {
	for n := range ch {
		if n.Update.AgentMessageChunk == nil {
			continue
		}

		if n.Update.AgentMessageChunk.Content.Text != nil {
			fmt.Print(n.Update.AgentMessageChunk.Content.Text.Text)
		}
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
