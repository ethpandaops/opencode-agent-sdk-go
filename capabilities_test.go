package opencodesdk

import (
	"errors"
	"strings"
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

func TestCheckMCPCapabilities(t *testing.T) {
	httpEntry := acp.McpServer{Http: &acp.McpServerHttpInline{Type: "http", Name: "remote", Url: "http://example.com/mcp"}}
	sseEntry := acp.McpServer{Sse: &acp.McpServerSseInline{Type: "sse", Name: "events", Url: "http://example.com/sse"}}
	stdioEntry := acp.McpServer{Stdio: &acp.McpServerStdio{Name: "local", Command: "/bin/echo"}}

	tests := []struct {
		name    string
		caps    acp.McpCapabilities
		servers []acp.McpServer
		wantErr bool
	}{
		{name: "no servers is always allowed", caps: acp.McpCapabilities{}, servers: nil},
		{name: "stdio always allowed (spec baseline)", caps: acp.McpCapabilities{}, servers: []acp.McpServer{stdioEntry}},
		{name: "http allowed when declared", caps: acp.McpCapabilities{Http: true}, servers: []acp.McpServer{httpEntry}},
		{name: "http blocked when undeclared", caps: acp.McpCapabilities{}, servers: []acp.McpServer{httpEntry}, wantErr: true},
		{name: "sse allowed when declared", caps: acp.McpCapabilities{Sse: true}, servers: []acp.McpServer{sseEntry}},
		{name: "sse blocked when undeclared", caps: acp.McpCapabilities{Http: true}, servers: []acp.McpServer{sseEntry}, wantErr: true},
		{name: "mixed entries flagged on first offender", caps: acp.McpCapabilities{Http: true}, servers: []acp.McpServer{httpEntry, sseEntry}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient()
			c.agentCaps = acp.AgentCapabilities{McpCapabilities: tt.caps}

			err := c.checkMCPCapabilities(tt.servers)

			switch {
			case tt.wantErr && err == nil:
				t.Fatalf("expected error for caps=%+v servers=%+v, got nil", tt.caps, tt.servers)
			case tt.wantErr && !errors.Is(err, ErrCapabilityUnavailable):
				t.Fatalf("expected ErrCapabilityUnavailable, got %v", err)
			case !tt.wantErr && err != nil:
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCheckMCPCapabilitiesErrorIncludesServerName(t *testing.T) {
	c := newTestClient()
	c.agentCaps = acp.AgentCapabilities{McpCapabilities: acp.McpCapabilities{}}

	err := c.checkMCPCapabilities([]acp.McpServer{
		{Http: &acp.McpServerHttpInline{Type: "http", Name: "my-server", Url: "http://example.com"}},
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "my-server") {
		t.Fatalf("error should name the offending server, got %q", err.Error())
	}
}
