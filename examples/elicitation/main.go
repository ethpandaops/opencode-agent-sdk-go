// Example: a tool that asks the user to confirm a destructive
// action via MCP elicitation. Registers a single SDK tool; when the
// agent invokes it the handler sends an elicitation through the
// loopback MCP bridge back to opencode, which routes it to the user.
//
// Run with `go run ./examples/elicitation`.
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

type destructiveTool struct{}

func (destructiveTool) Name() string        { return "delete_repo" }
func (destructiveTool) Description() string { return "Delete the git repository in the current cwd." }

func (destructiveTool) InputSchema() map[string]any {
	return opencodesdk.SimpleSchema(map[string]string{
		"path": "string",
	})
}

func (destructiveTool) Execute(ctx context.Context, in map[string]any) (opencodesdk.ToolResult, error) {
	path, _ := in["path"].(string)

	// Ask the user to confirm via MCP elicitation. opencode routes
	// this to its UI; the user answers and we get the response here.
	resp, err := opencodesdk.Elicit(ctx, opencodesdk.ElicitParams{
		Message: fmt.Sprintf("Really delete %q? Type 'yes' to confirm.", path),
		RequestedSchema: opencodesdk.SimpleSchema(map[string]string{
			"confirm": "string",
		}),
	})
	if err != nil {
		// If elicitation is unavailable (e.g. opencode does not
		// support it), fall back to auto-reject.
		return opencodesdk.ErrorResult(fmt.Sprintf("could not ask user: %v", err)), nil
	}

	if resp.Action != "accept" {
		return opencodesdk.TextResult("user declined"), nil
	}

	if resp.Content["confirm"] != "yes" {
		return opencodesdk.TextResult("user did not confirm"), nil
	}

	return opencodesdk.TextResult(fmt.Sprintf("would delete %s (dry-run)", path)), nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cwd, _ := os.Getwd()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithSDKTools(destructiveTool{}),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}

	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		exitf("Start: %v", startErr)
	}

	sess, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	res, err := sess.Prompt(ctx, acp.TextBlock("Call the delete_repo tool on /tmp/demo to test the confirmation flow."))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	fmt.Printf("\nstop: %s\n", res.StopReason)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
