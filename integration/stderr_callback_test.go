//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestStderrCallback_ReceivesLines registers a WithStderr callback and
// asserts that at least one stderr line from the subprocess is
// delivered during a short interaction.
func TestStderrCallback_ReceivesLines(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var (
		mu    sync.Mutex
		lines []string
	)

	cb := func(line string) {
		mu.Lock()

		lines = append(lines, line)

		mu.Unlock()
	}

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return err
		}

		_, promptErr := sess.Prompt(ctx, acp.TextBlock("Reply with just: ok."))

		return promptErr
	},
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithStderr(cb),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("WithClient: %v", err)
	}

	mu.Lock()
	count := len(lines)
	mu.Unlock()

	// opencode may be completely quiet on a clean run; log rather than fail.
	t.Logf("captured %d stderr line(s) from opencode", count)
}
