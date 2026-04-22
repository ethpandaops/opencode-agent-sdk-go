package opencodesdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/coder/acp-go-sdk"
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
	proc           *subprocess.Process
	dispatcher     *handlers.Dispatcher
	agentCaps      acp.AgentCapabilities
	agentInfo      acp.Implementation
	protocolVer    acp.ProtocolVersion
	authMethods    []acp.AuthMethod
	subprocessSpan trace.Span

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
	return &client{
		opts:           o,
		sessions:       make(map[acp.SessionId]*session),
		pendingUpdates: make(map[acp.SessionId][]acp.SessionNotification),
		observer:       observability.NewObserver(o.meterProvider, o.tracerProvider),
	}, nil
}

// Start spawns the opencode subprocess and runs the ACP initialize
// handshake.
func (c *client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return errors.New("opencodesdk: client has already been started")
	}

	if c.closed {
		return ErrClientClosed
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
			return fmt.Errorf("%w: %v", ErrCLINotFound, err)
		case errors.Is(err, cli.ErrUnsupportedVersion):
			return fmt.Errorf("%w: %v", ErrUnsupportedCLIVersion, err)
		default:
			return fmt.Errorf("discovering opencode CLI: %w", err)
		}
	}

	c.opts.logger.InfoContext(ctx, "opencode CLI discovered",
		slog.String("path", path),
		slog.String("version", version),
	)

	c.dispatcher = &handlers.Dispatcher{
		Logger: c.opts.logger,
		Callbacks: handlers.Callbacks{
			SessionUpdate: c.routeSessionUpdate,
			Permission:    c.instrumentedPermission(c.opts.canUseTool),
			FsWrite:       c.instrumentedFsWrite(c.opts.onFsWrite),
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

	// Subprocess span covers the child's lifetime — ended in Close.
	_, c.subprocessSpan = c.observer.StartSubprocessSpan(ctx, path)

	proc, err := subprocess.Spawn(ctx, subprocess.Config{
		Path:           path,
		Args:           c.opts.cliFlags,
		Env:            c.opts.env,
		Cwd:            c.opts.cwd,
		Logger:         c.opts.logger,
		StderrCallback: c.opts.stderr,
	}, c.dispatcher)
	if err != nil {
		c.observer.RecordCLISpawn(ctx, "error")
		c.subprocessSpan.End()
		c.subprocessSpan = nil

		return fmt.Errorf("spawning opencode acp: %w", err)
	}

	c.observer.RecordCLISpawn(ctx, "started")
	c.proc = proc

	if err := c.initialize(ctx); err != nil {
		_ = c.proc.Close()
		c.proc = nil

		return fmt.Errorf("ACP initialize: %w", err)
	}

	c.started = true

	c.watchOnce.Do(func() { go c.watchSubprocess() })

	return nil
}

// watchSubprocess fires when opencode exits unexpectedly mid-session.
// It logs the cause, closes every live session (so callers blocked on
// Updates() or a Prompt unblock), and leaves the client in the closed
// state so subsequent RPCs return ErrClientClosed instead of hanging.
func (c *client) watchSubprocess() {
	proc := c.proc
	if proc == nil {
		return
	}

	<-proc.Exited()

	err := proc.ExitErr()

	c.mu.Lock()
	closing := c.closed
	c.mu.Unlock()

	if closing {
		return
	}

	if err != nil {
		c.opts.logger.Error("opencode acp exited unexpectedly", slog.Any("error", err))
	} else {
		c.opts.logger.Warn("opencode acp exited unexpectedly (no error reported)")
	}

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

	resp, err := c.proc.Conn().Initialize(spanCtx, acp.InitializeRequest{
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
	proc := c.proc
	c.mu.Unlock()

	// Tear down all live sessions first so their updates channels close.
	c.sessionsMu.Lock()

	for _, s := range c.sessions {
		s.close()
	}

	c.sessions = map[acp.SessionId]*session{}

	c.sessionsMu.Unlock()

	if c.bridge != nil {
		if err := c.bridge.Close(context.Background()); err != nil {
			c.opts.logger.Warn("mcp bridge close failed", slog.Any("error", err))
		}
	}

	if c.subprocessSpan != nil {
		c.subprocessSpan.End()
		c.subprocessSpan = nil
	}

	if proc == nil {
		return nil
	}

	return proc.Close()
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

	req := acp.NewSessionRequest{
		Cwd:        cwdOrEmpty(merged),
		McpServers: merged.mcpServers,
	}

	resp, err := c.proc.Conn().NewSession(ctx, req)
	if err != nil {
		if retryErr := c.maybeRelaunchLoginAndRetry(ctx, merged, err); retryErr == nil {
			resp, err = c.proc.Conn().NewSession(ctx, req)
		}
	}

	if err != nil {
		return nil, wrapACPErr(err)
	}

	s := newSession(c, resp.SessionId, resp.Models, resp.Modes, resp.ConfigOptions, resp.Meta, merged.updatesBuffer)

	if err := c.applySessionConfig(ctx, s, merged); err != nil {
		c.teardownSession(s)

		return nil, err
	}

	return s, nil
}

// LoadSession rehydrates a session by ID.
func (c *client) LoadSession(ctx context.Context, id string, opts ...Option) (Session, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}

	merged := c.mergeOptions(opts)

	sessionID := acp.SessionId(id)

	// Register a placeholder session BEFORE the RPC so that replayed
	// session/update notifications are delivered into its updates channel.
	s := newSession(c, sessionID, nil, nil, nil, nil, merged.updatesBuffer)

	loadReq := acp.LoadSessionRequest{
		SessionId:  sessionID,
		Cwd:        cwdOrEmpty(merged),
		McpServers: merged.mcpServers,
	}

	resp, err := c.proc.Conn().LoadSession(ctx, loadReq)
	if err != nil {
		if retryErr := c.maybeRelaunchLoginAndRetry(ctx, merged, err); retryErr == nil {
			resp, err = c.proc.Conn().LoadSession(ctx, loadReq)
		}
	}

	if err != nil {
		c.teardownSession(s)

		return nil, wrapACPErr(err)
	}

	s.initialModels = resp.Models
	s.initialModes = resp.Modes
	s.initialOptions = resp.ConfigOptions
	s.meta = resp.Meta

	if err := c.applySessionConfig(ctx, s, merged); err != nil {
		c.teardownSession(s)

		return nil, err
	}

	return s, nil
}

// ListSessions enumerates sessions scoped to the configured cwd.
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

	resp, err := c.proc.Conn().ListSessions(ctx, req)
	if err != nil {
		return nil, "", wrapACPErr(err)
	}

	nextCursor := ""
	if resp.NextCursor != nil {
		nextCursor = *resp.NextCursor
	}

	return resp.Sessions, nextCursor, nil
}

// applySessionConfig applies model/mode options via set_config_option.
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

func (c *client) ensureStarted() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
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

// teardownSession is the rollback path for session creation: close the
// session's updates channel and remove it from routing. Callers invoke
// this when post-creation work (applySessionConfig) fails and the
// session must not leak.
func (c *client) teardownSession(s *session) {
	c.deregisterSession(s.id)
	s.close()
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

// instrumentedPermission wraps cb with observability. Returns nil when
// cb is nil so the dispatcher falls through to its default auto-reject
// path (which picks a "reject" option from the request instead of
// surfacing a JSON-RPC internal error to the agent).
func (c *client) instrumentedPermission(cb PermissionCallback) handlers.PermissionCallback {
	if cb == nil {
		return nil
	}

	return func(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		resp, err := cb(ctx, req)

		outcome := "error"

		switch {
		case err != nil:
		case resp.Outcome.Cancelled != nil:
			outcome = "cancelled"
		case resp.Outcome.Selected != nil:
			outcome = "selected:" + string(resp.Outcome.Selected.OptionId)
		}

		c.observer.RecordPermission(ctx, outcome)

		return resp, err
	}
}

// instrumentedFsWrite wraps cb with observability. Returns nil when cb
// is nil so the dispatcher's default "write to disk" path runs.
func (c *client) instrumentedFsWrite(cb FsWriteCallback) handlers.FsWriteCallback {
	if cb == nil {
		return nil
	}

	return func(ctx context.Context, req acp.WriteTextFileRequest) error {
		err := cb(ctx, req)
		if err != nil {
			c.observer.RecordFsDelegation(ctx, "write", "error")

			return err
		}

		c.observer.RecordFsDelegation(ctx, "write", "handled")

		return nil
	}
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

func acpStringPtr(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
