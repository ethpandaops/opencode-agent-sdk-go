package opencodesdk

import (
	"context"

	"github.com/coder/acp-go-sdk"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/handlers"
)

// ElicitationCallback is invoked when the agent issues a
// `elicitation/create` request (agent → client). The callback returns
// the user's response: accept with content, decline, or cancel.
//
// This is the agent-initiated counterpart to the tool-side [Elicit]
// helper: [Elicit] lets a Tool ask opencode's MCP client for input,
// whereas this callback receives requests originating from opencode
// itself via the unstable ACP `elicitation/create` method.
//
// opencode 1.14.20 does NOT currently emit elicitation/create over
// ACP — the schema reserves this under the unstable namespace. Wiring
// this callback is forward-compatible: when opencode (or another ACP
// agent) starts emitting it, the existing callback handles it. Until
// then the callback never fires and costs nothing.
//
// If ctx is cancelled the callback MUST return promptly. Returning a
// non-nil error surfaces as a JSON-RPC error to the agent; prefer
// returning a structured Decline outcome if the user simply refused.
type ElicitationCallback func(ctx context.Context, req acp.UnstableCreateElicitationRequest) (acp.UnstableCreateElicitationResponse, error)

// ElicitationCompleteCallback is invoked when the agent sends an
// `elicitation/complete` notification (agent → client, no response
// expected). opencode uses this to signal that a URL-mode elicitation
// (where the client opens a browser) has finished on the agent side.
//
// Notification-only: the callback's return path has no effect on the
// agent. Errors are logged by the SDK but not propagated.
type ElicitationCompleteCallback func(ctx context.Context, params acp.UnstableCompleteElicitationNotification)

// WithOnElicitation registers a callback for agent-initiated
// elicitation/create requests. Leave unset if the agent you target
// does not issue them (opencode 1.14.20 does not).
//
// When unset the SDK returns a Decline response automatically so the
// agent gracefully recovers rather than blocking on a missing handler.
func WithOnElicitation(cb ElicitationCallback) Option {
	return func(o *options) { o.onElicitation = cb }
}

// WithOnElicitationComplete registers a callback for
// elicitation/complete notifications. Notification-only; errors from
// the callback do not surface to the agent.
func WithOnElicitationComplete(cb ElicitationCompleteCallback) Option {
	return func(o *options) { o.onElicitationComplete = cb }
}

// DeclineElicitation is an [ElicitationCallback] helper returning a
// Decline response with no content. Useful as an explicit "not
// implemented" default when wiring the option in tests.
func DeclineElicitation(_ context.Context, _ acp.UnstableCreateElicitationRequest) (acp.UnstableCreateElicitationResponse, error) {
	return acp.UnstableCreateElicitationResponse{
		Decline: &acp.UnstableCreateElicitationDecline{Action: "decline"},
	}, nil
}

// instrumentedElicitation wraps cb with observability. When cb is nil
// we return nil so the dispatcher falls through to its default
// auto-decline path (a lint-worthy exception would be silently
// implementing the method with no observer recording).
func (c *client) instrumentedElicitation(cb ElicitationCallback) handlers.ElicitationCallback {
	if cb == nil {
		return nil
	}

	return func(ctx context.Context, req acp.UnstableCreateElicitationRequest) (acp.UnstableCreateElicitationResponse, error) {
		mode := elicitationMode(req)

		resp, err := cb(ctx, req)

		outcome := "error"

		switch {
		case err != nil:
		case resp.Accept != nil:
			outcome = "accept"
		case resp.Decline != nil:
			outcome = "decline"
		case resp.Cancel != nil:
			outcome = "cancel"
		}

		c.observer.RecordElicitationRequest(ctx, mode, outcome)

		return resp, err
	}
}

// wrapElicitationComplete adapts the root-package callback type to the
// internal handlers-package type. Returns nil when cb is nil so the
// dispatcher's no-op default is preserved.
func wrapElicitationComplete(cb ElicitationCompleteCallback) handlers.ElicitationCompleteCallback {
	if cb == nil {
		return nil
	}

	return func(ctx context.Context, params acp.UnstableCompleteElicitationNotification) {
		cb(ctx, params)
	}
}

// elicitationModeUnknown is returned when an inbound elicitation
// request has neither Form nor Url populated. Should be unreachable
// after acp-go-sdk's own payload validation, but keeps the observer
// label cardinality bounded if we ever see one.
const elicitationModeUnknown = "unknown"

// elicitationMode returns the string discriminator for an inbound
// elicitation request.
func elicitationMode(req acp.UnstableCreateElicitationRequest) string {
	switch {
	case req.Form != nil:
		return "form"
	case req.Url != nil:
		return "url"
	default:
		return elicitationModeUnknown
	}
}
