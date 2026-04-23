package opencodesdk

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/coder/acp-go-sdk"
)

// attachMaxTurns subscribes a turn-counter to s. opencode emits one or
// more agent_message_chunk notifications per assistant message, all
// sharing a `messageId`. Each distinct messageId is counted as one
// "turn"; once the limit is crossed the next distinct message triggers
// Session.Cancel on the in-flight prompt.
//
// The conventional opencode "turn" wraps an entire prompt/response
// cycle including any intermediate tool-calls. Counting unique
// assistant messageIds is the closest per-session approximation we can
// do without protocol-level support — each turn typically emits one
// assistant message followed by tool calls and a final assistant
// message, so the count tracks the visible reasoning/output messages.
//
// Chunks without a messageId (older ACP shapes) fall back to chunk
// counting; this is a best-effort backstop and may over-count if the
// agent emits many small chunks per message.
func attachMaxTurns(s *session, limit int) {
	if limit <= 0 {
		return
	}

	var (
		mu           sync.Mutex
		lastMsgID    string
		count        atomic.Int64
		cancelled    atomic.Bool
		fallbackNoID bool
	)

	s.Subscribe(UpdateHandlers{
		AgentMessage: func(ctx context.Context, chunk *acp.SessionUpdateAgentMessageChunk) {
			if chunk == nil {
				return
			}

			isNewMessage := false

			switch {
			case chunk.MessageId != nil && *chunk.MessageId != "":
				mu.Lock()
				if *chunk.MessageId != lastMsgID {
					lastMsgID = *chunk.MessageId
					isNewMessage = true
				}
				mu.Unlock()
			default:
				// No MessageId — fall back to per-chunk counting. Once we
				// take this path for a session we keep using it (mixing
				// the two would double-count).
				fallbackNoID = true
				isNewMessage = true
			}

			if !isNewMessage {
				return
			}

			n := count.Add(1)
			if n < int64(limit) {
				return
			}

			if !cancelled.CompareAndSwap(false, true) {
				return
			}

			s.logger.Debug("WithMaxTurns reached; cancelling in-flight turn",
				slog.Int("limit", limit),
				slog.Int64("turn", n),
				slog.Bool("fallback_no_messageid", fallbackNoID),
			)

			if err := s.Cancel(ctx); err != nil {
				s.logger.Debug("WithMaxTurns cancel failed", slog.Any("error", err))
			}
		},
	})
}
