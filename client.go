package opencodesdk

import (
	"context"

	"github.com/coder/acp-go-sdk"
)

// Client is a stateful, long-lived handle to an opencode acp subprocess.
// Obtain one with NewClient, then call Start to spawn the process and
// negotiate the ACP initialize handshake. Close when done.
type Client interface {
	// Start spawns the opencode subprocess, wires stdio into the ACP
	// protocol layer, and runs the initialize handshake. It must be
	// called exactly once per Client.
	Start(ctx context.Context) error

	// Close terminates the subprocess and releases resources. It is
	// idempotent and safe to call after Start failures.
	Close() error

	// Capabilities returns the agent capabilities negotiated during Start.
	// Returns the zero value if called before Start.
	Capabilities() acp.AgentCapabilities

	// AgentInfo returns the agent identity (name + version) reported
	// during Start.
	AgentInfo() acp.Implementation

	// AuthMethods returns the authentication methods the agent
	// advertised during initialize. Inspect TerminalAuthInstructions
	// on each method if you advertised _meta["terminal-auth"] via
	// WithTerminalAuthCapability.
	AuthMethods() []acp.AuthMethod

	// NewSession creates a new opencode session. If WithModel or
	// WithAgent are configured (either as client-level options passed
	// to NewClient or as per-call options passed here), they are
	// applied via session/set_config_option immediately after creation.
	NewSession(ctx context.Context, opts ...Option) (Session, error)

	// LoadSession rehydrates a previously-created session by ID.
	// opencode replays the session's message history via session/update
	// notifications before this method returns — callers that want to
	// observe the replay should subscribe to the returned Session's
	// Updates() channel before invoking further methods.
	//
	// Returns a typed error wrapping ErrAuthRequired if opencode cannot
	// load the session's associated credentials.
	LoadSession(ctx context.Context, id string, opts ...Option) (Session, error)

	// ListSessions enumerates sessions scoped to the configured cwd.
	// Cursor is opaque; pass the return value's NextCursor to paginate.
	ListSessions(ctx context.Context, cursor string) (sessions []SessionInfo, nextCursor string, err error)

	// ForkSession creates a new session branched from an existing one
	// (opencode unstable_forkSession RPC).
	ForkSession(ctx context.Context, parentID string, opts ...Option) (Session, error)

	// ResumeSession re-attaches to an existing session without the
	// history-replay side effects of LoadSession (opencode
	// unstable_resumeSession RPC).
	ResumeSession(ctx context.Context, sessionID string, opts ...Option) (Session, error)

	// UnstableSetModel issues opencode's unstable_setSessionModel RPC
	// directly. Prefer Session.SetModel for normal use.
	UnstableSetModel(ctx context.Context, sessionID, modelID string) error
}

// NewClient creates a new, un-started Client configured with the given
// options. Call Start to connect to opencode.
func NewClient(opts ...Option) (Client, error) {
	return newClient(apply(opts))
}
