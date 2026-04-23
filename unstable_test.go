package opencodesdk

import "testing"

func TestOpencodeVariant_Full(t *testing.T) {
	meta := map[string]any{
		"opencode": map[string]any{
			"modelId":           "anthropic/claude-sonnet-4",
			"variant":           "high",
			"availableVariants": []any{"low", "high", "maximum"},
		},
	}

	info, ok := OpencodeVariant(meta)
	if !ok {
		t.Fatalf("expected ok=true")
	}

	if info.ModelId != "anthropic/claude-sonnet-4" {
		t.Fatalf("unexpected modelId: %s", info.ModelId)
	}

	if info.Variant != "high" {
		t.Fatalf("unexpected variant: %s", info.Variant)
	}

	if len(info.AvailableVariants) != 3 || info.AvailableVariants[1] != "high" {
		t.Fatalf("unexpected availableVariants: %v", info.AvailableVariants)
	}
}

func TestOpencodeVariant_Missing(t *testing.T) {
	if _, ok := OpencodeVariant(nil); ok {
		t.Fatalf("expected ok=false on nil meta")
	}

	if _, ok := OpencodeVariant(map[string]any{"other": "x"}); ok {
		t.Fatalf("expected ok=false when opencode key absent")
	}

	if _, ok := OpencodeVariant(map[string]any{"opencode": "not-a-map"}); ok {
		t.Fatalf("expected ok=false when opencode value not a map")
	}
}

func TestOpencodeVariant_EmptyReturnsFalse(t *testing.T) {
	meta := map[string]any{"opencode": map[string]any{}}
	if _, ok := OpencodeVariant(meta); ok {
		t.Fatalf("expected ok=false for empty opencode block")
	}
}

func TestOpencodeVariant_NonStringVariantEntryDropped(t *testing.T) {
	meta := map[string]any{
		"opencode": map[string]any{
			"modelId":           "x",
			"availableVariants": []any{"a", 42, "b"},
		},
	}

	info, ok := OpencodeVariant(meta)
	if !ok {
		t.Fatalf("expected ok=true")
	}

	if len(info.AvailableVariants) != 2 {
		t.Fatalf("expected non-string entry to be dropped, got %v", info.AvailableVariants)
	}
}
