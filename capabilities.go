package opencodesdk

import (
	"context"
	"fmt"

	"github.com/coder/acp-go-sdk"
)

// checkPromptCapabilities verifies that every content block in blocks
// is permitted under the agent's advertised PromptCapabilities. Returns
// ErrCapabilityUnavailable wrapped with the offending block kind when
// the agent declined support.
//
// This is a client-side gate: opencode would reject unsupported blocks
// at the protocol layer, but surfacing the error locally gives callers
// a typed error, zero subprocess round-trip, and doesn't taint the
// session's error counters.
func (c *client) checkPromptCapabilities(blocks []acp.ContentBlock) error {
	caps := c.agentCaps.PromptCapabilities

	for _, b := range blocks {
		switch {
		case b.Image != nil && !caps.Image:
			return fmt.Errorf("%w: image", ErrCapabilityUnavailable)
		case b.Audio != nil && !caps.Audio:
			return fmt.Errorf("%w: audio", ErrCapabilityUnavailable)
		case b.Resource != nil && !caps.EmbeddedContext:
			return fmt.Errorf("%w: embedded resource (embeddedContext)", ErrCapabilityUnavailable)
		}
	}

	return nil
}

// checkMCPCapabilities verifies that every McpServer entry in servers
// is permitted under the agent's advertised McpCapabilities. Returns
// ErrCapabilityUnavailable wrapped with the offending transport + name
// when the agent declined support.
//
// Stdio entries are always permitted: the ACP spec requires agents to
// support stdio baseline regardless of whether they advertise it via
// the mcpCapabilities object (opencode 1.14.20 omits it from the
// advertisement but accepts stdio entries on session/new).
//
// This is a client-side gate: without it, opencode would either reject
// the session/new with a low-level schema error or silently ignore the
// entry depending on the advertising agent. Surfacing the mismatch
// locally gives callers a typed error and a clear transport label.
func (c *client) checkMCPCapabilities(servers []acp.McpServer) error {
	caps := c.agentCaps.McpCapabilities

	for _, s := range servers {
		switch {
		case s.Http != nil && !caps.Http:
			if c.observer != nil {
				c.observer.RecordMCPCapabilityReject(context.Background(), "http")
			}

			return fmt.Errorf("%w: mcp server %q requires http transport", ErrCapabilityUnavailable, s.Http.Name)
		case s.Sse != nil && !caps.Sse:
			if c.observer != nil {
				c.observer.RecordMCPCapabilityReject(context.Background(), "sse")
			}

			return fmt.Errorf("%w: mcp server %q requires sse transport", ErrCapabilityUnavailable, s.Sse.Name)
		}
	}

	return nil
}
