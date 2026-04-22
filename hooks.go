package opencodesdk

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/acp-go-sdk"
)

// HookEvent is the discriminator for a registered hook. Each event
// maps to a specific point in the SDK's lifecycle; see the constants
// below for the supported set.
type HookEvent string

// Hook event constants, parity-shaped with claude/codex. Only events
// with a real opencode backend signal are defined — claude events
// that don't map (e.g. compaction, subagent, task) are intentionally
// omitted.
const (
	// HookEventPreToolUse fires when opencode announces a tool call
	// via a session/update ToolCall notification. Notification-only
	// in opencode: the agent has already decided to run the tool by
	// the time the SDK sees it. Use WithCanUseTool or
	// HookEventPermissionRequest if you need to BLOCK a tool before
	// execution (requires opencode's permission ruleset to "ask").
	HookEventPreToolUse HookEvent = "pre_tool_use"

	// HookEventPostToolUse fires after a tool call transitions to
	// status=completed via ToolCallUpdate.
	HookEventPostToolUse HookEvent = "post_tool_use"

	// HookEventPostToolUseFailure fires after a tool call transitions
	// to status=failed via ToolCallUpdate.
	HookEventPostToolUseFailure HookEvent = "post_tool_use_failure"

	// HookEventUserPromptSubmit fires as the first step of
	// Session.Prompt, before the request is sent to opencode.
	// Returning HookOutput{Continue:false} aborts the prompt with an
	// error wrapping Reason.
	HookEventUserPromptSubmit HookEvent = "user_prompt_submit"

	// HookEventStop fires after Session.Prompt returns successfully.
	HookEventStop HookEvent = "stop"

	// HookEventStopFailure fires after Session.Prompt returns an
	// error.
	HookEventStopFailure HookEvent = "stop_failure"

	// HookEventSessionStart fires after a session is created (or
	// loaded/forked/resumed) and fully configured.
	HookEventSessionStart HookEvent = "session_start"

	// HookEventSessionEnd fires when a session is closed, either via
	// explicit teardown or via Client.Close.
	HookEventSessionEnd HookEvent = "session_end"

	// HookEventPermissionRequest fires at the entry of the
	// session/request_permission callback path, before the
	// PermissionCallback runs. Returning Continue=false synthesises a
	// "reject" response — useful for declarative tool blocking.
	HookEventPermissionRequest HookEvent = "permission_request"

	// HookEventPermissionDenied fires after the permission callback
	// returns a reject or cancelled outcome.
	HookEventPermissionDenied HookEvent = "permission_denied"

	// HookEventFileChanged fires as the first step of the
	// fs/write_text_file delegation path, before the FsWriteCallback
	// runs. Returning Continue=false refuses the write (the error
	// surfaces to the agent).
	HookEventFileChanged HookEvent = "file_changed"
)

// HookInput is the discriminated payload delivered to each hook
// callback. The Event field determines which other pointer fields
// are populated.
type HookInput struct {
	// Event is the hook event. Always set.
	Event HookEvent
	// SessionID is the opencode session id, or empty for pre-session
	// events.
	SessionID string

	// ToolCall is populated for PreToolUse.
	ToolCall *acp.SessionUpdateToolCall
	// ToolCallUpdate is populated for PostToolUse and
	// PostToolUseFailure.
	ToolCallUpdate *acp.SessionToolCallUpdate

	// PromptText is populated for UserPromptSubmit and carries the
	// concatenated text of every TextBlock in the outgoing prompt.
	PromptText string
	// PromptResult is populated for Stop.
	PromptResult *PromptResult
	// PromptError is populated for StopFailure.
	PromptError error

	// PermissionRequest is populated for PermissionRequest and
	// PermissionDenied.
	PermissionRequest *acp.RequestPermissionRequest
	// PermissionResponse is populated for PermissionDenied with the
	// response that triggered the denial.
	PermissionResponse *acp.RequestPermissionResponse

	// FileWrite is populated for FileChanged.
	FileWrite *acp.WriteTextFileRequest
}

// HookOutput is the result of a hook callback. For notification-only
// events the fields are ignored; for blocking-capable events
// (UserPromptSubmit, PermissionRequest, FileChanged) Continue=false
// blocks the triggering action and Reason is surfaced to the agent
// or the caller.
type HookOutput struct {
	// Continue=false blocks the triggering action on events that
	// support blocking (UserPromptSubmit, PermissionRequest,
	// FileChanged). Defaults to true.
	Continue bool
	// Reason is the human-readable explanation for a block. Surfaced
	// to the agent as part of the rejection response or as an error
	// message on the outgoing RPC.
	Reason string
	// SuppressOutput is a UI hint indicating that any log/trace
	// output for this event should be hidden. The SDK itself does
	// not drop anything — consumers implementing their own logging
	// should honour the flag.
	SuppressOutput bool
}

// allow returns a permissive HookOutput for the common case where a
// hook wants to do work but not block. Convenience constructor to
// reduce boilerplate in callback bodies.
func HookAllow() HookOutput { return HookOutput{Continue: true} }

// HookBlock returns a blocking HookOutput with the supplied reason.
func HookBlock(reason string) HookOutput {
	return HookOutput{Continue: false, Reason: reason}
}

// HookCallback is the callback signature for every hook. The
// returned HookOutput is ignored for notification-only events; for
// blocking events the first hook to return Continue=false wins (the
// SDK short-circuits subsequent hooks for the same event).
//
// If the callback returns a non-nil error, the event is treated as
// blocked (for blocking-capable events) or logged (for notification
// events). The error does NOT propagate up through the SDK call that
// triggered the hook except via the blocking pathway.
type HookCallback func(ctx context.Context, input HookInput) (HookOutput, error)

// HookMatcher is a set of callbacks registered for a single event,
// optionally filtered by a regex match against an event-specific
// primary string:
//
//   - tool events: match against ToolCall.Title
//   - prompt events: match against PromptText (UserPromptSubmit) or
//     session id (Stop, StopFailure)
//   - session events: match against session id
//   - permission events: match against the tool title in
//     PermissionRequest.ToolCall.Title
//   - file events: match against the absolute path being written
//
// A nil Matcher matches all events.
type HookMatcher struct {
	// Matcher filters which events the hooks run for. Nil matches all.
	Matcher *regexp.Regexp
	// Hooks is the list of callbacks to invoke for matching events,
	// in order.
	Hooks []HookCallback
	// Timeout caps each callback's execution time. Zero means no
	// SDK-imposed timeout (ctx.Deadline still applies).
	Timeout time.Duration
	// Once causes the matcher to fire at most once across the client
	// lifetime. Useful for setup hooks.
	Once bool

	// fired tracks "Once" exhaustion. Not user-visible.
	fired atomic.Bool
}

// WithHooks registers a set of hook matchers. Multiple WithHooks
// calls merge: each event's matcher list is appended.
//
// Example — log every tool that is about to run, and block writes to
// /etc:
//
//	opencodesdk.WithHooks(map[opencodesdk.HookEvent][]*opencodesdk.HookMatcher{
//	    opencodesdk.HookEventPreToolUse: {{
//	        Hooks: []opencodesdk.HookCallback{
//	            func(ctx context.Context, in opencodesdk.HookInput) (opencodesdk.HookOutput, error) {
//	                slog.InfoContext(ctx, "tool", "name", in.ToolCall.Title)
//	                return opencodesdk.HookAllow(), nil
//	            },
//	        },
//	    }},
//	    opencodesdk.HookEventFileChanged: {{
//	        Matcher: regexp.MustCompile(`^/etc/`),
//	        Hooks: []opencodesdk.HookCallback{
//	            func(_ context.Context, _ opencodesdk.HookInput) (opencodesdk.HookOutput, error) {
//	                return opencodesdk.HookBlock("refusing to write under /etc"), nil
//	            },
//	        },
//	    }},
//	})
func WithHooks(hooks map[HookEvent][]*HookMatcher) Option {
	return func(o *options) {
		if o.hooks == nil {
			o.hooks = make(map[HookEvent][]*HookMatcher, len(hooks))
		}

		for event, matchers := range hooks {
			o.hooks[event] = append(o.hooks[event], matchers...)
		}
	}
}

// hookDispatcher threads hook invocation through each relevant
// codepath. A nil dispatcher is valid and is treated as a no-op.
type hookDispatcher struct {
	hooks map[HookEvent][]*HookMatcher
	mu    sync.RWMutex
}

func newHookDispatcher(hooks map[HookEvent][]*HookMatcher) *hookDispatcher {
	if len(hooks) == 0 {
		return nil
	}

	return &hookDispatcher{hooks: hooks}
}

// dispatch runs every matcher+hook registered for event against
// input. Returns a merged HookOutput: Continue is true iff every
// firing hook returned Continue=true; the first blocking Reason
// wins. err accumulates the first non-nil error (which also flips
// Continue=false for blocking-capable events).
func (d *hookDispatcher) dispatch(ctx context.Context, event HookEvent, primary string, input HookInput) (HookOutput, error) {
	if d == nil {
		return HookAllow(), nil
	}

	d.mu.RLock()
	matchers := append([]*HookMatcher(nil), d.hooks[event]...)
	d.mu.RUnlock()

	if len(matchers) == 0 {
		return HookAllow(), nil
	}

	merged := HookAllow()

	for _, m := range matchers {
		if m.Matcher != nil && primary != "" && !m.Matcher.MatchString(primary) {
			continue
		}

		if m.Once && !m.fired.CompareAndSwap(false, true) {
			continue
		}

		for _, cb := range m.Hooks {
			callCtx := ctx

			var cancel context.CancelFunc
			if m.Timeout > 0 {
				callCtx, cancel = context.WithTimeout(ctx, m.Timeout)
			}

			out, err := cb(callCtx, input)

			if cancel != nil {
				cancel()
			}

			if err != nil {
				return HookBlock(fmt.Sprintf("hook error: %v", err)), err
			}

			if !out.Continue {
				if merged.Continue {
					merged = out
				}
			}

			if out.SuppressOutput {
				merged.SuppressOutput = true
			}
		}
	}

	return merged, nil
}

// ErrHookRejected is returned up through the SDK when a hook blocks
// UserPromptSubmit. The error wraps the hook's Reason.
var ErrHookRejected = errors.New("opencodesdk: hook rejected")
