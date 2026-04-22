// Example: LoadSessionHistory rehydrates a session and collects the
// replayed session/update notifications into a typed slice. Pass an
// existing session id via -session-id, or run once with
// `go run ./examples/quick_start` first and pass its output id here.
//
// Run with `go run ./examples/load_history -session-id ses_...`.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	sessionID := flag.String("session-id", "", "opencode session id to load")

	flag.Parse()

	if *sessionID == "" {
		fmt.Fprintln(os.Stderr, "required: -session-id <opencode session id>")
		fmt.Fprintln(os.Stderr, "tip: `opencode` persists session ids on disk; use examples/iter_sessions to list them.")
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cwd, _ := os.Getwd()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}

	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		exitf("Start: %v", startErr)
	}

	history, err := c.LoadSessionHistory(ctx, *sessionID)
	if err != nil {
		exitf("LoadSessionHistory: %v", err)
	}

	fmt.Printf("loaded %q with %d raw notifications, %d coalesced messages\n",
		history.Session.ID(), len(history.Notifications), len(history.Messages))

	for i, m := range history.Messages {
		preview := m.Text
		if len(preview) > 80 {
			preview = preview[:80] + "…"
		}

		fmt.Printf("  [%d] %-10s %s\n", i, m.Role, preview)
	}

	if history.Usage != nil {
		fmt.Printf("usage: ctx used=%d / size=%d\n", history.Usage.Used, history.Usage.Size)
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
