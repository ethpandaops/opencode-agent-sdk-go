package opencodesdk

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseUserInputRequest_HappyPath(t *testing.T) {
	input := map[string]any{
		"questions": []any{
			map[string]any{
				"id":       "q1",
				"header":   "Language",
				"question": "Pick a language.",
				"options": []any{
					map[string]any{"label": "Go", "description": "gopher"},
					map[string]any{"label": "Rust"},
				},
			},
			map[string]any{
				"question":     "Any secrets?",
				"isSecret":     true,
				"multi_select": true,
			},
		},
	}

	req, err := parseUserInputRequest(input)
	if err != nil {
		t.Fatalf("parseUserInputRequest: %v", err)
	}

	if len(req.Questions) != 2 {
		t.Fatalf("got %d questions, want 2", len(req.Questions))
	}

	q1 := req.Questions[0]
	if q1.ID != "q1" || q1.Header != "Language" || q1.Question != "Pick a language." {
		t.Errorf("q1 fields unexpected: %+v", q1)
	}

	if len(q1.Options) != 2 || q1.Options[0].Label != "Go" || q1.Options[0].Description != "gopher" {
		t.Errorf("q1 options unexpected: %+v", q1.Options)
	}

	q2 := req.Questions[1]
	if q2.ID != "question_2" {
		t.Errorf("q2 default ID = %q, want question_2", q2.ID)
	}

	if !q2.IsSecret || !q2.MultiSelect {
		t.Errorf("q2 flags not honored: %+v", q2)
	}
}

func TestParseUserInputRequest_Errors(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		wantErr string
	}{
		{
			name:    "nil input",
			input:   nil,
			wantErr: "missing tool input",
		},
		{
			name:    "no questions key",
			input:   map[string]any{},
			wantErr: "missing questions",
		},
		{
			name:    "empty questions array",
			input:   map[string]any{"questions": []any{}},
			wantErr: "missing questions",
		},
		{
			name:    "questions entries are not objects",
			input:   map[string]any{"questions": []any{"nope"}},
			wantErr: "no parseable questions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseUserInputRequest(tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestBuildUserInputResultPayload(t *testing.T) {
	req := &UserInputRequest{Questions: []Question{
		{ID: "q1"},
		{ID: "q2"},
	}}

	resp := &UserInputResponse{Answers: map[string]*UserInputAnswer{
		"q1": {Answers: []string{"Go", ""}},   // empty values dropped
		"q2": {Answers: []string{}},           // empty list dropped entirely
		"q3": {Answers: []string{"stranger"}}, // not in req.Questions; dropped
	}}

	payload, err := buildUserInputResultPayload(req, resp)
	if err != nil {
		t.Fatalf("buildUserInputResultPayload: %v", err)
	}

	answers, ok := payload["answers"].(map[string]any)
	if !ok {
		t.Fatalf("payload.answers shape: %T", payload["answers"])
	}

	if !reflect.DeepEqual(answers["q1"], []string{"Go"}) {
		t.Errorf("q1 answers = %v, want [Go]", answers["q1"])
	}

	if _, present := answers["q2"]; present {
		t.Errorf("q2 should have been dropped (empty)")
	}

	if _, present := answers["q3"]; present {
		t.Errorf("q3 should have been dropped (unknown id)")
	}
}

func TestBuildUserInputResultPayload_EmptyResponse(t *testing.T) {
	req := &UserInputRequest{Questions: []Question{{ID: "q1"}}}

	if _, err := buildUserInputResultPayload(req, nil); err == nil {
		t.Fatalf("expected error for nil response")
	}

	empty := &UserInputResponse{Answers: map[string]*UserInputAnswer{}}
	if _, err := buildUserInputResultPayload(req, empty); err == nil {
		t.Fatalf("expected error for empty answers map")
	}

	onlyUnknown := &UserInputResponse{Answers: map[string]*UserInputAnswer{
		"other": {Answers: []string{"x"}},
	}}
	if _, err := buildUserInputResultPayload(req, onlyUnknown); err == nil {
		t.Fatalf("expected error when no answers match any question id")
	}
}

func TestWithOnUserInput_StoresCallback(t *testing.T) {
	called := false
	cb := func(_ context.Context, _ *UserInputRequest) (*UserInputResponse, error) {
		called = true

		return &UserInputResponse{}, nil
	}

	o := apply([]Option{WithOnUserInput(cb)})

	if o.onUserInput == nil {
		t.Fatalf("WithOnUserInput did not set onUserInput")
	}

	_, _ = o.onUserInput(t.Context(), nil)

	if !called {
		t.Fatalf("stored callback was not the one supplied")
	}
}

func TestAssembleBridgeTools_AppendsAskUserQuestion(t *testing.T) {
	cb := func(_ context.Context, _ *UserInputRequest) (*UserInputResponse, error) {
		return &UserInputResponse{}, nil
	}

	c := &client{opts: apply([]Option{WithOnUserInput(cb)})}

	defs, err := c.assembleBridgeTools()
	if err != nil {
		t.Fatalf("assembleBridgeTools: %v", err)
	}

	if len(defs) != 1 || defs[0].Name != AskUserQuestionToolName {
		t.Fatalf("defs = %+v, want exactly one AskUserQuestion entry", defs)
	}

	schema := defs[0].Schema
	if schema["type"] != "object" {
		t.Errorf("schema.type = %v, want object", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties shape: %T", schema["properties"])
	}

	if _, ok := props["questions"]; !ok {
		t.Errorf("schema missing questions property: %+v", schema)
	}
}

func TestAssembleBridgeTools_CollisionError(t *testing.T) {
	cb := func(_ context.Context, _ *UserInputRequest) (*UserInputResponse, error) {
		return &UserInputResponse{}, nil
	}

	userTool := NewTool(AskUserQuestionToolName, "user's own", map[string]any{"type": "object"}, nil)

	c := &client{opts: apply([]Option{
		WithSDKTools(userTool),
		WithOnUserInput(cb),
	})}

	_, err := c.assembleBridgeTools()
	if err == nil {
		t.Fatalf("expected collision error, got nil")
	}

	if !strings.Contains(err.Error(), AskUserQuestionToolName) {
		t.Errorf("error does not mention tool name: %v", err)
	}
}

func TestAssembleBridgeTools_NoCallbackNoTool(t *testing.T) {
	c := &client{opts: apply(nil)}

	defs, err := c.assembleBridgeTools()
	if err != nil {
		t.Fatalf("assembleBridgeTools: %v", err)
	}

	if len(defs) != 0 {
		t.Errorf("defs = %d, want 0 when WithOnUserInput not set", len(defs))
	}
}

func TestAskUserQuestionHandler_EndToEnd(t *testing.T) {
	var captured *UserInputRequest

	cb := func(_ context.Context, req *UserInputRequest) (*UserInputResponse, error) {
		captured = req

		return &UserInputResponse{Answers: map[string]*UserInputAnswer{
			"q1": {Answers: []string{"Go"}},
		}}, nil
	}

	def := askUserQuestionBridgeDef(cb, nil)

	out, err := def.Handler(t.Context(), map[string]any{
		"questions": []any{
			map[string]any{
				"id":       "q1",
				"question": "Pick one",
				"options":  []any{map[string]any{"label": "Go"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if captured == nil || len(captured.Questions) != 1 || captured.Questions[0].ID != "q1" {
		t.Fatalf("callback did not receive parsed request: %+v", captured)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out.Text), &decoded); err != nil {
		t.Fatalf("unmarshal out.Text: %v", err)
	}

	answers, ok := decoded["answers"].(map[string]any)
	if !ok {
		t.Fatalf("decoded.answers shape: %T", decoded["answers"])
	}

	got, ok := answers["q1"].([]any)
	if !ok || len(got) != 1 || got[0] != "Go" {
		t.Errorf("answers.q1 = %v, want [Go]", got)
	}
}

func TestAskUserQuestionHandler_CallbackError(t *testing.T) {
	sentinel := errors.New("boom")

	cb := func(_ context.Context, _ *UserInputRequest) (*UserInputResponse, error) {
		return nil, sentinel
	}

	def := askUserQuestionBridgeDef(cb, nil)

	_, err := def.Handler(t.Context(), map[string]any{
		"questions": []any{map[string]any{"question": "?"}},
	})
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel error, got %v", err)
	}
}

func TestAskUserQuestionHandler_ParseError(t *testing.T) {
	cb := func(_ context.Context, _ *UserInputRequest) (*UserInputResponse, error) {
		t.Fatalf("callback must not be invoked on parse error")

		return &UserInputResponse{}, nil
	}

	def := askUserQuestionBridgeDef(cb, nil)

	_, err := def.Handler(t.Context(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "parsing AskUserQuestion input") {
		t.Fatalf("expected parse error, got %v", err)
	}
}
