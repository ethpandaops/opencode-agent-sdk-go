package opencodesdk

import (
	cryptorand "crypto/rand"
	"math/big"
	"time"
)

// RetryPolicy configures the per-attempt delay schedule used by
// ResilientQuery and EvaluateRetry. A zero-value policy resolves to
// DefaultRetryPolicy().
type RetryPolicy struct {
	// MaxRetries caps the number of retry attempts. Original call +
	// MaxRetries retries = MaxRetries+1 total attempts.
	MaxRetries int
	// BaseDelay is the base back-off window. Attempt N uses
	// BaseDelay * 2^(N-1), clamped to MaxDelay.
	BaseDelay time.Duration
	// MaxDelay caps the computed delay.
	MaxDelay time.Duration
	// JitterRatio is the fraction of BaseDelay to use as random
	// jitter. 0.0–1.0.
	JitterRatio float64
}

// DefaultRetryPolicy returns conservative defaults: 5 retries,
// 500ms base, 30s cap, 25% jitter.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:  5,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		JitterRatio: 0.25,
	}
}

// RetryDecision is the output of EvaluateRetry, summarising whether a
// retry should occur and how long to wait.
type RetryDecision struct {
	Class            ErrorClass
	Retryable        bool
	Attempt          int
	MaxRetries       int
	RecommendedDelay time.Duration
}

// EvaluateRetry combines ClassifyError with the supplied policy and
// attempt count to produce a retry decision. attempt is 1-based: pass
// 1 for the first retry consideration (after the initial call
// failed).
func EvaluateRetry(err error, attempt int, policy RetryPolicy) RetryDecision {
	normalised := normalisePolicy(policy)
	classification := ClassifyError(err)

	decision := RetryDecision{
		Class:      classification.Class,
		Retryable:  classification.Retryable,
		Attempt:    attempt,
		MaxRetries: normalised.MaxRetries,
	}

	if !classification.Retryable || attempt > normalised.MaxRetries {
		decision.Retryable = false

		return decision
	}

	// Prefer a server-supplied retry-after hint when the error carries one.
	if hint := parseRetryAfterSeconds(classification.Message); hint > 0 {
		delay := min(time.Duration(hint*float64(time.Second)), normalised.MaxDelay)
		decision.RecommendedDelay = delay

		return decision
	}

	decision.RecommendedDelay = computeDelay(normalised, attempt)

	return decision
}

func normalisePolicy(p RetryPolicy) RetryPolicy {
	def := DefaultRetryPolicy()
	if p.MaxRetries <= 0 {
		p.MaxRetries = def.MaxRetries
	}

	if p.BaseDelay <= 0 {
		p.BaseDelay = def.BaseDelay
	}

	if p.MaxDelay <= 0 {
		p.MaxDelay = def.MaxDelay
	}

	if p.JitterRatio <= 0 {
		p.JitterRatio = def.JitterRatio
	}

	return p
}

func computeDelay(p RetryPolicy, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := p.BaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= p.MaxDelay {
			delay = p.MaxDelay

			break
		}
	}

	return applyJitter(delay, p.JitterRatio, p.MaxDelay)
}

func applyJitter(delay time.Duration, ratio float64, maxDelay time.Duration) time.Duration {
	if delay <= 0 || ratio <= 0 {
		return delay
	}

	window := time.Duration(float64(delay) * ratio)
	if window <= 0 {
		return delay
	}

	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(window)+1))
	if err != nil {
		return delay
	}

	jittered := delay + time.Duration(n.Int64())
	if maxDelay > 0 && jittered > maxDelay {
		return maxDelay
	}

	return jittered
}
