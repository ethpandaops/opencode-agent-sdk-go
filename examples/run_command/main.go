// Demonstrates Session.RunCommand: invoke one of opencode's slash
// commands (advertised via session/update's available_commands_update
// notification) as a prompt turn. RunCommand is sugar for sending the
// command as a leading-slash text block via Session.Prompt — opencode
// interprets text starting with "/" as a command invocation.
//
//	go run ./examples/run_command
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return fmt.Errorf("new session: %w", err)
		}

		go func() {
			for n := range sess.Updates() {
				if n.Update.AgentMessageChunk != nil &&
					n.Update.AgentMessageChunk.Content.Text != nil {
					fmt.Print(n.Update.AgentMessageChunk.Content.Text.Text)
				}
			}
		}()

		// available_commands_update is a notification, not part of the
		// session/new response. Give it a tick to land before reading
		// AvailableCommands().
		deadline := time.Now().Add(2 * time.Second)
		for len(sess.AvailableCommands()) == 0 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}

		commands := sess.AvailableCommands()
		fmt.Printf("opencode advertised %d slash commands:\n", len(commands))

		for i, cmd := range commands {
			if i >= 10 {
				fmt.Printf("  … %d more\n", len(commands)-10)

				break
			}

			fmt.Printf("  /%-12s  %s\n", cmd.Name, cmd.Description)
		}

		// /help is one of the safer commands to demo. If unavailable,
		// pick the first advertised command.
		choice := "help"

		known := false

		for _, cmd := range commands {
			if cmd.Name == choice {
				known = true

				break
			}
		}

		if !known && len(commands) > 0 {
			choice = commands[0].Name
		}

		if choice == "" {
			fmt.Println("\nno commands available; nothing to invoke.")

			return nil
		}

		fmt.Printf("\n--- invoking /%s ---\n", choice)

		if _, err := sess.RunCommand(ctx, choice); err != nil {
			return fmt.Errorf("RunCommand(/%s): %w", choice, err)
		}

		fmt.Println()

		return nil
	},
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WithClient: %v\n", err)
		os.Exit(1)
	}
}
