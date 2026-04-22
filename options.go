package opencodesdk

import (
	"log/slog"
	"maps"
	"time"

	"github.com/coder/acp-go-sdk"
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

	// Session defaults applied by Client.NewSession / LoadSession /
	// each session created via this Client.
	model      string
	agent      string
	mcpServers []acp.McpServer

	// Per-session buffering for the updates channel. Zero → default (128).
	updatesBuffer int

	// Callbacks
	canUseTool PermissionCallback
	onFsWrite  FsWriteCallback

	// Auth
	terminalAuthCapability bool

	// In-process tools served via the loopback MCP bridge.
	sdkTools []Tool
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

// WithModel selects the model used by new sessions. The value must
// match one of the options exposed by opencode in the session/new
// configOptions (e.g. "anthropic/claude-sonnet-4-6" or
// "anthropic/claude-sonnet-4/high"). Applied via session/set_config_option
// immediately after session/new.
func WithModel(id string) Option {
	return func(o *options) { o.model = id }
}

// WithAgent selects the opencode agent (a.k.a. session mode) used by
// new sessions. Valid values map to opencode's agent names — typical
// defaults are "build", "plan", "general", "explore", "summarize".
// Applied via session/set_config_option immediately after session/new.
//
// Use "plan" to see session/request_permission prompts for edits; the
// default "build" agent auto-allows all tool calls.
func WithAgent(agent string) Option {
	return func(o *options) { o.agent = agent }
}

// WithMCPServers declares external MCP servers to attach to every new
// session. To expose in-process Go tools to the agent, use WithSDKTools
// (which lands in M6).
func WithMCPServers(servers ...acp.McpServer) Option {
	return func(o *options) {
		o.mcpServers = append(o.mcpServers, servers...)
	}
}

// WithUpdatesBuffer sets the buffer size of each Session.Updates()
// channel. If notifications arrive faster than the consumer drains,
// updates beyond this buffer are dropped and logged as a warning.
// Default: 128.
func WithUpdatesBuffer(n int) Option {
	return func(o *options) { o.updatesBuffer = n }
}

// WithTerminalAuthCapability advertises _meta["terminal-auth"]=true in
// the initialize handshake's ClientCapabilities. On opencode this
// causes AuthMethod entries to include a _meta["terminal-auth"] block
// with launch instructions (Command, Args, Env, Label). Use
// TerminalAuthInstructions to extract them, and AgentInfo + AuthMethods
// accessors to inspect them.
//
// This is a no-op for agents that don't honor the capability. Default: false.
func WithTerminalAuthCapability(enabled bool) Option {
	return func(o *options) { o.terminalAuthCapability = enabled }
}
