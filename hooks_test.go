package opencodesdk

import (
	"context"
	"errors"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

func TestHookDispatcher_Nil(t *testing.T) {
	var d *hookDispatcher

	out, err := d.dispatch(context.Background(), HookEventStop, "", HookInput{})
	if err != nil {
		t.Fatalf("nil dispatcher: unexpected error %v", err)
	}

	if !out.Continue {
		t.Fatal("nil dispatcher: expected Continue=true")
	}
}

func TestHookDispatcher_NoMatcher_FiresAll(t *testing.T) {
	var fired atomic.Int32

	d := newHookDispatcher(map[HookEvent][]*HookMatcher{
		HookEventPreToolUse: {{
			Hooks: []HookCallback{
				func(_ context.Context, _ HookInput) (HookOutput, error) {
					fired.Add(1)

					return HookAllow(), nil
				},
			},
		}},
	})

	out, err := d.dispatch(context.Background(), HookEventPreToolUse, "doit", HookInput{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if !out.Continue || fired.Load() != 1 {
		t.Fatalf("expected fired=1 continue=true, got fired=%d continue=%v", fired.Load(), out.Continue)
	}
}

func TestHookDispatcher_MatcherFilters(t *testing.T) {
	var fired atomic.Int32

	d := newHookDispatcher(map[HookEvent][]*HookMatcher{
		HookEventFileChanged: {{
			Matcher: regexp.MustCompile(`^/etc/`),
			Hooks: []HookCallback{
				func(_ context.Context, _ HookInput) (HookOutput, error) {
					fired.Add(1)

					return HookBlock("nope"), nil
				},
			},
		}},
	})

	out, _ := d.dispatch(context.Background(), HookEventFileChanged, "/home/a.txt", HookInput{})

	if fired.Load() != 0 {
		t.Fatalf("expected no fire for /home path, got %d", fired.Load())
	}

	if !out.Continue {
		t.Fatal("expected continue=true when matcher didn't fire")
	}

	out, _ = d.dispatch(context.Background(), HookEventFileChanged, "/etc/passwd", HookInput{})

	if fired.Load() != 1 {
		t.Fatalf("expected fire for /etc path, got %d", fired.Load())
	}

	if out.Continue {
		t.Fatal("expected continue=false after block")
	}

	if out.Reason != "nope" {
		t.Fatalf("expected reason=nope, got %q", out.Reason)
	}
}

func TestHookDispatcher_Once(t *testing.T) {
	var fired atomic.Int32

	d := newHookDispatcher(map[HookEvent][]*HookMatcher{
		HookEventSessionStart: {{
			Once: true,
			Hooks: []HookCallback{
				func(_ context.Context, _ HookInput) (HookOutput, error) {
					fired.Add(1)

					return HookAllow(), nil
				},
			},
		}},
	})

	for range 3 {
		_, _ = d.dispatch(context.Background(), HookEventSessionStart, "ses", HookInput{})
	}

	if fired.Load() != 1 {
		t.Fatalf("expected Once to fire once, got %d", fired.Load())
	}
}

func TestHookDispatcher_ErrorBlocks(t *testing.T) {
	d := newHookDispatcher(map[HookEvent][]*HookMatcher{
		HookEventUserPromptSubmit: {{
			Hooks: []HookCallback{
				func(_ context.Context, _ HookInput) (HookOutput, error) {
					return HookAllow(), errors.New("boom")
				},
			},
		}},
	})

	out, err := d.dispatch(context.Background(), HookEventUserPromptSubmit, "anything", HookInput{})
	if err == nil {
		t.Fatal("expected error")
	}

	if out.Continue {
		t.Fatal("errors should block")
	}
}

func TestHookDispatcher_Timeout(t *testing.T) {
	d := newHookDispatcher(map[HookEvent][]*HookMatcher{
		HookEventStop: {{
			Timeout: 10 * time.Millisecond,
			Hooks: []HookCallback{
				func(ctx context.Context, _ HookInput) (HookOutput, error) {
					select {
					case <-ctx.Done():
						return HookAllow(), ctx.Err()
					case <-time.After(time.Second):
						return HookAllow(), nil
					}
				},
			},
		}},
	})

	_, err := d.dispatch(context.Background(), HookEventStop, "", HookInput{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWithHooks_AccumulatesAcrossCalls(t *testing.T) {
	m1 := &HookMatcher{Hooks: []HookCallback{func(_ context.Context, _ HookInput) (HookOutput, error) { return HookAllow(), nil }}}
	m2 := &HookMatcher{Hooks: []HookCallback{func(_ context.Context, _ HookInput) (HookOutput, error) { return HookAllow(), nil }}}

	o := apply([]Option{
		WithHooks(map[HookEvent][]*HookMatcher{HookEventStop: {m1}}),
		WithHooks(map[HookEvent][]*HookMatcher{HookEventStop: {m2}}),
	})

	if got := len(o.hooks[HookEventStop]); got != 2 {
		t.Fatalf("expected 2 matchers after two WithHooks calls, got %d", got)
	}
}

func TestHookBlock_ConvenienceConstructors(t *testing.T) {
	allow := HookAllow()
	if !allow.Continue {
		t.Fatal("HookAllow must set Continue=true")
	}

	block := HookBlock("nope")
	if block.Continue || block.Reason != "nope" {
		t.Fatalf("HookBlock unexpected: %+v", block)
	}
}

func TestExtractPromptText(t *testing.T) {
	got := extractPromptText([]acp.ContentBlock{
		acp.TextBlock("hello"),
		acp.TextBlock("world"),
	})

	if got != "hello\nworld" {
		t.Fatalf("extractPromptText: got %q", got)
	}
}

func TestSynthReject_PrefersRejectOnce(t *testing.T) {
	req := acp.RequestPermissionRequest{Options: []acp.PermissionOption{
		{OptionId: "allow", Kind: acp.PermissionOptionKindAllowOnce},
		{OptionId: "reject", Kind: acp.PermissionOptionKindRejectOnce},
	}}
	resp := synthReject(req, "")

	if resp.Outcome.Selected == nil || resp.Outcome.Selected.OptionId != "reject" {
		t.Fatalf("expected reject option selected, got %+v", resp.Outcome)
	}
}

func TestSynthReject_FallsBackToCancelled(t *testing.T) {
	resp := synthReject(acp.RequestPermissionRequest{}, "why")
	if resp.Outcome.Cancelled == nil {
		t.Fatalf("expected cancelled outcome, got %+v", resp.Outcome)
	}
}

func TestIsRejectOutcome(t *testing.T) {
	cases := []struct {
		name string
		resp acp.RequestPermissionResponse
		want bool
	}{
		{"cancelled", acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
			Cancelled: &acp.RequestPermissionOutcomeCancelled{},
		}}, true},
		{"reject-option", acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: "reject_once"},
		}}, true},
		{"allow-option", acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: "allow_once"},
		}}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRejectOutcome(tc.resp); got != tc.want {
				t.Fatalf("want %v got %v", tc.want, got)
			}
		})
	}
}
