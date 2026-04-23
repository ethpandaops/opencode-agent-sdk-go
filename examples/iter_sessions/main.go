// Demonstrates Client.IterSessions — a cursor-paginated iterator over
// every opencode session in the configured cwd. Useful when you want a
// simple drain-all loop without hand-threading the cursor.
//
//	go run ./examples/iter_sessions
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		exitf("Start: %v", startErr)
	}

	count := 0

	for info, err := range c.IterSessions(ctx) {
		if err != nil {
			exitf("IterSessions: %v", err)
		}

		title := ""
		if info.Title != nil {
			title = *info.Title
		}

		fmt.Printf("%s  %s\n", info.SessionId, title)

		count++
	}

	fmt.Printf("\n%d sessions total\n", count)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
