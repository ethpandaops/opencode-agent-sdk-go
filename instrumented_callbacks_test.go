package opencodesdk

import (
	"context"
	"errors"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// TestInstrumentedPermission_NilCallbackReturnsNil guards against a
// regression where instrumentedPermission always returned a non-nil
// wrapper, which bypassed the dispatcher's default auto-reject path.
// With a nil user callback, the SDK must return nil so the dispatcher's
// own reject fallback (which selects a reject option from the request)
// runs instead of surfacing a JSON-RPC internal error to the agent.
func TestInstrumentedPermission_NilCallbackReturnsNil(t *testing.T) {
	c := newTestClient()

	got := c.instrumentedPermission(nil)
	if got != nil {
		t.Fatalf("instrumentedPermission(nil) = non-nil; want nil so dispatcher falls through")
	}
}

func TestInstrumentedPermission_WrapsUserCallback(t *testing.T) {
	c := newTestClient()

	called := false

	cb := func(_ context.Context, _ acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		called = true

		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Selected: &acp.RequestPermissionOutcomeSelected{OptionId: "allow_once"},
			},
		}, nil
	}

	wrapped := c.instrumentedPermission(cb)
	if wrapped == nil {
		t.Fatalf("instrumentedPermission(cb) = nil; want non-nil wrapper")
	}

	resp, err := wrapped(t.Context(), acp.RequestPermissionRequest{})
	if err != nil {
		t.Fatalf("wrapped cb: %v", err)
	}

	if !called {
		t.Fatalf("user callback was never invoked")
	}

	if resp.Outcome.Selected == nil || string(resp.Outcome.Selected.OptionId) != "allow_once" {
		t.Fatalf("wrapper did not propagate selected outcome: %+v", resp.Outcome)
	}
}

// TestInstrumentedFsWrite_NilCallbackReturnsNil guards against the same
// class of regression for fs/write_text_file. With a nil user callback,
// the SDK must return nil so the dispatcher's default "write to disk"
// path runs. Previously the wrapper was always non-nil and returned
// success without writing, silently breaking split-process setups.
func TestInstrumentedFsWrite_NilCallbackReturnsNil(t *testing.T) {
	c := newTestClient()

	got := c.instrumentedFsWrite(nil)
	if got != nil {
		t.Fatalf("instrumentedFsWrite(nil) = non-nil; want nil so dispatcher falls through")
	}
}

func TestInstrumentedFsWrite_WrapsUserCallback(t *testing.T) {
	c := newTestClient()

	called := false

	cb := func(_ context.Context, _ acp.WriteTextFileRequest) error {
		called = true

		return nil
	}

	wrapped := c.instrumentedFsWrite(cb)
	if wrapped == nil {
		t.Fatalf("instrumentedFsWrite(cb) = nil; want non-nil wrapper")
	}

	err := wrapped(t.Context(), acp.WriteTextFileRequest{Path: "/tmp/does-not-matter"})
	if err != nil {
		t.Fatalf("wrapped cb: %v", err)
	}

	if !called {
		t.Fatalf("user callback was never invoked")
	}
}

func TestInstrumentedFsWrite_PropagatesError(t *testing.T) {
	c := newTestClient()

	sentinel := errors.New("refused")

	cb := func(_ context.Context, _ acp.WriteTextFileRequest) error {
		return sentinel
	}

	wrapped := c.instrumentedFsWrite(cb)

	err := wrapped(t.Context(), acp.WriteTextFileRequest{Path: "/tmp/does-not-matter"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("wrapped err = %v, want errors.Is(sentinel)", err)
	}
}
