//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestListModelCapabilities_Smoke verifies the helper can spawn
// `opencode serve`, fetch /config/providers, tear down cleanly, and
// return a non-empty capability map. It does not assert on a specific
// model because the set depends on the user's opencode config — only
// that at least one entry was decoded.
func TestListModelCapabilities_Smoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	caps, err := opencodesdk.ListModelCapabilities(ctx,
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
	)
	skipIfCLIUnavailable(t, err)

	if err != nil {
		t.Fatalf("ListModelCapabilities: %v", err)
	}

	if len(caps) == 0 {
		t.Fatal("expected at least one model capability entry")
	}

	var (
		anyReasoning bool
		anyToolcall  bool
	)

	for key, mc := range caps {
		if key == "" {
			t.Errorf("empty key in capability map (entry: %+v)", mc)
		}

		if mc.ProviderID == "" || mc.ModelID == "" {
			t.Errorf("key %q: ProviderID/ModelID should be populated: %+v", key, mc)
		}

		if mc.Reasoning {
			anyReasoning = true
		}

		if mc.Toolcall {
			anyToolcall = true
		}
	}

	// opencode's default catalogue (models.dev) always includes
	// reasoning-capable and tool-call-capable entries; absence of
	// either would indicate a decoding regression.
	if !anyReasoning {
		t.Error("no reasoning-capable model found in catalogue (decoding regression?)")
	}

	if !anyToolcall {
		t.Error("no tool-call-capable model found in catalogue (decoding regression?)")
	}
}
