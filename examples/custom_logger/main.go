// Demonstrates routing opencodesdk's internal logs through a custom
// slog.Handler. The SDK always speaks slog — any compliant handler
// works. This example uses a minimal in-memory handler that tags
// records with a fixed "component=opencodesdk" prefix and mirrors them
// to stderr in a shape that matches your own application's logs.
//
// Swap in your favourite slog handler (slog-logrus, slog-zerolog,
// slog-zap, etc.) and the SDK's output will blend into the rest of
// your log pipeline with no further wiring.
//
//	go run ./examples/custom_logger
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// prefixHandler is a tiny slog.Handler that tags every record with a
// fixed component attribute and forwards to an inner text handler.
type prefixHandler struct {
	inner     slog.Handler
	component string
}

func (h *prefixHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}

func (h *prefixHandler) Handle(ctx context.Context, r slog.Record) error {
	r.AddAttrs(slog.String("component", h.component))

	return h.inner.Handle(ctx, r)
}

func (h *prefixHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &prefixHandler{inner: h.inner.WithAttrs(attrs), component: h.component}
}

func (h *prefixHandler) WithGroup(name string) slog.Handler {
	return &prefixHandler{inner: h.inner.WithGroup(name), component: h.component}
}

// sinkturingWriter forks writes to both stderr and an in-memory buffer,
// so the example can both show live logs and report a summary at the end.
type sinkturingWriter struct {
	mu    sync.Mutex
	lines []string
	dest  io.Writer
}

func (w *sinkturingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.lines = append(w.lines, string(p))

	return w.dest.Write(p)
}

func main() {
	sink := &sinkturingWriter{dest: os.Stderr}

	textHandler := slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug})

	logger := slog.New(&prefixHandler{inner: textHandler, component: "opencodesdk"})

	cwd, _ := os.Getwd()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return fmt.Errorf("new session: %w", err)
		}

		if _, err := sess.Prompt(ctx, acp.TextBlock("Reply with just: hello from a custom logger.")); err != nil {
			return fmt.Errorf("prompt: %w", err)
		}

		return nil
	},
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WithClient: %v\n", err)
		os.Exit(1)
	}

	sink.mu.Lock()
	total := len(sink.lines)
	sink.mu.Unlock()

	fmt.Printf("\n-- sinktured %d log line(s) through the custom handler --\n", total)
}
