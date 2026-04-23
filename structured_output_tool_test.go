package opencodesdk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStructuredOutputCapture_StoreDrain(t *testing.T) {
	c := newStructuredOutputCapture()

	if got := c.drain(); got != nil {
		t.Fatalf("drain on fresh capture = %v, want nil", got)
	}

	c.store(map[string]any{"answer": "4"})

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

	c.store(map[string]any{"v": 1})
	c.store(map[string]any{"v": 2})

	got := c.drain()
	if got == nil || got["v"] != 2 {
		t.Fatalf("drain = %v, want v=2 (latest wins)", got)
	}
}

func TestStructuredOutputBridgeDef_CapturesAndEchoes(t *testing.T) {
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

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out.Text), &decoded); err != nil {
		t.Fatalf("unmarshal out.Text: %v", err)
	}

	if decoded["answer"] != "4" {
		t.Errorf("out.Text answer = %v, want 4", decoded["answer"])
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
