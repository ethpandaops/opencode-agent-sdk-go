package opencodesdk

import (
	"io"
	"log/slog"
)

// discardLogger returns a *slog.Logger that discards all output. Used as
// the default when WithLogger is not supplied so the SDK is silent by
// default.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
