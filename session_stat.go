package opencodesdk

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
)

// IterSessions returns an iterator that walks every session the agent
// exposes in the configured cwd, transparently paginating through the
// cursor opencode returns. The iterator stops at the first error; the
// caller is responsible for draining or breaking the loop.
//
// Example:
//
//	for info, err := range client.IterSessions(ctx) {
//	    if err != nil {
//	        return err
//	    }
//	    fmt.Println(info.SessionId, info.Title)
//	}
func (c *client) IterSessions(ctx context.Context) iter.Seq2[SessionInfo, error] {
	return func(yield func(SessionInfo, error) bool) {
		cursor := ""

		for {
			if err := ctx.Err(); err != nil {
				yield(SessionInfo{}, err)

				return
			}

			sessions, next, err := c.ListSessions(ctx, cursor)
			if err != nil {
				yield(SessionInfo{}, err)

				return
			}

			for _, s := range sessions {
				if !yield(s, nil) {
					return
				}
			}

			if next == "" {
				return
			}

			cursor = next
		}
	}
}

// CallExtension issues a raw ACP extension JSON-RPC call. See
// Client.CallExtension for the contract.
func (c *client) CallExtension(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}

	if !strings.HasPrefix(method, "_") {
		return nil, fmt.Errorf("%w: %q", ErrExtensionMethodRequired, method)
	}

	raw, err := c.transport.Conn().CallExtension(ctx, method, params)
	if err != nil {
		return nil, wrapACPErr(err)
	}

	return raw, nil
}
