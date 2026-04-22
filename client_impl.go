package opencodesdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/coder/acp-go-sdk"

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

	mu          sync.Mutex
	started     bool
	closed      bool
	proc        *subprocess.Process
	dispatcher  *handlers.Dispatcher
	agentCaps   acp.AgentCapabilities
	agentInfo   acp.Implementation
	protocolVer acp.ProtocolVersion
	authMethods []acp.AuthMethod

	sessionsMu sync.RWMutex
	sessions   map[acp.SessionId]*session

	bridge *bridge.Bridge

	observer *observability.Observer
}

func newClient(o *options) (*client, error) {
	return &client{
		opts:     o,
		sessions: make(map[acp.SessionId]*session),
		observer: observability.NewObserver(o.meterProvider, o.tracerProvider),
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
	}

	if len(c.opts.sdkTools) > 0 {
		br, brErr := bridge.New(toolsToBridgeDefs(c.opts.sdkTools), c.opts.logger)
		if brErr != nil {
			return fmt.Errorf("creating mcp bridge: %w", brErr)
		}

		if brErr := br.Start(ctx); brErr != nil {
			return fmt.Errorf("starting mcp bridge: %w", brErr)
		}

		c.bridge = br
	}

	proc, err := subprocess.Spawn(ctx, subprocess.Config{
		Path:           path,
		Args:           c.opts.cliFlags,
		Env:            c.opts.env,
		Cwd:            c.opts.cwd,
		Logger:         c.opts.logger,
		StderrCallback: c.opts.stderr,
	}, c.dispatcher)
	if err != nil {
		return fmt.Errorf("spawning opencode acp: %w", err)
	}

	c.proc = proc

	if err := c.initialize(ctx); err != nil {
		_ = c.proc.Close()
		c.proc = nil

		return fmt.Errorf("ACP initialize: %w", err)
	}

	c.started = true

	return nil
}

func (c *client) initialize(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, c.opts.initializeTimeout)
	defer cancel()

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

	resp, err := c.proc.Conn().Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: caps,
	})
	if err != nil {
		return wrapACPErr(err)
	}

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
		return nil, wrapACPErr(err)
	}

	s := newSession(c, resp.SessionId, resp.Models, resp.Modes, resp.ConfigOptions, resp.Meta, merged.updatesBuffer)

	if err := c.applySessionConfig(ctx, s, merged); err != nil {
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

	resp, err := c.proc.Conn().LoadSession(ctx, acp.LoadSessionRequest{
		SessionId:  sessionID,
		Cwd:        cwdOrEmpty(merged),
		McpServers: merged.mcpServers,
	})
	if err != nil {
		c.deregisterSession(sessionID)
		s.close()

		return nil, wrapACPErr(err)
	}

	s.initialModels = resp.Models
	s.initialModes = resp.Modes
	s.initialOptions = resp.ConfigOptions
	s.meta = resp.Meta

	if err := c.applySessionConfig(ctx, s, merged); err != nil {
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
// per-call overrides. The caller-visible mcpServers list is extended
// with the bridge entry (if any) so every session/new/load gets the
// in-process tools.
func (c *client) mergeOptions(override []Option) *options {
	merged := *c.opts
	merged.mcpServers = append([]acp.McpServer{}, c.opts.mcpServers...)

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
// dispatcher can route session/update notifications to it.
func (c *client) registerSession(s *session) {
	c.sessionsMu.Lock()
	c.sessions[s.id] = s
	c.sessionsMu.Unlock()
}

// deregisterSession removes a session from the routing map.
func (c *client) deregisterSession(id acp.SessionId) {
	c.sessionsMu.Lock()
	delete(c.sessions, id)
	c.sessionsMu.Unlock()
}

// routeSessionUpdate is wired as the dispatcher's SessionUpdate
// callback. It looks up the target session and delivers the
// notification to its updates channel, or logs and drops if the
// session is unknown.
func (c *client) routeSessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	c.observer.RecordSessionUpdate(ctx, sessionUpdateVariant(n.Update))

	c.sessionsMu.RLock()
	s, ok := c.sessions[n.SessionId]
	c.sessionsMu.RUnlock()

	if !ok {
		c.opts.logger.Debug("session/update for unknown session; dropping",
			slog.String("session_id", string(n.SessionId)),
		)

		return nil
	}

	s.deliver(n)

	return nil
}

// instrumentedPermission wraps a PermissionCallback with observability
// recording. If cb is nil the wrapper is also nil — the dispatcher's
// default auto-reject path still runs, and we record that too.
func (c *client) instrumentedPermission(cb PermissionCallback) handlers.PermissionCallback {
	return func(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		if cb == nil {
			c.observer.RecordPermission(ctx, "auto_reject")

			return acp.RequestPermissionResponse{}, errNoPermissionCallback
		}

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

// instrumentedFsWrite wraps an FsWriteCallback with observability.
func (c *client) instrumentedFsWrite(cb FsWriteCallback) handlers.FsWriteCallback {
	return func(ctx context.Context, req acp.WriteTextFileRequest) error {
		outcome := "default_write"
		if cb != nil {
			outcome = "handled"
		}

		err := func() error {
			if cb == nil {
				return errFsDefaultPath
			}

			return cb(ctx, req)
		}()
		if err != nil {
			if errors.Is(err, errFsDefaultPath) {
				c.observer.RecordFsDelegation(ctx, "write", outcome)

				return nil // signal dispatcher to fall through to its default path
			}

			c.observer.RecordFsDelegation(ctx, "write", "error")

			return err
		}

		c.observer.RecordFsDelegation(ctx, "write", outcome)

		return nil
	}
}

var (
	errNoPermissionCallback = errors.New("opencodesdk: no permission callback configured")
	errFsDefaultPath        = errors.New("opencodesdk: fall through to default fs write")
)

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
