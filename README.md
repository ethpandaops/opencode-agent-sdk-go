# opencode-agent-sdk-go

A Go SDK that spawns the [`opencode`](https://github.com/sst/opencode)
CLI in its Agent Client Protocol (ACP) mode and drives it over stdio
JSON-RPC.

Built on top of [`coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk)
for the protocol layer. This package adds:

- opencode subprocess management (spawn, version check, graceful shutdown)
- an opinionated, functional-option API for sessions, prompts, model
  and mode selection
- typed wrappers for opencode's unstable session RPCs
  (`ForkSession`, `ResumeSession`, `UnstableSetModel`) and the
  `_meta.opencode.variant` model-variant channel
- generic session config switching via
  `Session.SetConfigOption(ctx, configID, value)` /
  `SetConfigOptionBool` — the canonical path behind `SetModel` /
  `SetMode`
- `Client.LoadSessionHistory` — rehydrate a session and capture
  opencode's replayed `session/update` notifications into a typed
  `SessionHistory` (raw notifications, coalesced messages, last
  usage)
- typed `session/update` subscribers (`Session.Subscribe` +
  `UpdateHandlers`) for AgentMessage, Plan, ToolCall, Mode, Usage, etc.
- turn-complete and updates-dropped hooks
  (`WithOnTurnComplete`, `WithOnUpdateDropped`)
- cursor-paginated session iterator (`Client.IterSessions`)
- a raw extension-method escape hatch (`Client.CallExtension`) for
  ACP `_`-prefixed methods the SDK doesn't wrap yet
- `session/request_permission` and `fs/write_text_file` callbacks
- observational cost + budget: `CostTracker`, `BudgetTracker`,
  `WithMaxBudgetUSD` (auto-cancels the in-flight turn when the cap
  is crossed), plus `ErrBudgetExceeded`
- typed error classification: `ClassifyError` returns an
  `ErrorClassification` with coarse `Class` plus a finer
  `SubClass` (prompt-too-long, rate-limit-tokens vs requests,
  invalid-schema, invalid-model, subprocess-died) so resilience
  wrappers can pick targeted strategies
- file-backed content helpers: `PathInput` (auto-detects image / audio /
  text / blob), `PDFFileInput`, `AudioFileInput`, `ImageFileInput`
- **in-process Go tools** via a loopback HTTP MCP bridge
  (`WithSDKTools`) — no separate MCP server to run
- opencode's `terminal-auth` auth-flow hint extraction
- prompt-capability preflight (image/audio/embedded-resource blocks
  are rejected locally with `ErrCapabilityUnavailable` when the agent
  didn't advertise support)
- OpenTelemetry metrics + spans under the `opencodesdk.*` namespace

## Status

Early. Pinned to opencode CLI **`1.14.20`** and ACP protocol version
**1**. The API surface is still shifting between minor versions.

## Requirements

- Go 1.26+
- `opencode` ≥ 1.14.20 in `$PATH`
- A completed `opencode auth login` (credentials are read by opencode
  itself at session-start time)

## Install

```bash
go get github.com/ethpandaops/opencode-agent-sdk-go
```

## Quick start

One-shot via `Query` (plain text):

```go
res, err := opencodesdk.Query(ctx, "Say hello in three words.", opencodesdk.WithCwd(cwd))
if err != nil {
    panic(err)
}
fmt.Println(res.AssistantText)
```

Multimodal via `QueryContent`:

```go
img, _ := opencodesdk.ImageFileInput("./screenshot.png")

res, err := opencodesdk.QueryContent(ctx,
    opencodesdk.Blocks(
        opencodesdk.TextBlock("Describe the attached image in one sentence."),
        img,
    ),
    opencodesdk.WithCwd(cwd),
)
```

Dynamic prompt streams via `QueryStreamContent` + an iterator helper:

```go
ch := make(chan []acp.ContentBlock)
go func() {
    defer close(ch)
    ch <- opencodesdk.Text("Reply with just: one")
    ch <- opencodesdk.Text("Reply with just: two")
}()

for res, err := range opencodesdk.QueryStreamContent(ctx,
    opencodesdk.PromptsFromChannel(ch),
    opencodesdk.WithCwd(cwd),
) {
    if err != nil {
        break
    }
    fmt.Println(res.AssistantText)
}
```

Long-lived client with streaming:

```go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    acp "github.com/coder/acp-go-sdk"
    opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
    cwd, _ := os.Getwd()

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    err := opencodesdk.WithClient(ctx, func(c opencodesdk.Client) error {
        sess, err := c.NewSession(ctx)
        if err != nil {
            return err
        }

        go func() {
            for n := range sess.Updates() {
                if n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.Content.Text != nil {
                    fmt.Print(n.Update.AgentMessageChunk.Content.Text.Text)
                }
            }
        }()

        res, err := sess.Prompt(ctx, acp.TextBlock("Say hello in three words."))
        if err != nil {
            return err
        }

        fmt.Printf("\nstop: %s\n", res.StopReason)
        return nil
    }, opencodesdk.WithCwd(cwd))

    if err != nil {
        panic(err)
    }
}
```

## In-process tools

Register a Go function as a tool and opencode can invoke it directly.
The SDK runs a loopback HTTP MCP server for you, authenticates it
with a random bearer token, and declares it in every `session/new`:

```go
reverse := opencodesdk.NewTool(
    "reverse",
    "Reverse the characters of the input string.",
    map[string]any{
        "type": "object",
        "properties": map[string]any{
            "text": map[string]any{"type": "string"},
        },
        "required": []string{"text"},
    },
    func(ctx context.Context, in map[string]any) (opencodesdk.ToolResult, error) {
        text, _ := in["text"].(string)
        runes := []rune(text)
        for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
            runes[i], runes[j] = runes[j], runes[i]
        }
        return opencodesdk.ToolResult{Text: string(runes)}, nil
    },
)

c, _ := opencodesdk.NewClient(opencodesdk.WithSDKTools(reverse))
```

Closures are live: reach into DB handles, config, whatever the host
process has. That's the entire reason to embed an agent inside a Go
program versus shelling out.

## Options overview

| Option | Purpose |
| --- | --- |
| `WithLogger(slog)` | structured logging |
| `WithCwd(path)` | working directory for opencode + sessions |
| `WithCLIPath(path)` | pin the opencode binary |
| `WithCLIFlags(args...)` | extra flags passed to `opencode acp` |
| `WithExtraArgs(map)` | map-shaped sister of `WithCLIFlags`; nil values render as bare `--flag`, non-nil as `--flag=value` |
| `WithEnv(map)` | overlay on inherited env |
| `WithStderr(fn)` | stderr callback |
| `WithUser(id)` | tags OTel spans + metrics with a `user` attribute (multi-tenant attribution) |
| `WithInitializeTimeout(d)` | handshake timeout (default 60s) |
| `WithSkipVersionCheck(bool)` | skip the ≥1.14.20 assertion |
| `WithModel(id)` | applied via `session/set_config_option` |
| `WithAgent(name)` | sets the opencode mode (`ModeBuild`, `ModePlan`, ...) |
| `WithInitialMode(id)` | ACP-terminology alias for `WithAgent` |
| `WithEffort(level)` | maps an abstract `EffortLow/Medium/High/Max` enum onto opencode's per-model variant strings (`/high`, `/xhigh`, `/max`, …) |
| `WithMaxTurns(n)` | client-side cap on assistant messages per session; auto-cancels when exceeded |
| `WithMCPServers(servers...)` | external MCP servers |
| `WithSDKTools(tools...)` | in-process tools via the bridge |
| `WithCanUseTool(cb)` | permission-prompt callback |
| `WithOnFsWrite(cb)` | intercept `fs/write_text_file` |
| `WithOnElicitation(cb)` | handle agent-initiated `elicitation/create` (ACP unstable); opencode 1.14.20 doesn't emit it yet — forward-compat stub |
| `WithOnElicitationComplete(cb)` | observe `elicitation/complete` notifications for URL-mode elicitation |
| `WithStrictCwdBoundary(bool)` | reject writes outside cwd |
| `WithAddDirs(dirs...)` | extra workspace roots (ACP unstable, capability-gated) |
| `WithPure()` | sugar for `--pure` — disables external opencode plugins |
| `WithTransport(factory)` | custom transport (test doubles / embedded setups) |
| `WithUpdatesBuffer(n)` | per-session update channel size |
| `WithTerminalAuthCapability(bool)` | opt into opencode's `terminal-auth` launch hints |
| `WithAutoLaunchLogin(bool)` | auto-spawn `opencode auth login` on `authRequired` |
| `WithMeterProvider(mp)` | OTel MeterProvider |
| `WithTracerProvider(tp)` | OTel TracerProvider |

## Utilities

A handful of utilities for common SDK workflows, mirrored against the
claude and codex sister SDKs:

- **MCP tool-author helpers** — `TextResult`, `ErrorResult`,
  `ImageResult`, `ParseArguments`, `SimpleSchema` build tool results
  and input schemas without hand-rolled `ToolResult` literals.
- **Typed errors** — `*CLINotFoundError`, `*ProcessError`,
  `*TransportError`, and `*RequestError` carry structured diagnostic
  context (`SearchedPaths`, `ExitCode`, `Stderr`, JSON-RPC code + data)
  alongside the `ErrCLINotFound`, `ErrClientClosed`,
  `ErrClientAlreadyConnected`, `ErrRequestTimeout`, `ErrTransport`
  sentinels. All SDK-originated errors satisfy the `OpencodeSDKError`
  marker interface so callers can distinguish them from arbitrary Go
  errors with a single `errors.As` check.
- **Transport health** — `Client.GetTransportHealth()` returns a
  `TransportHealth` snapshot with degradation flag, failure counts,
  and last-error details.
- **Session-cost tracker** — `NewCostTracker()` aggregates per-session
  cost and token usage from `UsageUpdate` notifications.
  `LoadSessionCost` / `SaveSessionCost` persist snapshots to
  `$XDG_DATA_HOME/opencode/sdk/session-costs/<id>.json`.
- **Structured output** — `DecodeStructuredOutput[T](result)` pulls a
  typed T from `QueryResult` (session-update meta first, JSON-fenced
  assistant text second). `WithOutputSchema(map[string]any)` advises
  the agent via `session/new._meta["structuredOutputSchema"]`.
- **Retry / classification** — `ClassifyError(err)` maps any SDK
  error to an `ErrorClass` + `RecoveryAction`. `EvaluateRetry` and
  `ResilientQuery` apply exponential back-off with jitter on
  retryable failures (rate limit, overload, transient connection).
- **Model catalogue** — `ListModels(ctx, opts...)` returns every
  model opencode advertises for the configured cwd without writing
  a full session loop.
- **Data-dir override** — `WithOpencodeHome(path)` sets
  `XDG_DATA_HOME` for the subprocess and for cost-snapshot
  persistence — convenient for tests and multi-env setups.
- **Hooks** — `WithHooks(...)` registers typed callbacks for 11
  lifecycle events (PreToolUse, PostToolUse, UserPromptSubmit, Stop,
  SessionStart/End, PermissionRequest/Denied, FileChanged, …).
  `HookOutput{Continue:false}` blocks the triggering action for the
  events that support blocking (UserPromptSubmit, PermissionRequest,
  FileChanged).
- **Tool-side elicitation** — `Elicit(ctx, params)` callable from
  within a `Tool.Execute` sends an MCP elicitation through the
  loopback bridge back to opencode, which routes it to the user.
  Returns the user's answer or `ErrElicitationUnavailable` when
  there's no bound session.

See [`doc.go`](./doc.go) for full package-level documentation.

### Observability

New metrics emitted alongside the existing `opencodesdk.*` surface:

- `opencodesdk.retry.attempt` (`class`, `outcome`) — ResilientQuery
  retry decisions.
- `opencodesdk.structured_output.decode` (`source`, `outcome`) —
  DecodeStructuredOutput invocations.
- `opencodesdk.transport.failure` (`kind`) — transport-layer
  failures observed by the Client.

#### Prometheus

Two paths:

```go
// 1. WithPrometheusRegisterer — SDK wires an OTel Prometheus
//    exporter internally.
reg := prometheus.NewRegistry()
_, _ = opencodesdk.Query(ctx, prompt,
    opencodesdk.WithPrometheusRegisterer(reg),
)

// 2. WithMeterProvider — bring your own OTel MeterProvider. Useful
//    when you're already running an OTel pipeline and want SDK
//    metrics to land alongside everything else.
opencodesdk.WithMeterProvider(myMeterProvider)
```

See `examples/prometheus_metrics` for the full scrape-server setup.

## Examples

See [`examples/`](./examples/) for seven working programs:

- `quick_start` — minimal round-trip
- `sdk_tools` — in-process tool via the bridge
- `external_mcp` — attach an external stdio MCP server via `WithMCPServers`
- `session_list` — list prior sessions with pagination
- `permission_callback` — interactive permission UX
- `fs_intercept` — capture writes in memory instead of on disk
- `plan_mode` — `WithInitialMode(ModePlan)` to trigger permission prompts out of the box
- `cost_tracker` — aggregate per-session cost and persist snapshots
- `resilient_query` — ResilientQuery with backoff + error classification
- `hooks` — typed lifecycle hooks via `WithHooks`
- `elicitation` — a tool that asks the user to confirm via MCP
  elicitation through the loopback bridge
- `prometheus_metrics` — expose SDK metrics via `/metrics` using
  `WithPrometheusRegisterer`

## Architecture

```
your Go app
    │
    ▼
opencodesdk  (this package)
    │
    ▼
coder/acp-go-sdk   (JSON-RPC framing + schema types)
    │
    ▼  stdio
opencode acp   (child process)

In parallel:
your Go app ─(WithSDKTools)→ loopback HTTP MCP bridge ─←─ opencode
```

The SDK is deliberately a thin opinionated wrapper — we do not
reimplement the ACP types or the JSON-RPC transport.

## License

MIT — see [LICENSE](./LICENSE).
