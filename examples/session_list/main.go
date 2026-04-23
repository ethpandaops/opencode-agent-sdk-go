// Enumerates prior opencode sessions scoped to the working directory
// and prints their IDs and titles. Demonstrates ListSessions + opaque
// cursor pagination.
//
//	go run ./examples/session_list
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

	err = c.Start(ctx)
	if err != nil {
		exitf("Start: %v", err)
	}

	fmt.Printf("sessions in %s:\n\n", cwd)

	cursor := ""
	total := 0

	for {
		batch, next, err := c.ListSessions(ctx, cursor)
		if err != nil {
			exitf("ListSessions: %v", err)
		}

		for _, s := range batch {
			total++
			title := ""

			if s.Title != nil {
				title = *s.Title
			}

			fmt.Printf("  %s  %s\n", s.SessionId, title)
		}

		if next == "" {
			break
		}

		cursor = next
	}

	fmt.Printf("\ntotal: %d\n", total)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
