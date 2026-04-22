// Demonstrates WithCanUseTool: a stdin-based permission prompt that
// asks the operator to approve each tool call interactively. opencode
// only emits session/request_permission when its permission rule
// evaluates to "ask" — the default build agent allows everything, so
// this example uses a user configuration snippet to trigger prompts.
//
// Triggering real permission prompts from an out-of-the-box opencode
// install requires setting `"permission": {"edit": "ask"}` in
// ~/.config/opencode/config.json; without that no permissions fire.
//
//	go run ./examples/permission_callback
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Run in a dedicated sandbox so an approved edit can't escape into
	// the user's current directory.
	sandbox, err := os.MkdirTemp("", "opencodesdk-perm-*")
	if err != nil {
		exitf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(sandbox)

	fmt.Printf("sandbox: %s\n", sandbox)

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(sandbox),
		opencodesdk.WithCanUseTool(promptTheOperator),
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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
		}
	}()

	_, err = sess.Prompt(ctx, acp.TextBlock("Write a Hello, World program in Go to hello.go."))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	fmt.Println()
}

func promptTheOperator(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	title := ""
	if req.ToolCall.Title != nil {
		title = *req.ToolCall.Title
	}

	fmt.Printf("\n---\npermission requested: %s\n", title)

	for i, opt := range req.Options {
		fmt.Printf("  %d. %s  (%s)\n", i+1, opt.Name, opt.Kind)
	}

	fmt.Print("choose: ")

	reader := bufio.NewReader(os.Stdin)

	line, err := reader.ReadString('\n')
	if err != nil {
		return acp.RequestPermissionResponse{}, err
	}

	line = strings.TrimSpace(line)

	idx := 0
	_, _ = fmt.Sscanf(line, "%d", &idx)
	idx--

	if idx < 0 || idx >= len(req.Options) {
		return opencodesdk.RejectOnce(ctx, req)
	}

	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: req.Options[idx].OptionId},
		},
	}, nil
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
