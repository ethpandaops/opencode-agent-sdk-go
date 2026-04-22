// Package opencodesdk is a Go SDK for driving the opencode CLI in its
// Agent Client Protocol (ACP) mode.
//
// The SDK spawns `opencode acp` as a subprocess, wires its stdio into
// the protocol layer supplied by github.com/coder/acp-go-sdk, and
// adds:
//
//   - a stateful [Client] and [Session] API with functional options
//     (NewClient, WithCwd, WithModel, WithAgent, WithSDKTools, etc.)
//   - typed wrappers for opencode-specific unstable RPCs
//     (Client.ForkSession, Client.ResumeSession,
//     Client.UnstableSetModel) and the _meta.opencode.variant channel
//     (OpencodeVariant)
//   - permission and filesystem callbacks surfaced via WithCanUseTool
//     and WithOnFsWrite
//   - in-process tools via a loopback HTTP MCP bridge declared in
//     session/new's mcpServers (WithSDKTools + the [Tool] interface)
//   - opencode's terminal-auth launch-instruction extraction
//     (WithTerminalAuthCapability, TerminalAuthInstructions)
//   - OpenTelemetry metrics and spans under the opencodesdk.* namespace
//     (WithMeterProvider, WithTracerProvider)
//
// # Quick start
//
//	c, err := opencodesdk.NewClient(opencodesdk.WithCwd("/tmp"))
//	if err != nil { /* ... */ }
//	defer c.Close()
//
//	ctx := context.Background()
//	if err := c.Start(ctx); err != nil { /* ... */ }
//
//	sess, err := c.NewSession(ctx)
//	if err != nil { /* ... */ }
//
//	go func() {
//	    for n := range sess.Updates() {
//	        // inspect n.Update.AgentMessageChunk, ToolCall, etc.
//	    }
//	}()
//
//	res, err := sess.Prompt(ctx, acp.TextBlock("Say hello."))
//
// # Requirements
//
//   - opencode CLI >= [MinimumCLIVersion] available in $PATH
//   - ACP protocol version [ProtocolVersion]
//   - A completed `opencode auth login` (the SDK does not initiate
//     auth; it catches missing-credentials errors and surfaces
//     [ErrAuthRequired] so callers can instruct the user)
//
// # Scope
//
// The SDK is opencode-focused. Because coder/acp-go-sdk is generic,
// the transport surface would work against any ACP v1 agent, but the
// opinionated options (agent modes, unstable_* wrappers,
// _meta.opencode parsers, HTTP MCP bridge port picker) are
// opencode-shaped.
//
// See INIT.md in the repository root for the protocol reference and
// the locked design decisions behind this SDK.
package opencodesdk
