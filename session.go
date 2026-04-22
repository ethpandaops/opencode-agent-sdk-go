package opencodesdk

import (
	"context"

	"github.com/coder/acp-go-sdk"
)

// Session is a stateful opencode conversation. Obtain one via
// Client.NewSession, Client.LoadSession, or (in M7) Client.ForkSession /
// Client.ResumeSession.
//
// Session is safe for concurrent Prompt/Cancel calls from different
// goroutines, but a single in-flight Prompt serializes all tool calls
// and updates on the session.
type Session interface {
	// ID returns the opencode session ID (e.g. "ses_24d2fc1e0ffe5YxDJSq64vW9LD").
	ID() string

	// Prompt submits a user turn and blocks until the turn completes.
	// It returns the final PromptResult (stop reason + optional usage)
	// or an error. Cancel the ctx to abort the turn; the error returned
	// in that case wraps ErrCancelled.
	Prompt(ctx context.Context, blocks ...acp.ContentBlock) (*PromptResult, error)

	// Cancel sends a session/cancel notification for the current turn.
	// This is advisory — the turn's pending Prompt call returns with
	// an error. Cancel is safe to call when no turn is in flight; it
	// becomes a no-op.
	Cancel(ctx context.Context) error

	// Updates returns a channel that delivers every session/update
	// notification for this session. The channel is buffered
	// (WithUpdatesBuffer, default 128). If the buffer fills, excess
	// notifications are dropped and logged at warn level.
	//
	// The channel remains open for the lifetime of the session and is
	// closed when the owning Client is closed.
	Updates() <-chan acp.SessionNotification

	// SetModel changes the model used for subsequent prompts. Sugar for
	// SetConfigOption(ctx, "model", modelID).
	SetModel(ctx context.Context, modelID string) error

	// SetMode changes the agent ("mode") used for subsequent prompts.
	// Sugar for SetConfigOption(ctx, "mode", modeID).
	SetMode(ctx context.Context, modeID string) error

	// SetConfigOption routes a session/set_config_option RPC with a
	// string value. configID must match one of the option ids reported in
	// InitialConfigOptions (e.g. "model", "mode", "provider"). The value
	// must be one of the valid value ids for that option.
	//
	// Prefer SetModel / SetMode for the two common cases.
	SetConfigOption(ctx context.Context, configID, value string) error

	// SetConfigOptionBool routes a session/set_config_option RPC with a
	// boolean value. Applicable to opencode config options whose
	// SessionConfigOption variant is a boolean (opencode currently does
	// not expose any via ACP; included for forward compatibility with
	// agents that do).
	SetConfigOptionBool(ctx context.Context, configID string, value bool) error

	// InitialModels returns the SessionModelState reported by opencode
	// at session creation (or the last loadSession/resume). May be nil
	// if the agent did not advertise model state.
	InitialModels() *acp.SessionModelState

	// InitialModes returns the SessionModeState reported at creation.
	// May be nil.
	InitialModes() *acp.SessionModeState

	// InitialConfigOptions returns the configOptions snapshot from
	// session creation.
	InitialConfigOptions() []acp.SessionConfigOption

	// Meta returns the raw _meta block from session creation. opencode
	// exposes variant info as `_meta.opencode.variant`; use
	// CurrentVariant for a typed accessor.
	Meta() map[string]any

	// AvailableModels returns the list of models advertised by opencode
	// for this session. Derived from InitialModels; returns nil if the
	// agent did not advertise any.
	AvailableModels() []acp.ModelInfo

	// AvailableCommands returns a snapshot of the slash commands the
	// agent currently advertises. opencode emits these once per session,
	// ~1 tick after the lifecycle response, so this accessor may be
	// empty immediately after NewSession and populated shortly after.
	// Callers that need the up-to-the-moment list should observe the
	// Updates() channel for acp.AvailableCommandsUpdate notifications.
	AvailableCommands() []acp.AvailableCommand

	// CurrentVariant returns the opencode-specific model-variant state
	// parsed from the session's _meta.opencode block, or nil if the
	// session did not advertise a variant.
	CurrentVariant() *VariantInfo

	// Subscribe installs a set of typed session/update callbacks and
	// returns an unsubscribe function. Handlers fire synchronously in
	// the SDK's dispatcher goroutine alongside delivery to Updates(),
	// so they must not block. Multiple subscribers are supported and
	// fire in registration order.
	Subscribe(handlers UpdateHandlers) (unsubscribe func())

	// DroppedUpdates returns the cumulative count of session/update
	// notifications dropped because the Updates() buffer was full since
	// the session was created.
	DroppedUpdates() int64
}

// PromptResult is the final outcome of Session.Prompt.
type PromptResult struct {
	// StopReason is the reason the agent stopped producing output for
	// this turn. opencode currently only returns "end_turn" on success;
	// cancel paths surface as ErrCancelled.
	StopReason acp.StopReason

	// Usage is the token accounting for this turn. The field is marked
	// unstable in ACP and may be absent for some agents; opencode
	// populates it reliably.
	Usage *acp.Usage

	// Meta is the _meta block from the PromptResponse. Typically nil.
	Meta map[string]any
}

// SessionInfo is a lightweight handle returned by Client.ListSessions.
type SessionInfo = acp.SessionInfo
