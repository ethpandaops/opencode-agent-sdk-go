// Package opencodesdk is a Go SDK for driving the opencode CLI in its
// Agent Client Protocol (ACP) mode.
//
// The SDK spawns `opencode acp` as a subprocess, wires its stdio into
// the protocol layer supplied by github.com/coder/acp-go-sdk, and
// adds:
//
//   - one-shot and lifecycle helpers ([Query], [QueryContent],
//     [QueryStream], [QueryStreamContent], [WithClient]) for simple
//     cases, plus a stateful [Client] and [Session] API with functional
//     options for long-running use. The *Content variants accept
//     multimodal prompts ([]acp.ContentBlock) alongside the iterator
//     helpers [PromptsFromStrings], [PromptsFromSlice],
//     [PromptsFromChannel], and [SinglePrompt].
//   - content-block ergonomics: [Text], [Blocks], [TextBlock],
//     [ImageBlock], [ImageFileInput] (load an image from disk),
//     [ResourceBlock], [ResourceLinkBlock].
//   - pluggable transport via [WithTransport] + [Transport]: swap the
//     default `opencode acp` subprocess for a test double or alternate
//     carrier without touching the rest of the API.
//   - ACP unstable `additionalDirectories` pass-through via
//     [WithAddDirs] (capability-gated on
//     SessionCapabilities.AdditionalDirectories; silently dropped when
//     the agent doesn't advertise support).
//   - opencode `--pure` shortcut via [WithPure] for disabling
//     external plugins on the spawned CLI.
//   - typed wrappers for opencode-specific unstable RPCs
//     (Client.ForkSession, Client.ResumeSession,
//     Client.UnstableSetModel) and the _meta.opencode.variant channel
//     (OpencodeVariant).
//   - typed session/update subscribers via Session.Subscribe and
//     [UpdateHandlers] for AgentMessage, Plan, ToolCall, Mode, etc.,
//     so callers can register per-variant callbacks instead of
//     draining Session.Updates() with a switch.
//   - a turn-complete hook (WithOnTurnComplete) and an
//     overflow-observation hook (WithOnUpdateDropped, plus
//     Session.DroppedUpdates()) for cross-cutting observability.
//   - a cursor-paginated session iterator ([Client.IterSessions])
//     over opencode's session/list RPC.
//   - a raw extension-method escape hatch
//     ([Client.CallExtension]) for ACP `_`-prefixed methods the SDK
//     does not yet expose as typed wrappers.
//   - prompt-capability preflight: Session.Prompt rejects content
//     blocks the agent did not advertise support for with
//     [ErrCapabilityUnavailable].
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
//   - typed errors for diagnostics ([CLINotFoundError], [ProcessError])
//     alongside the sentinel errors, and transport health observation
//     via [TransportHealth] + Client.GetTransportHealth.
//   - MCP tool-author conveniences ([TextResult], [ErrorResult],
//     [ImageResult], [ParseArguments], [SimpleSchema]) for building
//     [Tool] implementations and [ToolResult] values without hand-
//     rolled literals.
//   - persisted session-cost accounting via [CostTracker],
//     [LoadSessionCost], and [SaveSessionCost] — snapshots land
//     under $XDG_DATA_HOME/opencode/sdk/session-costs/.
//   - structured-output decoding via [DecodeStructuredOutput] and
//     [DecodePromptResult], with an agent advisory schema via
//     [WithOutputSchema].
//   - error classification + retry taxonomy via [ClassifyError],
//     [ErrorClassification], [RetryPolicy], [EvaluateRetry], and the
//     [ResilientQuery] wrapper that applies exponential backoff +
//     jitter for rate-limit / overload / transient-connection errors.
//   - one-shot model discovery via [ListModels] without hand-rolling
//     a session loop.
//   - subprocess data-dir isolation via [WithOpencodeHome].
//   - Prometheus metrics via [WithPrometheusRegisterer] (OTel
//     Prometheus exporter under the hood).
//   - typed lifecycle hooks via [WithHooks] covering tool calls,
//     prompt submission, permission requests, session lifecycle,
//     and file-write delegations. Blocking-capable events
//     (UserPromptSubmit, PermissionRequest, FileChanged) abort the
//     triggering action when a hook returns Continue=false.
//   - tool-side MCP elicitation via [Elicit] — callable from within
//     [Tool.Execute] to send a server-initiated prompt back through
//     the loopback bridge to opencode's user.
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
