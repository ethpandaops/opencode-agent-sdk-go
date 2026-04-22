package opencodesdk

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/mcp/bridge"
)

// fakeToolSession satisfies bridge.ToolSession for test wiring.
type fakeToolSession struct {
	lastParams *mcp.ElicitParams
	resp       *mcp.ElicitResult
	err        error
}

func (f *fakeToolSession) Elicit(_ context.Context, params *mcp.ElicitParams) (*mcp.ElicitResult, error) {
	f.lastParams = params

	return f.resp, f.err
}

func TestElicit_NoSessionInContext(t *testing.T) {
	_, err := Elicit(context.Background(), ElicitParams{Message: "hi"})
	if !errors.Is(err, ErrElicitationUnavailable) {
		t.Fatalf("want ErrElicitationUnavailable, got %v", err)
	}
}

func TestElicit_HappyPath(t *testing.T) {
	fake := &fakeToolSession{
		resp: &mcp.ElicitResult{
			Action:  "accept",
			Content: map[string]any{"ok": true},
		},
	}

	ctx := bridgeTestCtx(fake)

	out, err := Elicit(ctx, ElicitParams{
		Message: "Proceed?",
		RequestedSchema: SimpleSchema(map[string]string{
			"confirm": "bool",
		}),
	})
	if err != nil {
		t.Fatalf("Elicit: %v", err)
	}

	if out.Action != "accept" {
		t.Fatalf("Action: want accept, got %q", out.Action)
	}

	if got, ok := out.Content["ok"].(bool); !ok || !got {
		t.Fatalf("Content: unexpected %+v", out.Content)
	}

	if fake.lastParams == nil || fake.lastParams.Message != "Proceed?" {
		t.Fatalf("session did not receive the request message: %+v", fake.lastParams)
	}

	if fake.lastParams.Mode != string(ElicitModeForm) {
		t.Fatalf("default mode: want form, got %q", fake.lastParams.Mode)
	}
}

func TestElicit_PropagatesMode(t *testing.T) {
	fake := &fakeToolSession{resp: &mcp.ElicitResult{Action: "accept"}}

	ctx := bridgeTestCtx(fake)

	_, err := Elicit(ctx, ElicitParams{
		Message: "open url",
		Mode:    ElicitModeURL,
		URL:     "https://example.com/auth",
	})
	if err != nil {
		t.Fatalf("Elicit: %v", err)
	}

	if fake.lastParams.Mode != string(ElicitModeURL) {
		t.Fatalf("want url mode, got %q", fake.lastParams.Mode)
	}

	if fake.lastParams.URL != "https://example.com/auth" {
		t.Fatalf("URL not propagated: %q", fake.lastParams.URL)
	}
}

func TestElicit_WrapsSessionError(t *testing.T) {
	sentinel := errors.New("elicitation not supported")
	fake := &fakeToolSession{err: sentinel}

	ctx := bridgeTestCtx(fake)

	_, err := Elicit(ctx, ElicitParams{Message: "x"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want wrapped sentinel, got %v", err)
	}
}

// bridgeTestCtx is a test-only wrapper that installs a fake
// ToolSession into the ctx via the bridge's internal key. Lives in
// the test file so production code doesn't expose the key.
func bridgeTestCtx(sess bridge.ToolSession) context.Context {
	return bridge.ContextWithSessionForTest(context.Background(), sess)
}
