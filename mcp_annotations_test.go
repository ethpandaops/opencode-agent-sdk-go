package opencodesdk

import (
	"context"
	"testing"
)

func TestNewTool_NoAnnotations(t *testing.T) {
	tool := NewTool("sum", "add", map[string]any{"type": "object"}, func(_ context.Context, _ map[string]any) (ToolResult, error) {
		return ToolResult{Text: "ok"}, nil
	})

	at, ok := tool.(annotatedTool)
	if !ok {
		t.Fatal("funcTool should implement annotatedTool")
	}

	if at.toolAnnotations() != nil {
		t.Fatalf("expected nil annotations, got %+v", at.toolAnnotations())
	}
}

func TestNewTool_WithAnnotations_Forwarded(t *testing.T) {
	ann := ToolAnnotations{
		Title:           "Read File",
		ReadOnlyHint:    true,
		DestructiveHint: BoolPtr(false),
		IdempotentHint:  true,
		OpenWorldHint:   BoolPtr(false),
	}

	tool := NewTool("read", "read a file", map[string]any{"type": "object"},
		func(_ context.Context, _ map[string]any) (ToolResult, error) {
			return ToolResult{Text: "ok"}, nil
		},
		WithToolAnnotations(ann),
	)

	at, ok := tool.(annotatedTool)
	if !ok {
		t.Fatal("funcTool should implement annotatedTool")
	}

	got := at.toolAnnotations()
	if got == nil {
		t.Fatal("expected annotations to be attached")
	}

	if got.Title != ann.Title {
		t.Errorf("Title = %q, want %q", got.Title, ann.Title)
	}

	if got.ReadOnlyHint != ann.ReadOnlyHint {
		t.Errorf("ReadOnlyHint = %v, want %v", got.ReadOnlyHint, ann.ReadOnlyHint)
	}

	if got.DestructiveHint == nil || *got.DestructiveHint != false {
		t.Errorf("DestructiveHint = %v, want *false", got.DestructiveHint)
	}

	if got.IdempotentHint != ann.IdempotentHint {
		t.Errorf("IdempotentHint = %v, want %v", got.IdempotentHint, ann.IdempotentHint)
	}

	if got.OpenWorldHint == nil || *got.OpenWorldHint != false {
		t.Errorf("OpenWorldHint = %v, want *false", got.OpenWorldHint)
	}
}

func TestToolAnnotationsFor_PropagatesToMcpShape(t *testing.T) {
	tool := NewTool("write", "write a file", map[string]any{"type": "object"},
		func(_ context.Context, _ map[string]any) (ToolResult, error) {
			return ToolResult{Text: "ok"}, nil
		},
		WithToolAnnotations(ToolAnnotations{
			Title:           "Write File",
			DestructiveHint: BoolPtr(true),
		}),
	)

	ann := toolAnnotationsFor(tool)
	if ann == nil {
		t.Fatal("expected non-nil mcp.ToolAnnotations")
	}

	if ann.Title != "Write File" {
		t.Errorf("Title = %q", ann.Title)
	}

	if ann.DestructiveHint == nil || !*ann.DestructiveHint {
		t.Errorf("DestructiveHint = %v, want *true", ann.DestructiveHint)
	}
}

func TestToolAnnotationsFor_NilWhenMissing(t *testing.T) {
	tool := NewTool("noop", "", map[string]any{"type": "object"}, func(_ context.Context, _ map[string]any) (ToolResult, error) {
		return ToolResult{}, nil
	})

	if got := toolAnnotationsFor(tool); got != nil {
		t.Fatalf("expected nil annotations, got %+v", got)
	}
}

func TestBoolPtr(t *testing.T) {
	if BoolPtr(true) == nil || !*BoolPtr(true) {
		t.Fatal("BoolPtr(true) should point to true")
	}

	if BoolPtr(false) == nil || *BoolPtr(false) {
		t.Fatal("BoolPtr(false) should point to false")
	}
}
