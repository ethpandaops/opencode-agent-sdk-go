// Demonstrates in-process tools exposed to opencode via the loopback
// HTTP MCP bridge built into the SDK. The agent can call any Go
// function registered with WithSDKTools — no separate server, no IPC.
//
//	go run ./examples/sdk_tools
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	var reverseCalls atomic.Int32

	// Reverse takes a string and returns it backwards. The whole value
	// of SDK tools is demonstrated here — the closure holds a live
	// pointer to reverseCalls, something an external MCP server can't do.
	reverse := opencodesdk.NewTool(
		"reverse_string",
		"Reverse the characters of the input string. Use when the user asks for a reversed or inverted version of text.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "The text to reverse",
				},
			},
			"required": []string{"text"},
		},
		func(ctx context.Context, in map[string]any) (opencodesdk.ToolResult, error) {
			reverseCalls.Add(1)

			text, _ := in["text"].(string)
			runes := []rune(text)

			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}

			return opencodesdk.ToolResult{Text: string(runes)}, nil
		},
	)

	cwd, _ := os.Getwd()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithSDKTools(reverse),
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

	sess, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	go func() {
		for n := range sess.Updates() {
			if n.Update.ToolCall != nil {
				fmt.Printf("[tool_call] %s\n", n.Update.ToolCall.Title)
			}

			if n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.Content.Text != nil {
				fmt.Print(n.Update.AgentMessageChunk.Content.Text.Text)
			}
		}
	}()

	prompt := "Use the reverse_string tool to reverse the text 'Hello, opencode!' and then tell me the result."

	res, err := sess.Prompt(ctx, acp.TextBlock(prompt))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	fmt.Printf("\n\nstop reason: %s\n", res.StopReason)
	fmt.Printf("reverse_string invocations: %d\n", reverseCalls.Load())

	if reverseCalls.Load() == 0 {
		fmt.Println("\n" + strings.Repeat("-", 40))
		fmt.Println("note: the agent did not call the tool. Try a different prompt,")
		fmt.Println("or check that the model attached to your opencode auth supports")
		fmt.Println("custom tools well.")
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
