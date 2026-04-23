package opencodesdk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// TestMergeOptionsNormalizesMcpServerSlices guards against opencode's
// zod rejection of JSON `null` for McpServer args/env/headers. The
// acp-go-sdk types lack `omitempty` on those fields, so nil slices
// marshal as null unless mergeOptions normalizes them.
func TestMergeOptionsNormalizesMcpServerSlices(t *testing.T) {
	c := &client{
		opts: apply([]Option{
			WithMCPServers(
				acp.McpServer{Stdio: &acp.McpServerStdio{Name: "stdio", Command: "cmd"}},
				acp.McpServer{Http: &acp.McpServerHttpInline{Type: "http", Name: "http", Url: "https://e"}},
				acp.McpServer{Sse: &acp.McpServerSseInline{Type: "sse", Name: "sse", Url: "https://e"}},
			),
		}),
	}

	merged := c.mergeOptions(nil)

	if len(merged.mcpServers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(merged.mcpServers))
	}

	stdio := merged.mcpServers[0].Stdio
	if stdio.Args == nil {
		t.Errorf("stdio.Args is nil; expected empty slice")
	}

	if stdio.Env == nil {
		t.Errorf("stdio.Env is nil; expected empty slice")
	}

	if merged.mcpServers[1].Http.Headers == nil {
		t.Errorf("http.Headers is nil; expected empty slice")
	}

	if merged.mcpServers[2].Sse.Headers == nil {
		t.Errorf("sse.Headers is nil; expected empty slice")
	}

	buf, err := json.Marshal(merged.mcpServers)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, needle := range []string{`"args":null`, `"env":null`, `"headers":null`} {
		if strings.Contains(string(buf), needle) {
			t.Errorf("marshaled payload contains %s: %s", needle, buf)
		}
	}
}

// TestMergeOptionsResolvesTUIDefaultModel checks that mergeOptions
// pulls the opencode TUI's last-used model from model.json when the
// caller didn't pass WithModel.
func TestMergeOptionsResolvesTUIDefaultModel(t *testing.T) {
	dir := t.TempDir()

	subdir := filepath.Join(dir, "opencode")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	payload := `{
		"recent": [{"providerID": "openrouter", "modelID": "anthropic/claude-opus-4.7"}],
		"variant": {"openrouter/anthropic/claude-opus-4.7": "medium"}
	}`

	if err := os.WriteFile(filepath.Join(subdir, "model.json"), []byte(payload), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Setenv("XDG_STATE_HOME", dir)

	c := &client{opts: apply(nil)}

	merged := c.mergeOptions(nil)

	want := "openrouter/anthropic/claude-opus-4.7/medium"
	if merged.model != want {
		t.Errorf("merged.model = %q, want %q", merged.model, want)
	}
}

// TestMergeOptionsExplicitModelBeatsTUIDefault ensures an explicit
// WithModel wins over the TUI's model.json.
func TestMergeOptionsExplicitModelBeatsTUIDefault(t *testing.T) {
	dir := t.TempDir()

	subdir := filepath.Join(dir, "opencode")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	payload := `{"recent": [{"providerID": "openrouter", "modelID": "anthropic/claude-opus-4.7"}]}`
	if err := os.WriteFile(filepath.Join(subdir, "model.json"), []byte(payload), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Setenv("XDG_STATE_HOME", dir)

	c := &client{opts: apply([]Option{WithModel("opencode/big-pickle")})}

	merged := c.mergeOptions(nil)

	if merged.model != "opencode/big-pickle" {
		t.Errorf("merged.model = %q, want explicit opencode/big-pickle", merged.model)
	}
}
