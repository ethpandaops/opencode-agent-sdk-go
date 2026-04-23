// Demonstrates multimodal prompt input via the opencodesdk.QueryContent
// one-shot helper.
//
// This example shows three entry points:
//
//  1. opencodesdk.QueryContent     — one-shot text + image prompt
//  2. opencodesdk.ImageFileInput   — load an image from disk
//  3. opencodesdk.Text / Blocks     — ergonomic block constructors
//
// The legacy string-only opencodesdk.Query still works for plain text —
// QueryContent is only needed when you want to attach images, embedded
// resources, or other non-text content blocks.
//
// Image support requires the agent to advertise the "image" prompt
// capability during ACP initialize; opencode advertises it when
// attached to a multimodal-capable model. If the capability isn't
// advertised, the SDK rejects image blocks with ErrCapabilityUnavailable
// before the prompt reaches opencode.
//
//	go run ./examples/multimodal_input
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// A 2x2 red PNG, base64-encoded. Just enough to exercise the
// ImageBlock path — most providers can describe the colour and size.
const redSquarePNG = "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAF0lEQVQImWP8z8Dwn4GBgYmBgYGBgQEAFQIBAQrU/tQAAAAASUVORK5CYII="

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	// Write the inline PNG to a temp file so we can exercise
	// ImageFileInput alongside the inline ImageBlock path.
	path := filepath.Join(os.TempDir(), "opencodesdk-example-red.png")

	data, err := base64.StdEncoding.DecodeString(redSquarePNG)
	if err != nil {
		exitf("decode test png: %v", err)
	}

	if werr := os.WriteFile(path, data, 0o600); werr != nil {
		exitf("write test png: %v", werr)
	}

	defer os.Remove(path)

	img, err := opencodesdk.ImageFileInput(path)
	if err != nil {
		exitf("ImageFileInput: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Blocks(...) is sugar for []ContentBlock{...}. For text-only
	// prompts you can use opencodesdk.Text("hi") instead.
	blocks := opencodesdk.Blocks(
		opencodesdk.TextBlock("Describe the attached image in one short sentence. Include the dominant colour."),
		img,
	)

	res, err := opencodesdk.QueryContent(ctx, blocks,
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		exitf("QueryContent: %v", err)
	}

	fmt.Println(res.AssistantText)
	fmt.Printf("\nstop: %s\n", res.StopReason)

	if res.Usage != nil {
		fmt.Printf("tokens: in=%d out=%d total=%d\n",
			res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.TotalTokens)
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
