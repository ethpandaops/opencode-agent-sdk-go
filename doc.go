// Package opencodesdk is a Go SDK that spawns the `opencode acp` CLI as a
// subprocess and drives it over the Agent Client Protocol (ACP).
//
// The SDK is a thin, opinionated wrapper on top of
// github.com/coder/acp-go-sdk, adding:
//
//   - opencode subprocess lifecycle (binary discovery, version check, stdio
//     wiring, graceful shutdown)
//   - functional options that translate to session/new parameters and
//     post-session set_config_option calls
//   - typed wrappers for opencode-specific unstable methods
//     (unstable_forkSession, unstable_resumeSession, unstable_setSessionModel)
//     and _meta.opencode.variant parsing
//   - permission and fs/write_text_file handlers with callback hooks
//   - an in-process HTTP MCP bridge that keeps WithSDKTools working by
//     declaring a loopback MCP server in session/new's mcpServers
//   - auth-error surfacing for opencode's unimplemented authenticate handler
//   - OpenTelemetry observability under the opencodesdk.* namespace
//
// Minimum opencode CLI version: 1.14.20. ACP protocol version: 1.
//
// See INIT.md in the repository root for the full migration plan and
// protocol reference.
package opencodesdk
