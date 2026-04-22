package opencodesdk

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/coder/acp-go-sdk"
)

func TestRequestError_Error_FormatsCodeAndMessage(t *testing.T) {
	e := &RequestError{Code: -32603, Message: "boom"}

	got := e.Error()
	want := "acp error -32603: boom"

	if got != want {
		t.Fatalf("RequestError.Error() = %q, want %q", got, want)
	}
}

func TestWrapACPErr_NilPassthrough(t *testing.T) {
	if got := wrapACPErr(nil); got != nil {
		t.Fatalf("wrapACPErr(nil) = %v, want nil", got)
	}
}

func TestWrapACPErr_NonACPPassthrough(t *testing.T) {
	base := errors.New("some other error")

	got := wrapACPErr(base)
	if got != base { //nolint:errorlint // identity check is intentional
		t.Fatalf("wrapACPErr should return non-ACP errors unchanged; got %v", got)
	}
}

func TestWrapACPErr_AuthRequiredCode(t *testing.T) {
	acpErr := &acp.RequestError{Code: codeAuthRequired, Message: "login first"}

	got := wrapACPErr(acpErr)
	if !errors.Is(got, ErrAuthRequired) {
		t.Fatalf("expected wrapped err to satisfy errors.Is(ErrAuthRequired); got %v", got)
	}

	if msg := got.Error(); !contains(msg, "login first") {
		t.Fatalf("expected message to include acp detail; got %q", msg)
	}
}

func TestWrapACPErr_RequestCancelledCode(t *testing.T) {
	acpErr := &acp.RequestError{Code: codeRequestCancelled, Message: "cancelled"}

	got := wrapACPErr(acpErr)
	if !errors.Is(got, ErrCancelled) {
		t.Fatalf("expected wrapped err to satisfy errors.Is(ErrCancelled); got %v", got)
	}
}

func TestWrapACPErr_OtherCodeYieldsRequestError(t *testing.T) {
	acpErr := &acp.RequestError{Code: -32000 - 42, Message: "unknown", Data: map[string]any{"k": "v"}}

	got := wrapACPErr(acpErr)

	var re *RequestError
	if !errors.As(got, &re) {
		t.Fatalf("expected *RequestError, got %T (%v)", got, got)
	}

	if re.Code != -32042 {
		t.Fatalf("Code = %d, want -32042", re.Code)
	}

	if re.Message != "unknown" {
		t.Fatalf("Message = %q, want %q", re.Message, "unknown")
	}

	if re.Data == nil {
		t.Fatalf("expected Data to be preserved")
	}
}

func TestWrapACPErr_WrappedACPError(t *testing.T) {
	// errors.As should unwrap through fmt.Errorf wrapping.
	acpErr := &acp.RequestError{Code: codeAuthRequired, Message: "need creds"}
	wrapped := fmt.Errorf("calling NewSession: %w", acpErr)

	got := wrapACPErr(wrapped)
	if !errors.Is(got, ErrAuthRequired) {
		t.Fatalf("expected unwrap + classify through fmt.Errorf; got %v", got)
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	// A sanity check that sentinels are not accidentally the same value.
	sentinels := []error{
		ErrAuthRequired,
		ErrCancelled,
		ErrCLINotFound,
		ErrUnsupportedCLIVersion,
		ErrClientClosed,
		ErrClientNotStarted,
		ErrClientAlreadyConnected,
		ErrRequestTimeout,
		ErrTransport,
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}

			if errors.Is(a, b) {
				t.Fatalf("sentinel %d (%v) should not satisfy errors.Is against sentinel %d (%v)", i, a, j, b)
			}
		}
	}
}

func TestWrapACPErrCtx_DeadlineWrapsErrRequestTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Simulate an RPC returning a deadline-exceeded error.
	base := fmt.Errorf("rpc failed: %w", context.DeadlineExceeded)

	got := wrapACPErrCtx(ctx, base)
	if !errors.Is(got, ErrRequestTimeout) {
		t.Fatalf("expected errors.Is(ErrRequestTimeout), got %v", got)
	}

	// Still chains to the underlying deadline for consumers that
	// already check for it.
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("expected errors.Is(context.DeadlineExceeded), got %v", got)
	}
}

func TestWrapACPErrCtx_NonDeadlineFallsThroughToWrapACPErr(t *testing.T) {
	// An ACP auth error should still classify via wrapACPErr paths.
	acpErr := &acp.RequestError{Code: codeAuthRequired, Message: "no creds"}

	got := wrapACPErrCtx(context.Background(), acpErr)
	if !errors.Is(got, ErrAuthRequired) {
		t.Fatalf("expected ErrAuthRequired, got %v", got)
	}
}

func TestTransportError_IsAndUnwrap(t *testing.T) {
	cause := errors.New("broken pipe")
	te := &TransportError{Reason: "subprocess", Err: cause}

	if !errors.Is(te, ErrTransport) {
		t.Fatal("expected errors.Is(te, ErrTransport)")
	}

	if !errors.Is(te, cause) {
		t.Fatal("expected errors.Is(te, cause) via Unwrap")
	}

	if te.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestTransportError_ErrorMessageShapes(t *testing.T) {
	cases := []struct {
		name string
		te   *TransportError
	}{
		{"reason+cause", &TransportError{Reason: "subprocess", Err: errors.New("boom")}},
		{"cause-only", &TransportError{Err: errors.New("boom")}},
		{"reason-only", &TransportError{Reason: "subprocess"}},
		{"empty", &TransportError{}},
	}

	for _, tc := range cases {
		if tc.te.Error() == "" {
			t.Fatalf("%s: empty Error()", tc.name)
		}
	}
}

func TestOpencodeSDKError_MarkerInterface(t *testing.T) {
	var target OpencodeSDKError

	typed := []error{
		&RequestError{Code: -32603, Message: "x"},
		&CLINotFoundError{SearchedPaths: []string{"/nope"}},
		&ProcessError{ExitCode: 1},
		&TransportError{Reason: "subprocess"},
	}

	for _, err := range typed {
		if !errors.As(err, &target) {
			t.Fatalf("%T did not satisfy OpencodeSDKError marker", err)
		}
	}

	// Plain Go errors must NOT satisfy the marker.
	if errors.As(errors.New("plain"), &target) {
		t.Fatal("plain error should not satisfy OpencodeSDKError")
	}
}

// contains is a case-sensitive substring helper to keep the test file
// stdlib-only.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}

	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}

	return false
}
