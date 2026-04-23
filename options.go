package opencodesdk

import (
	"log/slog"
	"maps"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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
	cliExtraArgs     map[string]*string
	env              map[string]string
	cwd              string
	opencodeHome     string
	skipVersionCheck bool
	stderr           func(line string)
	user             string

	// Logging
	logger *slog.Logger

	// Handshake
	initializeTimeout time.Duration

	// Session defaults applied by Client.NewSession / LoadSession /
	// each session created via this Client.
	model      string
	agent      string
	effort     Effort
	maxTurns   int
	mcpServers []acp.McpServer
	// additionalDirectories is forwarded as ACP's unstable
	// additionalDirectories field on session/new, session/load,
	// session/fork, session/resume. Gated on the agent advertising
	// SessionCapabilities.AdditionalDirectories during initialize; if
	// unsupported, the values are silently dropped and a warning logged.
	additionalDirectories []string

	// Per-session buffering for the updates channel. Zero → default (128).
	updatesBuffer int

	// Tool filters applied on top of canUseTool. allowedTools is a set
	// of tool names (matched against acp.ToolCall.Title) that are
	// auto-approved; disallowedTools is auto-rejected. disallowedTools
	// wins ties. Tools not named in either list fall through to
	// canUseTool (or the dispatcher default).
	allowedTools    []string
	disallowedTools []string

	// Callbacks
	canUseTool            PermissionCallback
	onFsWrite             FsWriteCallback
	onTurnComplete        TurnCompleteCallback
	onUpdateDropped       UpdateDroppedCallback
	onElicitation         ElicitationCallback
	onElicitationComplete ElicitationCompleteCallback
	onUserInput           UserInputCallback

	// Hook registrations keyed by event. Populated via WithHooks.
	hooks map[HookEvent][]*HookMatcher

	// Auth
	terminalAuthCapability bool
	autoLaunchLogin        bool

	// Filesystem safety
	strictCwdBoundary bool

	// In-process tools served via the loopback MCP bridge.
	sdkTools []Tool

	// Structured output schema passed to the agent via session/new's
	// _meta["structuredOutputSchema"]. Purely advisory — opencode does
	// not enforce it.
	outputSchema map[string]any

	// budgetTracker, when non-nil, is subscribed to every session
	// created by the Client so that usage_update notifications feed
	// its running totals. Crossing any configured cap raises
	// ErrBudgetExceeded. Populated via WithMaxBudgetUSD or
	// WithBudgetTracker.
	budgetTracker *BudgetTracker
	// autoCancelOnBudget requests that the Client call Session.Cancel
	// automatically when the budget is exceeded. Set implicitly by
	// WithMaxBudgetUSD; WithBudgetTracker leaves it false.
	autoCancelOnBudget bool

	// transportFactory, when non-nil, replaces the default
	// subprocess-backed transport. Client.Start calls the factory
	// instead of spawning `opencode acp`. See WithTransport.
	transportFactory TransportFactory

	// Observability providers. nil → OTel global providers (which
	// default to noops).
	meterProvider  metric.MeterProvider
	tracerProvider trace.TracerProvider
	// promRegisterer, when non-nil AND meterProvider is nil, causes
	// the SDK to construct a Prometheus-backed MeterProvider at
	// Client.Start time. Exposed via WithPrometheusRegisterer.
	promRegisterer prometheus.Registerer
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

// WithExtraArgs is a map-shaped sister of WithCLIFlags for callers who
// want to pass `--flag` or `--flag=value` entries by name. Each map
// entry is rendered as one argv token: a nil value yields `--<name>`
// (a bare boolean flag), a non-nil value yields `--<name>=<value>`.
//
// Map iteration order is randomised by Go's runtime — callers that
// care about the order of identical flags should use WithCLIFlags
// instead. WithExtraArgs is additive: subsequent calls accumulate.
//
// Mirrors the same option on the sister claude/codex SDKs.
func WithExtraArgs(args map[string]*string) Option {
	return func(o *options) {
		if o.cliExtraArgs == nil {
			o.cliExtraArgs = make(map[string]*string, len(args))
		}

		maps.Copy(o.cliExtraArgs, args)
	}
}

// WithUser tags the SDK's OpenTelemetry spans and metrics with the
// supplied user identifier. The value is exported as the `user` span
// attribute and metric label on every operation that records OTel data
// (initialize, prompt, tool call, permission, fs delegation,
// session/update, transport health). Useful for multi-tenant hosts
// that want per-user dashboards / cost attribution.
//
// The value is not sent to opencode; it lives entirely in the SDK's
// observability layer.
func WithUser(user string) Option {
	return func(o *options) { o.user = user }
}

// WithEnv provides additional environment variables for the opencode
// subprocess. The SDK inherits os.Environ() by default and overlays
// these values (later entries win).
//
// Useful opencode env toggles:
//
//   - OPENCODE_ENABLE_QUESTION_TOOL=1 enables opencode's interactive
//     "question" tool (disabled by default). When set, the agent may
//     call the tool to route free-form questions back to the client,
//     which the SDK surfaces through session/request_permission.
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

// WithOpencodeHome overrides the XDG_DATA_HOME-derived data directory
// opencode uses for session storage, credentials, and persisted SDK
// artifacts (e.g. session-cost snapshots from CostTracker). The path
// is exported to the subprocess as $XDG_DATA_HOME so opencode picks
// it up on launch, and the SDK uses it as the base for
// LoadSessionCost / SaveSessionCost.
//
// Useful for test isolation and for running multiple opencode
// environments side by side.
func WithOpencodeHome(path string) Option {
	return func(o *options) { o.opencodeHome = path }
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

// Built-in opencode session modes. These match the values advertised
// under ACP's `mode` SessionConfigOption in opencode 1.14.20. Users
// may also configure additional modes in opencode.json.
const (
	// ModeBuild is opencode's default mode. It executes tools based
	// on the configured permission ruleset.
	ModeBuild = "build"
	// ModePlan is opencode's read-only planning mode. It denies all
	// edit tools inline (does NOT route through
	// session/request_permission).
	ModePlan = "plan"
)

// WithAgent selects the opencode agent (a.k.a. session mode) used by
// new sessions. Valid values map to opencode's agent names — typical
// defaults are "build", "plan", "general", "explore", "summarize".
// Applied via session/set_config_option immediately after session/new.
//
// To drive session/request_permission through WithCanUseTool, the user
// must configure explicit "ask" rules in their opencode.json (see the
// WithCanUseTool doc). The built-in plan agent denies edits inline
// rather than asking, so it does not route through the callback path.
//
// WithInitialMode is an alias for this option using ACP terminology
// ("mode" rather than "agent"). Prefer whichever wording matches the
// vocabulary already used by your codebase.
func WithAgent(agent string) Option {
	return func(o *options) { o.agent = agent }
}

// WithInitialMode is ACP-terminology sugar for WithAgent. Pass one of
// the ModeBuild / ModePlan constants (or any other mode id advertised
// under the session's `mode` config option) to select the session's
// starting mode. Applied via session/set_config_option immediately
// after session/new.
//
// Exactly equivalent to WithAgent — the two names exist so callers
// who think in ACP's "mode" vocabulary and callers who think in
// opencode's "agent" vocabulary both find the option they expect.
// When both are supplied, the later one wins.
func WithInitialMode(mode string) Option {
	return WithAgent(mode)
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

// WithOnTurnComplete registers a callback that fires after every
// Session.Prompt completes, whether it succeeded or errored. Useful for
// centralised logging, metrics, or pushing the final assistant message
// into an external store without wrapping every call site.
//
// The callback runs synchronously after Prompt returns to its caller.
// Long-running work should be dispatched off the callback goroutine.
func WithOnTurnComplete(cb TurnCompleteCallback) Option {
	return func(o *options) { o.onTurnComplete = cb }
}

// WithOnUpdateDropped registers a callback invoked whenever a
// session/update notification is dropped because the Session.Updates()
// buffer was full. The callback receives the session ID and the new
// cumulative drop count.
//
// Use this to detect that the consumer of Updates() is falling behind
// — consider increasing WithUpdatesBuffer or offloading the drain loop
// to a goroutine.
func WithOnUpdateDropped(cb UpdateDroppedCallback) Option {
	return func(o *options) { o.onUpdateDropped = cb }
}

// WithMeterProvider sets the OTel MeterProvider for SDK metrics. When
// nil, the OTel global provider is used (which is a noop unless the
// application has installed one).
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(o *options) { o.meterProvider = mp }
}

// WithTracerProvider sets the OTel TracerProvider for SDK spans.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(o *options) { o.tracerProvider = tp }
}

// WithPrometheusRegisterer wires SDK metrics to the supplied
// Prometheus registerer via the official
// go.opentelemetry.io/otel/exporters/prometheus bridge. The SDK
// creates a Prometheus-backed MeterProvider at Client.Start time and
// installs it as the MeterProvider for the client's Observer.
//
// Callers who already supply an MeterProvider through
// WithMeterProvider take precedence — that MeterProvider wins and the
// registerer is ignored. Callers who want both a direct
// MeterProvider AND Prometheus scraping should register the
// Prometheus exporter themselves.
//
// The registered metrics are scraped in OpenMetrics format with
// whatever HTTP handler the caller wires up against reg (typically
// `promhttp.HandlerFor(reg, ...)`).
func WithPrometheusRegisterer(reg prometheus.Registerer) Option {
	return func(o *options) { o.promRegisterer = reg }
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

// WithAutoLaunchLogin enables automatic relaunch of the terminal-auth
// command when opencode reports authRequired (-32000). The SDK spawns
// the command parsed from the auth method's _meta["terminal-auth"]
// block with stdio inherited from the parent process, waits for it to
// exit, then retries the failing session/new or session/load once.
//
// Requires WithTerminalAuthCapability(true) so opencode actually
// advertises launch instructions. Default: false.
func WithAutoLaunchLogin(enabled bool) Option {
	return func(o *options) {
		o.autoLaunchLogin = enabled
		if enabled {
			o.terminalAuthCapability = true
		}
	}
}

// WithStrictCwdBoundary rejects fs/write_text_file delegations for
// paths outside the configured cwd. When enabled without a configured
// cwd, every write is rejected. Default: false (writes are allowed
// anywhere the process has filesystem access to).
func WithStrictCwdBoundary(enabled bool) Option {
	return func(o *options) { o.strictCwdBoundary = enabled }
}

// WithPure disables opencode's external plugins, matching the CLI's
// `--pure` flag. Plugins installed via `opencode plugin` are skipped
// for the spawned process, yielding a reproducible runtime surface
// (no hook-injection from user-installed plugins).
//
// Sugar for WithCLIFlags("--pure").
func WithPure() Option {
	return WithCLIFlags("--pure")
}

// WithMaxBudgetUSD caps total USD spend observed across every session
// on the owning Client. The SDK installs a BudgetTracker, auto-
// subscribes each new session to its UsageUpdate stream, and — when
// the cap is crossed — calls Session.Cancel on the in-flight turn.
// Subsequent Session.Prompt calls return a wrapped ErrBudgetExceeded.
//
// The tracker is accessible via Client.BudgetTracker() for callers
// that want to inspect the running totals or near-completion state.
//
// Caveats:
//
//   - Budget observation is cumulative across all sessions on the
//     Client. Each NewClient starts a fresh tally.
//   - Cost is reported by opencode's provider layer; when a provider
//     omits pricing, the USD cap has no effect. Use
//     WithBudgetTracker + MaxTotalTokens for providers without
//     pricing metadata.
//   - Cancellation is best-effort: an in-flight Prompt observes
//     ErrCancelled (wrapped with ErrBudgetExceeded) but a turn that
//     has already emitted its final response lands normally.
func WithMaxBudgetUSD(budgetUSD float64) Option {
	return func(o *options) {
		tracker, err := NewBudgetTracker(BudgetTrackerOptions{MaxCostUSD: &budgetUSD})
		if err != nil {
			// NewBudgetTracker only fails on bad CompletionThreshold;
			// we pass the default so this path is unreachable.
			return
		}

		o.budgetTracker = tracker
		o.autoCancelOnBudget = true
	}
}

// WithBudgetTracker installs a caller-supplied BudgetTracker on the
// Client. Every session created by the Client is auto-subscribed to
// feed the tracker, but unlike WithMaxBudgetUSD the SDK does NOT
// auto-cancel sessions — the caller is responsible for acting on
// tracker.Status() or CheckBudget().
//
// Useful when the caller wants a pre-populated tracker (e.g. restored
// from persisted CostTracker state) or a token-cap-only policy.
func WithBudgetTracker(t *BudgetTracker) Option {
	return func(o *options) {
		o.budgetTracker = t
		o.autoCancelOnBudget = false
	}
}

// WithMaxTurns caps the number of agent turns observed on each session
// created by the Client. opencode has no protocol-level turn limit, so
// the cap is enforced client-side: every assistant_message_chunk
// notification with start==true bumps a counter, and once the limit is
// crossed the SDK calls Session.Cancel on the in-flight turn. Subsequent
// Prompt calls on the same session run normally (opencode itself does
// not track the cap).
//
// A value of 0 (the default) disables the cap. Counts are per-session,
// not per-Client. Useful as a backstop against runaway agent loops.
func WithMaxTurns(n int) Option {
	return func(o *options) { o.maxTurns = n }
}

// WithAddDirs appends additional workspace root directories activated
// for every session created on this Client. Paths must be absolute.
//
// This forwards to ACP's unstable additionalDirectories field on
// session/new, session/load, session/fork, and session/resume. It
// requires the agent to advertise SessionCapabilities.AdditionalDirectories
// during initialize — when unsupported, the values are ignored and a
// warning is logged at session-creation time.
//
// Additional per-call entries can be supplied via WithAddDirs as a
// per-session option override; they extend the Client-level defaults.
func WithAddDirs(dirs ...string) Option {
	return func(o *options) {
		o.additionalDirectories = append(o.additionalDirectories, dirs...)
	}
}
