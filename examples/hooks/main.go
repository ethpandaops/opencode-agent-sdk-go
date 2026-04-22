// Example: WithHooks wires typed callbacks into opencode's
// session/update stream + lifecycle + permission path. This example
// logs every tool opencode is about to run, counts the turns, and
// refuses any fs/write_text_file delegation targeting /etc/*.
//
// Run with `go run ./examples/hooks`.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cwd, _ := os.Getwd()

	var turns atomic.Int32

	hooks := map[opencodesdk.HookEvent][]*opencodesdk.HookMatcher{
		opencodesdk.HookEventSessionStart: {{
			Hooks: []opencodesdk.HookCallback{func(_ context.Context, in opencodesdk.HookInput) (opencodesdk.HookOutput, error) {
				fmt.Printf("[hook] session started: %s\n", in.SessionID)

				return opencodesdk.HookAllow(), nil
			}},
		}},
		opencodesdk.HookEventPreToolUse: {{
			Hooks: []opencodesdk.HookCallback{func(_ context.Context, in opencodesdk.HookInput) (opencodesdk.HookOutput, error) {
				fmt.Printf("[hook] pre-tool: %s (kind=%s)\n", in.ToolCall.Title, in.ToolCall.Kind)

				return opencodesdk.HookAllow(), nil
			}},
		}},
		opencodesdk.HookEventPostToolUse: {{
			Hooks: []opencodesdk.HookCallback{func(_ context.Context, in opencodesdk.HookInput) (opencodesdk.HookOutput, error) {
				status := ""
				if in.ToolCallUpdate != nil && in.ToolCallUpdate.Status != nil {
					status = string(*in.ToolCallUpdate.Status)
				}

				fmt.Printf("[hook] post-tool status=%s\n", status)

				return opencodesdk.HookAllow(), nil
			}},
		}},
		opencodesdk.HookEventUserPromptSubmit: {{
			Hooks: []opencodesdk.HookCallback{func(_ context.Context, in opencodesdk.HookInput) (opencodesdk.HookOutput, error) {
				turns.Add(1)
				fmt.Printf("[hook] prompt #%d: %q\n", turns.Load(), truncate(in.PromptText, 60))

				return opencodesdk.HookAllow(), nil
			}},
		}},
		opencodesdk.HookEventStop: {{
			Hooks: []opencodesdk.HookCallback{func(_ context.Context, in opencodesdk.HookInput) (opencodesdk.HookOutput, error) {
				if in.PromptResult != nil {
					fmt.Printf("[hook] stop: stopReason=%s\n", in.PromptResult.StopReason)
				}

				return opencodesdk.HookAllow(), nil
			}},
		}},
		opencodesdk.HookEventFileChanged: {{
			Matcher: regexp.MustCompile(`^/etc/`),
			Hooks: []opencodesdk.HookCallback{func(_ context.Context, _ opencodesdk.HookInput) (opencodesdk.HookOutput, error) {
				return opencodesdk.HookBlock("refusing to write under /etc"), nil
			}},
		}},
	}

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithHooks(hooks),
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

	res, err := sess.Prompt(ctx, acp.TextBlock("List three file-system tools you would use and explain why (briefly)."))
	if err != nil {
		exitf("Prompt: %v", err)
	}

	fmt.Printf("\ntotal prompts observed: %d, stop: %s\n", turns.Load(), res.StopReason)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "…"
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
