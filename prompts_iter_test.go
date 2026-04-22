package opencodesdk

import (
	"slices"
	"testing"

	"github.com/coder/acp-go-sdk"
)

// collect drains an iter.Seq[[]acp.ContentBlock] into a slice so tests can
// assert on the materialised output.
func collectPromptIter(seq func(yield func([]acp.ContentBlock) bool)) [][]acp.ContentBlock {
	var out [][]acp.ContentBlock

	for p := range seq {
		out = append(out, p)
	}

	return out
}

func TestPromptsFromStrings_WrapsEach(t *testing.T) {
	got := collectPromptIter(PromptsFromStrings([]string{"a", "b", "c"}))
	if len(got) != 3 {
		t.Fatalf("got %d prompts, want 3", len(got))
	}

	for i, want := range []string{"a", "b", "c"} {
		if len(got[i]) != 1 {
			t.Fatalf("prompt %d: len = %d, want 1", i, len(got[i]))
		}

		if got[i][0].Text == nil || got[i][0].Text.Text != want {
			actual := ""
			if got[i][0].Text != nil {
				actual = got[i][0].Text.Text
			}

			t.Fatalf("prompt %d text = %q, want %q", i, actual, want)
		}
	}
}

func TestPromptsFromStrings_Empty(t *testing.T) {
	if got := collectPromptIter(PromptsFromStrings(nil)); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
}

func TestPromptsFromSlice_PreservesOrderAndBlocks(t *testing.T) {
	input := [][]acp.ContentBlock{
		{TextBlock("first")},
		{TextBlock("second-a"), TextBlock("second-b")},
	}

	got := collectPromptIter(PromptsFromSlice(input))
	if !slices.EqualFunc(got[0], input[0], blockTextEq) {
		t.Fatalf("prompt 0 drift")
	}

	if !slices.EqualFunc(got[1], input[1], blockTextEq) {
		t.Fatalf("prompt 1 drift")
	}
}

func TestPromptsFromChannel_StopsAtClose(t *testing.T) {
	ch := make(chan []acp.ContentBlock, 2)

	ch <- []acp.ContentBlock{TextBlock("x")}

	ch <- []acp.ContentBlock{TextBlock("y")}

	close(ch)

	got := collectPromptIter(PromptsFromChannel(ch))
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestPromptsFromChannel_RespectsYieldBreak(t *testing.T) {
	ch := make(chan []acp.ContentBlock, 4)
	for range 4 {
		ch <- []acp.ContentBlock{TextBlock("p")}
	}

	close(ch)

	got := 0

	for range PromptsFromChannel(ch) {
		got++

		if got == 2 {
			break
		}
	}

	if got != 2 {
		t.Fatalf("consumed %d, want 2", got)
	}
}

func TestSinglePrompt_YieldsOnce(t *testing.T) {
	got := collectPromptIter(SinglePrompt(TextBlock("only")))
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
}

func TestSinglePrompt_EmptyNoYield(t *testing.T) {
	if got := collectPromptIter(SinglePrompt()); got != nil {
		t.Fatalf("expected no yield, got %v", got)
	}
}

func blockTextEq(a, b acp.ContentBlock) bool {
	if a.Text == nil || b.Text == nil {
		return a.Text == b.Text
	}

	return a.Text.Text == b.Text.Text
}
