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
	"github.com/ethpandaops/opencode-agent-sdk-go/internal/subprocess"
)

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
}

func newClient(o *options) (*client, error) {
	return &client{
		opts:     o,
		sessions: make(map[acp.SessionId]*session),
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
			// Permission + FsWrite callbacks land in M4.
		},
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

	resp, err := c.proc.Conn().Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: false,
		},
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
// per-call overrides.
func (c *client) mergeOptions(override []Option) *options {
	merged := *c.opts
	merged.mcpServers = append([]acp.McpServer{}, c.opts.mcpServers...)

	for _, opt := range override {
		opt(&merged)
	}

	return &merged
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
func (c *client) routeSessionUpdate(_ context.Context, n acp.SessionNotification) error {
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
