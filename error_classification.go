package opencodesdk

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// ErrorClass is the coarse classification bucket for a failure the
// SDK surfaces. Parity with the claude/codex sister SDKs; the opencode
// taxonomy drops a few claude-specific classes that don't map to the
// ACP transport (compaction, max_output_tokens, etc.).
type ErrorClass string

const (
	ErrorClassUnknown             ErrorClass = "unknown"
	ErrorClassAuthentication      ErrorClass = "authentication"
	ErrorClassRateLimit           ErrorClass = "rate_limit"
	ErrorClassOverload            ErrorClass = "overload"
	ErrorClassCancelled           ErrorClass = "cancelled"
	ErrorClassTransientConnection ErrorClass = "transient_connection"
	ErrorClassCapability          ErrorClass = "capability"
	ErrorClassCLI                 ErrorClass = "cli"
	ErrorClassExecution           ErrorClass = "execution"
)

// ErrorSubClass refines ErrorClass with a more specific diagnostic
// bucket for failures whose coarse class doesn't fully describe the
// remediation. Empty by default; populated only when the classifier
// finds a recognisable signal in the message or JSON-RPC code.
//
// Subclass is advisory: existing callers that match on ErrorClass
// alone continue to work unchanged. Resilience wrappers may use the
// subclass to pick targeted retry strategies (e.g. strip media on
// PromptTooLong rather than back-off).
type ErrorSubClass string

const (
	// ErrorSubClassNone is the zero value — no finer classification.
	ErrorSubClassNone ErrorSubClass = ""
	// ErrorSubClassPromptTooLong indicates the prompt (or accumulated
	// context) exceeded the model's context window. Parity with
	// claude's PromptTooLongDetails. Not retryable without mutating
	// the prompt (strip media, compact history, switch model).
	ErrorSubClassPromptTooLong ErrorSubClass = "prompt_too_long"
	// ErrorSubClassRateLimitTokens indicates a token-per-minute cap was
	// hit, as opposed to requests-per-minute.
	ErrorSubClassRateLimitTokens ErrorSubClass = "rate_limit_tokens"
	// ErrorSubClassRateLimitRequests indicates a requests-per-minute
	// cap was hit.
	ErrorSubClassRateLimitRequests ErrorSubClass = "rate_limit_requests"
	// ErrorSubClassInvalidSchema indicates the agent reported malformed
	// params (JSON-RPC -32602 / "expected array, received null" /
	// "invalid_schema"). Typically not retryable; the caller must
	// correct the payload.
	ErrorSubClassInvalidSchema ErrorSubClass = "invalid_schema"
	// ErrorSubClassInvalidModel indicates the supplied model id was
	// not recognised. Typically surfaces from WithModel / SetModel.
	ErrorSubClassInvalidModel ErrorSubClass = "invalid_model"
	// ErrorSubClassProviderError indicates the upstream model provider
	// returned an opaque failure (not rate-limit, not overload). Often
	// retryable with back-off but may also be a permanent provider
	// issue.
	ErrorSubClassProviderError ErrorSubClass = "provider_error"
	// ErrorSubClassSubprocessDied indicates the opencode subprocess
	// exited unexpectedly mid-RPC. The client is degraded; retry
	// requires a new Client.
	ErrorSubClassSubprocessDied ErrorSubClass = "subprocess_died"
)

// RecoveryAction is the SDK's suggested next step for a classified
// failure. Callers are free to ignore the suggestion.
type RecoveryAction string

const (
	RecoveryActionNone             RecoveryAction = "none"
	RecoveryActionRefreshAuth      RecoveryAction = "refresh_auth"
	RecoveryActionRetryWithBackoff RecoveryAction = "retry_with_backoff"
	RecoveryActionFallback         RecoveryAction = "fallback"
	RecoveryActionStop             RecoveryAction = "stop"
)

// ErrorClassification is the result of ClassifyError. Fields are best
// effort: HTTPStatus is populated when the failure is ultimately a
// *RequestError from the ACP transport.
type ErrorClassification struct {
	// Class is the coarse bucket.
	Class ErrorClass
	// SubClass is a finer-grained diagnostic bucket. ErrorSubClassNone
	// when the classifier found no recognisable signal.
	SubClass ErrorSubClass
	// RecoveryAction is the suggested next step.
	RecoveryAction RecoveryAction
	// Retryable reports whether ResilientQuery would retry.
	Retryable bool
	// HTTPStatus is the JSON-RPC error code when the root cause is a
	// *RequestError, or nil otherwise.
	HTTPStatus *int
	// Message is the human-readable failure message the classifier
	// inspected.
	Message string
}

// ClassifyError inspects err and returns a best-effort ErrorClassification.
// Returns ErrorClassUnknown when err is nil or defies matching.
func ClassifyError(err error) ErrorClassification {
	if err == nil {
		return ErrorClassification{Class: ErrorClassUnknown, RecoveryAction: RecoveryActionNone}
	}

	classification := ErrorClassification{Message: err.Error()}

	switch {
	case errors.Is(err, ErrAuthRequired):
		classification.Class = ErrorClassAuthentication
		classification.RecoveryAction = RecoveryActionRefreshAuth
	case errors.Is(err, ErrCancelled):
		classification.Class = ErrorClassCancelled
		classification.RecoveryAction = RecoveryActionStop
	case errors.Is(err, ErrCapabilityUnavailable):
		classification.Class = ErrorClassCapability
		classification.RecoveryAction = RecoveryActionStop
	case errors.Is(err, ErrCLINotFound), errors.Is(err, ErrUnsupportedCLIVersion):
		classification.Class = ErrorClassCLI
		classification.RecoveryAction = RecoveryActionStop
	default:
		classifyFromRequestError(err, &classification)

		if classification.Class == "" {
			classifyFromMessage(classification.Message, &classification)
		}
	}

	if classification.Class == "" {
		classification.Class = ErrorClassExecution
	}

	if classification.SubClass == "" {
		classification.SubClass = subclassFromMessage(classification.Message)
	}

	if classification.RecoveryAction == "" {
		classification.RecoveryAction = recoveryForClass(classification.Class)
	}

	classification.Retryable = isRetryableClass(classification.Class) && classification.SubClass != ErrorSubClassPromptTooLong

	return classification
}

// subclassFromMessage inspects msg for common finer-grained signals.
// Returns ErrorSubClassNone when no subclass pattern matches.
func subclassFromMessage(msg string) ErrorSubClass {
	joined := strings.ToLower(msg)

	switch {
	case containsAny(joined,
		"context length", "context window", "context_length_exceeded",
		"prompt is too long", "maximum context length", "too many tokens",
		"input is too long", "request too large",
	):
		return ErrorSubClassPromptTooLong
	case containsAny(joined, "tokens per minute", "tpm", "token rate"):
		return ErrorSubClassRateLimitTokens
	case containsAny(joined, "requests per minute", "rpm", "request rate"):
		return ErrorSubClassRateLimitRequests
	case containsAny(joined, "invalid params", "expected array", "invalid_request_error",
		"invalid_schema", "malformed", "schema validation"):
		return ErrorSubClassInvalidSchema
	case containsAny(joined, "model not found", "unknown model", "invalid model", "model does not exist"):
		return ErrorSubClassInvalidModel
	case containsAny(joined, "provider error", "upstream", "bad gateway", "provider_returned_error"):
		return ErrorSubClassProviderError
	case containsAny(joined, "opencode acp exited", "subprocess", "process failed", "broken pipe"):
		return ErrorSubClassSubprocessDied
	}

	return ErrorSubClassNone
}

func classifyFromRequestError(err error, out *ErrorClassification) {
	var re *RequestError
	if !errors.As(err, &re) {
		return
	}

	code := re.Code
	out.HTTPStatus = &code

	switch {
	case code == 401 || code == 403:
		out.Class = ErrorClassAuthentication
		out.RecoveryAction = RecoveryActionRefreshAuth
	case code == 408 || code == 409 || (code >= 500 && code < 600 && code != 503 && code != 529):
		out.Class = ErrorClassExecution
	case code == 429:
		out.Class = ErrorClassRateLimit
		out.RecoveryAction = RecoveryActionRetryWithBackoff
	case code == 503 || code == 529:
		out.Class = ErrorClassOverload
		out.RecoveryAction = RecoveryActionRetryWithBackoff
	}
}

func classifyFromMessage(msg string, out *ErrorClassification) {
	joined := strings.ToLower(msg)

	switch {
	case containsAny(joined, "econnreset", "epipe", "connection reset", "broken pipe",
		"connection refused", "network is unreachable", "temporarily unavailable",
		"i/o timeout", "dial tcp"):
		out.Class = ErrorClassTransientConnection
		out.RecoveryAction = RecoveryActionRetryWithBackoff
	case containsAny(joined, "rate limit", "429"):
		out.Class = ErrorClassRateLimit
		out.RecoveryAction = RecoveryActionRetryWithBackoff
	case containsAny(joined, "overload", "server overloaded", "529"):
		out.Class = ErrorClassOverload
		out.RecoveryAction = RecoveryActionRetryWithBackoff
	case containsAny(joined, "unauthorized", "api key", "authentication"):
		out.Class = ErrorClassAuthentication
		out.RecoveryAction = RecoveryActionRefreshAuth
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}

	return false
}

func isRetryableClass(class ErrorClass) bool {
	switch class {
	case ErrorClassRateLimit, ErrorClassOverload, ErrorClassTransientConnection:
		return true
	}

	return false
}

func recoveryForClass(class ErrorClass) RecoveryAction {
	switch class {
	case ErrorClassAuthentication:
		return RecoveryActionRefreshAuth
	case ErrorClassRateLimit, ErrorClassOverload, ErrorClassTransientConnection:
		return RecoveryActionRetryWithBackoff
	case ErrorClassCancelled, ErrorClassCapability, ErrorClassCLI:
		return RecoveryActionStop
	}

	return RecoveryActionNone
}

// retryAfterSecondsPattern matches e.g. "retry after 30s" in error
// messages. Used by the retry policy's delay heuristic.
var retryAfterSecondsPattern = regexp.MustCompile(`(?i)retry(?:ing)?(?:\s+after)?\s+(\d+(?:\.\d+)?)`)

// parseRetryAfterSeconds extracts the first retry-after duration in
// seconds from text, or returns 0 when none is present.
func parseRetryAfterSeconds(text string) float64 {
	match := retryAfterSecondsPattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return 0
	}

	v, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0
	}

	return v
}
