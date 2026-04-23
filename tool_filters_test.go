package opencodesdk

import (
	"context"
	"errors"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

const (
	optAllowOnce  = "once"
	optRejectOnce = "reject"
	toolRead      = "read"
)

func permReq(toolName string) acp.RequestPermissionRequest {
	title := toolName

	return acp.RequestPermissionRequest{
		SessionId: "ses_test",
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "call_1",
			Title:      &title,
		},
		Options: []acp.PermissionOption{
			{OptionId: optAllowOnce, Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow once"},
			{OptionId: "always", Kind: acp.PermissionOptionKindAllowAlways, Name: "Always allow"},
			{OptionId: optRejectOnce, Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject"},
		},
	}
}

func selected(t *testing.T, resp acp.RequestPermissionResponse) acp.PermissionOptionId {
	t.Helper()

	if resp.Outcome.Selected == nil {
		t.Fatalf("outcome was not `selected`: %+v", resp.Outcome)
	}

	return resp.Outcome.Selected.OptionId
}

func TestComposeCanUseTool_NoFilters_ReturnsUserUnchanged(t *testing.T) {
	user := func(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		return acp.RequestPermissionResponse{}, errors.New("user called")
	}

	cb := composeCanUseTool(nil, nil, user)

	_, err := cb(context.Background(), permReq("edit"))
	if err == nil || err.Error() != "user called" {
		t.Fatalf("expected user callback to be invoked unchanged, got err=%v", err)
	}
}

func TestComposeCanUseTool_NoFiltersNoUser_ReturnsNil(t *testing.T) {
	if cb := composeCanUseTool(nil, nil, nil); cb != nil {
		t.Fatalf("expected nil callback so dispatcher default applies, got %v", cb)
	}
}

func TestComposeCanUseTool_AllowedBypassesUserCallback(t *testing.T) {
	var userCalled bool

	user := func(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		userCalled = true

		return acp.RequestPermissionResponse{}, errors.New("should not be called")
	}

	cb := composeCanUseTool([]string{"edit"}, nil, user)

	resp, err := cb(context.Background(), permReq("edit"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if userCalled {
		t.Fatalf("user callback must not run when tool is pre-allowed")
	}

	if got := selected(t, resp); got != optAllowOnce {
		t.Fatalf("expected allow_once option id %q, got %q", optAllowOnce, got)
	}
}

func TestComposeCanUseTool_DisallowedBypassesUserCallback(t *testing.T) {
	var userCalled bool

	user := func(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		userCalled = true

		return acp.RequestPermissionResponse{}, errors.New("should not be called")
	}

	cb := composeCanUseTool(nil, []string{"bash"}, user)

	resp, err := cb(context.Background(), permReq("bash"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if userCalled {
		t.Fatalf("user callback must not run when tool is pre-denied")
	}

	if got := selected(t, resp); got != optRejectOnce {
		t.Fatalf("expected reject option id %q, got %q", optRejectOnce, got)
	}
}

func TestComposeCanUseTool_DisallowWinsOverAllow(t *testing.T) {
	cb := composeCanUseTool([]string{"edit"}, []string{"edit"}, nil)

	resp, err := cb(context.Background(), permReq("edit"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := selected(t, resp); got != optRejectOnce {
		t.Fatalf("disallow should win: expected reject, got %q", got)
	}
}

func TestComposeCanUseTool_UnmatchedFallsThroughToUser(t *testing.T) {
	var seen string

	user := func(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		if req.ToolCall.Title != nil {
			seen = *req.ToolCall.Title
		}

		return AllowOnce(context.Background(), req)
	}

	cb := composeCanUseTool([]string{"edit"}, []string{"bash"}, user)

	resp, err := cb(context.Background(), permReq(toolRead))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if seen != toolRead {
		t.Fatalf("user callback should see unmatched tool, got %q", seen)
	}

	if got := selected(t, resp); got != optAllowOnce {
		t.Fatalf("expected user callback's AllowOnce, got %q", got)
	}
}

func TestComposeCanUseTool_UnmatchedWithoutUserRejects(t *testing.T) {
	cb := composeCanUseTool([]string{"edit"}, nil, nil)

	resp, err := cb(context.Background(), permReq("bash"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := selected(t, resp); got != optRejectOnce {
		t.Fatalf("unmatched with allow list set must reject, got %q", got)
	}
}

func TestComposeCanUseTool_NilTitleFallsThrough(t *testing.T) {
	var userCalled bool

	user := func(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		userCalled = true

		return AllowOnce(context.Background(), req)
	}

	cb := composeCanUseTool([]string{"edit"}, []string{"bash"}, user)

	req := permReq("edit")
	req.ToolCall.Title = nil

	resp, err := cb(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !userCalled {
		t.Fatalf("nil title should fall through to user callback")
	}

	if got := selected(t, resp); got != optAllowOnce {
		t.Fatalf("expected user AllowOnce, got %q", got)
	}
}

func TestWithAllowedAndDisallowed_StoreOnOptions(t *testing.T) {
	o := apply([]Option{
		WithAllowedTools("edit", "read"),
		WithAllowedTools("write"),
		WithDisallowedTools("bash"),
	})

	if len(o.allowedTools) != 3 {
		t.Fatalf("allowedTools len = %d, want 3", len(o.allowedTools))
	}

	if o.allowedTools[0] != "edit" || o.allowedTools[1] != "read" || o.allowedTools[2] != "write" {
		t.Fatalf("allowedTools accumulate in order, got %v", o.allowedTools)
	}

	if len(o.disallowedTools) != 1 || o.disallowedTools[0] != "bash" {
		t.Fatalf("disallowedTools = %v, want [bash]", o.disallowedTools)
	}
}
