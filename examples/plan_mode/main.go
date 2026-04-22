// Demonstrates WithInitialMode(ModePlan). opencode's plan mode ships
// with a permission ruleset that DENIES every edit — it does not route
// edit requests through session/request_permission. Asking plan to
// modify a file reliably produces an inline refusal from the model,
// which is useful when you want a read-only conversation where the
// agent can reason about changes without applying them.
//
// WithInitialMode is an ACP-terminology alias for WithAgent; pick
// whichever reads better in context. ModeBuild and ModePlan are the
// built-in mode ids that ship with opencode 1.14.20.
//
// If you want the interactive ask-path (session/request_permission)
// instead, use the default `build` agent and set
// `"permission": {"edit": "ask"}` in ~/.config/opencode/config.json —
// see examples/permission_callback.
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

	// Run in a dedicated sandbox. Plan mode denies edits, but custom
	// user rules can override; keep any accidental writes out of the
	// user's workspace.
	sandbox, err := os.MkdirTemp("", "opencodesdk-plan-*")
	if err != nil {
		exitf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(sandbox)

	fmt.Printf("sandbox: %s\n", sandbox)

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(sandbox),
		opencodesdk.WithInitialMode(opencodesdk.ModePlan),
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

	fmt.Println("available modes:")

	for _, m := range sess.AvailableModes() {
		marker := " "
		if string(m.Id) == opencodesdk.ModePlan {
			marker = "*"
		}

		fmt.Printf("  %s %s (%s)\n", marker, m.Id, m.Name)
	}

	fmt.Println()

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

	prompt := "Draft a one-file hello.go program and write it to disk. " +
		"If you can't, describe the program and explain why."

	res, err := sess.Prompt(ctx, acp.TextBlock(prompt))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	fmt.Printf("\n\nstop reason: %s\n", res.StopReason)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
