// Example elicitation_callback demonstrates the agent-initiated
// elicitation path: WithOnElicitation receives elicitation/create
// requests from the agent and returns an Accept / Decline / Cancel
// response. This is the mirror of the tool-side Elicit helper shown
// in examples/elicitation (which goes tool → opencode MCP client →
// user). Here the agent asks the SDK user directly via ACP's
// unstable elicitation/create method.
//
// opencode 1.14.20 does NOT currently emit elicitation/create over
// ACP — the schema reserves this method under the unstable namespace.
// Wiring the callback is forward-compatible: when opencode (or
// another ACP agent) starts emitting it, this handler will receive
// the requests. Until then the callback never fires.
//
// Run:
//
//	go run ./examples/elicitation_callback
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	acp "github.com/coder/acp-go-sdk"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cwd, _ := os.Getwd()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	res, err := opencodesdk.Query(ctx, "Reply with one short sentence introducing yourself.",
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		// Auto-accept form elicitations with an empty content map, log
		// URL elicitations and decline them. A real UI would render
		// the form from req.Form.RequestedSchema or open req.Url.Url
		// in a browser.
		opencodesdk.WithOnElicitation(func(_ context.Context, req acp.UnstableCreateElicitationRequest) (acp.UnstableCreateElicitationResponse, error) {
			switch {
			case req.Form != nil:
				logger.Info("elicitation/create form",
					slog.String("message", req.Form.Message),
					slog.Any("schema", req.Form.RequestedSchema),
				)

				return acp.UnstableCreateElicitationResponse{
					Accept: &acp.UnstableCreateElicitationAccept{
						Action:  "accept",
						Content: map[string]any{},
					},
				}, nil
			case req.Url != nil:
				logger.Info("elicitation/create url",
					slog.String("message", req.Url.Message),
					slog.String("url", req.Url.Url),
				)

				return opencodesdk.DeclineElicitation(ctx, req)
			default:
				return opencodesdk.DeclineElicitation(ctx, req)
			}
		}),
		// URL-mode elicitations complete out-of-band (user closes the
		// browser window etc.). opencode sends elicitation/complete
		// notifications to tell us the flow ended on its side.
		opencodesdk.WithOnElicitationComplete(func(_ context.Context, params acp.UnstableCompleteElicitationNotification) {
			logger.Info("elicitation/complete", slog.String("elicitation_id", string(params.ElicitationId)))
		}),
	)
	if err != nil {
		exitf("Query: %v", err)
	}

	fmt.Printf("stop: %s\n%s\n", res.StopReason, res.AssistantText)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
