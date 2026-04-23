package opencodesdk

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// TextResult returns a ToolResult whose primary payload is text. The
// equivalent of building `ToolResult{Text: text}` inline, kept for
// parity with the claude/codex sister SDKs so tool authors can write
// `return opencodesdk.TextResult("ok"), nil`.
func TextResult(text string) ToolResult {
	return ToolResult{Text: text}
}

// ErrorResult returns a ToolResult flagged as an application-level
// error. The agent sees the message as a failed tool call but the
// transport call itself succeeded — use this for recoverable errors
// the agent should adapt to. For catastrophic failures, return an
// error from the Tool.Execute instead.
func ErrorResult(message string) ToolResult {
	return ToolResult{Text: message, IsError: true}
}

// ImageResult returns a ToolResult whose text payload is a
// human-readable placeholder and whose Structured payload carries the
// image bytes base64-encoded alongside the mime type. opencode's MCP
// bridge forwards the Structured block as structuredContent and
// agents that understand image content negotiate from there.
//
// The mimeType must be a full image/* type, e.g. "image/png".
func ImageResult(data []byte, mimeType string) ToolResult {
	return ToolResult{
		Text: fmt.Sprintf("[image %s %d bytes]", mimeType, len(data)),
		Structured: map[string]any{
			"type":     "image",
			"data":     base64.StdEncoding.EncodeToString(data),
			"mimeType": mimeType,
		},
	}
}

// ParseArguments decodes a Tool.Execute input map into dst. dst must
// be a pointer to a struct (or any json-compatible value). The map is
// round-tripped through JSON so json tags, `omitempty`, and
// json.Unmarshaler implementations on dst are honored.
//
// Returns a descriptive error when dst is not a pointer or when any
// individual field fails to decode — callers typically return the
// error from their Execute as-is.
func ParseArguments(in map[string]any, dst any) error {
	if dst == nil {
		return fmt.Errorf("opencodesdk: ParseArguments: dst must be non-nil")
	}

	raw, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("opencodesdk: ParseArguments: marshal input: %w", err)
	}

	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("opencodesdk: ParseArguments: decode into %T: %w", dst, err)
	}

	return nil
}

// SimpleSchema builds a minimal JSON-schema object from a
// field-name → type-string map. Supported type strings (case
// insensitive): "string", "int", "int64", "integer", "float",
// "float64", "number", "bool", "boolean", "[]string", "object",
// "any". Every listed field is marked required; for a looser shape,
// build the schema inline.
//
// Example:
//
//	schema := opencodesdk.SimpleSchema(map[string]string{
//	    "path":    "string",
//	    "recurse": "bool",
//	    "limit":   "int",
//	})
func SimpleSchema(fields map[string]string) map[string]any {
	props := make(map[string]any, len(fields))
	required := make([]string, 0, len(fields))

	for name, kind := range fields {
		props[name] = simpleSchemaEntry(kind)
		required = append(required, name)
	}

	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

func simpleSchemaEntry(kind string) map[string]any {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "string":
		return map[string]any{"type": "string"}
	case "int", "int64", "integer":
		return map[string]any{"type": "integer"}
	case "float", "float64", "number":
		return map[string]any{"type": "number"}
	case "bool", "boolean":
		return map[string]any{"type": "boolean"}
	case "[]string":
		return map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		}
	case "object":
		return map[string]any{"type": "object"}
	case "any", "":
		return map[string]any{}
	default:
		return map[string]any{"type": kind}
	}
}
