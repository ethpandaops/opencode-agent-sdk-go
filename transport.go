package opencodesdk

import (
	"context"

	"github.com/coder/acp-go-sdk"
)

// Transport is the minimal abstraction the Client uses to talk to an
// opencode (or ACP-compatible) agent. The default Client uses a
// subprocess-backed implementation that spawns `opencode acp`;
// alternative transports can be supplied via [WithTransport] for
// testing or embedded integrations.
//
// Implementations must be safe for concurrent use: the SDK issues RPCs
// (session/new, session/prompt, etc.) from its own goroutines while
// the transport delivers agent-initiated notifications back to the
// handler supplied via [TransportFactory].
type Transport interface {
	// Conn returns the ACP client-side connection. Non-nil once the
	// transport is ready to carry traffic. The SDK invokes Initialize
	// itself immediately after the factory returns, then drives the
	// rest of the lifecycle through this connection.
	Conn() *acp.ClientSideConnection

	// Close terminates the transport and releases all resources.
	// Idempotent.
	Close() error
}

// WatchableTransport is an optional extension to [Transport]. Transports
// that wrap a process, TCP connection, or similar resource can
// implement it to let the SDK detect unexpected death (and close live
// sessions accordingly). Subprocess-backed transports implement this.
type WatchableTransport interface {
	Transport

	// Exited is closed when the transport has died. It is safe to call
	// before or after the transport has died.
	Exited() <-chan struct{}

	// ExitErr returns the exit error observed by the transport, or nil
	// if the transport exited cleanly (or has not yet exited).
	ExitErr() error
}

// TransportFactory constructs a [Transport]. The SDK invokes it once at
// [Client.Start] time (or at one-shot [Query] time) after resolving all
// options.
//
// handler is the SDK-supplied agent-facing dispatcher; implementations
// must wire it as the [acp.Client] on their [acp.ClientSideConnection].
// Handler methods route agent-initiated RPCs (session/update,
// session/request_permission, fs/read_text_file, fs/write_text_file,
// terminal/*) back into the SDK.
//
// The factory owns the full lifecycle of the returned Transport —
// Client.Close invokes Transport.Close, but any construction-time
// resources (e.g. an in-memory pipe, a network dial) are the factory's
// responsibility to clean up on early return.
type TransportFactory func(ctx context.Context, handler acp.Client) (Transport, error)

// WithTransport registers a custom [TransportFactory], bypassing the
// default `opencode acp` subprocess. When set, [Client.Start] skips CLI
// discovery, version checks, subprocess spawn, and stderr wiring; the
// factory is called instead and is expected to return a working
// Transport.
//
// The primary use case is test doubles: wire an [acp.ClientSideConnection]
// against an in-memory [acp.AgentSideConnection] so the SDK can be
// exercised without a live `opencode` binary on the box. See
// `transport_test.go` for an example.
//
// Version check behaviour: callers using WithTransport should also
// supply WithSkipVersionCheck(true) unless their factory arranges its
// own CLI-version story.
func WithTransport(factory TransportFactory) Option {
	return func(o *options) { o.transportFactory = factory }
}
