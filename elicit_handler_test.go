package opencodesdk

import (
	"context"
	"errors"
	"testing"

	"github.com/coder/acp-go-sdk"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/handlers"
)

func TestWithOnElicitation_StoresCallback(t *testing.T) {
	t.Parallel()

	called := false
	cb := func(context.Context, acp.UnstableCreateElicitationRequest) (acp.UnstableCreateElicitationResponse, error) {
		called = true

		return DeclineElicitation(t.Context(), acp.UnstableCreateElicitationRequest{})
	}

	o := apply([]Option{WithOnElicitation(cb)})
	if o.onElicitation == nil {
		t.Fatalf("WithOnElicitation did not set onElicitation")
	}

	_, err := o.onElicitation(t.Context(), acp.UnstableCreateElicitationRequest{
		Form: &acp.UnstableCreateElicitationForm{Message: "hi", Mode: "form"},
	})
	if err != nil {
		t.Fatalf("callback invocation failed: %v", err)
	}

	if !called {
		t.Fatalf("callback was not invoked")
	}
}

func TestWithOnElicitationComplete_StoresCallback(t *testing.T) {
	t.Parallel()

	called := false
	cb := func(context.Context, acp.UnstableCompleteElicitationNotification) {
		called = true
	}

	o := apply([]Option{WithOnElicitationComplete(cb)})

	if o.onElicitationComplete == nil {
		t.Fatalf("WithOnElicitationComplete did not set onElicitationComplete")
	}

	o.onElicitationComplete(t.Context(), acp.UnstableCompleteElicitationNotification{})

	if !called {
		t.Fatalf("callback was not invoked")
	}
}

func TestDeclineElicitation_ReturnsDecline(t *testing.T) {
	t.Parallel()

	resp, err := DeclineElicitation(t.Context(), acp.UnstableCreateElicitationRequest{})
	if err != nil {
		t.Fatalf("DeclineElicitation returned error: %v", err)
	}

	if resp.Decline == nil {
		t.Fatalf("DeclineElicitation did not set Decline variant")
	}

	if resp.Decline.Action != "decline" {
		t.Fatalf("Decline.Action = %q; want %q", resp.Decline.Action, "decline")
	}
}

func TestInstrumentedElicitation_NilReturnsNil(t *testing.T) {
	t.Parallel()

	c := newTestClient()
	if got := c.instrumentedElicitation(nil); got != nil {
		t.Fatalf("instrumentedElicitation(nil) = non-nil; want nil so dispatcher auto-declines")
	}
}

func TestInstrumentedElicitation_RoutesAccept(t *testing.T) {
	t.Parallel()

	c := newTestClient()

	cb := func(context.Context, acp.UnstableCreateElicitationRequest) (acp.UnstableCreateElicitationResponse, error) {
		return acp.UnstableCreateElicitationResponse{
			Accept: &acp.UnstableCreateElicitationAccept{Action: "accept", Content: map[string]any{"ok": true}},
		}, nil
	}

	wrapped := c.instrumentedElicitation(cb)
	if wrapped == nil {
		t.Fatalf("wrapped callback is nil")
	}

	resp, err := wrapped(t.Context(), acp.UnstableCreateElicitationRequest{
		Form: &acp.UnstableCreateElicitationForm{Message: "pick one", Mode: "form"},
	})
	if err != nil {
		t.Fatalf("wrapped callback error: %v", err)
	}

	if resp.Accept == nil {
		t.Fatalf("expected Accept variant, got %+v", resp)
	}
}

func TestInstrumentedElicitation_RoutesError(t *testing.T) {
	t.Parallel()

	c := newTestClient()
	wantErr := errors.New("callback failed")

	cb := func(context.Context, acp.UnstableCreateElicitationRequest) (acp.UnstableCreateElicitationResponse, error) {
		return acp.UnstableCreateElicitationResponse{}, wantErr
	}

	wrapped := c.instrumentedElicitation(cb)

	_, err := wrapped(t.Context(), acp.UnstableCreateElicitationRequest{
		Form: &acp.UnstableCreateElicitationForm{Mode: "form"},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v; want %v", err, wantErr)
	}
}

func TestWrapElicitationComplete_NilReturnsNil(t *testing.T) {
	t.Parallel()

	if got := wrapElicitationComplete(nil); got != nil {
		t.Fatalf("wrapElicitationComplete(nil) = non-nil; want nil")
	}
}

func TestWrapElicitationComplete_RoutesCall(t *testing.T) {
	t.Parallel()

	var got acp.UnstableCompleteElicitationNotification

	cb := func(_ context.Context, p acp.UnstableCompleteElicitationNotification) {
		got = p
	}

	wrapped := wrapElicitationComplete(cb)

	want := acp.UnstableCompleteElicitationNotification{ElicitationId: "abc"}
	wrapped(t.Context(), want)

	if got.ElicitationId != want.ElicitationId {
		t.Fatalf("callback not invoked with expected params: got=%+v want=%+v", got, want)
	}
}

func TestElicitationMode_Discriminator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  acp.UnstableCreateElicitationRequest
		want string
	}{
		{name: "form", req: acp.UnstableCreateElicitationRequest{Form: &acp.UnstableCreateElicitationForm{}}, want: "form"},
		{name: "url", req: acp.UnstableCreateElicitationRequest{Url: &acp.UnstableCreateElicitationUrl{}}, want: "url"},
		{name: "empty", req: acp.UnstableCreateElicitationRequest{}, want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := elicitationMode(tt.req); got != tt.want {
				t.Fatalf("elicitationMode(%s) = %q; want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestDispatcher_UnstableCreateElicitation_DefaultDecline(t *testing.T) {
	t.Parallel()

	d := &handlers.Dispatcher{Logger: discardLogger()}

	resp, err := d.UnstableCreateElicitation(t.Context(), acp.UnstableCreateElicitationRequest{
		Form: &acp.UnstableCreateElicitationForm{Mode: "form"},
	})
	if err != nil {
		t.Fatalf("default handler returned error: %v", err)
	}

	if resp.Decline == nil {
		t.Fatalf("default handler did not return Decline; got %+v", resp)
	}
}

func TestDispatcher_UnstableCreateElicitation_RoutesCallback(t *testing.T) {
	t.Parallel()

	accepted := false

	d := &handlers.Dispatcher{
		Logger: discardLogger(),
		Callbacks: handlers.Callbacks{
			Elicitation: func(context.Context, acp.UnstableCreateElicitationRequest) (acp.UnstableCreateElicitationResponse, error) {
				accepted = true

				return acp.UnstableCreateElicitationResponse{
					Accept: &acp.UnstableCreateElicitationAccept{Action: "accept"},
				}, nil
			},
		},
	}

	resp, err := d.UnstableCreateElicitation(t.Context(), acp.UnstableCreateElicitationRequest{
		Form: &acp.UnstableCreateElicitationForm{Mode: "form"},
	})
	if err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	if !accepted {
		t.Fatalf("callback was not invoked")
	}

	if resp.Accept == nil {
		t.Fatalf("expected Accept response, got %+v", resp)
	}
}

func TestDispatcher_UnstableCompleteElicitation_RoutesCallback(t *testing.T) {
	t.Parallel()

	var got acp.UnstableCompleteElicitationNotification

	d := &handlers.Dispatcher{
		Logger: discardLogger(),
		Callbacks: handlers.Callbacks{
			ElicitationComplete: func(_ context.Context, p acp.UnstableCompleteElicitationNotification) {
				got = p
			},
		},
	}

	want := acp.UnstableCompleteElicitationNotification{ElicitationId: "xyz"}
	if err := d.UnstableCompleteElicitation(t.Context(), want); err != nil {
		t.Fatalf("notification handler returned error: %v", err)
	}

	if got.ElicitationId != want.ElicitationId {
		t.Fatalf("callback got %+v; want %+v", got, want)
	}
}

func TestDispatcher_UnstableCompleteElicitation_NoCallbackNoop(t *testing.T) {
	t.Parallel()

	d := &handlers.Dispatcher{Logger: discardLogger()}

	if err := d.UnstableCompleteElicitation(t.Context(), acp.UnstableCompleteElicitationNotification{}); err != nil {
		t.Fatalf("no-callback path returned error: %v", err)
	}
}
