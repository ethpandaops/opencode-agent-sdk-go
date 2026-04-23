// Demonstrates the typed errors opencodesdk surfaces and how to branch
// on them with errors.Is / errors.As.
//
// The SDK exposes:
//
//   - Sentinel errors (ErrCLINotFound, ErrUnsupportedCLIVersion,
//     ErrAuthRequired, ErrCancelled, ErrClientClosed, ErrClientNotStarted,
//     ErrClientAlreadyConnected, ErrRequestTimeout, ErrTransport)
//   - Typed errors (*RequestError, *CLINotFoundError, *ProcessError,
//     *TransportError) that all satisfy the OpencodeSDKError marker
//     interface so callers can distinguish SDK-originated errors from
//     arbitrary Go errors in the same code path.
//
// This example trips each sentinel case deliberately so you can see
// the shape of the resulting error.
//
//	go run ./examples/error_handling
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	demoCLINotFound(ctx, logger)
	fmt.Println()

	demoClientNotStarted(ctx, logger)
	fmt.Println()

	demoClientClosed(ctx, logger)
	fmt.Println()

	demoClientAlreadyConnected(ctx, logger)
	fmt.Println()

	demoSDKErrorMarker(ctx, logger)
}

// demoCLINotFound shows ErrCLINotFound by pinning WithCLIPath at a
// path that does not exist.
func demoCLINotFound(ctx context.Context, logger *slog.Logger) {
	fmt.Println("== demoCLINotFound ==")

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCLIPath("/nonexistent/opencode-binary"),
	)
	if err != nil {
		fmt.Printf("NewClient: %v\n", err)

		return
	}
	defer c.Close()

	err = c.Start(ctx)
	classify(err)
}

// demoClientNotStarted shows ErrClientNotStarted by calling NewSession
// before Start.
func demoClientNotStarted(ctx context.Context, logger *slog.Logger) {
	fmt.Println("== demoClientNotStarted ==")

	c, err := opencodesdk.NewClient(opencodesdk.WithLogger(logger))
	if err != nil {
		fmt.Printf("NewClient: %v\n", err)

		return
	}
	defer c.Close()

	_, err = c.NewSession(ctx)
	classify(err)
}

// demoClientClosed shows ErrClientClosed by using a Client after Close.
func demoClientClosed(ctx context.Context, logger *slog.Logger) {
	fmt.Println("== demoClientClosed ==")

	c, err := opencodesdk.NewClient(opencodesdk.WithLogger(logger))
	if err != nil {
		fmt.Printf("NewClient: %v\n", err)

		return
	}

	if closeErr := c.Close(); closeErr != nil {
		fmt.Printf("Close: %v\n", closeErr)
	}

	_, err = c.NewSession(ctx)
	classify(err)
}

// demoClientAlreadyConnected shows ErrClientAlreadyConnected by
// calling Start twice on the same Client.
func demoClientAlreadyConnected(ctx context.Context, logger *slog.Logger) {
	fmt.Println("== demoClientAlreadyConnected ==")

	c, err := opencodesdk.NewClient(opencodesdk.WithLogger(logger))
	if err != nil {
		fmt.Printf("NewClient: %v\n", err)

		return
	}
	defer c.Close()

	err = c.Start(ctx)
	if err != nil {
		fmt.Printf("first Start failed (expected when opencode is missing/unauthenticated): %v\n", err)

		return
	}

	err = c.Start(ctx)
	classify(err)
}

// demoSDKErrorMarker shows the OpencodeSDKError marker interface:
// every typed SDK error satisfies it, which lets callers distinguish
// SDK-originated errors from arbitrary Go errors flowing through the
// same code path.
func demoSDKErrorMarker(_ context.Context, logger *slog.Logger) {
	fmt.Println("== demoSDKErrorMarker ==")

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCLIPath("/nonexistent/opencode-binary"),
	)
	if err != nil {
		fmt.Printf("NewClient: %v\n", err)

		return
	}
	defer c.Close()

	err = c.Start(context.Background())

	var sdkErr opencodesdk.OpencodeSDKError
	if errors.As(err, &sdkErr) {
		fmt.Printf("err is an opencodesdk error: %T → %v\n", sdkErr, sdkErr)
	} else {
		fmt.Printf("err is NOT an opencodesdk error: %v\n", err)
	}
}

// classify prints which sentinel the error matches, if any, then
// demonstrates unwrapping a *RequestError with errors.As.
func classify(err error) {
	if err == nil {
		fmt.Println("no error")

		return
	}

	fmt.Printf("error: %v\n", err)

	switch {
	case errors.Is(err, opencodesdk.ErrCLINotFound):
		fmt.Println("  → matches ErrCLINotFound")
	case errors.Is(err, opencodesdk.ErrUnsupportedCLIVersion):
		fmt.Println("  → matches ErrUnsupportedCLIVersion")
	case errors.Is(err, opencodesdk.ErrAuthRequired):
		fmt.Println("  → matches ErrAuthRequired (run `opencode auth login`)")
	case errors.Is(err, opencodesdk.ErrCancelled):
		fmt.Println("  → matches ErrCancelled")
	case errors.Is(err, opencodesdk.ErrClientClosed):
		fmt.Println("  → matches ErrClientClosed")
	case errors.Is(err, opencodesdk.ErrClientNotStarted):
		fmt.Println("  → matches ErrClientNotStarted")
	case errors.Is(err, opencodesdk.ErrClientAlreadyConnected):
		fmt.Println("  → matches ErrClientAlreadyConnected")
	case errors.Is(err, opencodesdk.ErrRequestTimeout):
		fmt.Println("  → matches ErrRequestTimeout")
	case errors.Is(err, opencodesdk.ErrTransport):
		fmt.Println("  → matches ErrTransport")
	}

	var re *opencodesdk.RequestError
	if errors.As(err, &re) {
		fmt.Printf("  → unwrapped RequestError: code=%d message=%q\n", re.Code, re.Message)
	}

	var te *opencodesdk.TransportError
	if errors.As(err, &te) {
		fmt.Printf("  → unwrapped TransportError: reason=%q cause=%v\n", te.Reason, te.Err)
	}
}
