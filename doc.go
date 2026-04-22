// Package opencodesdk is a Go SDK for driving the opencode CLI in its
// Agent Client Protocol (ACP) mode.
//
// The SDK spawns `opencode acp` as a subprocess, wires its stdio into
// the protocol layer supplied by github.com/coder/acp-go-sdk, and
// adds:
//
//   - one-shot and lifecycle helpers ([Query], [QueryStream],
//     [WithClient]) for simple cases, plus a stateful [Client] and
//     [Session] API with functional options for long-running use.
//   - typed wrappers for opencode-specific unstable RPCs
//     (Client.ForkSession, Client.ResumeSession,
//     Client.UnstableSetModel) and the _meta.opencode.variant channel
//     (OpencodeVariant).
//   - permission and filesystem callbacks surfaced via WithCanUseTool
//     and WithOnFsWrite, plus cwd-scoped write enforcement
//     (WithStrictCwdBoundary).
//   - in-process tools via a loopback HTTP MCP bridge declared in
//     session/new's mcpServers (WithSDKTools + the [Tool] interface).
//   - opencode's terminal-auth launch-instruction extraction
//     (WithTerminalAuthCapability, TerminalAuthInstructions,
//     WithAutoLaunchLogin).
//   - OpenTelemetry metrics and spans under the opencodesdk.* namespace
//     (WithMeterProvider, WithTracerProvider).
//
// # Quick start
//
// One-shot:
//
//	res, err := opencodesdk.Query(ctx, "Say hello.", opencodesdk.WithCwd(cwd))
//
// Lifecycle helper:
//
//	err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
//	    sess, err := c.NewSession(ctx)
//	    if err != nil { return err }
//	    _, err = sess.Prompt(ctx, acp.TextBlock("Say hello."))
//	    return err
//	}, opencodesdk.WithCwd(cwd))
//
// # Requirements
//
//   - opencode CLI >= [MinimumCLIVersion] available in $PATH
//   - ACP protocol version [ProtocolVersion]
//   - A completed `opencode auth login` (the SDK does not initiate
//     auth on its own; with [WithAutoLaunchLogin] it can exec the
//     command opencode advertises in _meta["terminal-auth"])
//
// # Scope
//
// The SDK is opencode-focused. Because coder/acp-go-sdk is generic,
// the transport surface would work against any ACP v1 agent, but the
// opinionated options (agent modes, unstable_* wrappers,
// _meta.opencode parsers, HTTP MCP bridge port picker) are
// opencode-shaped.
package opencodesdk
