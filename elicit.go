package opencodesdk

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/mcp/bridge"
)

// ErrElicitationUnavailable is returned by Elicit when the ctx does
// not carry a bridge-bound ToolSession. This happens when Elicit is
// called outside of a Tool.Execute invocation, or when the client is
// not running the SDK's loopback MCP bridge (i.e. WithSDKTools was
// not configured).
var ErrElicitationUnavailable = errors.New("opencodesdk: elicitation unavailable; call Elicit from within a Tool.Execute invocation")

// ElicitMode discriminates the two elicitation delivery modes
// defined by MCP. "form" renders a structured input form from
// RequestedSchema; "url" redirects the user to an external URL and
// resumes when the ElicitationCompleteNotification arrives.
type ElicitMode string

const (
	// ElicitModeForm requests a structured form response shaped by
	// ElicitParams.RequestedSchema. This is the default and the only
	// mode currently supported by opencode's MCP client.
	ElicitModeForm ElicitMode = "form"
	// ElicitModeURL asks the client to open ElicitParams.URL in a
	// browser. opencode may or may not honour this mode.
	ElicitModeURL ElicitMode = "url"
)

// ElicitParams is the request a tool sends back through the MCP
// bridge to ask opencode's user a question. Exactly one of
// RequestedSchema (for form mode) or URL (for url mode) should be
// populated.
type ElicitParams struct {
	// Message is the human-readable prompt displayed to the user.
	Message string
	// Mode selects between form and url delivery. Empty defaults to
	// form.
	Mode ElicitMode
	// RequestedSchema is a JSON Schema describing the form fields
	// expected in the response. Only consulted for form mode.
	RequestedSchema map[string]any
	// URL is the external URL to open when Mode is url.
	URL string
	// ElicitationID is an opaque token correlating this request with
	// its completion notification. Optional for form mode; required
	// for url mode.
	ElicitationID string
	// Meta passes arbitrary extensibility data through as the MCP
	// `_meta` block.
	Meta map[string]any
}

// ElicitResult is the response the MCP client produced. Action is
// one of "accept" (user provided Content), "decline" (user refused
// the prompt), or "cancel" (user dismissed the prompt without
// answering).
type ElicitResult struct {
	// Action is "accept", "decline", or "cancel".
	Action string
	// Content is the user-supplied form data. Populated when Action
	// is "accept" and Mode was form.
	Content map[string]any
	// Meta mirrors the `_meta` block from the MCP response.
	Meta map[string]any
}

// Elicit sends an MCP elicitation request back to opencode from
// within a Tool.Execute invocation. The ctx MUST be the same ctx the
// SDK passed into Execute — it carries the bridge-bound MCP session
// used to deliver the request.
//
// Returns ErrElicitationUnavailable when the SDK cannot locate a
// bound session (e.g. Elicit is called outside a Tool handler, or
// WithSDKTools was not configured).
//
// This is a thin wrapper over modelcontextprotocol/go-sdk's
// ServerSession.Elicit. If opencode's MCP client implementation does
// not support elicitation, the call fails with a JSON-RPC error
// surfaced as a normal Go error.
//
// Example usage inside a tool:
//
//	func (t *myTool) Execute(ctx context.Context, in map[string]any) (opencodesdk.ToolResult, error) {
//	    resp, err := opencodesdk.Elicit(ctx, opencodesdk.ElicitParams{
//	        Message: "Proceed with the destructive migration?",
//	        RequestedSchema: opencodesdk.SimpleSchema(map[string]string{
//	            "confirm": "string",
//	        }),
//	    })
//	    if err != nil {
//	        return opencodesdk.ErrorResult("could not ask user"), nil
//	    }
//	    if resp.Action != "accept" || resp.Content["confirm"] != "yes" {
//	        return opencodesdk.TextResult("aborted by user"), nil
//	    }
//	    // proceed…
//	}
func Elicit(ctx context.Context, params ElicitParams) (*ElicitResult, error) {
	sess := bridge.SessionFromContext(ctx)
	if sess == nil {
		return nil, ErrElicitationUnavailable
	}

	mode := string(params.Mode)
	if mode == "" {
		mode = string(ElicitModeForm)
	}

	req := &mcp.ElicitParams{
		Message:         params.Message,
		Mode:            mode,
		RequestedSchema: params.RequestedSchema,
		URL:             params.URL,
		ElicitationID:   params.ElicitationID,
		Meta:            params.Meta,
	}

	resp, err := sess.Elicit(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("opencodesdk: elicitation failed: %w", err)
	}

	out := &ElicitResult{
		Action:  resp.Action,
		Content: resp.Content,
		Meta:    resp.Meta,
	}

	return out, nil
}
