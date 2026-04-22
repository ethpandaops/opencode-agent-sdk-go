package opencodesdk

import (
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
