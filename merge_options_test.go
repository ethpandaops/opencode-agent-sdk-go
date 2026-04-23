package opencodesdk

import (
	"testing"

	"github.com/coder/acp-go-sdk"
)

func TestMergeOptionsDeepCopiesMutableState(t *testing.T) {
	c := &client{
		opts: apply([]Option{
			WithEnv(map[string]string{"CLIENT_LEVEL": "yes"}),
			WithCLIFlags("--initial"),
			WithMCPServers(acp.McpServer{Stdio: &acp.McpServerStdio{Name: "base", Command: "base"}}),
			WithSDKTools(NewTool("t", "", map[string]any{"type": "object"}, nil)),
		}),
	}

	merged := c.mergeOptions(nil)

	merged.env["OVERRIDE"] = "mutated"
	merged.cliFlags = append(merged.cliFlags, "--mutated")
	merged.mcpServers = append(merged.mcpServers, acp.McpServer{Stdio: &acp.McpServerStdio{Name: "extra", Command: "extra"}})
	merged.sdkTools = append(merged.sdkTools, NewTool("u", "", map[string]any{"type": "object"}, nil))

	if _, ok := c.opts.env["OVERRIDE"]; ok {
		t.Fatalf("client env was mutated through merged.env")
	}

	if len(c.opts.cliFlags) != 1 {
		t.Fatalf("client cliFlags mutated through merged: %v", c.opts.cliFlags)
	}

	if len(c.opts.mcpServers) != 1 {
		t.Fatalf("client mcpServers mutated through merged: %d", len(c.opts.mcpServers))
	}

	if len(c.opts.sdkTools) != 1 {
		t.Fatalf("client sdkTools mutated through merged: %d", len(c.opts.sdkTools))
	}
}

func TestMergeOptionsCallOverrideWinsWithoutBleed(t *testing.T) {
	c := &client{
		opts: apply([]Option{WithModel("client-default")}),
	}

	override := []Option{WithModel("per-call")}

	merged := c.mergeOptions(override)

	if merged.model != "per-call" {
		t.Fatalf("expected per-call override, got %q", merged.model)
	}

	if c.opts.model != "client-default" {
		t.Fatalf("override leaked into client opts: %q", c.opts.model)
	}
}
