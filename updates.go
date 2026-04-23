package opencodesdk

import (
	"context"

	"github.com/coder/acp-go-sdk"
)

// UpdateHandlers is a set of typed callbacks invoked for each variant of
// a session/update notification. Every field is optional; nil handlers
// are skipped.
//
// Handlers run synchronously in the SDK's dispatcher goroutine before
// the notification is forwarded to Session.Updates(). They therefore
// share backpressure with that channel — long-running work should be
// dispatched off the handler goroutine, not performed inline. The ctx
// passed in is the dispatcher context; it is cancelled when the Client
// closes.
//
// Subscribers supplement, rather than replace, the Session.Updates()
// channel. Callers can use either, both, or neither.
type UpdateHandlers struct {
	// UserMessage fires for user_message_chunk notifications. These are
	// only emitted during LoadSession history replay.
	UserMessage func(ctx context.Context, chunk *acp.SessionUpdateUserMessageChunk)

	// AgentMessage fires for each agent_message_chunk streamed during a
	// turn. Concatenate `.Content` text across chunks to assemble the
	// assistant's reply.
	AgentMessage func(ctx context.Context, chunk *acp.SessionUpdateAgentMessageChunk)

	// AgentThought fires for thinking/reasoning chunks. Not all models
	// emit these; opencode surfaces them when the model advertises a
	// thinking channel.
	AgentThought func(ctx context.Context, chunk *acp.SessionUpdateAgentThoughtChunk)

	// ToolCall fires the first time the agent announces a tool call.
	ToolCall func(ctx context.Context, tc *acp.SessionUpdateToolCall)

	// ToolCallUpdate fires on every subsequent update to a tool call
	// (status transitions, streaming content, final output).
	ToolCallUpdate func(ctx context.Context, tcu *acp.SessionToolCallUpdate)

	// Plan fires when the agent publishes or replaces its execution
	// plan. opencode emits these from the `todowrite` tool. The agent
	// always sends the full plan; treat it as a replace.
	Plan func(ctx context.Context, plan *acp.SessionUpdatePlan)

	// AvailableCommands fires when the set of slash commands the agent
	// advertises changes. opencode typically emits this once shortly
	// after session creation.
	AvailableCommands func(ctx context.Context, upd *acp.SessionAvailableCommandsUpdate)

	// CurrentMode fires when the agent's current mode changes. Note
	// that calls to Session.SetMode emit this update.
	CurrentMode func(ctx context.Context, upd *acp.SessionCurrentModeUpdate)

	// ConfigOption fires when a session/set_config_option call completes
	// and opencode broadcasts the new value.
	ConfigOption func(ctx context.Context, upd *acp.SessionConfigOptionUpdate)

	// SessionInfo fires when the agent updates session metadata (e.g.
	// title).
	SessionInfo func(ctx context.Context, upd *acp.SessionSessionInfoUpdate)

	// Usage fires when the agent reports cumulative token or cost usage
	// for the session.
	Usage func(ctx context.Context, upd *acp.SessionUsageUpdate)
}

// TurnCompleteCallback fires after every Session.Prompt call returns,
// regardless of success. On error, result is nil and err is non-nil; on
// success, both are set. Registered via WithOnTurnComplete.
type TurnCompleteCallback func(ctx context.Context, sessionID string, result *PromptResult, err error)

// UpdateDroppedCallback fires when a session/update notification could
// not be delivered because Session.Updates() was full. count is the new
// cumulative drop count for the session. Registered via
// WithOnUpdateDropped.
type UpdateDroppedCallback func(ctx context.Context, sessionID string, count int64)
