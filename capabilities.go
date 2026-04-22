package opencodesdk

import (
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
