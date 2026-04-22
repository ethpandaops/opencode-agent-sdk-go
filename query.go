package opencodesdk

import (
	"context"
	"fmt"
	"iter"
	"time"

	"github.com/coder/acp-go-sdk"
)

// trailingUpdatesGrace caps how long runTurn waits after Session.Prompt
// returns for any final session/update notifications still in flight.
// opencode typically delivers them synchronously, but JSON-RPC gives no
// strict ordering guarantee between the prompt response and the last
// notification, so a short grace window avoids truncating the final
// agent_message_chunk.
const trailingUpdatesGrace = 100 * time.Millisecond

// QueryResult is the aggregated result of a single prompt turn. It is
// what Query returns and what QueryStream yields per input.
type QueryResult struct {
	// SessionID is the opencode session the prompt ran against.
	SessionID string

	// AssistantText is the concatenated text of every
	// agent_message_chunk emitted during the turn. Tool-call output,
	// thoughts, and structured content are not included here; consult
	// Notifications for the raw stream.
	AssistantText string

	// StopReason is the final stop reason reported by opencode
	// (typically "end_turn").
	StopReason acp.StopReason

	// Usage is the token accounting for the turn, if the provider
	// reported it.
	Usage *acp.Usage

	// Notifications is the ordered list of session/update notifications
	// received during the turn. Useful for callers that want to
	// introspect tool calls, thoughts, or plan updates after the fact.
	Notifications []acp.SessionNotification
}

// Query is a one-shot convenience entry point. It spawns opencode,
// creates a new session, runs a single prompt to completion, and tears
// the subprocess down. The full set of [Option]s is honored, including
// [WithSDKTools], [WithCanUseTool], and [WithOnFsWrite]. For longer-
// lived interactions use [NewClient] or [WithClient].
func Query(ctx context.Context, prompt string, opts ...Option) (*QueryResult, error) {
	var result *QueryResult

	err := WithClient(ctx, func(c Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		res, err := runTurn(ctx, sess, acp.TextBlock(prompt))
		if err != nil {
			return err
		}

		result = res

		return nil
	}, opts...)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// QueryStream runs a series of prompts against a single opencode session
// and yields one QueryResult per prompt in input order. The yielded
// sequence short-circuits on the first error; callers must drain or
// explicitly break out of the iterator.
//
// The session, subprocess, and any registered SDK tools are torn down
// when the iterator goroutine returns (either naturally or via break).
// Cancelling ctx is the supported way to interrupt mid-stream.
func QueryStream(ctx context.Context, prompts []string, opts ...Option) iter.Seq2[*QueryResult, error] {
	return func(yield func(*QueryResult, error) bool) {
		c, err := NewClient(opts...)
		if err != nil {
			yield(nil, err)

			return
		}

		defer func() { _ = c.Close() }()

		if startErr := c.Start(ctx); startErr != nil {
			yield(nil, fmt.Errorf("opencodesdk: Client.Start: %w", startErr))

			return
		}

		sess, err := c.NewSession(ctx)
		if err != nil {
			yield(nil, fmt.Errorf("opencodesdk: NewSession: %w", err))

			return
		}

		for _, p := range prompts {
			if err := ctx.Err(); err != nil {
				yield(nil, err)

				return
			}

			res, err := runTurn(ctx, sess, acp.TextBlock(p))
			if !yield(res, err) {
				return
			}

			if err != nil {
				return
			}
		}
	}
}

// runTurn executes a single prompt, drains the session's updates
// channel until the prompt completes, and aggregates a QueryResult.
//
// The drain goroutine must exit per-turn so this function works for both
// single-shot Query (one turn then session teardown) and QueryStream
// (many turns on a persistent session). We can't rely on the updates
// channel closing, because opencode keeps it open for the session's
// lifetime. Instead we signal the goroutine via a stop channel after
// Prompt returns, giving trailing notifications a brief grace window to
// land first.
func runTurn(ctx context.Context, sess Session, blocks ...acp.ContentBlock) (*QueryResult, error) {
	updates := sess.Updates()

	stop := make(chan struct{})
	done := make(chan struct{})

	var (
		notifications []acp.SessionNotification
		assistantText []byte
	)

	capture := func(n acp.SessionNotification) {
		notifications = append(notifications, n)

		if n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.Content.Text != nil {
			assistantText = append(assistantText, n.Update.AgentMessageChunk.Content.Text.Text...)
		}
	}

	go func() {
		defer close(done)

		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				// Drain anything already queued without blocking, then exit.
				for {
					select {
					case n, ok := <-updates:
						if !ok {
							return
						}

						capture(n)
					default:
						return
					}
				}
			case n, ok := <-updates:
				if !ok {
					return
				}

				capture(n)
			}
		}
	}()

	res, promptErr := sess.Prompt(ctx, blocks...)

	// Grace window for any trailing session/update notifications still
	// in flight from opencode. Short-circuits if ctx is already done.
	grace := time.NewTimer(trailingUpdatesGrace)
	select {
	case <-grace.C:
	case <-ctx.Done():
		grace.Stop()
	}

	close(stop)
	<-done

	if promptErr != nil {
		return nil, promptErr
	}

	return &QueryResult{
		SessionID:     sess.ID(),
		AssistantText: string(assistantText),
		StopReason:    res.StopReason,
		Usage:         res.Usage,
		Notifications: notifications,
	}, nil
}
