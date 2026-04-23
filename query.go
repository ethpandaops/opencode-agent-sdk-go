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
//
// For multimodal prompts (text + image + embedded resource) use
// [QueryContent].
func Query(ctx context.Context, prompt string, opts ...Option) (*QueryResult, error) {
	return QueryContent(ctx, []acp.ContentBlock{acp.TextBlock(prompt)}, opts...)
}

// QueryContent is the multimodal variant of [Query]. It accepts a slice
// of content blocks — built via [Text], [Blocks], [TextBlock],
// [ImageBlock], [ImageFileInput], [ResourceBlock], etc. — allowing a
// single one-shot prompt to carry text, images, and embedded resources.
//
// Non-text blocks require the agent to advertise the matching
// [PromptCapabilities] bit (Image, Audio, EmbeddedContext) during the
// ACP initialize handshake. Blocks whose capability isn't advertised
// are rejected up front with [ErrCapabilityUnavailable].
//
// blocks must be non-empty.
func QueryContent(ctx context.Context, blocks []acp.ContentBlock, opts ...Option) (*QueryResult, error) {
	if len(blocks) == 0 {
		return nil, fmt.Errorf("opencodesdk: QueryContent requires at least one content block")
	}

	var result *QueryResult

	err := WithClient(ctx, func(c Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		res, err := runTurn(ctx, sess, blocks...)
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
//
// For dynamic prompt generation (e.g. feeding prompts from a channel,
// or building multimodal prompts mid-flight) use [QueryStreamContent].
func QueryStream(ctx context.Context, prompts []string, opts ...Option) iter.Seq2[*QueryResult, error] {
	return QueryStreamContent(ctx, PromptsFromStrings(prompts), opts...)
}

// QueryStreamContent is the iterator-backed, multimodal variant of
// [QueryStream]. It consumes an iterator of prompts — each prompt being
// a slice of content blocks — and runs each against the same
// opencode session, yielding one QueryResult per prompt.
//
// Use the helper constructors to build the input iterator:
//
//   - [PromptsFromStrings]  — static slice of text prompts
//   - [PromptsFromSlice]    — static slice of multimodal prompts
//   - [PromptsFromChannel]  — channel of prompts generated on the fly
//   - [SinglePrompt]        — single-prompt iterator (useful for tests)
//
// The yielded sequence short-circuits on the first error. The session,
// subprocess, and registered SDK tools are torn down when the iterator
// goroutine returns.
func QueryStreamContent(ctx context.Context, prompts iter.Seq[[]acp.ContentBlock], opts ...Option) iter.Seq2[*QueryResult, error] {
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

		for blocks := range prompts {
			if err := ctx.Err(); err != nil {
				yield(nil, err)

				return
			}

			if len(blocks) == 0 {
				if !yield(nil, fmt.Errorf("opencodesdk: QueryStreamContent: empty prompt")) {
					return
				}

				return
			}

			res, err := runTurn(ctx, sess, blocks...)
			if !yield(res, err) {
				return
			}

			if err != nil {
				return
			}
		}
	}
}

// PromptsFromStrings adapts a []string into an iter.Seq[[]ContentBlock]
// suitable for QueryStreamContent. Each string becomes a single
// TextBlock prompt.
func PromptsFromStrings(prompts []string) iter.Seq[[]acp.ContentBlock] {
	return func(yield func([]acp.ContentBlock) bool) {
		for _, p := range prompts {
			if !yield([]acp.ContentBlock{acp.TextBlock(p)}) {
				return
			}
		}
	}
}

// PromptsFromSlice adapts a [][]ContentBlock into an
// iter.Seq[[]ContentBlock] for QueryStreamContent. Use when the full
// prompt list (multimodal or not) is already materialised.
func PromptsFromSlice(prompts [][]acp.ContentBlock) iter.Seq[[]acp.ContentBlock] {
	return func(yield func([]acp.ContentBlock) bool) {
		for _, p := range prompts {
			if !yield(p) {
				return
			}
		}
	}
}

// PromptsFromChannel drains prompts sent on ch until the channel is
// closed. Suitable for long-running consumers that generate prompts
// dynamically (e.g. from a queue, or in response to earlier results).
// Closing ch signals end-of-stream to QueryStreamContent.
func PromptsFromChannel(ch <-chan []acp.ContentBlock) iter.Seq[[]acp.ContentBlock] {
	return func(yield func([]acp.ContentBlock) bool) {
		for p := range ch {
			if !yield(p) {
				return
			}
		}
	}
}

// SinglePrompt returns an iter.Seq[[]ContentBlock] yielding exactly one
// prompt. Handy in tests and for symmetry with sister SDKs.
func SinglePrompt(blocks ...acp.ContentBlock) iter.Seq[[]acp.ContentBlock] {
	return func(yield func([]acp.ContentBlock) bool) {
		if len(blocks) == 0 {
			return
		}

		yield(blocks)
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

	// Surface a StructuredOutput payload captured via WithOutputSchema
	// onto the notifications stream so DecodeStructuredOutput finds it
	// at priority 0, ahead of the text-parse fallback.
	if promptErr == nil && res != nil {
		if payload, ok := res.Meta[structuredOutputMetaKey]; ok {
			notifications = append(notifications, acp.SessionNotification{
				Meta: map[string]any{structuredOutputMetaKey: payload},
			})
		}
	}

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
