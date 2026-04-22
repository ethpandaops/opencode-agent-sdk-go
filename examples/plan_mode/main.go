// Demonstrates WithAgent("plan"): opencode's plan agent is the easiest
// way to observe session/request_permission prompts out of the box. Its
// permission ruleset is configured as {"edit": "ask", <plansDir>: "allow"}
// so any edit outside the plans directory prompts the client — the default
// build agent, by contrast, auto-allows everything.
//
// The example pairs WithAgent("plan") with a WithCanUseTool callback that
// approves every request once so you can see the full request → response
// round-trip without losing the turn.
//
//	go run ./examples/plan_mode
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
		opencodesdk.WithAgent("plan"),
		opencodesdk.WithCanUseTool(autoApprove),
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err = c.Start(ctx)
	if err != nil {
		exitf("Start: %v", err)
	}

	sess, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	go func() {
		for n := range sess.Updates() {
			if n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.Content.Text != nil {
				fmt.Print(n.Update.AgentMessageChunk.Content.Text.Text)
			}

			if n.Update.ToolCall != nil {
				fmt.Printf("\n[tool_call] %s\n", n.Update.ToolCall.Title)
			}
		}
	}()

	prompt := "Draft a one-file hello.go program and write it to disk. Keep it tiny."

	res, err := sess.Prompt(ctx, acp.TextBlock(prompt))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	fmt.Printf("\n\nstop reason: %s\n", res.StopReason)
}

func autoApprove(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	title := ""
	if req.ToolCall.Title != nil {
		title = *req.ToolCall.Title
	}

	fmt.Printf("\n[permission] auto-approving: %s\n", title)

	return opencodesdk.AllowOnce(ctx, req)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
