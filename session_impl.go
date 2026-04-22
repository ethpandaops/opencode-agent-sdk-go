package opencodesdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/acp-go-sdk"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/observability"
)

// session is the concrete Session implementation.
type session struct {
	id     acp.SessionId
	client *client
	logger *slog.Logger

	updates chan acp.SessionNotification

	mu             sync.Mutex
	closed         bool
	cancelIntended bool

	initialModels  *acp.SessionModelState
	initialModes   *acp.SessionModeState
	initialOptions []acp.SessionConfigOption
	meta           map[string]any

	// commands is the latest AvailableCommandsUpdate payload the agent
	// has sent. opencode emits this once per session shortly after the
	// lifecycle response. Protected by mu.
	commands []acp.AvailableCommand

	// currentModel / currentMode track the live model + agent-mode for
	// observability labelling. Seeded from the lifecycle response and
	// updated whenever SetModel/SetMode succeeds.
	currentModel string
	currentMode  string

	// toolCallStart tracks the start time of each in-flight tool call
	// so RecordToolCall can emit a duration on the terminal update.
	toolCallStart map[acp.ToolCallId]toolCallObservation
}

type toolCallObservation struct {
	started time.Time
	name    string
	kind    string
}

// newSession constructs a session bound to c with the supplied id and
// initial state. It registers the session in c.sessions and drains any
// pending notifications buffered during the NewSession RPC round-trip.
func newSession(c *client, id acp.SessionId, models *acp.SessionModelState, modes *acp.SessionModeState, opts []acp.SessionConfigOption, meta map[string]any, bufferSize int) *session {
	if bufferSize <= 0 {
		bufferSize = 128
	}

	s := &session{
		id:             id,
		client:         c,
		logger:         c.opts.logger.With(slog.String("session_id", string(id))),
		updates:        make(chan acp.SessionNotification, bufferSize),
		initialModels:  models,
		initialModes:   modes,
		initialOptions: opts,
		meta:           meta,
		toolCallStart:  make(map[acp.ToolCallId]toolCallObservation),
	}

	if models != nil {
		s.currentModel = string(models.CurrentModelId)
	}

	if modes != nil {
		s.currentMode = string(modes.CurrentModeId)
	}

	c.registerSession(s)

	return s
}

func (s *session) ID() string { return string(s.id) }

func (s *session) Updates() <-chan acp.SessionNotification { return s.updates }

func (s *session) InitialModels() *acp.SessionModelState {
	return s.initialModels
}

func (s *session) InitialModes() *acp.SessionModeState {
	return s.initialModes
}

func (s *session) InitialConfigOptions() []acp.SessionConfigOption {
	return s.initialOptions
}

func (s *session) Meta() map[string]any { return s.meta }

// AvailableModels returns the set of models advertised at session
// creation. opencode does not re-emit this via session/update, so the
// returned slice is stable for the session's lifetime.
func (s *session) AvailableModels() []acp.ModelInfo {
	if s.initialModels == nil {
		return nil
	}

	return s.initialModels.AvailableModels
}

// AvailableCommands returns the current slash-command snapshot. The
// slice is a copy so callers may mutate it freely.
func (s *session) AvailableCommands() []acp.AvailableCommand {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.commands) == 0 {
		return nil
	}

	out := make([]acp.AvailableCommand, len(s.commands))
	copy(out, s.commands)

	return out
}

// CurrentVariant returns the opencode-specific variant info parsed from
// the session's _meta.opencode block, or nil if absent.
func (s *session) CurrentVariant() *VariantInfo {
	if s.meta == nil {
		return nil
	}

	info, ok := OpencodeVariant(s.meta)
	if !ok {
		return nil
	}

	return info
}

// deliver pushes a notification into this session's updates channel.
// Called from the dispatcher goroutine. Non-blocking: if the buffer is
// full the notification is dropped and a warning is logged.
//
// Notifications that carry state we cache on the session (e.g.
// available_commands_update) or observability signals (tool_call and
// usage_update) are captured here before the update is forwarded, so
// that AvailableCommands() and OTel metrics reflect them even if no
// consumer ever drains Updates().
func (s *session) deliver(n acp.SessionNotification) {
	s.mu.Lock()

	if s.closed {
		s.mu.Unlock()

		return
	}

	s.observeInLock(n)

	s.mu.Unlock()

	select {
	case s.updates <- n:
	default:
		s.logger.Warn("session updates channel full; dropping notification",
			slog.Int("buffer", cap(s.updates)),
		)
	}
}

// observeInLock captures cacheable and observable signals from a
// notification. Caller must hold s.mu.
func (s *session) observeInLock(n acp.SessionNotification) {
	ctx := context.Background()

	switch {
	case n.Update.AvailableCommandsUpdate != nil:
		cmds := n.Update.AvailableCommandsUpdate.AvailableCommands
		s.commands = append(s.commands[:0], cmds...)

	case n.Update.ToolCall != nil:
		tc := n.Update.ToolCall
		s.toolCallStart[tc.ToolCallId] = toolCallObservation{
			started: time.Now(),
			name:    tc.Title,
			kind:    string(tc.Kind),
		}

		if isTerminalToolCallStatus(tc.Status) {
			s.emitToolCallTerminal(ctx, tc.ToolCallId, tc.Title, string(tc.Kind), string(tc.Status))
		}

	case n.Update.ToolCallUpdate != nil:
		tcu := n.Update.ToolCallUpdate
		if tcu.Status == nil || !isTerminalToolCallStatus(*tcu.Status) {
			return
		}

		name := ""
		if tcu.Title != nil {
			name = *tcu.Title
		}

		kind := ""
		if tcu.Kind != nil {
			kind = string(*tcu.Kind)
		}

		s.emitToolCallTerminal(ctx, tcu.ToolCallId, name, kind, string(*tcu.Status))

	case n.Update.UsageUpdate != nil && n.Update.UsageUpdate.Cost != nil:
		cost := n.Update.UsageUpdate.Cost
		s.client.observer.RecordCost(ctx, cost.Amount, cost.Currency, s.currentModel)
	}
}

// emitToolCallTerminal records the terminal tool_call event and clears
// the start-time bookkeeping for the given tool call id. Caller must
// hold s.mu.
func (s *session) emitToolCallTerminal(ctx context.Context, id acp.ToolCallId, name, kind, status string) {
	obs, known := s.toolCallStart[id]

	var duration time.Duration

	if known {
		duration = time.Since(obs.started)

		if name == "" {
			name = obs.name
		}

		if kind == "" {
			kind = obs.kind
		}

		delete(s.toolCallStart, id)
	}

	s.client.observer.RecordToolCall(ctx, name, kind, status, duration)
}

// isTerminalToolCallStatus reports whether a ToolCallStatus value
// represents a terminal state (completed or failed).
func isTerminalToolCallStatus(s acp.ToolCallStatus) bool {
	return s == acp.ToolCallStatusCompleted || s == acp.ToolCallStatusFailed
}

// close shuts down the session's updates channel. Called when the
// owning Client closes, or if NewSession cleanup must roll back.
func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.closed = true
	close(s.updates)
}

// Prompt submits a user turn.
func (s *session) Prompt(ctx context.Context, blocks ...acp.ContentBlock) (*PromptResult, error) {
	if len(blocks) == 0 {
		return nil, errors.New("opencodesdk: Prompt requires at least one content block")
	}

	// Reset cancelIntended at entry: a stray Cancel() call before any
	// Prompt runs would otherwise leave the flag sticky and misclassify
	// the next unrelated error as ErrCancelled.
	s.mu.Lock()
	s.cancelIntended = false
	labels := observability.PromptLabels{Model: s.currentModel, Mode: s.currentMode}
	s.mu.Unlock()

	spanCtx, span := s.client.observer.StartPromptSpan(ctx, string(s.id), labels)
	defer span.End()

	ctx = spanCtx
	started := time.Now()

	// Watch for ctx cancellation so we can send session/cancel
	// notification to opencode. Without this, cancelling ctx would
	// close the Prompt request but leave opencode running the turn.
	watchDone := make(chan struct{})
	defer close(watchDone)

	go func() {
		select {
		case <-watchDone:
			return
		case <-ctx.Done():
			// Best-effort cancel; use a detached context because ctx is done.
			s.mu.Lock()
			s.cancelIntended = true
			s.mu.Unlock()

			// Intentionally detached: ctx is already Done, but we still
			// need to reach opencode to notify it of the cancel.
			cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second) //nolint:contextcheck // intentional: cancel requires a live ctx
			defer cancel()

			if err := s.client.proc.Conn().Cancel(cancelCtx, acp.CancelNotification{SessionId: s.id}); err != nil {
				s.logger.Debug("send session/cancel failed", slog.Any("error", err))
			}
		}
	}()

	resp, err := s.client.proc.Conn().Prompt(ctx, acp.PromptRequest{
		SessionId: s.id,
		Prompt:    blocks,
	})
	if err != nil {
		s.mu.Lock()
		intended := s.cancelIntended
		s.cancelIntended = false
		s.mu.Unlock()

		if intended || errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("%w: %w", ErrCancelled, err)
		}

		return nil, wrapACPErr(err)
	}

	s.client.observer.RecordPrompt(ctx, time.Since(started), string(resp.StopReason), tokensFromUsage(resp.Usage), labels)

	return &PromptResult{
		StopReason: resp.StopReason,
		Usage:      resp.Usage,
		Meta:       resp.Meta,
	}, nil
}

// tokensFromUsage extracts the Observer's TokenCounts from an
// acp.Usage. Zero values are fine — Observer skips zeroed buckets.
func tokensFromUsage(u *acp.Usage) observability.TokenCounts {
	if u == nil {
		return observability.TokenCounts{}
	}

	tc := observability.TokenCounts{
		Input:  int64(u.InputTokens),
		Output: int64(u.OutputTokens),
	}

	if u.CachedReadTokens != nil {
		tc.CachedRead = int64(*u.CachedReadTokens)
	}

	if u.CachedWriteTokens != nil {
		tc.CachedWrite = int64(*u.CachedWriteTokens)
	}

	if u.ThoughtTokens != nil {
		tc.Thought = int64(*u.ThoughtTokens)
	}

	return tc
}

// Cancel emits a session/cancel notification for the current turn.
func (s *session) Cancel(ctx context.Context) error {
	s.mu.Lock()
	s.cancelIntended = true
	s.mu.Unlock()

	return s.client.proc.Conn().Cancel(ctx, acp.CancelNotification{SessionId: s.id})
}

// SetModel maps to session/set_config_option with configId="model".
func (s *session) SetModel(ctx context.Context, modelID string) error {
	if err := s.setConfigOption(ctx, "model", modelID); err != nil {
		return err
	}

	s.mu.Lock()
	s.currentModel = modelID
	s.mu.Unlock()

	return nil
}

// SetMode maps to session/set_config_option with configId="mode".
func (s *session) SetMode(ctx context.Context, modeID string) error {
	if err := s.setConfigOption(ctx, "mode", modeID); err != nil {
		return err
	}

	s.mu.Lock()
	s.currentMode = modeID
	s.mu.Unlock()

	return nil
}

func (s *session) setConfigOption(ctx context.Context, configID, value string) error {
	_, err := s.client.proc.Conn().SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: s.id,
			ConfigId:  acp.SessionConfigId(configID),
			Value:     acp.SessionConfigValueId(value),
		},
	})
	if err != nil {
		return wrapACPErr(err)
	}

	return nil
}
