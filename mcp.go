package opencodesdk

import (
	"context"
)

// Tool is an in-process tool that the opencode agent can invoke. Tools
// are registered via WithSDKTools and served to opencode through a
// loopback HTTP MCP server spun up by the Client at Start time. The
// server name opencode sees is prefixed with `opencodesdk_`.
//
// Implementations must be safe for concurrent Execute calls — opencode
// may invoke the same tool from parallel tool-call chains.
type Tool interface {
	// Name is the unique identifier for this tool. Must be a valid MCP
	// tool name (alphanumeric + underscore/hyphen). The name is used
	// verbatim on the wire; opencode typically prefixes it with the
	// MCP server name before presenting to the agent.
	Name() string

	// Description is a human-readable explanation of what the tool
	// does, shown to the agent model.
	Description() string

	// InputSchema is a JSON Schema (2020-12 draft) describing the
	// expected input shape. Type must be "object". See
	// https://json-schema.org for the schema syntax.
	InputSchema() map[string]any

	// Execute runs the tool against the supplied arguments and returns
	// a result. Return a non-nil error to mark the call as failed;
	// opencode surfaces the error message to the agent.
	Execute(ctx context.Context, input map[string]any) (ToolResult, error)
}

// ToolResult is the structured output of a Tool.Execute call.
type ToolResult struct {
	// Text is the primary textual response shown to the agent. Usually
	// the only field callers need.
	Text string

	// Structured, when non-nil, is returned as structuredContent in the
	// MCP CallToolResult — useful for agents that can parse JSON.
	Structured any

	// IsError marks the result as an error. The MCP spec treats this
	// as semantic: the call "succeeded" at the transport layer but the
	// tool encountered an application-level problem. Use this for
	// recoverable errors that the agent should see and adapt to.
	IsError bool
}

// ToolFunc is a convenience shape for simple tools that don't need
// state beyond a closure.
type ToolFunc func(ctx context.Context, input map[string]any) (ToolResult, error)

// NewTool constructs a Tool from a name, description, JSON schema, and
// a handler function.
//
// Example:
//
//	t := opencodesdk.NewTool(
//	    "sum",
//	    "Add two numbers",
//	    map[string]any{
//	        "type": "object",
//	        "properties": map[string]any{
//	            "a": map[string]any{"type": "number"},
//	            "b": map[string]any{"type": "number"},
//	        },
//	        "required": []string{"a", "b"},
//	    },
//	    func(ctx context.Context, in map[string]any) (opencodesdk.ToolResult, error) {
//	        a := in["a"].(float64)
//	        b := in["b"].(float64)
//	        return opencodesdk.ToolResult{Text: fmt.Sprintf("%v", a+b)}, nil
//	    },
//	)
func NewTool(name, description string, schema map[string]any, fn ToolFunc) Tool {
	return &funcTool{name: name, description: description, schema: schema, fn: fn}
}

// WithSDKTools registers in-process tools that opencode can invoke.
// The SDK starts a loopback HTTP MCP server when Client.Start is
// called, protects it with a random bearer token, and declares it in
// every session/new's mcpServers list.
//
// If no tools are registered (WithSDKTools is never called or called
// with zero tools), no MCP bridge is started.
func WithSDKTools(tools ...Tool) Option {
	return func(o *options) {
		o.sdkTools = append(o.sdkTools, tools...)
	}
}

// funcTool is the concrete Tool implementation used by NewTool.
type funcTool struct {
	name        string
	description string
	schema      map[string]any
	fn          ToolFunc
}

func (t *funcTool) Name() string                { return t.name }
func (t *funcTool) Description() string         { return t.description }
func (t *funcTool) InputSchema() map[string]any { return t.schema }
func (t *funcTool) Execute(ctx context.Context, input map[string]any) (ToolResult, error) {
	return t.fn(ctx, input)
}
