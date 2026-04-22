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
}

func newClient(o *options) (*client, error) {
	return &client{opts: o}, nil
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
		// Callbacks populated in later milestones.
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
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true

	if c.proc == nil {
		return nil
	}

	return c.proc.Close()
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
