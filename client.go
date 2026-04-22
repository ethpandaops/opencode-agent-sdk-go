package opencodesdk

import (
	"context"
	"encoding/json"
	"iter"

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

	// LoadSessionHistory loads an existing session and captures the
	// replay notifications opencode emits during session/load into a
	// typed SessionHistory. Prefer this over LoadSession when the
	// caller wants the full historical transcript as a slice rather
	// than streaming replay via Updates().
	//
	// The returned Session is ready for further Prompt/Cancel calls;
	// its Updates() channel carries only post-replay notifications.
	LoadSessionHistory(ctx context.Context, id string, opts ...Option) (*SessionHistory, error)

	// ListSessions enumerates sessions scoped to the configured cwd.
	//
	// opencode 1.14.20 paginates via a stringified page number ("0",
	// "1", "2", …) rather than an opaque token. Pass empty string or
	// "0" for the first page. Page size is fixed at 100. The returned
	// nextCursor is the opaque echo from the agent; for opencode it is
	// always either empty (last page) or the next page number — pass
	// it back as-is to fetch the next page. Prefer IterSessions for
	// transparent pagination.
	ListSessions(ctx context.Context, cursor string) (sessions []SessionInfo, nextCursor string, err error)

	// ForkSession creates a new session branched from an existing one
	// (ACP's unstable `session/fork` RPC).
	ForkSession(ctx context.Context, parentID string, opts ...Option) (Session, error)

	// ResumeSession re-attaches to an existing session without the
	// history-replay side effects of LoadSession (ACP's unstable
	// `session/resume` RPC).
	ResumeSession(ctx context.Context, sessionID string, opts ...Option) (Session, error)

	// UnstableSetModel issues ACP's unstable `session/set_model` RPC
	// directly. Prefer Session.SetModel for normal use.
	UnstableSetModel(ctx context.Context, sessionID, modelID string) error

	// IterSessions returns an iterator over every session the agent
	// exposes in the configured cwd, paginating transparently via the
	// cursor opencode returns. The iterator short-circuits on the first
	// error. Prefer this over hand-rolling a ListSessions loop.
	IterSessions(ctx context.Context) iter.Seq2[SessionInfo, error]

	// CallExtension issues a raw JSON-RPC call for an ACP extension
	// method. The method name must begin with an underscore per the
	// ACP spec's extension convention. Returns the raw JSON response
	// from the agent.
	//
	// Prefer the typed wrappers (ForkSession, ResumeSession,
	// UnstableSetModel, etc.) when they cover the RPC you need. Use
	// this only for opencode- or agent-specific extensions that the
	// SDK does not yet expose.
	CallExtension(ctx context.Context, method string, params any) (json.RawMessage, error)

	// GetTransportHealth returns a snapshot of the transport-layer
	// health observed so far. Cheap to call; intended for dashboards,
	// liveness probes, and diagnostics.
	GetTransportHealth() TransportHealth

	// BudgetTracker returns the tracker installed via
	// WithMaxBudgetUSD or WithBudgetTracker. Returns nil when no
	// budget policy was configured.
	BudgetTracker() *BudgetTracker

	// CancelAll sends session/cancel to every live session owned by
	// this Client. Returns a joined error when any individual cancel
	// RPC fails (unaffected sessions are still signaled). ACP has no
	// connection-level interrupt primitive; this is a client-side fan
	// out over Session.Cancel, useful for coordinated shutdown or
	// panic paths where the caller no longer tracks individual
	// Session handles.
	CancelAll(ctx context.Context) error
}

// NewClient creates a new, un-started Client configured with the given
// options. Call Start to connect to opencode.
func NewClient(opts ...Option) (Client, error) {
	return newClient(apply(opts))
}
