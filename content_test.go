package opencodesdk

import (
	"testing"

	"github.com/coder/acp-go-sdk"
)

func TestContentBlockConstructors(t *testing.T) {
	text := TextBlock("hello")
	if text.Text == nil || text.Text.Text != "hello" {
		t.Fatalf("TextBlock did not populate .Text")
	}

	img := ImageBlock("iVBORw0K", mimePNG)
	if img.Image == nil || img.Image.MimeType != mimePNG {
		t.Fatalf("ImageBlock did not populate .Image")
	}

	audio := AudioBlock("UklGR", "audio/wav")
	if audio.Audio == nil || audio.Audio.MimeType != "audio/wav" {
		t.Fatalf("AudioBlock did not populate .Audio")
	}

	link := ResourceLinkBlock("name", "uri://x")
	if link.ResourceLink == nil || link.ResourceLink.Uri != "uri://x" {
		t.Fatalf("ResourceLinkBlock did not populate .ResourceLink")
	}
}

func TestConstantAliasesMatchACP(t *testing.T) {
	// Sanity: verify re-exported constants are the same underlying values
	// as the acp package. Prevents silent drift if acp renames.
	if StopReasonEndTurn != acp.StopReasonEndTurn {
		t.Fatalf("StopReasonEndTurn alias drift")
	}

	if ToolKindEdit != acp.ToolKindEdit {
		t.Fatalf("ToolKindEdit alias drift")
	}

	if ToolCallStatusCompleted != acp.ToolCallStatusCompleted {
		t.Fatalf("ToolCallStatusCompleted alias drift")
	}
}
