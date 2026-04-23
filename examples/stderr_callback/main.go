// Demonstrates WithStderr for capturing opencode's stderr output. The
// SDK forwards each line opencode writes to stderr to the supplied
// callback — useful for surfacing diagnostics in your own UI or
// aggregating them with the app's logs.
//
//	go run ./examples/stderr_callback
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	var (
		mu       sync.Mutex
		captured []string
	)

	stderrCB := func(line string) {
		mu.Lock()

		captured = append(captured, line)

		mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return fmt.Errorf("new session: %w", err)
		}

		_, err = sess.Prompt(ctx, acp.TextBlock("Reply with just the word ready."))
		if err != nil {
			return fmt.Errorf("prompt: %w", err)
		}

		return nil
	},
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithStderr(stderrCB),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WithClient: %v\n", err)
		os.Exit(1)
	}

	mu.Lock()
	defer mu.Unlock()

	fmt.Printf("captured %d stderr line(s) from opencode\n", len(captured))

	show := min(len(captured), 20)

	for _, line := range captured[:show] {
		fmt.Printf("  | %s\n", line)
	}

	if show < len(captured) {
		fmt.Printf("  … %d more lines suppressed\n", len(captured)-show)
	}
}
