package opencodesdk

import (
	"context"
	"strings"
	"testing"
)

func TestStructuredOutputCapture_StoreDrain(t *testing.T) {
	c := newStructuredOutputCapture()

	if got := c.drain(); got != nil {
		t.Fatalf("drain on fresh capture = %v, want nil", got)
	}

	if n := c.store(map[string]any{"answer": "4"}); n != 1 {
		t.Fatalf("first store returned calls = %d, want 1", n)
	}

	got := c.drain()
	if got == nil || got["answer"] != "4" {
		t.Fatalf("drain after store = %v, want answer=4", got)
	}

	if leftover := c.drain(); leftover != nil {
		t.Fatalf("second drain = %v, want nil (slot should clear)", leftover)
	}
}

func TestStructuredOutputCapture_LatestWins(t *testing.T) {
	c := newStructuredOutputCapture()

	if n := c.store(map[string]any{"v": 1}); n != 1 {
		t.Fatalf("first store returned calls = %d, want 1", n)
	}

	if n := c.store(map[string]any{"v": 2}); n != 2 {
		t.Fatalf("second store returned calls = %d, want 2 (counter persists until drain)", n)
	}

	got := c.drain()
	if got == nil || got["v"] != 2 {
		t.Fatalf("drain = %v, want v=2 (latest wins)", got)
	}

	// Drain also resets the counter so the next turn starts fresh.
	if n := c.store(map[string]any{"v": 3}); n != 1 {
		t.Fatalf("store after drain returned calls = %d, want 1 (drain must reset)", n)
	}
}

// TestStructuredOutputBridgeDef_CapturesAndAcknowledges exercises the
// happy path: the handler stores the caller's payload, returns a
// short acknowledgement (NOT the JSON-echoed input that earlier
// versions sent back — that loop-trapped weaker models like
// qwen3.6-class into re-calling the tool forever), and exposes the
// payload via out.Structured for programmatic consumers.
func TestStructuredOutputBridgeDef_CapturesAndAcknowledges(t *testing.T) {
	schema := map[string]any{
		"type": schemaTypeObject,
		"properties": map[string]any{
			"answer":     map[string]any{"type": "string"},
			"confidence": map[string]any{"type": "number"},
		},
		"required": []string{"answer"},
	}

	capture := newStructuredOutputCapture()

	def := structuredOutputBridgeDef(schema, capture, nil)

	if def.Name != StructuredOutputToolName {
		t.Fatalf("def.Name = %q, want %q", def.Name, StructuredOutputToolName)
	}

	if def.Schema["type"] != schemaTypeObject {
		t.Errorf("def.Schema.type = %v, want %s", def.Schema["type"], schemaTypeObject)
	}

	input := map[string]any{"answer": "4", "confidence": float64(99)}

	out, err := def.Handler(t.Context(), input)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	got := capture.drain()
	if got == nil || got["answer"] != "4" {
		t.Errorf("capture did not record input: %v", got)
	}

	if out.Text != structuredOutputCapturedAck {
		t.Errorf("out.Text = %q, want capture acknowledgement (no input echo)", out.Text)
	}

	if !strings.Contains(out.Text, "End your turn") {
		t.Errorf("out.Text missing end-turn directive: %q", out.Text)
	}

	structured, ok := out.Structured.(map[string]any)
	if !ok {
		t.Fatalf("out.Structured = %T, want map[string]any", out.Structured)
	}

	if structured["answer"] != "4" {
		t.Errorf("out.Structured.answer = %v, want 4", structured["answer"])
	}
}

// TestStructuredOutputBridgeDef_RepeatCallReturnsDistinctAck asserts
// the 2nd+ call within a single turn gets a differently-worded
// acknowledgement so the model can recover from a wrong retry loop
// instead of repeatedly seeing a happy-path message. Does NOT set
// IsError — flipping that on a successful capture would trigger
// error-handling prompt paths on some harnesses.
func TestStructuredOutputBridgeDef_RepeatCallReturnsDistinctAck(t *testing.T) {
	schema := map[string]any{"type": schemaTypeObject}
	capture := newStructuredOutputCapture()
	def := structuredOutputBridgeDef(schema, capture, nil)

	first, err := def.Handler(t.Context(), map[string]any{"answer": "4"})
	if err != nil {
		t.Fatalf("first handler: %v", err)
	}

	if first.Text != structuredOutputCapturedAck {
		t.Errorf("first call text = %q, want captured ack", first.Text)
	}

	if first.IsError {
		t.Errorf("first call IsError = true, want false")
	}

	second, err := def.Handler(t.Context(), map[string]any{"answer": "4"})
	if err != nil {
		t.Fatalf("second handler: %v", err)
	}

	if second.Text != structuredOutputRepeatAck {
		t.Errorf("second call text = %q, want repeat ack", second.Text)
	}

	if second.IsError {
		t.Errorf("second call IsError = true, want false (captured-but-repeat is not an error)")
	}

	if !strings.Contains(second.Text, "already captured") {
		t.Errorf("repeat ack does not mention prior capture: %q", second.Text)
	}
}

func TestAssembleBridgeTools_AppendsStructuredOutput(t *testing.T) {
	schema := map[string]any{"type": schemaTypeObject, "properties": map[string]any{"x": map[string]any{"type": "string"}}}

	c := &client{opts: apply([]Option{WithOutputSchema(schema)})}

	defs, err := c.assembleBridgeTools()
	if err != nil {
		t.Fatalf("assembleBridgeTools: %v", err)
	}

	if len(defs) != 1 || defs[0].Name != StructuredOutputToolName {
		t.Fatalf("defs = %+v, want one StructuredOutput entry", defs)
	}

	if c.structuredOutput == nil {
		t.Errorf("client.structuredOutput should have been initialized")
	}
}

func TestAssembleBridgeTools_StructuredOutputCollision(t *testing.T) {
	schema := map[string]any{"type": schemaTypeObject}
	userTool := NewTool(StructuredOutputToolName, "clash", map[string]any{"type": schemaTypeObject}, nil)

	c := &client{opts: apply([]Option{
		WithSDKTools(userTool),
		WithOutputSchema(schema),
	})}

	_, err := c.assembleBridgeTools()
	if err == nil || !strings.Contains(err.Error(), StructuredOutputToolName) {
		t.Fatalf("expected collision error mentioning %q, got %v", StructuredOutputToolName, err)
	}
}

func TestAssembleBridgeTools_BothImplicitTools(t *testing.T) {
	schema := map[string]any{"type": schemaTypeObject}

	c := &client{opts: apply([]Option{
		WithOutputSchema(schema),
		WithOnUserInput(func(_ context.Context, _ *UserInputRequest) (*UserInputResponse, error) {
			return &UserInputResponse{}, nil
		}),
	})}

	defs, err := c.assembleBridgeTools()
	if err != nil {
		t.Fatalf("assembleBridgeTools: %v", err)
	}

	names := make(map[string]bool, len(defs))
	for _, d := range defs {
		names[d.Name] = true
	}

	if !names[AskUserQuestionToolName] || !names[StructuredOutputToolName] {
		t.Errorf("expected both implicit tools; got %v", names)
	}
}

func TestStructuredOutputInstructionText_IncludesSchema(t *testing.T) {
	schema := map[string]any{
		"type": schemaTypeObject,
		"properties": map[string]any{
			"magic": map[string]any{"type": "string"},
		},
	}

	text := structuredOutputInstructionText(schema)

	if !strings.Contains(text, StructuredOutputToolName) {
		t.Errorf("instruction missing tool name: %s", text)
	}

	if !strings.Contains(text, "magic") {
		t.Errorf("instruction missing stringified schema: %s", text)
	}
}
