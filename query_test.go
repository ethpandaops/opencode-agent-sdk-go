package opencodesdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

// fakeSession is a test double for Session used to exercise runTurn
// without a live subprocess. All methods return zero values / no-ops
// except Prompt, Updates, and ID, which are driven by the test setup.
type fakeSession struct {
	id        string
	updates   chan acp.SessionNotification
	prompt    func(ctx context.Context, blocks ...acp.ContentBlock) (*PromptResult, error)
	cancelFn  func(context.Context) error
	setModel  func(context.Context, string) error
	initModes *acp.SessionModeState
}

func (f *fakeSession) ID() string { return f.id }

func (f *fakeSession) Prompt(ctx context.Context, blocks ...acp.ContentBlock) (*PromptResult, error) {
	return f.prompt(ctx, blocks...)
}

func (f *fakeSession) Cancel(ctx context.Context) error {
	if f.cancelFn != nil {
		return f.cancelFn(ctx)
	}

	return nil
}

func (f *fakeSession) Updates() <-chan acp.SessionNotification { return f.updates }

func (f *fakeSession) SetModel(ctx context.Context, m string) error {
	if f.setModel != nil {
		return f.setModel(ctx, m)
	}

	return nil
}

func (f *fakeSession) SetMode(_ context.Context, _ string) error       { return nil }
func (f *fakeSession) InitialModels() *acp.SessionModelState           { return nil }
func (f *fakeSession) InitialModes() *acp.SessionModeState             { return f.initModes }
func (f *fakeSession) InitialConfigOptions() []acp.SessionConfigOption { return nil }
func (f *fakeSession) Meta() map[string]any                            { return nil }
func (f *fakeSession) AvailableModels() []acp.ModelInfo                { return nil }
func (f *fakeSession) AvailableCommands() []acp.AvailableCommand       { return nil }
func (f *fakeSession) CurrentVariant() *VariantInfo                    { return nil }
func (f *fakeSession) Subscribe(_ UpdateHandlers) func()               { return func() {} }
func (f *fakeSession) DroppedUpdates() int64                           { return 0 }

func newFakeSession(buf int) *fakeSession {
	return &fakeSession{
		id:      "ses_fake_1",
		updates: make(chan acp.SessionNotification, buf),
	}
}

// chunkNotification returns a session/update carrying an agent_message_chunk
// with the given text.
func chunkNotification(sessionID, text string) acp.SessionNotification {
	return acp.SessionNotification{
		SessionId: acp.SessionId(sessionID),
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.TextBlock(text),
			},
		},
	}
}

func TestRunTurn_AggregatesTextChunksAndNotifications(t *testing.T) {
	s := newFakeSession(8)

	usage := &acp.Usage{InputTokens: 3, OutputTokens: 7, TotalTokens: 10}

	s.prompt = func(_ context.Context, _ ...acp.ContentBlock) (*PromptResult, error) {
		// Emit two text chunks + a non-text notification before returning.
		hello := chunkNotification(s.id, "hello ")
		s.updates <- hello

		world := chunkNotification(s.id, "world")
		s.updates <- world

		plan := acp.SessionNotification{
			SessionId: acp.SessionId(s.id),
			Update: acp.SessionUpdate{
				Plan: &acp.SessionUpdatePlan{},
			},
		}
		s.updates <- plan

		// Close drives the drain loop to exit promptly so the test is
		// deterministic.
		close(s.updates)

		return &PromptResult{StopReason: acp.StopReasonEndTurn, Usage: usage}, nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	res, err := runTurn(ctx, s, acp.TextBlock("hi"))
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	if res.AssistantText != "hello world" {
		t.Fatalf("AssistantText = %q, want %q", res.AssistantText, "hello world")
	}

	if len(res.Notifications) != 3 {
		t.Fatalf("Notifications len = %d, want 3", len(res.Notifications))
	}

	if res.SessionID != s.id {
		t.Fatalf("SessionID = %q, want %q", res.SessionID, s.id)
	}

	if res.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("StopReason = %q, want %q", res.StopReason, acp.StopReasonEndTurn)
	}

	if res.Usage == nil || res.Usage.TotalTokens != 10 {
		t.Fatalf("Usage not propagated: %+v", res.Usage)
	}
}

func TestRunTurn_PromptErrorIsReturned(t *testing.T) {
	s := newFakeSession(1)

	sentinel := errors.New("boom")

	s.prompt = func(_ context.Context, _ ...acp.ContentBlock) (*PromptResult, error) {
		close(s.updates)

		return nil, sentinel
	}

	res, err := runTurn(t.Context(), s, acp.TextBlock("x"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want errors.Is(sentinel)", err)
	}

	if res != nil {
		t.Fatalf("expected nil result on error, got %+v", res)
	}
}

func TestRunTurn_CtxCancelledDuringPromptReturnsPromptErr(t *testing.T) {
	s := newFakeSession(1)

	ctx, cancel := context.WithCancel(t.Context())

	s.prompt = func(pctx context.Context, _ ...acp.ContentBlock) (*PromptResult, error) {
		<-pctx.Done()

		return nil, pctx.Err()
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	res, err := runTurn(ctx, s, acp.TextBlock("x"))
	if err == nil {
		t.Fatalf("expected non-nil error from cancelled prompt, got res=%+v", res)
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want errors.Is(context.Canceled)", err)
	}
}

func TestRunTurn_EmptyUsageStillReturnsResult(t *testing.T) {
	s := newFakeSession(1)

	s.prompt = func(_ context.Context, _ ...acp.ContentBlock) (*PromptResult, error) {
		close(s.updates)

		return &PromptResult{StopReason: acp.StopReasonEndTurn}, nil
	}

	res, err := runTurn(t.Context(), s, acp.TextBlock("x"))
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	if res.Usage != nil {
		t.Fatalf("Usage should be nil when prompt returned none; got %+v", res.Usage)
	}

	if res.AssistantText != "" {
		t.Fatalf("AssistantText should be empty; got %q", res.AssistantText)
	}
}

func TestWithClient_NilFnReturnsError(t *testing.T) {
	err := WithClient(t.Context(), nil)
	if err == nil {
		t.Fatalf("expected error for nil fn")
	}
}

func TestWithClient_PreCancelledCtxReturnsCtxErr(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := WithClient(ctx, func(Client) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled; got %v", err)
	}
}

func TestQueryStream_PreCancelledCtxYieldsCtxErr(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	seq := QueryStream(ctx, []string{"a", "b"})

	var firstErr error

	// Iterate the sequence; we expect the client construction / Start to
	// surface the cancelled ctx as the first (and only) yielded error.
	for _, err := range seq {
		firstErr = err

		break
	}

	if firstErr == nil {
		t.Fatalf("expected yielded err to be non-nil")
	}
}

func TestQueryResult_FieldsZeroed(t *testing.T) {
	// Sanity: ensure a zero-value QueryResult doesn't surprise consumers.
	var r QueryResult

	if r.SessionID != "" || r.AssistantText != "" || r.StopReason != "" || r.Usage != nil || r.Notifications != nil {
		t.Fatalf("QueryResult zero-value unexpectedly populated: %+v", r)
	}
}
