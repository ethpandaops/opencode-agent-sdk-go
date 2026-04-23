// Demonstrates WithMCPServers: attach an external MCP server to every
// new session. The server runs in its own process — opencode launches
// it over whichever transport is declared (stdio, HTTP, or SSE) and
// discovers its tools automatically.
//
// This example defaults to the everything-server reference
// implementation over stdio:
//
//	npx -y @modelcontextprotocol/server-everything
//
// Override the command + args with the EXTERNAL_MCP_COMMAND (space-separated)
// environment variable to point at a different stdio MCP server, e.g.
//
//	EXTERNAL_MCP_COMMAND="uvx mcp-server-time" go run ./examples/external_mcp
//
// For HTTP/SSE transports, substitute `acp.McpServer{Http: ...}` for the
// stdio block below.
package main

import (
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cmd, args := parseCommand(os.Getenv("EXTERNAL_MCP_COMMAND"))

	external := acp.McpServer{
		Stdio: &acp.McpServerStdio{
			Name:    "everything",
			Command: cmd,
			Args:    args,
			Env:     []acp.EnvVariable{},
		},
	}

	cwd, _ := os.Getwd()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithMCPServers(external),
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

	fmt.Printf("connected: %s %s\nattached MCP server: %s %v\n\n",
		c.AgentInfo().Name, c.AgentInfo().Version, cmd, args)

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

	prompt := "List the tools you have available from any MCP server and briefly describe what each one does."

	res, err := sess.Prompt(ctx, acp.TextBlock(prompt))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	fmt.Printf("\n\nstop reason: %s\n", res.StopReason)
}

func parseCommand(spec string) (string, []string) {
	if spec == "" {
		return "npx", []string{"-y", "@modelcontextprotocol/server-everything"}
	}

	parts := strings.Fields(spec)

	return parts[0], parts[1:]
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
