// Package handlers provides a Dispatcher that implements acp.Client so
// the coder/acp-go-sdk ClientSideConnection can dispatch agent→client
// RPCs and notifications into opencodesdk. Each method routes to a
// user-supplied callback when present, and falls back to a sensible
// default otherwise (pass-through for fs/write, reject for permission,
// no-op for session/update, unsupported for terminal).
package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/coder/acp-go-sdk"
)

// PermissionCallback is invoked when the agent requests permission for
// a tool call. Return the option the user selected, or return with
// ctx.Err != nil to signal cancellation.
type PermissionCallback func(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)

// FsWriteCallback is invoked when the agent delegates a file write.
// Return a non-nil error to refuse the write (opencode will surface
// this as a tool failure).
type FsWriteCallback func(ctx context.Context, req acp.WriteTextFileRequest) error

// SessionUpdateCallback is invoked for every session/update notification
// streamed by the agent.
type SessionUpdateCallback func(ctx context.Context, params acp.SessionNotification) error

// Callbacks bundles all user-supplied handler hooks.
type Callbacks struct {
	Permission    PermissionCallback
	FsWrite       FsWriteCallback
	SessionUpdate SessionUpdateCallback
}

// Dispatcher implements acp.Client by routing incoming RPCs into the
// supplied Callbacks.
type Dispatcher struct {
	Callbacks Callbacks
	Logger    *slog.Logger

	// StrictCwdBoundary rejects fs/write_text_file delegations for any
	// path outside Cwd. If StrictCwdBoundary is true but Cwd is empty,
	// every write is rejected.
	StrictCwdBoundary bool
	Cwd               string
}

var _ acp.Client = (*Dispatcher)(nil)

// SessionUpdate handles inbound session/update notifications. The
// callback is invoked synchronously; a non-nil error is returned to the
// coder SDK which surfaces it as a notification-handling failure.
func (d *Dispatcher) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	if d.Callbacks.SessionUpdate != nil {
		return d.Callbacks.SessionUpdate(ctx, params)
	}

	return nil
}

// RequestPermission handles agent-initiated permission requests. If no
// callback is set, the request is rejected with the "reject" option (if
// offered) and an error is logged so developers notice the missing
// handler.
func (d *Dispatcher) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	if d.Callbacks.Permission != nil {
		return d.Callbacks.Permission(ctx, params)
	}

	d.Logger.WarnContext(ctx, "permission request with no callback; auto-rejecting",
		slog.String("tool_call_id", string(params.ToolCall.ToolCallId)),
	)

	rejectID := acp.PermissionOptionId("reject")

	for _, opt := range params.Options {
		if opt.Kind == acp.PermissionOptionKindRejectOnce || opt.Kind == acp.PermissionOptionKindRejectAlways {
			rejectID = opt.OptionId

			break
		}
	}

	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: rejectID},
		},
	}, nil
}

// ReadTextFile handles fs/read_text_file delegations. opencode never
// emits these at present, but we implement the method anyway because
// we advertise fs.readTextFile capability and coder SDK requires the
// full interface.
func (d *Dispatcher) ReadTextFile(_ context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	if !filepath.IsAbs(params.Path) {
		return acp.ReadTextFileResponse{}, fmt.Errorf("fs/read_text_file: path must be absolute: %q", params.Path)
	}

	data, err := os.ReadFile(params.Path)
	if err != nil {
		return acp.ReadTextFileResponse{}, fmt.Errorf("fs/read_text_file: %w", err)
	}

	content := string(data)
	if params.Line != nil || params.Limit != nil {
		content = applyLineLimit(content, params.Line, params.Limit)
	}

	return acp.ReadTextFileResponse{Content: content}, nil
}

// WriteTextFile handles fs/write_text_file delegations. opencode emits
// this after an approved edit to sync the client's in-memory buffer
// with on-disk state. Default behavior is to write through; callers
// can override via FsWriteCallback.
func (d *Dispatcher) WriteTextFile(ctx context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if d.StrictCwdBoundary {
		if err := assertWithinCwd(params.Path, d.Cwd); err != nil {
			return acp.WriteTextFileResponse{}, err
		}
	}

	if d.Callbacks.FsWrite != nil {
		if err := d.Callbacks.FsWrite(ctx, params); err != nil {
			return acp.WriteTextFileResponse{}, err
		}

		return acp.WriteTextFileResponse{}, nil
	}

	if !filepath.IsAbs(params.Path) {
		return acp.WriteTextFileResponse{}, fmt.Errorf("fs/write_text_file: path must be absolute: %q", params.Path)
	}

	dir := filepath.Dir(params.Path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return acp.WriteTextFileResponse{}, fmt.Errorf("fs/write_text_file mkdir: %w", err)
		}
	}

	if err := os.WriteFile(params.Path, []byte(params.Content), 0o644); err != nil { //nolint:gosec // intentional: agent-requested write
		return acp.WriteTextFileResponse{}, fmt.Errorf("fs/write_text_file: %w", err)
	}

	return acp.WriteTextFileResponse{}, nil
}

// assertWithinCwd verifies that a write target is inside the
// configured cwd. cwd must be non-empty when StrictCwdBoundary is on —
// otherwise every write is refused because there's no boundary to
// check against.
func assertWithinCwd(target, cwd string) error {
	if cwd == "" {
		return fmt.Errorf("fs/write_text_file: strict-cwd-boundary enabled with no cwd configured; rejecting %q", target)
	}

	if !filepath.IsAbs(target) {
		return fmt.Errorf("fs/write_text_file: path must be absolute: %q", target)
	}

	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("fs/write_text_file: resolve cwd %q: %w", cwd, err)
	}

	clean := filepath.Clean(target)

	rel, err := filepath.Rel(absCwd, clean)
	if err != nil {
		return fmt.Errorf("fs/write_text_file: path %q not under cwd %q: %w", target, absCwd, err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("fs/write_text_file: path %q escapes cwd %q", target, absCwd)
	}

	return nil
}

// Terminal methods — opencode never uses these. We return not-implemented.
var errTerminalUnsupported = errors.New("terminal/* not supported by opencode ACP")

func (d *Dispatcher) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, errTerminalUnsupported
}

func (d *Dispatcher) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, errTerminalUnsupported
}

func (d *Dispatcher) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, errTerminalUnsupported
}

func (d *Dispatcher) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, errTerminalUnsupported
}

func (d *Dispatcher) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, errTerminalUnsupported
}

// applyLineLimit slices content per the ACP fs/read_text_file spec:
// line is 1-based start, limit caps the number of returned lines.
func applyLineLimit(content string, line, limit *int) string {
	lines := strings.Split(content, "\n")

	start := 0
	if line != nil && *line > 0 {
		start = min(*line-1, len(lines))
	}

	end := len(lines)
	if limit != nil && *limit > 0 && start+*limit < end {
		end = start + *limit
	}

	return strings.Join(lines[start:end], "\n")
}
