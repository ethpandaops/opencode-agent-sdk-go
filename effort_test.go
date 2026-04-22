package opencodesdk

import (
	"reflect"
	"testing"
)

func TestEffortPriority_KnownLevels(t *testing.T) {
	cases := map[Effort][]string{
		EffortNone:   {"none"},
		EffortLow:    {"low"},
		EffortMedium: {"medium", "low"},
		EffortHigh:   {"high", "medium"},
		EffortMax:    {"max", "xhigh", "high"},
	}

	for level, want := range cases {
		got := effortPriority(level)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("effortPriority(%q) = %v, want %v", level, got, want)
		}
	}
}

func TestEffortPriority_UnknownLevelReturnsNil(t *testing.T) {
	if got := effortPriority(""); got != nil {
		t.Fatalf("effortPriority(\"\") = %v, want nil", got)
	}

	if got := effortPriority(Effort("bogus")); got != nil {
		t.Fatalf("effortPriority(bogus) = %v, want nil", got)
	}
}

func TestChooseVariant_FirstMatchWins(t *testing.T) {
	const high = string(EffortHigh)

	available := []string{string(EffortLow), string(EffortMedium), high}
	if got := chooseVariant(available, []string{string(EffortMax), "xhigh", high}); got != high {
		t.Errorf("Max → got %q, want %q", got, high)
	}

	if got := chooseVariant(available, []string{high, string(EffortMedium)}); got != high {
		t.Errorf("High → got %q, want %q", got, high)
	}

	if got := chooseVariant(available, []string{string(EffortNone)}); got != "" {
		t.Errorf("None on no-none model → got %q, want \"\"", got)
	}
}

func TestChooseVariant_GPT5Family(t *testing.T) {
	available := []string{"none", "low", "medium", "high", "xhigh"}

	cases := map[Effort]string{
		EffortNone:   "none",
		EffortLow:    "low",
		EffortMedium: "medium",
		EffortHigh:   "high",
		EffortMax:    "xhigh",
	}

	for level, want := range cases {
		if got := chooseVariant(available, effortPriority(level)); got != want {
			t.Errorf("gpt-5.x %q → %q, want %q", level, got, want)
		}
	}
}

func TestChooseVariant_OpusMaxAvailable(t *testing.T) {
	available := []string{"low", "medium", "high", "xhigh", "max"}
	if got := chooseVariant(available, effortPriority(EffortMax)); got != "max" {
		t.Errorf("Opus EffortMax → %q, want max", got)
	}
}

func TestChooseVariant_NoVariantsReturnsEmpty(t *testing.T) {
	if got := chooseVariant(nil, effortPriority(EffortHigh)); got != "" {
		t.Errorf("got %q, want empty when no variants available", got)
	}
}

func TestModelHasExplicitVariant(t *testing.T) {
	cases := map[string]bool{
		"":                               false,
		"anthropic/claude-sonnet-4":      false,
		"anthropic/claude-sonnet-4/high": true,
		"anthropic/claude-opus-4-7/max":  true,
		"openai/gpt-5/none":              true,
		"singletoken":                    false,
		"openai/gpt-5":                   false,
		"x/y/z/extra":                    true,
	}

	for model, want := range cases {
		if got := modelHasExplicitVariant(model); got != want {
			t.Errorf("modelHasExplicitVariant(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestWithEffort_StoresLevel(t *testing.T) {
	o := apply([]Option{WithEffort(EffortHigh)})
	if o.effort != EffortHigh {
		t.Fatalf("WithEffort(High): options.effort = %q, want %q", o.effort, EffortHigh)
	}
}
