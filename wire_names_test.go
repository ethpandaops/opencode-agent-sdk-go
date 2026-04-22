package opencodesdk

import (
	"testing"

	"github.com/coder/acp-go-sdk"
)

// TestWireMethodNames_MatchOpencode locks in the JSON-RPC method
// strings the SDK sends for the unstable session-management RPCs.
// opencode 1.14.20 accepts these standard names; the empirical probe
// against a live `opencode acp` confirmed `session/fork`,
// `session/resume`, and `session/set_model` while rejecting the
// underscore-prefixed `unstable_*` variants.
//
// If acp-go-sdk ever renames these constants the SDK's behaviour
// against opencode would silently break, so this test guards against
// regressions in the upstream protocol layer.
func TestWireMethodNames_MatchOpencode(t *testing.T) {
	cases := map[string]string{
		"session/fork":      acp.AgentMethodSessionFork,
		"session/resume":    acp.AgentMethodSessionResume,
		"session/set_model": acp.AgentMethodSessionSetModel,
		"session/new":       acp.AgentMethodSessionNew,
		"session/load":      acp.AgentMethodSessionLoad,
		"session/list":      acp.AgentMethodSessionList,
		"session/prompt":    acp.AgentMethodSessionPrompt,
		"session/cancel":    acp.AgentMethodSessionCancel,
		"session/set_mode":  acp.AgentMethodSessionSetMode,
	}

	for want, got := range cases {
		if got != want {
			t.Errorf("acp wire constant changed: got %q, want %q", got, want)
		}
	}
}
