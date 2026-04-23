package opencodesdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/observability"
)

// ResilientQueryOptions configures ResilientQuery.
type ResilientQueryOptions struct {
	// RetryPolicy controls the per-attempt back-off schedule. Leave as
	// zero value to use DefaultRetryPolicy().
	RetryPolicy RetryPolicy
	// OnRetry, when set, is invoked before each retry sleep with the
	// attempt number (1-based), the retry decision, and the underlying
	// error. Useful for logging and metric emission.
	OnRetry func(ctx context.Context, attempt int, decision RetryDecision, err error)
	// Logger receives debug-level retry notices. Nil is acceptable.
	Logger *slog.Logger
}

// ResilientQuery wraps Query with an automatic retry loop governed by
// a RetryPolicy and ClassifyError. Transient failures (rate limit,
// overload, transient connection errors) are retried with exponential
// back-off + jitter. Authentication, capability, and CLI-not-found
// errors surface immediately because no retry can recover them.
//
// Example:
//
//	res, err := opencodesdk.ResilientQuery(ctx, "hello",
//	    opencodesdk.ResilientQueryOptions{
//	        RetryPolicy: opencodesdk.RetryPolicy{MaxRetries: 3},
//	    },
//	    opencodesdk.WithCwd("/repo"),
//	)
func ResilientQuery(ctx context.Context, prompt string, rq ResilientQueryOptions, opts ...Option) (*QueryResult, error) {
	policy := normalisePolicy(rq.RetryPolicy)
	obs := observability.NewObserver(otel.GetMeterProvider(), otel.GetTracerProvider())

	var lastErr error

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		result, err := Query(ctx, prompt, opts...)
		if err == nil {
			return result, nil
		}

		lastErr = err

		decision := EvaluateRetry(err, attempt+1, policy)
		if !decision.Retryable {
			obs.RecordRetryAttempt(ctx, string(decision.Class), "fatal")

			return nil, err
		}

		delay := decision.RecommendedDelay

		obs.RecordRetryAttempt(ctx, string(decision.Class), "retry")

		if rq.OnRetry != nil {
			rq.OnRetry(ctx, attempt+1, decision, err)
		}

		if rq.Logger != nil {
			rq.Logger.DebugContext(ctx, "opencodesdk: retrying after error",
				slog.Int("attempt", attempt+1),
				slog.String("class", string(decision.Class)),
				slog.Duration("delay", delay),
				slog.Any("error", err),
			)
		}

		if !sleepOrCancel(ctx, delay) {
			return nil, ctx.Err()
		}
	}

	obs.RecordRetryAttempt(ctx, string(ClassifyError(lastErr).Class), "exhausted")

	return nil, fmt.Errorf("opencodesdk: ResilientQuery: exhausted %d retries: %w", policy.MaxRetries, lastErr)
}

// sleepOrCancel sleeps for d or returns false if ctx is cancelled
// before the duration elapses.
func sleepOrCancel(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// IsRetryable reports whether err would be retried by ResilientQuery
// under the default policy. Thin convenience wrapper around
// ClassifyError for callers who want a boolean predicate.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	return ClassifyError(err).Retryable
}

// UnwrapRequestError returns the underlying *RequestError when err
// wraps one, or nil otherwise. Provided as a convenience alongside
// errors.As for callers who want a one-liner.
func UnwrapRequestError(err error) *RequestError {
	var re *RequestError
	if errors.As(err, &re) {
		return re
	}

	return nil
}
