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

// deliver pushes a notification into this session's updates channel.
// Called from the dispatcher goroutine. Non-blocking: if the buffer is
// full the notification is dropped and a warning is logged.
func (s *session) deliver(n acp.SessionNotification) {
	s.mu.Lock()

	if s.closed {
		s.mu.Unlock()

		return
	}

	s.mu.Unlock()

	select {
	case s.updates <- n:
	default:
		s.logger.Warn("session updates channel full; dropping notification",
			slog.Int("buffer", cap(s.updates)),
		)
	}
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

	spanCtx, span := s.client.observer.StartPromptSpan(ctx, string(s.id))
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

	s.client.observer.RecordPrompt(ctx, time.Since(started), string(resp.StopReason), tokensFromUsage(resp.Usage))

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
	return s.setConfigOption(ctx, "model", modelID)
}

// SetMode maps to session/set_config_option with configId="mode".
func (s *session) SetMode(ctx context.Context, modeID string) error {
	return s.setConfigOption(ctx, "mode", modeID)
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
