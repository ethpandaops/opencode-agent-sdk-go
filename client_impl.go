package opencodesdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/cli"
	"github.com/ethpandaops/opencode-agent-sdk-go/internal/handlers"
	"github.com/ethpandaops/opencode-agent-sdk-go/internal/mcp/bridge"
	"github.com/ethpandaops/opencode-agent-sdk-go/internal/observability"
	"github.com/ethpandaops/opencode-agent-sdk-go/internal/subprocess"
)

const bridgeMcpName = "opencodesdk"

// client is the concrete Client implementation.
type client struct {
	opts *options

	mu             sync.Mutex
	started        bool
	closed         bool
	transport      Transport
	dispatcher     *handlers.Dispatcher
	agentCaps      acp.AgentCapabilities
	agentInfo      acp.Implementation
	protocolVer    acp.ProtocolVersion
	authMethods    []acp.AuthMethod
	subprocessSpan trace.Span
	// transportErr, when non-nil, is the TransportError recorded when
	// watchSubprocess observed the subprocess dying mid-session. RPC
	// call sites surface it in preference to generic io/ctx errors.
	transportErr *TransportError

	sessionsMu sync.RWMutex
	sessions   map[acp.SessionId]*session
	// pendingUpdates holds session/update notifications that arrived
	// before the corresponding session was registered. This happens in
	// narrow windows around NewSession/ForkSession where opencode emits
	// notifications (e.g. available_commands_update fires ~1 tick after
	// the lifecycle response) concurrently with our registration path.
	// Drained into the session channel at registerSession time.
	// Per-session cap: pendingUpdatesCap.
	pendingUpdates map[acp.SessionId][]acp.SessionNotification

	bridge *bridge.Bridge

	observer *observability.Observer
	health   healthTracker
	hooks    *hookDispatcher

	// watchOnce guards the subprocess crash-monitor goroutine.
	watchOnce sync.Once
}

// pendingUpdatesCap bounds the number of notifications we buffer for a
// session before it has been registered. An opencode session emits a
// handful of notifications in its first tick; 64 covers that plus
// headroom without risking unbounded growth if a session ID never
// registers (in which case Close drops the buffer).
const pendingUpdatesCap = 64

func newClient(o *options) (*client, error) {
	mp, err := resolveMeterProvider(o)
	if err != nil {
		return nil, err
	}

	return &client{
		opts:           o,
		sessions:       make(map[acp.SessionId]*session),
		pendingUpdates: make(map[acp.SessionId][]acp.SessionNotification),
		observer:       observability.NewObserver(mp, o.tracerProvider),
		hooks:          newHookDispatcher(o.hooks),
	}, nil
}

// resolveMeterProvider materialises a MeterProvider from the
// configured options. If WithMeterProvider was supplied it wins.
// Otherwise, if WithPrometheusRegisterer was supplied we construct a
// Prometheus-backed provider. Otherwise the returned provider is nil
// (Observer falls back to the OTel global provider, which is a noop
// unless the application installs one).
func resolveMeterProvider(o *options) (metric.MeterProvider, error) {
	switch {
	case o.meterProvider != nil:
		return o.meterProvider, nil
	case o.promRegisterer != nil:
		mp, err := observability.NewPrometheusMeterProvider(o.promRegisterer)
		if err != nil {
			return nil, fmt.Errorf("opencodesdk: configure prometheus meter provider: %w", err)
		}

		return mp, nil
	default:
		// Explicit noop MeterProvider would also work; nil lets the
		// Observer pick up the OTel global provider.
		return nil, nil //nolint:nilnil // both-nil is the "use global" signal
	}
}

// Start spawns the opencode subprocess (or runs the registered
// WithTransport factory) and runs the ACP initialize handshake.
func (c *client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return ErrClientAlreadyConnected
	}

	if c.closed {
		return ErrClientClosed
	}

	c.dispatcher = &handlers.Dispatcher{
		Logger: c.opts.logger,
		Callbacks: handlers.Callbacks{
			SessionUpdate:       c.routeSessionUpdate,
			Permission:          c.instrumentedPermission(composeCanUseTool(c.opts.allowedTools, c.opts.disallowedTools, c.opts.canUseTool)),
			FsWrite:             c.instrumentedFsWrite(c.opts.onFsWrite),
			Elicitation:         c.instrumentedElicitation(c.opts.onElicitation),
			ElicitationComplete: wrapElicitationComplete(c.opts.onElicitationComplete),
		},
		StrictCwdBoundary: c.opts.strictCwdBoundary,
		Cwd:               c.opts.cwd,
	}

	if len(c.opts.sdkTools) > 0 {
		br, brErr := bridge.New(toolsToBridgeDefs(c.opts.sdkTools), c.opts.logger, c.observer)
		if brErr != nil {
			return fmt.Errorf("creating mcp bridge: %w", brErr)
		}

		if brErr := br.Start(ctx); brErr != nil {
			return fmt.Errorf("starting mcp bridge: %w", brErr)
		}

		c.bridge = br
	}

	tr, err := c.buildTransport(ctx)
	if err != nil {
		return err
	}

	c.transport = tr

	if err := c.initialize(ctx); err != nil {
		_ = c.transport.Close()
		c.transport = nil

		return fmt.Errorf("ACP initialize: %w", err)
	}

	c.started = true

	c.watchOnce.Do(func() { go c.watchSubprocess() })

	return nil
}

// buildTransport returns the Transport to use for this Client. When a
// WithTransport factory is configured, CLI discovery and subprocess
// spawn are skipped entirely. Otherwise the default subprocess-backed
// transport is returned.
func (c *client) buildTransport(ctx context.Context) (Transport, error) {
	if c.opts.transportFactory != nil {
		tr, err := c.opts.transportFactory(ctx, c.dispatcher)
		if err != nil {
			return nil, fmt.Errorf("opencodesdk: transport factory: %w", err)
		}

		if tr == nil {
			return nil, errors.New("opencodesdk: transport factory returned nil transport")
		}

		_, c.subprocessSpan = c.observer.StartSubprocessSpan(ctx, "custom")

		return tr, nil
	}

	path, version, err := (&cli.Discoverer{
		Path:             c.opts.cliPath,
		SkipVersionCheck: c.opts.skipVersionCheck,
		MinimumVersion:   MinimumCLIVersion,
		Logger:           c.opts.logger,
	}).Discover(ctx)
	if err != nil {
		switch {
		case errors.Is(err, cli.ErrNotFound):
			searched := []string{"$PATH"}
			if c.opts.cliPath != "" {
				searched = []string{c.opts.cliPath}
			}

			return nil, &CLINotFoundError{SearchedPaths: searched, Err: err}
		case errors.Is(err, cli.ErrUnsupportedVersion):
			return nil, fmt.Errorf("%w: %v", ErrUnsupportedCLIVersion, err)
		default:
			return nil, fmt.Errorf("discovering opencode CLI: %w", err)
		}
	}

	c.opts.logger.InfoContext(ctx, "opencode CLI discovered",
		slog.String("path", path),
		slog.String("version", version),
	)

	// Subprocess span covers the child's lifetime — ended in Close.
	_, c.subprocessSpan = c.observer.StartSubprocessSpan(ctx, path)

	proc, err := subprocess.Spawn(ctx, subprocess.Config{
		Path:           path,
		Args:           subprocessArgs(c.opts),
		Env:            subprocessEnv(c.opts),
		Cwd:            c.opts.cwd,
		Logger:         c.opts.logger,
		StderrCallback: c.opts.stderr,
	}, c.dispatcher)
	if err != nil {
		c.observer.RecordCLISpawn(ctx, "error")
		c.subprocessSpan.End()
		c.subprocessSpan = nil

		return nil, fmt.Errorf("spawning opencode acp: %w", err)
	}

	c.observer.RecordCLISpawn(ctx, "started")

	return proc, nil
}

// watchSubprocess fires when opencode exits unexpectedly mid-session.
// It logs the cause, closes every live session (so callers blocked on
// Updates() or a Prompt unblock), and leaves the client in the closed
// state so subsequent RPCs return ErrClientClosed instead of hanging.
//
// Transports that don't implement WatchableTransport (e.g. test
// doubles backed by an in-memory pipe) skip this watcher entirely —
// those transports have no notion of "exited" and are tied to the
// caller's own lifetime.
func (c *client) watchSubprocess() {
	watch, ok := c.transport.(WatchableTransport)
	if !ok {
		return
	}

	<-watch.Exited()

	err := watch.ExitErr()

	c.mu.Lock()
	closing := c.closed
	c.mu.Unlock()

	if closing {
		return
	}

	if err != nil {
		c.opts.logger.Error("opencode acp exited unexpectedly", slog.Any("error", err))
		c.health.recordFailure("subprocess", err.Error())
	} else {
		c.opts.logger.Warn("opencode acp exited unexpectedly (no error reported)")
		c.health.recordFailure("subprocess", "opencode acp exited unexpectedly")
	}

	c.observer.RecordTransportFailure(context.Background(), "subprocess")
	c.health.markDegraded()

	// Close all live sessions so their Updates channels drain and any
	// blocked Prompt calls unwind through context cancellation.
	c.sessionsMu.Lock()

	for _, s := range c.sessions {
		s.close()
	}

	c.sessions = map[acp.SessionId]*session{}
	c.pendingUpdates = map[acp.SessionId][]acp.SessionNotification{}
	c.sessionsMu.Unlock()

	c.mu.Lock()
	c.closed = true
	c.transportErr = &TransportError{Reason: "subprocess", Err: err}
	c.mu.Unlock()
}

func (c *client) initialize(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, c.opts.initializeTimeout)
	defer cancel()

	spanCtx, span := c.observer.StartInitializeSpan(initCtx)
	defer span.End()

	started := time.Now()

	caps := acp.ClientCapabilities{
		Fs: acp.FileSystemCapabilities{
			ReadTextFile:  true,
			WriteTextFile: true,
		},
		Terminal: false,
	}
	if c.opts.terminalAuthCapability {
		caps.Meta = map[string]any{"terminal-auth": true}
	}

	resp, err := c.transport.Conn().Initialize(spanCtx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: caps,
	})
	if err != nil {
		c.observer.RecordInitializeDuration(spanCtx, time.Since(started), false)

		return wrapACPErr(err)
	}

	c.observer.RecordInitializeDuration(spanCtx, time.Since(started), true)

	c.protocolVer = resp.ProtocolVersion
	c.agentCaps = resp.AgentCapabilities
	c.authMethods = resp.AuthMethods

	if resp.AgentInfo != nil {
		c.agentInfo = *resp.AgentInfo
	}

	c.opts.logger.InfoContext(ctx, "opencode acp initialized",
		slog.Any("protocol_version", resp.ProtocolVersion),
		slog.String("agent_name", c.agentInfo.Name),
		slog.String("agent_version", c.agentInfo.Version),
	)

	return nil
}

// Close terminates the subprocess.
func (c *client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()

		return nil
	}

	c.closed = true
	proc := c.transport
	c.mu.Unlock()

	// Tear down all live sessions first so their updates channels close.
	c.sessionsMu.Lock()

	ids := make([]string, 0, len(c.sessions))

	for _, s := range c.sessions {
		ids = append(ids, s.ID())
		s.close()
	}

	c.sessions = map[acp.SessionId]*session{}

	c.sessionsMu.Unlock()

	for _, id := range ids {
		c.fireHookSessionEnd(context.Background(), id)
	}

	if c.subprocessSpan != nil {
		c.subprocessSpan.End()
		c.subprocessSpan = nil
	}

	// Shut down the subprocess BEFORE the MCP bridge. opencode holds a
	// long-lived streamable-HTTP connection to the bridge, and
	// http.Server.Shutdown blocks until all active connections close —
	// so closing the bridge first stalls for the full 5s timeout waiting
	// on opencode's still-open socket. Killing the subprocess first
	// drops that connection, letting Shutdown return immediately.
	var procErr error

	if proc != nil {
		procErr = proc.Close()
	}

	if c.bridge != nil {
		if err := c.bridge.Close(context.Background()); err != nil {
			c.opts.logger.Warn("mcp bridge close failed", slog.Any("error", err))
		}
	}

	return procErr
}

// Capabilities returns the agent capabilities negotiated during Start.
func (c *client) Capabilities() acp.AgentCapabilities {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.agentCaps
}

// AgentInfo returns the agent identity reported during Start.
func (c *client) AgentInfo() acp.Implementation {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.agentInfo
}

// GetTransportHealth returns the current transport-health snapshot.
func (c *client) GetTransportHealth() TransportHealth {
	return c.health.get()
}

// BudgetTracker returns the budget tracker configured via
// WithMaxBudgetUSD or WithBudgetTracker. Returns nil when unset.
func (c *client) BudgetTracker() *BudgetTracker {
	return c.opts.budgetTracker
}

// CancelAll fans session/cancel notifications out across every live
// session on the Client. See Client.CancelAll for semantics.
func (c *client) CancelAll(ctx context.Context) error {
	c.sessionsMu.RLock()
	targets := make([]*session, 0, len(c.sessions))

	for _, s := range c.sessions {
		targets = append(targets, s)
	}

	c.sessionsMu.RUnlock()

	if len(targets) == 0 {
		return nil
	}

	errs := make([]error, 0, len(targets))

	for _, s := range targets {
		if err := s.Cancel(ctx); err != nil {
			errs = append(errs, fmt.Errorf("session %s: %w", s.ID(), err))
		}
	}

	return errors.Join(errs...)
}

// AuthMethods returns the authentication methods the agent advertised
// during Start.
func (c *client) AuthMethods() []acp.AuthMethod {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Return a copy to prevent caller mutation of our internal state.
	out := make([]acp.AuthMethod, len(c.authMethods))
	copy(out, c.authMethods)

	return out
}

// NewSession creates a new opencode session.
func (c *client) NewSession(ctx context.Context, opts ...Option) (Session, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}

	merged := c.mergeOptions(opts)

	if err := c.checkMCPCapabilities(merged.mcpServers); err != nil {
		return nil, err
	}

	req := acp.NewSessionRequest{
		Cwd:                   cwdOrEmpty(merged),
		McpServers:            merged.mcpServers,
		AdditionalDirectories: c.resolveAdditionalDirs(ctx, merged),
		Meta:                  sessionNewMeta(merged),
	}

	resp, err := c.transport.Conn().NewSession(ctx, req)
	if err != nil {
		if retryErr := c.maybeRelaunchLoginAndRetry(ctx, merged, err); retryErr == nil {
			resp, err = c.transport.Conn().NewSession(ctx, req)
		}
	}

	if err != nil {
		return nil, wrapACPErrCtx(ctx, err)
	}

	s := newSession(c, resp.SessionId, resp.Models, resp.Modes, resp.ConfigOptions, resp.Meta, merged.updatesBuffer)

	if err := c.applySessionConfig(ctx, s, merged); err != nil {
		c.teardownSession(s)

		return nil, err
	}

	c.attachBudgetTracker(s)
	c.fireHookSessionStart(ctx, s.ID())

	return s, nil
}

// LoadSession rehydrates a session by ID.
func (c *client) LoadSession(ctx context.Context, id string, opts ...Option) (Session, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}

	merged := c.mergeOptions(opts)

	if err := c.checkMCPCapabilities(merged.mcpServers); err != nil {
		return nil, err
	}

	sessionID := acp.SessionId(id)

	// Register a placeholder session BEFORE the RPC so that replayed
	// session/update notifications are delivered into its updates channel.
	s := newSession(c, sessionID, nil, nil, nil, nil, merged.updatesBuffer)

	loadReq := acp.LoadSessionRequest{
		SessionId:             sessionID,
		Cwd:                   cwdOrEmpty(merged),
		McpServers:            merged.mcpServers,
		AdditionalDirectories: c.resolveAdditionalDirs(ctx, merged),
		Meta:                  sessionNewMeta(merged),
	}

	resp, err := c.transport.Conn().LoadSession(ctx, loadReq)
	if err != nil {
		if retryErr := c.maybeRelaunchLoginAndRetry(ctx, merged, err); retryErr == nil {
			resp, err = c.transport.Conn().LoadSession(ctx, loadReq)
		}
	}

	if err != nil {
		c.teardownSession(s)

		return nil, wrapACPErrCtx(ctx, err)
	}

	s.initialModels = resp.Models
	s.initialModes = resp.Modes
	s.initialOptions = resp.ConfigOptions
	s.meta = resp.Meta

	if err := c.applySessionConfig(ctx, s, merged); err != nil {
		c.teardownSession(s)

		return nil, err
	}

	c.attachBudgetTracker(s)
	c.fireHookSessionStart(ctx, s.ID())

	return s, nil
}

// ListSessions enumerates sessions scoped to the configured cwd.
//
// Cursor is the opaque pagination token echoed by the agent. opencode
// 1.14.20 returns sessions in pages of 100 keyed by stringified page
// number ("0", "1", "2", …); empty string requests the first page.
// When opencode returns no NextCursor (last page), this method returns
// "" — callers driving a manual pagination loop should treat that as
// end-of-results. Prefer IterSessions for transparent pagination.
func (c *client) ListSessions(ctx context.Context, cursor string) ([]SessionInfo, string, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, "", err
	}

	req := acp.ListSessionsRequest{
		Cwd: acpStringPtr(c.opts.cwd),
	}

	if cursor != "" {
		req.Cursor = &cursor
	}

	resp, err := c.transport.Conn().ListSessions(ctx, req)
	if err != nil {
		return nil, "", wrapACPErr(err)
	}

	nextCursor := ""
	if resp.NextCursor != nil {
		nextCursor = *resp.NextCursor
	}

	return resp.Sessions, nextCursor, nil
}

// applySessionConfig applies model/mode/effort options via
// set_config_option (model + mode) and the variant-resolver path
// (effort).
func (c *client) applySessionConfig(ctx context.Context, s *session, o *options) error {
	if o.model != "" {
		if err := s.SetModel(ctx, o.model); err != nil {
			return fmt.Errorf("applying WithModel(%q): %w", o.model, err)
		}
	}

	if o.agent != "" {
		if err := s.SetMode(ctx, o.agent); err != nil {
			return fmt.Errorf("applying WithAgent(%q): %w", o.agent, err)
		}
	}

	if o.effort != "" {
		modelID := o.model
		if modelID == "" {
			s.mu.Lock()
			modelID = s.currentModel
			s.mu.Unlock()
		}

		if modelID == "" {
			s.logger.Debug("WithEffort no-op: no model id available to probe variants",
				slog.String("effort", string(o.effort)),
			)
		} else if err := c.applyEffortOnSession(ctx, s, o.effort, modelID); err != nil {
			return fmt.Errorf("applying WithEffort(%q): %w", o.effort, err)
		}
	}

	if o.maxTurns > 0 {
		attachMaxTurns(s, o.maxTurns)
	}

	return nil
}

// mergeOptions produces a merged options set: Client-level defaults +
// per-call overrides. All mutable collections on the result are freshly
// allocated so per-call overrides cannot corrupt the Client-level
// options. The caller-visible mcpServers list is extended with the
// bridge entry (if any) so every session/new/load gets the in-process
// tools.
func (c *client) mergeOptions(override []Option) *options {
	merged := *c.opts

	// mcpServers must be a non-nil slice: opencode 1.14.20 rejects
	// session/new when the JSON payload serializes the field as null
	// (error -32602 "Invalid params"; zod: "expected array, received
	// null"). Starting from an empty slice also ensures the bridge
	// prepend path always sees a slice it can extend.
	merged.mcpServers = append([]acp.McpServer{}, c.opts.mcpServers...)
	merged.cliFlags = append([]string(nil), c.opts.cliFlags...)
	merged.sdkTools = append([]Tool(nil), c.opts.sdkTools...)
	merged.additionalDirectories = append([]string(nil), c.opts.additionalDirectories...)

	if c.opts.env != nil {
		merged.env = make(map[string]string, len(c.opts.env))
		maps.Copy(merged.env, c.opts.env)
	}

	for _, opt := range override {
		opt(&merged)
	}

	if c.bridge != nil {
		merged.mcpServers = append([]acp.McpServer{c.bridgeMcpServerEntry()}, merged.mcpServers...)
	}

	return &merged
}

// bridgeMcpServerEntry builds the McpServer union entry that points
// opencode at our loopback MCP server, with the bearer token in the
// Authorization header.
func (c *client) bridgeMcpServerEntry() acp.McpServer {
	return acp.McpServer{
		Http: &acp.McpServerHttpInline{
			Type: "http",
			Name: bridgeMcpName,
			Url:  c.bridge.URL(),
			Headers: []acp.HttpHeader{
				{Name: "Authorization", Value: "Bearer " + c.bridge.Token()},
			},
		},
	}
}

// toolsToBridgeDefs maps opencodesdk.Tool instances to the bridge-
// internal ToolDef shape used by internal/mcp/bridge.
func toolsToBridgeDefs(tools []Tool) []bridge.ToolDef {
	out := make([]bridge.ToolDef, 0, len(tools))

	for _, t := range tools {
		tool := t // capture
		out = append(out, bridge.ToolDef{
			Name:        tool.Name(),
			Description: tool.Description(),
			Schema:      tool.InputSchema(),
			Annotations: toolAnnotationsFor(tool),
			Handler: func(ctx context.Context, in map[string]any) (*bridge.ToolOutput, error) {
				res, err := tool.Execute(ctx, in)
				if err != nil {
					return nil, err
				}

				return &bridge.ToolOutput{
					Text:       res.Text,
					Structured: res.Structured,
					IsError:    res.IsError,
				}, nil
			},
		})
	}

	return out
}

// toolAnnotationsFor pulls ToolAnnotations off a Tool (when it opts
// in via the unexported annotatedTool interface) and translates them
// into the mcp-go-sdk ToolAnnotations shape the bridge forwards
// on-wire. Returns nil when no annotations were configured.
func toolAnnotationsFor(t Tool) *mcp.ToolAnnotations {
	at, ok := t.(annotatedTool)
	if !ok {
		return nil
	}

	ann := at.toolAnnotations()
	if ann == nil {
		return nil
	}

	out := &mcp.ToolAnnotations{
		Title:           ann.Title,
		ReadOnlyHint:    ann.ReadOnlyHint,
		DestructiveHint: ann.DestructiveHint,
		IdempotentHint:  ann.IdempotentHint,
		OpenWorldHint:   ann.OpenWorldHint,
	}

	return out
}

func (c *client) ensureStarted() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		// watchSubprocess closes the client AND stashes a
		// TransportError when the subprocess dies mid-session. Surface
		// that typed error in preference to the generic closed
		// sentinel so callers can errors.As it to see the cause.
		if c.transportErr != nil {
			return c.transportErr
		}

		return ErrClientClosed
	}

	if !c.started {
		return ErrClientNotStarted
	}

	return nil
}

// registerSession records s in the client's session map so the
// dispatcher can route session/update notifications to it. Any
// notifications that arrived before registration are flushed to s
// before this returns so ordering is preserved.
func (c *client) registerSession(s *session) {
	c.sessionsMu.Lock()
	c.sessions[s.id] = s
	pending := c.pendingUpdates[s.id]
	delete(c.pendingUpdates, s.id)
	c.sessionsMu.Unlock()

	for _, n := range pending {
		s.deliver(n)
	}
}

// deregisterSession removes a session from the routing map and drops
// any pending-updates buffer for it.
func (c *client) deregisterSession(id acp.SessionId) {
	c.sessionsMu.Lock()
	delete(c.sessions, id)
	delete(c.pendingUpdates, id)
	c.sessionsMu.Unlock()
}

// lookupSession returns the registered session for id, or nil if no
// session with that id is currently tracked.
func (c *client) lookupSession(id acp.SessionId) *session {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	return c.sessions[id]
}

// teardownSession is the rollback path for session creation: close the
// session's updates channel and remove it from routing. Callers invoke
// this when post-creation work (applySessionConfig) fails and the
// session must not leak. Fires HookEventSessionEnd for observers.
func (c *client) teardownSession(s *session) {
	c.deregisterSession(s.id)
	s.close()
	c.fireHookSessionEnd(context.Background(), s.ID())
}

// attachBudgetTracker subscribes the configured BudgetTracker (if any)
// to s so UsageUpdate notifications feed its accumulated totals. When
// WithMaxBudgetUSD was used, the subscription also calls Session.Cancel
// once the budget is exceeded so the in-flight turn aborts.
func (c *client) attachBudgetTracker(s *session) {
	bt := c.opts.budgetTracker
	if bt == nil {
		return
	}

	autoCancel := c.opts.autoCancelOnBudget
	sessionID := s.ID()

	s.Subscribe(UpdateHandlers{
		Usage: func(ctx context.Context, upd *acp.SessionUsageUpdate) {
			bt.ObserveUsage(sessionID)(ctx, upd)

			if !autoCancel {
				return
			}

			if bt.CheckBudget() == nil {
				return
			}

			if err := s.Cancel(ctx); err != nil {
				s.logger.Debug("budget-exceeded cancel failed", slog.Any("error", err))
			}
		},
	})
}

// routeSessionUpdate is wired as the dispatcher's SessionUpdate
// callback. It looks up the target session and delivers the
// notification to its updates channel. If the session isn't registered
// yet (NewSession/ForkSession race window), the notification is
// buffered in pendingUpdates and flushed when registerSession runs.
func (c *client) routeSessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	c.observer.RecordSessionUpdate(ctx, sessionUpdateVariant(n.Update))

	c.sessionsMu.Lock()

	s, ok := c.sessions[n.SessionId]
	if ok {
		c.sessionsMu.Unlock()
		s.deliver(n)

		return nil
	}

	buf := c.pendingUpdates[n.SessionId]
	if len(buf) >= pendingUpdatesCap {
		c.sessionsMu.Unlock()
		c.opts.logger.Warn("session/update for unregistered session; dropping (buffer full)",
			slog.String("session_id", string(n.SessionId)),
			slog.Int("cap", pendingUpdatesCap),
		)

		return nil
	}

	c.pendingUpdates[n.SessionId] = append(buf, n)
	c.sessionsMu.Unlock()

	return nil
}

// instrumentedPermission wraps cb with observability and hook
// dispatch. The wrapper always returns a non-nil handlers.PermissionCallback
// when hooks are configured, so PermissionRequest/PermissionDenied
// hooks still fire even when the user did not register their own
// PermissionCallback. When cb is nil and no hooks are active, we
// return nil so the dispatcher falls through to its default
// auto-reject path.
func (c *client) instrumentedPermission(cb PermissionCallback) handlers.PermissionCallback {
	if cb == nil && c.hooks == nil {
		return nil
	}

	return func(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		// Pre-hook: give HookEventPermissionRequest a chance to block.
		if c.hooks != nil {
			decision, hookErr := c.hooks.dispatch(ctx, HookEventPermissionRequest, deref(req.ToolCall.Title), HookInput{
				Event:             HookEventPermissionRequest,
				SessionID:         string(req.SessionId),
				PermissionRequest: &req,
			})
			if hookErr != nil || !decision.Continue {
				resp := synthReject(req, decision.Reason)

				c.observer.RecordPermission(ctx, "hook_blocked")
				c.fireHookPermissionDenied(ctx, &req, &resp)

				return resp, hookErr
			}
		}

		var (
			resp acp.RequestPermissionResponse
			err  error
		)

		if cb != nil {
			resp, err = cb(ctx, req)
		} else {
			resp = synthReject(req, "no permission callback configured")
		}

		outcome := "error"

		switch {
		case err != nil:
		case resp.Outcome.Cancelled != nil:
			outcome = "cancelled"
		case resp.Outcome.Selected != nil:
			outcome = "selected:" + string(resp.Outcome.Selected.OptionId)
		}

		c.observer.RecordPermission(ctx, outcome)

		// Post-hook: fire PermissionDenied when the outcome was a reject.
		if err == nil && isRejectOutcome(resp) {
			c.fireHookPermissionDenied(ctx, &req, &resp)
		}

		return resp, err
	}
}

// fireHookSessionStart runs HookEventSessionStart. Notification-only.
func (c *client) fireHookSessionStart(ctx context.Context, sessionID string) {
	if c.hooks == nil {
		return
	}

	_, _ = c.hooks.dispatch(ctx, HookEventSessionStart, sessionID, HookInput{
		Event:     HookEventSessionStart,
		SessionID: sessionID,
	})
}

// fireHookSessionEnd runs HookEventSessionEnd. Notification-only.
func (c *client) fireHookSessionEnd(ctx context.Context, sessionID string) {
	if c.hooks == nil {
		return
	}

	_, _ = c.hooks.dispatch(ctx, HookEventSessionEnd, sessionID, HookInput{
		Event:     HookEventSessionEnd,
		SessionID: sessionID,
	})
}

// fireHookPermissionDenied runs HookEventPermissionDenied. This event
// is notification-only; Continue=false is ignored.
func (c *client) fireHookPermissionDenied(ctx context.Context, req *acp.RequestPermissionRequest, resp *acp.RequestPermissionResponse) {
	if c.hooks == nil {
		return
	}

	_, _ = c.hooks.dispatch(ctx, HookEventPermissionDenied, deref(req.ToolCall.Title), HookInput{
		Event:              HookEventPermissionDenied,
		SessionID:          string(req.SessionId),
		PermissionRequest:  req,
		PermissionResponse: resp,
	})
}

// synthReject produces a RequestPermissionResponse rejecting the
// request. Picks a reject_once option when present, reject_always as
// a fallback, and finally emits a Cancelled outcome when the request
// carried no reject option at all.
func synthReject(req acp.RequestPermissionRequest, _ string) acp.RequestPermissionResponse {
	for _, opt := range req.Options {
		if opt.Kind == acp.PermissionOptionKindRejectOnce {
			return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
				Selected: &acp.RequestPermissionOutcomeSelected{OptionId: opt.OptionId},
			}}
		}
	}

	for _, opt := range req.Options {
		if opt.Kind == acp.PermissionOptionKindRejectAlways {
			return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
				Selected: &acp.RequestPermissionOutcomeSelected{OptionId: opt.OptionId},
			}}
		}
	}

	return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
		Cancelled: &acp.RequestPermissionOutcomeCancelled{},
	}}
}

// isRejectOutcome reports whether resp represents a denial (either a
// reject_* option id or a cancelled outcome).
func isRejectOutcome(resp acp.RequestPermissionResponse) bool {
	switch {
	case resp.Outcome.Cancelled != nil:
		return true
	case resp.Outcome.Selected != nil:
		id := string(resp.Outcome.Selected.OptionId)

		return containsAny(id, "reject", "deny")
	}

	return false
}

// instrumentedFsWrite wraps cb with observability + hook dispatch.
// The wrapper returns a non-nil callback when hooks are registered,
// so HookEventFileChanged still fires even when the user did not
// install their own FsWriteCallback — the default behaviour there is
// to write the file to disk.
func (c *client) instrumentedFsWrite(cb FsWriteCallback) handlers.FsWriteCallback {
	if cb == nil && c.hooks == nil {
		return nil
	}

	return func(ctx context.Context, req acp.WriteTextFileRequest) error {
		if c.hooks != nil {
			decision, hookErr := c.hooks.dispatch(ctx, HookEventFileChanged, req.Path, HookInput{
				Event:     HookEventFileChanged,
				SessionID: string(req.SessionId),
				FileWrite: &req,
			})
			if hookErr != nil {
				c.observer.RecordFsDelegation(ctx, "write", "hook_error")

				return hookErr
			}

			if !decision.Continue {
				c.observer.RecordFsDelegation(ctx, "write", "hook_blocked")

				reason := decision.Reason
				if reason == "" {
					reason = "hook blocked write"
				}

				return errors.New(reason)
			}
		}

		if cb == nil {
			// Hooks configured but no user callback: replicate the
			// dispatcher's default "write to disk" behaviour so the
			// hook event still fires without changing the wire outcome.
			if err := defaultFsWrite(req); err != nil {
				c.observer.RecordFsDelegation(ctx, "write", "error")

				return err
			}

			c.observer.RecordFsDelegation(ctx, "write", "handled")

			return nil
		}

		err := cb(ctx, req)
		if err != nil {
			c.observer.RecordFsDelegation(ctx, "write", "error")

			return err
		}

		c.observer.RecordFsDelegation(ctx, "write", "handled")

		return nil
	}
}

// defaultFsWrite mirrors the handlers package's default fs/write
// behaviour for the hook-fallthrough path. Kept in sync with
// internal/handlers/dispatcher.go.
func defaultFsWrite(req acp.WriteTextFileRequest) error {
	if !filepath.IsAbs(req.Path) {
		return fmt.Errorf("fs/write_text_file: path must be absolute: %q", req.Path)
	}

	dir := filepath.Dir(req.Path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("fs/write_text_file mkdir: %w", err)
		}
	}

	if err := os.WriteFile(req.Path, []byte(req.Content), 0o644); err != nil { //nolint:gosec // intentional: agent-requested write
		return fmt.Errorf("fs/write_text_file: %w", err)
	}

	return nil
}

var (
	errNoLoginLaunch = errors.New("opencodesdk: no terminal-auth launch instructions to execute")
)

// maybeRelaunchLoginAndRetry checks whether err represents authRequired
// and, if WithAutoLaunchLogin is enabled, spawns the opencode-supplied
// terminal-auth command with stdio inherited from the parent process.
// Returns nil on successful relaunch (caller should retry the RPC), or
// a non-nil error if relaunch was not applicable or failed.
func (c *client) maybeRelaunchLoginAndRetry(ctx context.Context, merged *options, rpcErr error) error {
	if !merged.autoLaunchLogin {
		return errNoLoginLaunch
	}

	if !errors.Is(wrapACPErr(rpcErr), ErrAuthRequired) {
		return errNoLoginLaunch
	}

	launch := c.firstTerminalAuthLaunch()
	if launch == nil {
		return errNoLoginLaunch
	}

	c.opts.logger.InfoContext(ctx, "launching opencode auth flow",
		slog.String("command", launch.Command),
		slog.Any("args", launch.Args),
	)

	cmd := exec.CommandContext(ctx, launch.Command, launch.Args...) //nolint:gosec // launched from opencode-supplied _meta["terminal-auth"]
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if len(launch.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range launch.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: terminal-auth command failed: %w", rpcErr, err)
	}

	return nil
}

// firstTerminalAuthLaunch returns the first TerminalAuthLaunch parsed
// from the agent's advertised auth methods, or nil if none was
// advertised.
func (c *client) firstTerminalAuthLaunch() *TerminalAuthLaunch {
	c.mu.Lock()
	methods := append([]acp.AuthMethod(nil), c.authMethods...)
	c.mu.Unlock()

	for _, m := range methods {
		if launch, ok := TerminalAuthInstructions(m); ok {
			return launch
		}
	}

	return nil
}

// sessionUpdateVariant returns the discriminator name of a
// SessionUpdate union, used as the variant label on the session/update
// counter.
func sessionUpdateVariant(u acp.SessionUpdate) string {
	switch {
	case u.UserMessageChunk != nil:
		return "user_message_chunk"
	case u.AgentMessageChunk != nil:
		return "agent_message_chunk"
	case u.AgentThoughtChunk != nil:
		return "agent_thought_chunk"
	case u.ToolCall != nil:
		return "tool_call"
	case u.ToolCallUpdate != nil:
		return "tool_call_update"
	case u.Plan != nil:
		return "plan"
	case u.AvailableCommandsUpdate != nil:
		return "available_commands_update"
	case u.CurrentModeUpdate != nil:
		return "current_mode_update"
	case u.ConfigOptionUpdate != nil:
		return "config_option_update"
	case u.SessionInfoUpdate != nil:
		return "session_info_update"
	case u.UsageUpdate != nil:
		return "usage_update"
	default:
		return "unknown"
	}
}

// cwdOrEmpty returns o.cwd or the empty string. opencode validates
// that cwd is non-empty for NewSessionRequest.
func cwdOrEmpty(o *options) string {
	return o.cwd
}

// resolveAdditionalDirs returns the additionalDirectories slice to send
// with session/new, session/load, session/fork, session/resume. When
// the agent did not advertise SessionCapabilities.AdditionalDirectories
// during initialize, the values are dropped and a warning is logged so
// the caller notices the option was silently ignored.
func (c *client) resolveAdditionalDirs(ctx context.Context, o *options) []string {
	if len(o.additionalDirectories) == 0 {
		return nil
	}

	if c.agentCaps.SessionCapabilities.AdditionalDirectories == nil {
		// opencode does not currently advertise this capability, so
		// every session would log a warning here. Keep the signal at
		// debug until opencode lights it up.
		c.opts.logger.DebugContext(ctx, "WithAddDirs ignored: agent does not advertise additionalDirectories capability",
			slog.Int("count", len(o.additionalDirectories)),
		)

		return nil
	}

	out := make([]string, len(o.additionalDirectories))
	copy(out, o.additionalDirectories)

	return out
}

// sessionNewMeta builds the _meta block sent with session/new. Only
// populated for options that should surface to the agent, currently
// WithOutputSchema.
func sessionNewMeta(o *options) map[string]any {
	if o.outputSchema == nil {
		return nil
	}

	return map[string]any{"structuredOutputSchema": o.outputSchema}
}

// subprocessArgs flattens the configured CLI flags + extra-args map
// into a single argv slice. WithCLIFlags entries appear first (in the
// order they were registered); WithExtraArgs entries follow. Map
// iteration order is randomised — callers that care about ordering
// should use WithCLIFlags instead.
func subprocessArgs(o *options) []string {
	if len(o.cliExtraArgs) == 0 {
		return o.cliFlags
	}

	out := make([]string, 0, len(o.cliFlags)+len(o.cliExtraArgs))
	out = append(out, o.cliFlags...)

	for name, value := range o.cliExtraArgs {
		if value == nil {
			out = append(out, "--"+name)

			continue
		}

		out = append(out, "--"+name+"="+*value)
	}

	return out
}

// subprocessEnv builds the environment overlay for the opencode
// subprocess. WithOpencodeHome (if set) is exported as XDG_DATA_HOME
// so opencode stores sessions and credentials under the caller-
// specified path.
func subprocessEnv(o *options) map[string]string {
	if o.opencodeHome == "" {
		return o.env
	}

	out := make(map[string]string, len(o.env)+1)
	maps.Copy(out, o.env)

	// WithEnv takes precedence; only set XDG_DATA_HOME when the caller
	// hasn't explicitly configured it themselves.
	if _, ok := out["XDG_DATA_HOME"]; !ok {
		out["XDG_DATA_HOME"] = o.opencodeHome
	}

	return out
}

func acpStringPtr(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// deref returns *p or "" when p is nil. Hooks that match against
// optional *string fields go through this to avoid nil panics.
func deref(p *string) string {
	if p == nil {
		return ""
	}

	return *p
}
