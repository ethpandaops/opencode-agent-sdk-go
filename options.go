package opencodesdk

import (
	"log/slog"
	"maps"
	"time"
)

// Option configures a Client or a one-shot Query. Options are applied
// with the functional options pattern: call NewClient(WithCwd(...), ...)
// or Query(ctx, "hi", WithModel("..."), ...).
type Option func(*options)

// options is the internal aggregator for all WithXxx settings. It is
// intentionally unexported — callers configure through the Option
// constructors.
type options struct {
	// Subprocess lifecycle
	cliPath          string
	cliFlags         []string
	env              map[string]string
	cwd              string
	skipVersionCheck bool
	stderr           func(line string)

	// Logging
	logger *slog.Logger

	// Handshake
	initializeTimeout time.Duration
}

// defaultOptions returns the zero-value options with safe defaults.
func defaultOptions() *options {
	return &options{
		initializeTimeout: 60 * time.Second,
	}
}

func apply(opts []Option) *options {
	o := defaultOptions()

	for _, opt := range opts {
		opt(o)
	}

	if o.logger == nil {
		o.logger = discardLogger()
	}

	return o
}

// WithLogger sets the structured logger the SDK uses for diagnostics.
// If not set, the SDK is silent.
func WithLogger(l *slog.Logger) Option {
	return func(o *options) { o.logger = l }
}

// WithCLIPath pins the path to the opencode binary. If not set, the
// SDK looks up `opencode` in $PATH.
func WithCLIPath(path string) Option {
	return func(o *options) { o.cliPath = path }
}

// WithCLIFlags appends extra flags to the `opencode acp` invocation. The
// SDK always passes `--hostname 127.0.0.1 --port 0` to keep opencode's
// internal REST server on loopback; those are not overridable.
func WithCLIFlags(flags ...string) Option {
	return func(o *options) {
		o.cliFlags = append(o.cliFlags, flags...)
	}
}

// WithEnv provides additional environment variables for the opencode
// subprocess. The SDK inherits os.Environ() by default and overlays
// these values (later entries win).
func WithEnv(env map[string]string) Option {
	return func(o *options) {
		if o.env == nil {
			o.env = make(map[string]string, len(env))
		}

		maps.Copy(o.env, env)
	}
}

// WithCwd sets the working directory for the opencode subprocess and the
// default `cwd` sent with session/new. Absolute paths are required.
func WithCwd(path string) Option {
	return func(o *options) { o.cwd = path }
}

// WithStderr registers a callback that receives each line written to
// opencode's stderr. If not set, stderr is forwarded to the configured
// logger at debug level.
func WithStderr(fn func(line string)) Option {
	return func(o *options) { o.stderr = fn }
}

// WithInitializeTimeout caps how long Start waits for the ACP
// initialize handshake. Default: 60s.
func WithInitializeTimeout(d time.Duration) Option {
	return func(o *options) { o.initializeTimeout = d }
}

// WithSkipVersionCheck disables the MinimumCLIVersion assertion during
// Start. Useful for local development against unreleased opencode builds.
func WithSkipVersionCheck(skip bool) Option {
	return func(o *options) { o.skipVersionCheck = skip }
}
