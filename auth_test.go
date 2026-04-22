package opencodesdk

import (
	"testing"

	"github.com/coder/acp-go-sdk"
)

func TestTerminalAuthInstructions_OpencodeAgentMeta(t *testing.T) {
	m := acp.AuthMethod{
		Agent: &acp.AuthMethodAgent{
			Id:   "opencode-login",
			Name: "Login with opencode",
			Meta: map[string]any{
				"terminal-auth": map[string]any{
					"command": "opencode",
					"args":    []any{"auth", "login"},
					"label":   "OpenCode Login",
				},
			},
		},
	}

	got, ok := TerminalAuthInstructions(m)
	if !ok {
		t.Fatalf("expected instructions, got ok=false")
	}

	if got.Command != "opencode" || got.Label != "OpenCode Login" {
		t.Fatalf("unexpected instructions: %+v", got)
	}

	if len(got.Args) != 2 || got.Args[0] != "auth" || got.Args[1] != "login" {
		t.Fatalf("unexpected args: %v", got.Args)
	}
}

func TestTerminalAuthInstructions_MissingMeta(t *testing.T) {
	m := acp.AuthMethod{
		Agent: &acp.AuthMethodAgent{Id: "bare", Name: "Bare"},
	}

	if _, ok := TerminalAuthInstructions(m); ok {
		t.Fatalf("expected ok=false when _meta absent")
	}
}

func TestTerminalAuthInstructions_EmptyCommandReturnsFalse(t *testing.T) {
	m := acp.AuthMethod{
		Agent: &acp.AuthMethodAgent{
			Id:   "x",
			Name: "x",
			Meta: map[string]any{
				"terminal-auth": map[string]any{"label": "no command"},
			},
		},
	}

	if _, ok := TerminalAuthInstructions(m); ok {
		t.Fatalf("expected ok=false when command missing")
	}
}

func TestTerminalAuthInstructions_EnvParsed(t *testing.T) {
	m := acp.AuthMethod{
		Agent: &acp.AuthMethodAgent{
			Id:   "x",
			Name: "x",
			Meta: map[string]any{
				"terminal-auth": map[string]any{
					"command": "foo",
					"env":     map[string]any{"K": "v", "bad": 42},
				},
			},
		},
	}

	got, ok := TerminalAuthInstructions(m)
	if !ok {
		t.Fatalf("expected ok=true")
	}

	if got.Env["K"] != "v" {
		t.Fatalf("expected K=v, got %v", got.Env)
	}

	if _, exists := got.Env["bad"]; exists {
		t.Fatalf("expected non-string env value to be dropped, got %v", got.Env)
	}
}
