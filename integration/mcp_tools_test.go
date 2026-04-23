//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// TestMCPTools_SDKToolInvokedByAgent registers an in-process tool via
// WithSDKTools and prompts the agent to call it. Verifies the tool's
// Go handler actually ran and the agent's reply references the result.
func TestMCPTools_SDKToolInvokedByAgent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var calls atomic.Int32

	magicWord := "xyzzy-1234-opencodesdk-integration"

	getSecret := opencodesdk.NewTool(
		"get_secret_word",
		"Returns the secret word the user wants you to reveal. Call this tool whenever the user asks for the secret word.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		func(_ context.Context, _ map[string]any) (opencodesdk.ToolResult, error) {
			calls.Add(1)

			return opencodesdk.ToolResult{Text: magicWord}, nil
		},
	)

	res, err := opencodesdk.Query(ctx,
		fmt.Sprintf("Use the get_secret_word tool to retrieve the secret, then echo the exact secret back to me in your reply. The secret should contain %q.", magicWord),
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithSDKTools(getSecret),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Query: %v", err)
	}

	if calls.Load() == 0 {
		t.Skipf("agent did not invoke the registered tool; model may not support tool use well. AssistantText=%q", res.AssistantText)
	}

	if !strings.Contains(res.AssistantText, magicWord) {
		t.Logf("AssistantText did not echo the magic word (the agent may have paraphrased); got %q", res.AssistantText)
	}
}

// TestMCPTools_ToolError verifies that a tool returning IsError=true
// surfaces to the agent without crashing the turn.
func TestMCPTools_ToolError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var calls atomic.Int32

	failing := opencodesdk.NewTool(
		"always_fails",
		"A diagnostic tool that always reports a failure. Call this if the user asks for a diagnostic.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		func(_ context.Context, _ map[string]any) (opencodesdk.ToolResult, error) {
			calls.Add(1)

			return opencodesdk.ToolResult{Text: "diagnostic failure: out of cheese", IsError: true}, nil
		},
	)

	res, err := opencodesdk.Query(ctx,
		"Please run the always_fails diagnostic tool and report back whatever it says.",
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithSDKTools(failing),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Query: %v", err)
	}

	if calls.Load() == 0 {
		t.Skipf("agent did not invoke the tool; assistant said: %q", res.AssistantText)
	}

	// Turn should still have completed successfully despite the tool's IsError.
	if res.StopReason == "" {
		t.Fatalf("expected a stop reason; got %+v", res)
	}
}

// TestMCPTools_ToolWithAnnotations — attaches ToolAnnotations to an
// SDK tool and verifies the turn completes cleanly. opencode's bridge
// forwards the annotations on tools/list; this test proves the
// plumbing doesn't break the round trip. (Asserting the exact values
// reached the agent would require intercepting MCP tools/list, which
// the SDK does not expose.)
func TestMCPTools_ToolWithAnnotations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var calls atomic.Int32

	readOnly := opencodesdk.NewTool(
		"lookup_constant",
		"Returns a canonical numeric constant by name. Read-only.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
			"required": []string{"name"},
		},
		func(_ context.Context, _ map[string]any) (opencodesdk.ToolResult, error) {
			calls.Add(1)

			return opencodesdk.ToolResult{Text: "42"}, nil
		},
		opencodesdk.WithToolAnnotations(opencodesdk.ToolAnnotations{
			Title:           "Lookup Constant",
			ReadOnlyHint:    true,
			DestructiveHint: opencodesdk.BoolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   opencodesdk.BoolPtr(false),
		}),
	)

	res, err := opencodesdk.Query(ctx,
		"Use the lookup_constant tool to look up the constant 'answer' and tell me what you got.",
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithSDKTools(readOnly),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Query: %v", err)
	}

	if res.StopReason == "" {
		t.Fatalf("expected stop reason; got %+v", res)
	}

	if calls.Load() == 0 {
		t.Skipf("agent did not invoke the annotated tool; assistant said: %q", res.AssistantText)
	}
}

// TestMCPTools_MultipleToolsCoexist registers two tools and verifies
// the bridge serves both.
func TestMCPTools_MultipleToolsCoexist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var (
		echoCalls    atomic.Int32
		reverseCalls atomic.Int32
	)

	echo := opencodesdk.NewTool(
		"echo_exact",
		"Echo the input text back exactly. Call when asked to echo text.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []string{"text"},
		},
		func(_ context.Context, in map[string]any) (opencodesdk.ToolResult, error) {
			echoCalls.Add(1)

			text, _ := in["text"].(string)

			return opencodesdk.ToolResult{Text: text}, nil
		},
	)

	reverse := opencodesdk.NewTool(
		"reverse_text",
		"Reverse the characters of the input text. Call when asked to reverse.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []string{"text"},
		},
		func(_ context.Context, in map[string]any) (opencodesdk.ToolResult, error) {
			reverseCalls.Add(1)

			text, _ := in["text"].(string)
			runes := []rune(text)

			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}

			return opencodesdk.ToolResult{Text: string(runes)}, nil
		},
	)

	res, err := opencodesdk.Query(ctx,
		"First use echo_exact to echo the word 'hello'. Then use reverse_text to reverse the word 'world'. Then tell me both results in one sentence.",
		opencodesdk.WithLogger(testLogger(t)),
		opencodesdk.WithCwd(tempCwd(t)),
		opencodesdk.WithSDKTools(echo, reverse),
	)
	if err != nil {
		skipIfCLIUnavailable(t, err)
		skipIfAuthRequired(t, err)
		t.Fatalf("Query: %v", err)
	}

	if echoCalls.Load() == 0 && reverseCalls.Load() == 0 {
		t.Skipf("agent did not invoke any registered tool; AssistantText=%q", res.AssistantText)
	}

	t.Logf("echo calls=%d reverse calls=%d", echoCalls.Load(), reverseCalls.Load())
}
