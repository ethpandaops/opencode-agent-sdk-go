package opencodesdk

import (
	"errors"
	"fmt"

	"github.com/coder/acp-go-sdk"
)

// ACP JSON-RPC error codes we inspect.
const (
	codeAuthRequired     = -32000
	codeRequestCancelled = -32800
)

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

// wrapACPErr converts a *acp.RequestError to opencodesdk-native error
// types. Returns err unchanged when it is not a *acp.RequestError.
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
