// Example: Session.SetConfigOption routes session/set_config_option
// for arbitrary config ids reported by opencode at session creation.
// SetModel / SetMode are convenience wrappers over this for the two
// common cases.
//
// Run with `go run ./examples/set_config_option`.
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

	sess, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	fmt.Println("available config options reported by opencode at session/new:")

	for _, opt := range sess.InitialConfigOptions() {
		switch {
		case opt.Select != nil:
			fmt.Printf("  - %s (name=%q, select, current=%q)\n", opt.Select.Id, opt.Select.Name, opt.Select.CurrentValue)
		case opt.Boolean != nil:
			fmt.Printf("  - %s (name=%q, boolean, current=%v)\n", opt.Boolean.Id, opt.Boolean.Name, opt.Boolean.CurrentValue)
		}
	}

	// Two convenience wrappers map to well-known config ids.
	if err := sess.SetModel(ctx, "anthropic/claude-sonnet-4"); err != nil {
		fmt.Fprintf(os.Stderr, "SetModel: %v (often -32602 if model id not recognised)\n", err)
	}

	if err := sess.SetMode(ctx, "build"); err != nil {
		fmt.Fprintf(os.Stderr, "SetMode: %v\n", err)
	}

	// Generic path — works for any config id opencode exposes. Here we
	// probe a hypothetical "reasoning_effort" option; opencode returns
	// an error for unknown ids, which we surface without aborting.
	if err := sess.SetConfigOption(ctx, "reasoning_effort", "medium"); err != nil {
		fmt.Fprintf(os.Stderr, "SetConfigOption(reasoning_effort): %v\n", err)
	}

	// Boolean variant — for config options whose SessionConfigOption
	// variant is boolean. opencode 1.14.x does not expose any of these
	// via ACP yet; the method is present for forward compatibility.
	if err := sess.SetConfigOptionBool(ctx, "auto_compact", true); err != nil {
		fmt.Fprintf(os.Stderr, "SetConfigOptionBool(auto_compact): %v\n", err)
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
