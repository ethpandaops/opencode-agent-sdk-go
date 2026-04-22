// Demonstrates sending a multimodal prompt to opencode via ACP
// content blocks. The example builds a prompt with mixed text + a
// tiny inline base64-encoded PNG image, then asks the agent to
// describe what it sees.
//
// ACP content-block constructors (re-exported from coder/acp-go-sdk):
//
//   - acp.TextBlock(str)
//   - acp.ImageBlock(base64Data, mimeType)
//   - acp.AudioBlock(base64Data, mimeType)
//   - acp.ResourceLinkBlock(name, uri)
//   - acp.ResourceBlock(embedded)
//
// Image support requires the agent to advertise the "image" prompt
// capability; opencode does when attached to a multimodal-capable
// model.
//
//	go run ./examples/multimodal_input
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// A 2x2 red PNG, base64-encoded. Just enough to exercise the
// ImageBlock path — most providers can describe the colour and size.
const redSquarePNG = "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAF0lEQVQImWP8z8Dwn4GBgYmBgYGBgQEAFQIBAQrU/tQAAAAASUVORK5CYII="

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	// Sanity-check the literal is decodable; prevents silent corruption
	// if someone edits it inline.
	if _, err := base64.StdEncoding.DecodeString(redSquarePNG); err != nil {
		fmt.Fprintf(os.Stderr, "image literal is not valid base64: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
		sess, err := c.NewSession(ctx)
		if err != nil {
			return fmt.Errorf("new session: %w", err)
		}

		go streamAssistantText(sess.Updates())

		blocks := []acp.ContentBlock{
			acp.TextBlock("Describe the attached image in one short sentence. Include the dominant colour."),
			acp.ImageBlock(redSquarePNG, "image/png"),
		}

		res, err := sess.Prompt(ctx, blocks...)
		if err != nil {
			return fmt.Errorf("prompt: %w", err)
		}

		time.Sleep(100 * time.Millisecond)

		fmt.Printf("\n\nstop: %s\n", res.StopReason)

		return nil
	},
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WithClient: %v\n", err)
		os.Exit(1)
	}
}

func streamAssistantText(ch <-chan acp.SessionNotification) {
	for n := range ch {
		if n.Update.AgentMessageChunk == nil {
			continue
		}

		if n.Update.AgentMessageChunk.Content.Text != nil {
			fmt.Print(n.Update.AgentMessageChunk.Content.Text.Text)
		}
	}
}
