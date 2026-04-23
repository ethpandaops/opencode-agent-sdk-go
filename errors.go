package opencodesdk

import (
	"context"
	"errors"
	"fmt"

	"github.com/coder/acp-go-sdk"
)

// ACP JSON-RPC error codes we inspect.
const (
	codeAuthRequired     = -32000
	codeRequestCancelled = -32800
)

// OpencodeSDKError is the marker interface implemented by every typed
// error constructed by opencodesdk. Callers can use errors.As against
// this interface to distinguish SDK-originated errors from arbitrary
// Go errors flowing through the same code path without caring about
// the specific variant.
//
//	var sdkErr opencodesdk.OpencodeSDKError
//	if errors.As(err, &sdkErr) {
//	    // err originated from opencodesdk and carries SDK semantics.
//	}
type OpencodeSDKError interface {
	error
	// opencodeSDKError is an unexported sentinel method so only types
	// defined in this package can claim the interface.
	opencodeSDKError()
}

// Sentinel errors for callers to check against with errors.Is.
var (
	// ErrAuthRequired is returned when opencode reports that the user is
	// not authenticated. Instruct the user to run `opencode auth login`
	// in their terminal and retry. Wraps a *RequestError.
	ErrAuthRequired = errors.New("opencode authentication required; run `opencode auth login` in a terminal")

	// ErrCancelled is returned when a prompt turn is cancelled mid-flight.
	ErrCancelled = errors.New("prompt cancelled")

	// ErrCLINotFound is returned when the opencode binary cannot be
	// located in $PATH or at the path supplied via WithCLIPath.
	ErrCLINotFound = errors.New("opencode CLI not found")

	// ErrUnsupportedCLIVersion is returned when the discovered opencode
	// binary is older than MinimumCLIVersion.
	ErrUnsupportedCLIVersion = errors.New("opencode CLI version is older than MinimumCLIVersion")

	// ErrClientClosed is returned by Client methods after Close has been called.
	ErrClientClosed = errors.New("client is closed")

	// ErrClientNotStarted is returned by Client methods called before Start.
	ErrClientNotStarted = errors.New("client has not been started")

	// ErrClientAlreadyConnected is returned by Client.Start when the
	// client has already been started. Each Client may be Started at
	// most once — construct a fresh Client via NewClient for a new
	// subprocess.
	ErrClientAlreadyConnected = errors.New("client has already been started")

	// ErrRequestTimeout is returned by Client / Session RPCs whose
	// context carried a deadline that fired before the agent responded.
	// Wraps context.DeadlineExceeded so existing callers checking for
	// that via errors.Is continue to work.
	ErrRequestTimeout = errors.New("request timed out")

	// ErrCapabilityUnavailable is returned when Session.Prompt is called
	// with a content block (image, audio, embedded resource) that the
	// agent did not advertise support for in its PromptCapabilities.
	ErrCapabilityUnavailable = errors.New("agent does not advertise required prompt capability")

	// ErrUpdateQueueOverflow is recorded when a session/update
	// notification is dropped because the Session.Updates() buffer was
	// full. It is not returned directly — callers observe the condition
	// via Session.DroppedUpdates or the `opencodesdk.session.updates.dropped`
	// metric.
	ErrUpdateQueueOverflow = errors.New("session updates buffer overflowed; notifications were dropped")

	// ErrExtensionMethodRequired is returned by Client.CallExtension
	// when the supplied method name does not begin with an underscore.
	// The ACP spec reserves `_`-prefixed methods for extensions.
	ErrExtensionMethodRequired = errors.New("extension method names must begin with \"_\"")

	// ErrStructuredOutputMissing is returned by DecodeStructuredOutput
	// when neither the PromptResult.Meta block nor the QueryResult
	// AssistantText carries a decodable payload for the requested type.
	ErrStructuredOutputMissing = errors.New("opencodesdk: structured output missing")

	// ErrTransport is returned when the opencode acp subprocess (or
	// equivalent transport) terminates unexpectedly, leaving no
	// in-flight RPC able to complete. See TransportError for the typed
	// companion carrying the underlying cause.
	ErrTransport = errors.New("opencodesdk: transport closed unexpectedly")

	// ErrSessionNotFound is returned by StatSession when the requested
	// session ID does not exist in opencode's local database (or the
	// database file is missing entirely).
	ErrSessionNotFound = errors.New("opencodesdk: session not found")
)

// RequestError is the typed JSON-RPC error surface exposed to callers. It
// wraps *acp.RequestError from the protocol layer so callers can match
// on ACP error codes with errors.As without depending on the coder SDK.
type RequestError struct {
	Code    int
	Message string
	Data    any
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("acp error %d: %s", e.Code, e.Message)
}

func (*RequestError) opencodeSDKError() {}

// CLINotFoundError is the typed companion to ErrCLINotFound. It records
// the paths the SDK searched while trying to locate the opencode
// binary so callers can produce an actionable diagnostic.
//
// The error chain always includes ErrCLINotFound so `errors.Is(err,
// ErrCLINotFound)` works on both the typed and sentinel forms.
type CLINotFoundError struct {
	// SearchedPaths lists the candidate paths the SDK evaluated, in the
	// order they were tried. At minimum this includes "$PATH" when no
	// explicit WithCLIPath was supplied.
	SearchedPaths []string
	// Err is the underlying cause, if any (e.g. the exec.LookPath error).
	Err error
}

func (e *CLINotFoundError) Error() string {
	if len(e.SearchedPaths) == 0 {
		if e.Err != nil {
			return fmt.Sprintf("opencode CLI not found: %v", e.Err)
		}

		return "opencode CLI not found"
	}

	return fmt.Sprintf("opencode CLI not found in: %v", e.SearchedPaths)
}

func (e *CLINotFoundError) Unwrap() error {
	return e.Err
}

// Is reports whether target is ErrCLINotFound. This lets callers write
// `errors.Is(err, ErrCLINotFound)` when they only care about the kind.
func (e *CLINotFoundError) Is(target error) bool {
	return target == ErrCLINotFound //nolint:errorlint // intentional sentinel identity check
}

func (*CLINotFoundError) opencodeSDKError() {}

// ProcessError is the typed companion surfaced when the opencode
// subprocess terminates with a non-zero exit status. The SDK
// constructs this in the watchSubprocess path for callers that want
// to inspect exit code / stderr rather than match on a sentinel.
type ProcessError struct {
	// ExitCode is the subprocess exit code. -1 when unavailable (e.g.
	// signal-terminated before a status was recorded).
	ExitCode int
	// Stderr, when non-empty, is the final tail of the subprocess's
	// stderr as captured by the SDK's stderr forwarder.
	Stderr string
	// Err is the underlying os/exec error (typically *exec.ExitError).
	Err error
}

func (e *ProcessError) Error() string {
	switch {
	case e.Err != nil && e.Stderr != "":
		return fmt.Sprintf("opencode acp process failed (exit %d): %v: %s", e.ExitCode, e.Err, e.Stderr)
	case e.Err != nil:
		return fmt.Sprintf("opencode acp process failed (exit %d): %v", e.ExitCode, e.Err)
	case e.Stderr != "":
		return fmt.Sprintf("opencode acp process failed (exit %d): %s", e.ExitCode, e.Stderr)
	default:
		return fmt.Sprintf("opencode acp process failed (exit %d)", e.ExitCode)
	}
}

func (e *ProcessError) Unwrap() error {
	return e.Err
}

func (*ProcessError) opencodeSDKError() {}

// TransportError is the typed companion to ErrTransport. It is
// returned when the opencode acp subprocess (or equivalent transport)
// closes unexpectedly — callers blocked on an RPC observe this
// instead of a silent context cancellation.
//
// The error chain always includes ErrTransport so callers can write
// `errors.Is(err, ErrTransport)` when they only care about the kind,
// or `errors.As(err, &te)` when they want the underlying cause.
type TransportError struct {
	// Reason is a short label describing which transport layer
	// observed the failure ("subprocess", "custom", ...).
	Reason string
	// Err is the underlying cause, if any (e.g. *exec.ExitError,
	// io.EOF). May be nil when the transport reported a graceful close
	// with no Go-level error.
	Err error
}

func (e *TransportError) Error() string {
	switch {
	case e.Err != nil && e.Reason != "":
		return fmt.Sprintf("opencodesdk: transport closed unexpectedly (%s): %v", e.Reason, e.Err)
	case e.Err != nil:
		return fmt.Sprintf("opencodesdk: transport closed unexpectedly: %v", e.Err)
	case e.Reason != "":
		return fmt.Sprintf("opencodesdk: transport closed unexpectedly (%s)", e.Reason)
	default:
		return "opencodesdk: transport closed unexpectedly"
	}
}

func (e *TransportError) Unwrap() error { return e.Err }

// Is reports whether target is ErrTransport. Callers matching on the
// sentinel observe the typed error regardless of Reason / cause.
func (e *TransportError) Is(target error) bool {
	return target == ErrTransport //nolint:errorlint // intentional sentinel identity check
}

func (*TransportError) opencodeSDKError() {}

// wrapACPErr converts a *acp.RequestError to opencodesdk-native error
// types. Returns err unchanged when it is not a *acp.RequestError.
//
// Callers with a live context should prefer wrapACPErrCtx so
// context.DeadlineExceeded surfaces as ErrRequestTimeout.
func wrapACPErr(err error) error {
	if err == nil {
		return nil
	}

	var re *acp.RequestError
	if !errors.As(err, &re) {
		return err
	}

	switch re.Code {
	case codeAuthRequired:
		return fmt.Errorf("%w: %s", ErrAuthRequired, re.Message)
	case codeRequestCancelled:
		return fmt.Errorf("%w: %s", ErrCancelled, re.Message)
	}

	return &RequestError{Code: re.Code, Message: re.Message, Data: re.Data}
}

// wrapACPErrCtx is wrapACPErr with context-deadline detection: when
// ctx expired and err reflects that (via errors.Is(err,
// context.DeadlineExceeded)), the result wraps ErrRequestTimeout.
func wrapACPErrCtx(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) || (ctx != nil && ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		return fmt.Errorf("%w: %w", ErrRequestTimeout, err)
	}

	return wrapACPErr(err)
}
