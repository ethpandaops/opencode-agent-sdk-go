package opencodesdk

import (
	"context"

	"github.com/coder/acp-go-sdk"
)

// Client is a stateful, long-lived handle to an opencode acp subprocess.
// Obtain one with NewClient, then call Start to spawn the process and
// negotiate the ACP initialize handshake. Close when done.
//
// Session operations (NewSession, LoadSession, ListSessions, Prompt,
// Cancel) land in milestone M3.
type Client interface {
	// Start spawns the opencode subprocess, wires stdio into the ACP
	// protocol layer, and runs the initialize handshake. It must be
	// called exactly once per Client; subsequent calls return ErrClientNotStarted.
	Start(ctx context.Context) error

	// Close terminates the subprocess and releases resources. It is
	// idempotent and safe to call after Start failures.
	Close() error

	// Capabilities returns the agent capabilities negotiated during Start.
	// Returns the zero value if called before Start.
	Capabilities() acp.AgentCapabilities

	// AgentInfo returns the agent identity (name + version) reported
	// during Start. Returns the zero value if called before Start.
	AgentInfo() acp.Implementation
}

// NewClient creates a new, un-started Client configured with the given
// options. Call Start to connect to opencode.
func NewClient(opts ...Option) (Client, error) {
	return newClient(apply(opts))
}
