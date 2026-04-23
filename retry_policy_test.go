package opencodesdk

import (
	"errors"
	"testing"
	"time"
)

func TestClassifyError_Nil(t *testing.T) {
	c := ClassifyError(nil)
	if c.Class != ErrorClassUnknown {
		t.Fatalf("want unknown, got %s", c.Class)
	}
}

func TestClassifyError_Sentinels(t *testing.T) {
	cases := []struct {
		err   error
		class ErrorClass
	}{
		{ErrAuthRequired, ErrorClassAuthentication},
		{ErrCancelled, ErrorClassCancelled},
		{ErrCapabilityUnavailable, ErrorClassCapability},
		{ErrCLINotFound, ErrorClassCLI},
		{ErrUnsupportedCLIVersion, ErrorClassCLI},
	}

	for _, tc := range cases {
		if got := ClassifyError(tc.err).Class; got != tc.class {
			t.Fatalf("%v: want %s, got %s", tc.err, tc.class, got)
		}
	}
}

func TestClassifyError_RequestError(t *testing.T) {
	cases := []struct {
		code   int
		class  ErrorClass
		retry  bool
		action RecoveryAction
	}{
		{401, ErrorClassAuthentication, false, RecoveryActionRefreshAuth},
		{429, ErrorClassRateLimit, true, RecoveryActionRetryWithBackoff},
		{503, ErrorClassOverload, true, RecoveryActionRetryWithBackoff},
		{500, ErrorClassExecution, false, RecoveryActionNone},
	}

	for _, tc := range cases {
		err := &RequestError{Code: tc.code, Message: "x"}
		c := ClassifyError(err)

		if c.Class != tc.class {
			t.Fatalf("code %d: class: want %s, got %s", tc.code, tc.class, c.Class)
		}

		if c.Retryable != tc.retry {
			t.Fatalf("code %d: retryable: want %v, got %v", tc.code, tc.retry, c.Retryable)
		}

		if c.RecoveryAction != tc.action {
			t.Fatalf("code %d: action: want %s, got %s", tc.code, tc.action, c.RecoveryAction)
		}

		if c.HTTPStatus == nil || *c.HTTPStatus != tc.code {
			t.Fatalf("code %d: HTTPStatus: want %d, got %v", tc.code, tc.code, c.HTTPStatus)
		}
	}
}

func TestClassifyError_TransientConnection(t *testing.T) {
	c := ClassifyError(errors.New("read: connection reset by peer"))
	if c.Class != ErrorClassTransientConnection || !c.Retryable {
		t.Fatalf("unexpected: %+v", c)
	}
}

func TestEvaluateRetry_NotRetryable(t *testing.T) {
	d := EvaluateRetry(ErrAuthRequired, 1, RetryPolicy{})
	if d.Retryable {
		t.Fatal("auth must not be retryable")
	}
}

func TestEvaluateRetry_Retryable(t *testing.T) {
	d := EvaluateRetry(&RequestError{Code: 429, Message: "rate limited"}, 1, RetryPolicy{MaxRetries: 3, BaseDelay: 10 * time.Millisecond})
	if !d.Retryable {
		t.Fatal("expected retryable")
	}

	if d.RecommendedDelay <= 0 {
		t.Fatal("expected non-zero delay")
	}
}

func TestEvaluateRetry_ExhaustedAttempts(t *testing.T) {
	d := EvaluateRetry(&RequestError{Code: 503}, 99, RetryPolicy{MaxRetries: 3})
	if d.Retryable {
		t.Fatal("past MaxRetries must not be retryable")
	}
}

func TestIsRetryable(t *testing.T) {
	if IsRetryable(nil) {
		t.Fatal("nil must not be retryable")
	}

	if !IsRetryable(&RequestError{Code: 429}) {
		t.Fatal("429 should be retryable")
	}

	if IsRetryable(ErrCancelled) {
		t.Fatal("cancellation should not be retryable")
	}
}

func TestUnwrapRequestError(t *testing.T) {
	re := &RequestError{Code: 123, Message: "boom"}
	if UnwrapRequestError(re) != re {
		t.Fatal("expected same pointer")
	}

	if UnwrapRequestError(errors.New("other")) != nil {
		t.Fatal("expected nil for non-RequestError")
	}
}

func TestComputeDelay_ExponentialWithCap(t *testing.T) {
	p := RetryPolicy{MaxRetries: 10, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond, JitterRatio: 0}

	for attempt := 1; attempt <= 15; attempt++ {
		d := computeDelay(p, attempt)
		if d > p.MaxDelay {
			t.Fatalf("attempt %d: delay %s exceeds cap %s", attempt, d, p.MaxDelay)
		}
	}
}
