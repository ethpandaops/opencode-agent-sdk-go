package opencodesdk

import (
	"io"
	"log/slog"
)

// NopLogger returns a *slog.Logger that discards all output. It is
// convenient for tests and for callers that want to silence the SDK
// without wiring up a real handler. Passing the result to [WithLogger]
// is equivalent to not calling [WithLogger] at all — the SDK is silent
// by default.
func NopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// discardLogger is the internal alias used when WithLogger is not
// supplied. Kept as a separate symbol so existing call sites keep
// working and so the public NopLogger can evolve independently.
func discardLogger() *slog.Logger {
	return NopLogger()
}
