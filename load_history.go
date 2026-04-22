package opencodesdk

import (
	"context"
	"strings"
	"time"

	"github.com/coder/acp-go-sdk"
)

// replayGracePeriod is how long LoadSessionHistory waits after
// session/load returns for trailing replay notifications still in
// flight through the SDK dispatcher. opencode emits replay
// notifications synchronously during the RPC, but the SDK dispatches
// them on a background goroutine so a short grace window avoids
// truncating history. Matches the runTurn grace window.
const replayGracePeriod = 200 * time.Millisecond

// HistoryMessage is a typed view of a replayed session message,
// extracted from the session/update notifications that opencode emits
// during session/load.
type HistoryMessage struct {
	// Role is either "user", "assistant", or "thought". Derived from
	// the notification variant (UserMessageChunk / AgentMessageChunk /
	// AgentThoughtChunk).
	Role string
	// Text is the concatenated text content of the message. Non-text
	// content blocks (image, audio, embedded resource) are ignored when
	// building Text — consult the Notifications field on SessionHistory
	// for the raw payload.
	Text string
}

// SessionHistory is the replay of a loaded opencode session. Returned
// by Client.LoadSessionHistory.
type SessionHistory struct {
	// Session is the loaded opencode session. It is ready for further
	// Prompt/Cancel calls. The session's Updates() channel has been
	// drained of the replay notifications captured below; subsequent
	// activity flows through it as usual.
	Session Session

	// Notifications is the raw session/update notifications opencode
	// emitted while replaying the session's history. Preserves
	// original order, including non-message updates (plan, tool_call,
	// available_commands_update, usage_update, etc.).
	Notifications []acp.SessionNotification

	// Messages is a convenience projection of Notifications that
	// collapses user/assistant/thought text chunks into single
	// HistoryMessage entries. Adjacent chunks with the same role are
	// concatenated. Non-text updates do not appear here.
	Messages []HistoryMessage

	// Usage is the last UsageUpdate observed during replay (opencode
	// emits these cumulatively). Nil if the session had no usage
	// events.
	Usage *acp.SessionUsageUpdate
}

// LoadSessionHistory loads an opencode session and drains the history
// replay that opencode emits via session/update during session/load.
//
// Use this instead of LoadSession when you want the full historical
// transcript as a typed slice. The returned Session is ready for
// subsequent Prompt/Cancel calls; its Updates() channel carries only
// post-replay notifications.
//
// Caveats:
//
//   - Opencode does not mark the end of replay, so the SDK waits for a
//     short grace window after session/load returns before concluding
//     that replay is complete. Very large histories may emit beyond the
//     grace window; increase WithUpdatesBuffer if drops are observed.
//   - Drained notifications are removed from the session's Updates()
//     channel. Callers that want to observe replay live (e.g. for a
//     streaming UI) should use LoadSession directly.
func (c *client) LoadSessionHistory(ctx context.Context, id string, opts ...Option) (*SessionHistory, error) {
	sess, err := c.LoadSession(ctx, id, opts...)
	if err != nil {
		return nil, err
	}

	notifications := drainReplay(ctx, sess.Updates(), replayGracePeriod)

	history := &SessionHistory{
		Session:       sess,
		Notifications: notifications,
		Messages:      messagesFromNotifications(notifications),
	}

	for i := range notifications {
		if u := notifications[i].Update.UsageUpdate; u != nil {
			history.Usage = u
		}
	}

	return history, nil
}

// drainReplay pulls notifications from updates until no more arrive
// within grace. Returns notifications in arrival order.
func drainReplay(ctx context.Context, updates <-chan acp.SessionNotification, grace time.Duration) []acp.SessionNotification {
	var out []acp.SessionNotification

	timer := time.NewTimer(grace)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return out
		case n, ok := <-updates:
			if !ok {
				return out
			}

			out = append(out, n)

			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}

			timer.Reset(grace)
		case <-timer.C:
			return out
		}
	}
}

// messagesFromNotifications reduces the raw notification stream into
// per-role HistoryMessage entries. Adjacent chunks with the same role
// are concatenated. Non-message updates are skipped.
func messagesFromNotifications(ns []acp.SessionNotification) []HistoryMessage {
	if len(ns) == 0 {
		return nil
	}

	out := make([]HistoryMessage, 0, len(ns)/4)

	var (
		curRole string
		curText strings.Builder
	)

	flush := func() {
		if curRole == "" {
			return
		}

		text := curText.String()
		curText.Reset()

		if text == "" {
			curRole = ""

			return
		}

		out = append(out, HistoryMessage{Role: curRole, Text: text})
		curRole = ""
	}

	for _, n := range ns {
		role, text := extractMessageChunk(n.Update)
		if role == "" {
			continue
		}

		if role != curRole {
			flush()

			curRole = role
		}

		curText.WriteString(text)
	}

	flush()

	return out
}

// extractMessageChunk returns (role, text) for message-bearing update
// variants, or ("", "") for everything else.
func extractMessageChunk(u acp.SessionUpdate) (string, string) {
	switch {
	case u.UserMessageChunk != nil && u.UserMessageChunk.Content.Text != nil:
		return "user", u.UserMessageChunk.Content.Text.Text
	case u.AgentMessageChunk != nil && u.AgentMessageChunk.Content.Text != nil:
		return "assistant", u.AgentMessageChunk.Content.Text.Text
	case u.AgentThoughtChunk != nil && u.AgentThoughtChunk.Content.Text != nil:
		return "thought", u.AgentThoughtChunk.Content.Text.Text
	}

	return "", ""
}
