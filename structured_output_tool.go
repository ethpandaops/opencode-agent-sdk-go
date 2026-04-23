package opencodesdk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/mcp/bridge"
)

// StructuredOutputToolName is the MCP tool name registered by the SDK
// when WithOutputSchema is set. Mirrors Claude Code's convention and
// opencode's own JS-SDK implementation (opencode PR #8161).
const StructuredOutputToolName = "StructuredOutput"

// structuredOutputCapture is a client-scoped latching slot the
// implicit StructuredOutput tool writes to. Session.Prompt drains it
// after each turn and surfaces the payload via PromptResult.Meta.
//
// The slot is single-valued: concurrent prompts on the same Client
// that both trigger StructuredOutput may clobber each other. See
// WithOutputSchema's godoc for the concurrency caveat.
type structuredOutputCapture struct {
	mu      sync.Mutex
	payload map[string]any
}

func newStructuredOutputCapture() *structuredOutputCapture {
	return &structuredOutputCapture{}
}

// store records the latest tool-call payload. Overwrites any previous
// value — "latest wins" semantics.
func (c *structuredOutputCapture) store(payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.payload = payload
}

// drain returns the stored payload (or nil) and clears the slot.
func (c *structuredOutputCapture) drain() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.payload
	c.payload = nil

	return p
}

// structuredOutputBridgeDef builds the implicit StructuredOutput tool
// definition. The input schema IS the caller-supplied schema from
// WithOutputSchema — providers validate tool_use inputs against this
// schema at the API boundary, giving us tool-use-level enforcement
// for free on Anthropic and OpenAI models.
func structuredOutputBridgeDef(schema map[string]any, capture *structuredOutputCapture, logger *slog.Logger) bridge.ToolDef {
	return bridge.ToolDef{
		Name:        StructuredOutputToolName,
		Description: structuredOutputToolDescription,
		Schema:      schema,
		Handler: func(ctx context.Context, in map[string]any) (*bridge.ToolOutput, error) {
			capture.store(in)

			if logger != nil {
				logger.DebugContext(ctx, "StructuredOutput captured", slog.Int("fields", len(in)))
			}

			text, err := json.Marshal(in)
			if err != nil {
				return nil, fmt.Errorf("opencodesdk: marshaling StructuredOutput payload: %w", err)
			}

			return &bridge.ToolOutput{
				Text:       string(text),
				Structured: in,
			}, nil
		},
	}
}

// structuredOutputToolDescription mirrors the wording opencode's JS
// SDK uses server-side so prompts transfer unchanged.
const structuredOutputToolDescription = "Deliver the final answer by calling this tool exactly once with a JSON object matching the tool's input schema. " +
	"Do not emit prose before or after the call. " +
	"When this tool is available it is the ONLY valid way to return your answer."

// structuredOutputInstructionText builds the system-style nudge
// prepended to every prompt when WithOutputSchema is set. The schema
// is stringified into the instruction so even models that disregard
// tool availability see the contract.
func structuredOutputInstructionText(schema map[string]any) string {
	raw, err := json.Marshal(schema)
	if err != nil {
		raw = []byte("{}")
	}

	return "You MUST deliver your final answer by calling the `" + StructuredOutputToolName + "` tool exactly once. " +
		"Do not respond with prose, markdown, code fences, or any text outside the tool call. " +
		"The tool's input schema defines the required shape:\n\n" + string(raw)
}
