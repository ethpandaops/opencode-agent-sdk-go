package opencodesdk

import (
	"context"

	acp "github.com/coder/acp-go-sdk"
)

// WithAllowedTools names opencode tools that the SDK should auto-approve
// without invoking the WithCanUseTool callback. Names are matched against
// acp.ToolCall.Title (opencode emits the tool name there — e.g. "edit",
// "bash", "read", "write"). Unknown names match nothing.
//
// opencode only emits session/request_permission for tools whose own
// permission ruleset resolves to "ask" (see WithCanUseTool). WithAllowedTools
// filters that ask-list further:
//
//   - Tool in allow list → pick the first allow_once option and skip the
//     WithCanUseTool callback entirely.
//   - Tool in disallow list (set via WithDisallowedTools) → pick the first
//     reject_once option. Disallow wins ties.
//   - Tool in neither list → delegate to WithCanUseTool, or the default
//     dispatcher behavior (auto-reject with warning) if no callback was set.
//
// Multiple calls accumulate. Matching is exact and case-sensitive — pass
// the opencode tool name as it appears in session/request_permission.
//
// Sister SDK parity: mirrors claude-agent-sdk-go's WithAllowedTools and
// codex-agent-sdk-go's WithAllowedTools.
func WithAllowedTools(names ...string) Option {
	return func(o *options) {
		o.allowedTools = append(o.allowedTools, names...)
	}
}

// WithDisallowedTools names opencode tools that the SDK should auto-reject
// without invoking the WithCanUseTool callback. See WithAllowedTools for
// the filter semantics. Disallow entries win over allow entries for the
// same name.
//
// Sister SDK parity: mirrors claude-agent-sdk-go's WithDisallowedTools and
// codex-agent-sdk-go's WithDisallowedTools.
func WithDisallowedTools(names ...string) Option {
	return func(o *options) {
		o.disallowedTools = append(o.disallowedTools, names...)
	}
}

// composeCanUseTool wraps the user-supplied PermissionCallback (if any)
// with the allow/deny list filter. When neither list is set and the user
// supplied a callback, the callback is returned unchanged so there is
// zero overhead. When neither list is set and there is no callback, the
// return value is nil and the dispatcher's default reject-with-warning
// behavior applies.
func composeCanUseTool(allow, deny []string, user PermissionCallback) PermissionCallback {
	if len(allow) == 0 && len(deny) == 0 {
		return user
	}

	allowSet := toSet(allow)
	denySet := toSet(deny)

	return func(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		name := ""
		if req.ToolCall.Title != nil {
			name = *req.ToolCall.Title
		}

		if _, rejected := denySet[name]; rejected {
			return RejectOnce(ctx, req)
		}

		if _, allowed := allowSet[name]; allowed {
			return AllowOnce(ctx, req)
		}

		if user != nil {
			return user(ctx, req)
		}

		return RejectOnce(ctx, req)
	}
}

func toSet(s []string) map[string]struct{} {
	if len(s) == 0 {
		return nil
	}

	m := make(map[string]struct{}, len(s))
	for _, v := range s {
		m[v] = struct{}{}
	}

	return m
}
