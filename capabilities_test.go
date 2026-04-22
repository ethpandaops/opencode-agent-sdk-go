package opencodesdk

import (
	"errors"
	"testing"

	"github.com/coder/acp-go-sdk"
)

func TestCheckPromptCapabilities(t *testing.T) {
	mkBlock := func(kind string) acp.ContentBlock {
		switch kind {
		case "image":
			return acp.ImageBlock("iVBORw0K", "image/png")
		case "audio":
			return acp.AudioBlock("UklGR", "audio/wav")
		case "resource":
			return acp.ResourceBlock(acp.EmbeddedResourceResource{})
		default:
			return acp.TextBlock("hi")
		}
	}

	tests := []struct {
		name    string
		caps    acp.PromptCapabilities
		block   string
		wantErr bool
	}{
		{name: "text always allowed", caps: acp.PromptCapabilities{}, block: "text"},
		{name: "image allowed when declared", caps: acp.PromptCapabilities{Image: true}, block: "image"},
		{name: "image blocked when undeclared", caps: acp.PromptCapabilities{}, block: "image", wantErr: true},
		{name: "audio allowed when declared", caps: acp.PromptCapabilities{Audio: true}, block: "audio"},
		{name: "audio blocked when undeclared", caps: acp.PromptCapabilities{}, block: "audio", wantErr: true},
		{name: "resource allowed when embeddedContext", caps: acp.PromptCapabilities{EmbeddedContext: true}, block: "resource"},
		{name: "resource blocked when undeclared", caps: acp.PromptCapabilities{}, block: "resource", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient()
			c.agentCaps = acp.AgentCapabilities{PromptCapabilities: tt.caps}

			err := c.checkPromptCapabilities([]acp.ContentBlock{mkBlock(tt.block)})

			switch {
			case tt.wantErr && err == nil:
				t.Fatalf("expected error for caps=%+v block=%q, got nil", tt.caps, tt.block)
			case tt.wantErr && !errors.Is(err, ErrCapabilityUnavailable):
				t.Fatalf("expected ErrCapabilityUnavailable, got %v", err)
			case !tt.wantErr && err != nil:
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCheckPromptCapabilitiesMixedBlocks(t *testing.T) {
	c := newTestClient()
	c.agentCaps = acp.AgentCapabilities{PromptCapabilities: acp.PromptCapabilities{Image: true}}

	blocks := []acp.ContentBlock{
		acp.TextBlock("hi"),
		acp.ImageBlock("iVBOR", "image/png"),
		acp.AudioBlock("UklGR", "audio/wav"),
	}

	err := c.checkPromptCapabilities(blocks)
	if !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("expected ErrCapabilityUnavailable from audio block, got %v", err)
	}
}
