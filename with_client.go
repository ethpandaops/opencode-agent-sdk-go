package opencodesdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// WithClient manages a [Client]'s lifecycle around a caller-supplied
// function. It constructs the client with the given options, runs
// [Client.Start], invokes fn with the ready client, and guarantees
// [Client.Close] on return regardless of how fn exits.
//
// fn's error is returned verbatim. If Close itself fails, the error is
// logged via the client's logger but does not override fn's result.
// If fn is nil, WithClient returns an error immediately without starting
// a client.
func WithClient(ctx context.Context, fn func(Client) error, opts ...Option) error {
	if fn == nil {
		return errors.New("opencodesdk: WithClient fn is nil")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	c, err := NewClient(opts...)
	if err != nil {
		return fmt.Errorf("opencodesdk: NewClient: %w", err)
	}

	defer func() {
		if closeErr := c.Close(); closeErr != nil {
			// Best-effort: surface the close error on the underlying
			// logger but do not override fn's return value — callers care
			// primarily about their own work's outcome.
			if impl, ok := c.(*client); ok && impl.opts.logger != nil {
				impl.opts.logger.Warn("opencodesdk: Client.Close failed",
					slog.Any("error", closeErr),
				)
			}
		}
	}()

	if err := c.Start(ctx); err != nil {
		return fmt.Errorf("opencodesdk: Client.Start: %w", err)
	}

	return fn(c)
}
