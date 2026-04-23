// Demonstrates WithOnFsWrite: intercept opencode's fs/write_text_file
// delegations and redirect them into an in-memory map rather than the
// real filesystem. Useful for sandboxed evaluation or auditing.
//
// opencode only emits fs/write_text_file after an approved "ask" edit.
// For this example to fire the callback you need
// `"permission": {"edit": "ask"}` in ~/.config/opencode/config.json
// and a WithCanUseTool handler that approves.
//
//	go run ./examples/fs_intercept
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

type virtualFS struct {
	mu    sync.Mutex
	files map[string]string
}

func (v *virtualFS) write(_ context.Context, req acp.WriteTextFileRequest) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.files == nil {
		v.files = map[string]string{}
	}

	v.files[req.Path] = req.Content
	fmt.Printf("[fs] intercepted write %s (%d bytes)\n", req.Path, len(req.Content))

	return nil
}

func (v *virtualFS) dump() {
	v.mu.Lock()
	defer v.mu.Unlock()

	if len(v.files) == 0 {
		fmt.Println("(no files captured)")

		return
	}

	for path, content := range v.files {
		fmt.Printf("--- %s ---\n%s\n", path, content)
	}
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	vfs := &virtualFS{}

	// Run in a dedicated sandbox. opencode's `write` tool writes directly
	// via Node fs, bypassing our interceptor; pointing cwd at a scratch
	// dir keeps any non-delegated writes out of the user's workspace.
	sandbox, err := os.MkdirTemp("", "opencodesdk-fsintercept-*")
	if err != nil {
		exitf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(sandbox)

	fmt.Printf("sandbox: %s\n", sandbox)

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(sandbox),
		opencodesdk.WithCanUseTool(opencodesdk.AllowOnce),
		opencodesdk.WithOnFsWrite(vfs.write),
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err = c.Start(ctx)
	if err != nil {
		exitf("Start: %v", err)
	}

	sess, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	go func() {
		for range sess.Updates() {
		}
	}()

	_, err = sess.Prompt(ctx, acp.TextBlock("Create a tiny Python script hello.py that prints 'hi' and nothing else."))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	fmt.Println("\n\ncaptured writes:")
	vfs.dump()
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
