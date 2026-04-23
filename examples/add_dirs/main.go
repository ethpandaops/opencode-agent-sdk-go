// Demonstrates opencodesdk.WithAddDirs — forwarding ACP's unstable
// `additionalDirectories` field to opencode.
//
// When opencode advertises SessionCapabilities.AdditionalDirectories
// during ACP initialize, the SDK passes the extra directories on
// session/new, session/load, session/fork, and session/resume so the
// agent can read files beyond the configured cwd. When the capability
// is missing, the SDK silently drops the option and logs a warning —
// no wire error.
//
//	go run ./examples/add_dirs
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cwd, err := os.Getwd()
	if err != nil {
		exitf("getwd: %v", err)
	}

	// Typical use case: point opencode at a sibling directory that
	// lives outside the working tree — e.g. a shared schema repo, a
	// vendored resource directory, or a cache dir. We use /tmp here so
	// the example runs without setup.
	extra := filepath.Join(os.TempDir(), "opencode-example-shared")

	if mkErr := os.MkdirAll(extra, 0o755); mkErr != nil {
		exitf("mkdir extra: %v", mkErr)
	}

	defer os.RemoveAll(extra)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithAddDirs(extra),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}

	defer c.Close()

	if startErr := c.Start(ctx); startErr != nil {
		exitf("Start: %v", startErr)
	}

	caps := c.Capabilities()
	if caps.SessionCapabilities.AdditionalDirectories == nil {
		fmt.Printf("opencode %s does NOT advertise additionalDirectories — WithAddDirs is a no-op.\n",
			c.AgentInfo().Version)
		fmt.Println("The SDK logged a warning and continued; session/new ran without the extra root.")
	} else {
		fmt.Printf("opencode %s advertises additionalDirectories — WithAddDirs is active.\n",
			c.AgentInfo().Version)
	}

	// The session itself goes through the capability probe implicitly:
	// the SDK only attaches AdditionalDirectories when the agent
	// advertised support. Creating the session proves the wire path.
	sess, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	fmt.Printf("session: %s\n", sess.ID())
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
