package opencodesdk

import (
	"context"
	"testing"

	"github.com/coder/acp-go-sdk"
)

func mkReq(kinds ...acp.PermissionOptionKind) acp.RequestPermissionRequest {
	opts := make([]acp.PermissionOption, len(kinds))
	for i, k := range kinds {
		opts[i] = acp.PermissionOption{
			OptionId: acp.PermissionOptionId("opt-" + string(k)),
			Name:     "opt-" + string(k),
			Kind:     k,
		}
	}

	return acp.RequestPermissionRequest{Options: opts}
}

func TestAllowOncePicksAllowOnce(t *testing.T) {
	req := mkReq(acp.PermissionOptionKindRejectOnce, acp.PermissionOptionKindAllowOnce, acp.PermissionOptionKindAllowAlways)

	resp, err := AllowOnce(context.Background(), req)
	if err != nil {
		t.Fatalf("AllowOnce: %v", err)
	}

	if resp.Outcome.Selected == nil {
		t.Fatalf("expected Selected outcome, got %+v", resp.Outcome)
	}

	if string(resp.Outcome.Selected.OptionId) != "opt-allow_once" {
		t.Fatalf("unexpected option: %s", resp.Outcome.Selected.OptionId)
	}
}

func TestAllowAlwaysFallsBackToAllowOnce(t *testing.T) {
	req := mkReq(acp.PermissionOptionKindRejectOnce, acp.PermissionOptionKindAllowOnce)

	resp, err := AllowAlways(context.Background(), req)
	if err != nil {
		t.Fatalf("AllowAlways: %v", err)
	}

	if string(resp.Outcome.Selected.OptionId) != "opt-allow_once" {
		t.Fatalf("expected fallback to allow_once, got %s", resp.Outcome.Selected.OptionId)
	}
}

func TestRejectOnceErrorsWhenNoReject(t *testing.T) {
	req := mkReq(acp.PermissionOptionKindAllowOnce)

	_, err := RejectOnce(context.Background(), req)
	if err == nil {
		t.Fatalf("expected error when no reject option offered")
	}
}

func TestPickByKindErrorsOnEmpty(t *testing.T) {
	_, err := pickByKind(acp.RequestPermissionRequest{}, acp.PermissionOptionKindAllowOnce)
	if err == nil {
		t.Fatalf("expected error on empty options list")
	}
}
