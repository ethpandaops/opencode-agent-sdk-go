package opencodesdk

import (
	"github.com/coder/acp-go-sdk"
)

// TerminalAuthLaunch describes how a client can spawn an interactive
// login flow for the user. opencode populates this via its
// _meta["terminal-auth"] extension on AuthMethodAgent when the client
// declared _meta["terminal-auth"]=true in ClientCapabilities at
// initialize time (see WithTerminalAuthCapability).
//
// A typical shape on opencode 1.14.20:
//
//	TerminalAuthLaunch{
//	    Command: "opencode",
//	    Args:    []string{"auth", "login"},
//	    Label:   "OpenCode Login",
//	}
type TerminalAuthLaunch struct {
	Command string
	Args    []string
	Env     map[string]string
	Label   string
}

// TerminalAuthInstructions extracts the opencode-specific
// _meta["terminal-auth"] block from an AuthMethod if present. Returns
// (nil, false) if the method does not advertise a terminal launch.
//
// Callers can use the returned TerminalAuthLaunch to offer the user
// an interactive login UX (exec the command in a PTY, wait, retry the
// failing ACP call). The SDK does not do this automatically — auth is
// always user-initiated out of band.
func TerminalAuthInstructions(m acp.AuthMethod) (*TerminalAuthLaunch, bool) {
	var meta map[string]any

	switch {
	case m.Agent != nil:
		meta = m.Agent.Meta
	case m.Terminal != nil:
		meta = m.Terminal.Meta
	case m.EnvVar != nil:
		meta = m.EnvVar.Meta
	default:
		return nil, false
	}

	raw, ok := meta["terminal-auth"]
	if !ok {
		return nil, false
	}

	block, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}

	out := &TerminalAuthLaunch{}

	if v, ok := block["command"].(string); ok {
		out.Command = v
	}

	if v, ok := block["label"].(string); ok {
		out.Label = v
	}

	if v, ok := block["args"].([]any); ok {
		out.Args = make([]string, 0, len(v))

		for _, e := range v {
			if s, ok := e.(string); ok {
				out.Args = append(out.Args, s)
			}
		}
	}

	if v, ok := block["env"].(map[string]any); ok {
		out.Env = make(map[string]string, len(v))

		for k, ev := range v {
			if s, ok := ev.(string); ok {
				out.Env[k] = s
			}
		}
	}

	if out.Command == "" {
		return nil, false
	}

	return out, true
}
