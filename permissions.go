package opencodesdk

import (
	"context"
	"errors"

	"github.com/coder/acp-go-sdk"
)

// PermissionCallback is invoked when opencode asks the client to
// authorize a tool call (session/request_permission). The callback
// returns the outcome to send back to the agent.
//
// If the callback blocks and ctx is cancelled, the callback MUST return
// promptly so the SDK can respond to opencode with outcome=cancelled.
// The returned error is sent to opencode as a JSON-RPC internal error —
// prefer returning a structured PermissionResponse with a reject option
// over returning err unless something catastrophic happened.
type PermissionCallback func(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)

// FsWriteCallback is invoked when opencode delegates a file write via
// fs/write_text_file. opencode emits this after an approved edit to
// sync client buffers with on-disk state. Return a non-nil error to
// refuse the write (opencode surfaces the failure to the agent).
//
// When no callback is configured the SDK's default behavior is to
// write the file through (with absolute-path enforcement and parent
// directory creation).
type FsWriteCallback func(ctx context.Context, req acp.WriteTextFileRequest) error

// WithCanUseTool registers a callback for session/request_permission.
// If unset, the SDK auto-rejects every permission request and logs a
// warning — opencode surfaces this to the agent as tool rejection.
//
// Important: opencode only emits session/request_permission when its
// own permission ruleset resolves to "ask" for the tool. The out-of-
// the-box opencode config has "*": "allow", so no callbacks fire until
// the user sets explicit rules in their opencode.json, e.g.:
//
//	{
//	  "permission": {
//	    "edit":  "ask",
//	    "write": "ask",
//	    "bash":  "ask"
//	  }
//	}
//
// The plan agent has its own ruleset that denies edits inline (no ask),
// so permission prompts from plan mode are not reachable via this
// callback either.
func WithCanUseTool(cb PermissionCallback) Option {
	return func(o *options) { o.canUseTool = cb }
}

// WithOnFsWrite registers a callback for fs/write_text_file
// delegations. If unset, the SDK writes the file to disk.
//
// opencode does NOT use fs/write_text_file as its primary write path —
// the built-in write and edit tools write directly to the local
// filesystem via Node fs. fs/write_text_file is a secondary sync
// notification opencode sends after an approved "edit" permission, so
// editor clients (VS Code, Zed, …) can update their in-memory buffer.
// That means this callback only fires after:
//
//  1. opencode's ruleset is configured with permission.edit = "ask"
//     (see WithCanUseTool for config example), and
//  2. the WithCanUseTool callback allows the edit.
//
// The write has already happened on opencode's side by the time this
// callback runs; returning an error from it does not undo the write,
// it only surfaces the error back over JSON-RPC.
func WithOnFsWrite(cb FsWriteCallback) Option {
	return func(o *options) { o.onFsWrite = cb }
}

// AllowOnce is a PermissionCallback helper that selects the first
// "allow_once" option if present. It falls back to the first option
// on offer, or errors if none exist. Safe default for developer loops.
func AllowOnce(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return pickByKind(req, acp.PermissionOptionKindAllowOnce)
}

// AllowAlways selects the first "allow_always" option, falling back to
// allow_once then first.
func AllowAlways(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	if resp, err := pickByKind(req, acp.PermissionOptionKindAllowAlways); err == nil {
		return resp, nil
	}

	return pickByKind(req, acp.PermissionOptionKindAllowOnce)
}

// RejectOnce selects the first "reject_once" option, falling back to
// reject_always then first.
func RejectOnce(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	if resp, err := pickByKind(req, acp.PermissionOptionKindRejectOnce); err == nil {
		return resp, nil
	}

	return pickByKind(req, acp.PermissionOptionKindRejectAlways)
}

func pickByKind(req acp.RequestPermissionRequest, kind acp.PermissionOptionKind) (acp.RequestPermissionResponse, error) {
	for _, opt := range req.Options {
		if opt.Kind == kind {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Selected: &acp.RequestPermissionOutcomeSelected{OptionId: opt.OptionId},
				},
			}, nil
		}
	}

	if len(req.Options) == 0 {
		return acp.RequestPermissionResponse{}, errors.New("opencodesdk: no permission options on offer")
	}

	return acp.RequestPermissionResponse{}, errNoOptionOfKind
}

var errNoOptionOfKind = errors.New("no permission option of requested kind")
