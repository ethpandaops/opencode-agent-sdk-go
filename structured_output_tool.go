package opencodesdk

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/mcp/bridge"
)

// StructuredOutputToolName is the MCP tool name registered by the SDK
// when WithOutputSchema is set. Mirrors Claude Code's convention and
// opencode's own JS-SDK implementation (opencode PR #8161).
const StructuredOutputToolName = "StructuredOutput"

// structuredOutputCapturedAck is the text the StructuredOutput bridge
// tool returns to the agent after a successful call. It is
// deliberately short, directive, and does NOT echo the caller's
// input back. The earlier design returned the JSON-marshalled input
// as the tool result text; weaker models (e.g. qwen3.6 class) read
// that echo as the system rejecting their format and re-called the
// tool, looping until MaxTurns. An explicit "stop" acknowledgement
// steers the model to end the turn after one call.
const structuredOutputCapturedAck = "Structured output captured. Do not call `" +
	StructuredOutputToolName + "` again. End your turn now without any further prose, " +
	"tool calls, or commentary."

// structuredOutputRepeatAck is returned on the 2nd and subsequent
// StructuredOutput invocations within a single capture lifetime. It
// carries enough signal for the model to recover ("you already
// delivered; stop") without the echo that caused the original loop
// and without raising IsError — "completed again" is semantically
// wrong and triggers error-handling prompt paths on some harnesses.
const structuredOutputRepeatAck = "Structured output was already captured for this turn. " +
	"Do not call `" + StructuredOutputToolName + "` again. End your turn now."

// structuredOutputCapture is a client-scoped latching slot the
// implicit StructuredOutput tool writes to. Session.Prompt drains it
// after each turn and surfaces the payload via PromptResult.Meta.
//
// The slot is single-valued: concurrent prompts on the same Client
// that both trigger StructuredOutput may clobber each other. See
// WithOutputSchema's godoc for the concurrency caveat.
//
// calls counts invocations against the current stored payload so the
// bridge handler can distinguish the first successful capture from
// redundant retries and return a differently-worded acknowledgement
// for each.
type structuredOutputCapture struct {
	mu      sync.Mutex
	payload map[string]any
	calls   int
}

func newStructuredOutputCapture() *structuredOutputCapture {
	return &structuredOutputCapture{}
}

// store records the latest tool-call payload and returns the
// observed invocation count (1 for the first call, 2+ for repeats
// against the same, undrained slot). Overwrites the stored payload
// — "latest wins" semantics.
func (c *structuredOutputCapture) store(payload map[string]any) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.payload = payload
	c.calls++

	return c.calls
}

// drain returns the stored payload (or nil), clears the slot, and
// resets the per-turn invocation counter.
func (c *structuredOutputCapture) drain() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.payload
	c.payload = nil
	c.calls = 0

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
			calls := capture.store(in)

			if logger != nil {
				logger.DebugContext(ctx, "StructuredOutput captured",
					slog.Int("fields", len(in)),
					slog.Int("calls", calls),
				)
			}

			text := structuredOutputCapturedAck
			if calls > 1 {
				text = structuredOutputRepeatAck

				if logger != nil {
					logger.WarnContext(ctx, "StructuredOutput called more than once in the same turn",
						slog.Int("calls", calls),
					)
				}
			}

			return &bridge.ToolOutput{
				Text:       text,
				Structured: in,
			}, nil
		},
	}
}

// structuredOutputToolDescription mirrors the wording opencode's JS
// SDK uses server-side so prompts transfer unchanged, with an added
// "call exactly once then stop" clause for models that read the
// description but not the system-prompt nudge.
const structuredOutputToolDescription = "Deliver the final answer by calling this tool exactly once with a JSON object matching the tool's input schema. " +
	"Do not emit prose before or after the call. " +
	"Do not call this tool more than once per turn — the first call captures your answer, additional calls are discarded. " +
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

	return "You MUST deliver your final answer by calling the `" + StructuredOutputToolName + "` tool exactly once and then end your turn immediately. " +
		"Do not call the tool more than once. Do not respond with prose, markdown, code fences, or any text outside the tool call. " +
		"The tool's input schema defines the required shape:\n\n" + string(raw)
}
