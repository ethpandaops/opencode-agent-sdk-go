package opencodesdk

import (
	"errors"
	"testing"

	"github.com/coder/acp-go-sdk"
)

type sampleStruct struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

const schemaTypeObject = "object"

func TestDecodeStructuredOutput_FromNotifications(t *testing.T) {
	result := &QueryResult{
		Notifications: []acp.SessionNotification{
			{
				Meta: map[string]any{
					"structuredOutput": map[string]any{
						"name":  "gopher",
						"count": float64(42),
					},
				},
			},
		},
	}

	got, err := DecodeStructuredOutput[sampleStruct](result)
	if err != nil {
		t.Fatalf("DecodeStructuredOutput: %v", err)
	}

	if got.Name != "gopher" || got.Count != 42 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestDecodeStructuredOutput_FromFencedAssistantText(t *testing.T) {
	result := &QueryResult{
		AssistantText: "Here is the result:\n```json\n{\"name\":\"go\",\"count\":1}\n```\n",
	}

	got, err := DecodeStructuredOutput[sampleStruct](result)
	if err != nil {
		t.Fatalf("DecodeStructuredOutput: %v", err)
	}

	if got.Name != "go" || got.Count != 1 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestDecodeStructuredOutput_FromBareBracedSpan(t *testing.T) {
	result := &QueryResult{
		AssistantText: "before {\"name\":\"x\",\"count\":7} after",
	}

	got, err := DecodeStructuredOutput[sampleStruct](result)
	if err != nil {
		t.Fatalf("DecodeStructuredOutput: %v", err)
	}

	if got.Name != "x" || got.Count != 7 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestDecodeStructuredOutput_NoPayload(t *testing.T) {
	_, err := DecodeStructuredOutput[sampleStruct](&QueryResult{AssistantText: "no structure here"})
	if !errors.Is(err, ErrStructuredOutputMissing) {
		t.Fatalf("expected ErrStructuredOutputMissing, got %v", err)
	}
}

func TestDecodeStructuredOutput_NilResult(t *testing.T) {
	_, err := DecodeStructuredOutput[sampleStruct](nil)
	if err == nil {
		t.Fatal("expected error for nil result")
	}
}

func TestDecodePromptResult(t *testing.T) {
	result := &PromptResult{
		Meta: map[string]any{
			"structuredOutput": map[string]any{"name": "p", "count": float64(3)},
		},
	}

	got, err := DecodePromptResult[sampleStruct](result)
	if err != nil {
		t.Fatalf("DecodePromptResult: %v", err)
	}

	if got.Name != "p" || got.Count != 3 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestDecodePromptResult_Missing(t *testing.T) {
	_, err := DecodePromptResult[sampleStruct](&PromptResult{Meta: map[string]any{}})
	if !errors.Is(err, ErrStructuredOutputMissing) {
		t.Fatal("expected ErrStructuredOutputMissing")
	}
}

func TestWithOutputSchema_PopulatesOptions(t *testing.T) {
	schema := SimpleSchema(map[string]string{"name": "string"})
	o := apply([]Option{WithOutputSchema(schema)})

	if o.outputSchema == nil || o.outputSchema["type"] != schemaTypeObject {
		t.Fatalf("expected schema to be stored, got %+v", o.outputSchema)
	}
}

func TestWithOutputSchema_UnwrapsJSONSchemaEnvelope(t *testing.T) {
	envelope := map[string]any{
		"type": "json_schema",
		"schema": map[string]any{
			"type": schemaTypeObject,
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
			"required": []string{"name"},
		},
	}

	o := apply([]Option{WithOutputSchema(envelope)})

	if o.outputSchema == nil {
		t.Fatal("expected inner schema stored, got nil")
	}

	if o.outputSchema["type"] != schemaTypeObject {
		t.Errorf("expected inner type=object, got %v", o.outputSchema["type"])
	}

	if _, wrapped := o.outputSchema["schema"]; wrapped {
		t.Errorf("envelope was not unwrapped: %+v", o.outputSchema)
	}
}

func TestWithOutputSchema_EnvelopeWithoutInnerIsCleared(t *testing.T) {
	envelope := map[string]any{"type": "json_schema"}

	o := apply([]Option{WithOutputSchema(envelope)})

	if o.outputSchema != nil {
		t.Errorf("expected nil for envelope with no inner, got %+v", o.outputSchema)
	}
}

func TestWithOutputSchema_BareSchemaPassesThrough(t *testing.T) {
	bare := map[string]any{
		"type":       schemaTypeObject,
		"properties": map[string]any{"x": map[string]any{"type": "string"}},
	}

	o := apply([]Option{WithOutputSchema(bare)})

	if o.outputSchema == nil || o.outputSchema["type"] != schemaTypeObject {
		t.Errorf("bare schema corrupted: %+v", o.outputSchema)
	}
}

func TestSessionNewMeta_NoSchema(t *testing.T) {
	if m := sessionNewMeta(apply(nil)); m != nil {
		t.Fatalf("expected nil meta, got %+v", m)
	}
}

func TestSessionNewMeta_WithSchema(t *testing.T) {
	o := apply([]Option{WithOutputSchema(map[string]any{"type": schemaTypeObject})})
	m := sessionNewMeta(o)

	if m == nil || m["structuredOutputSchema"] == nil {
		t.Fatalf("expected meta to carry schema, got %+v", m)
	}
}

func TestExtractJSONCandidate_Preferences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"fenced-json", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"fenced-bare", "```\n{\"a\":2}\n```", `{"a":2}`},
		{"braced-span", "hi {\"a\":3} there", `{"a":3}`},
		{"array-span", "before [1,2,3] after", `[1,2,3]`},
		{"none", "no json at all", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractJSONCandidate(tc.in)
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}
