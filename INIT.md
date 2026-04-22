# opencode-agent-sdk-go — INIT

A Go SDK that spawns `opencode acp` as a subprocess and drives it via the Agent Client Protocol (ACP). Forked from `codex-agent-sdk-go`, but after deep research we are **not porting** the codex fork's protocol layer — we build on top of [`coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk) instead.

This document is the migration plan + protocol reference, derived from:
- Live probing of `opencode acp` v1.14.20 on 2026-04-22
- Deep source audit of `sst/opencode` at tag `v1.14.20` (commit `3175a3c`)
- Full read of the ACP spec at https://agentclientprotocol.com
- Review of `coder/acp-go-sdk` v0.12.0, `agentclientprotocol/typescript-sdk`, and Zed's client (`zed-industries/zed/crates/agent_servers/src/acp.rs`)

---

## Locked decisions

1. **Module path:** `github.com/ethpandaops/opencode-agent-sdk-go`
2. **Package name:** `opencodesdk`
3. **ACP transport:** adopt [`github.com/coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk) v0.12.0+ as the protocol dependency. Do not reimplement JSON-RPC framing, schema types, or method dispatch.
4. **Minimum opencode CLI:** `1.14.20` (pinned; verified at `initialize` time against `agentInfo.version`)
5. **ACP protocol version:** `1`
6. **`WithSDKTools`: keep**, reimplemented via an in-process HTTP MCP server injected into every `session/new` (see § HTTP MCP bridge).
7. **Observability:** redesign around ACP-native events under the `opencodesdk.*` metric/span namespace. Codex-specific counters are dropped.
8. **Authentication:** do not call `authenticate` — opencode's handler is unimplemented and throws. Catch `-32000 authRequired` from `session/new`/`session/load`, surface a typed error telling the user to run `opencode auth login`.
9. **Session persistence:** delegate entirely to opencode (SQLite-backed in the opencode core). Drop the codex fork's SQLite dependency and `internal/session/` package.
10. **Scope:** opencode only. The SDK is compatible with any ACP v1 agent because coder/acp-go-sdk is generic, but the opinionated options, unstable-method wrappers, and HTTP MCP bridge are opencode-shaped.

## Workflow

- **Remote:** `git@github.com:ethpandaops/opencode-agent-sdk-go.git` (manually created, empty shell).
- **Branch:** `feat/baseline` — single long-lived migration branch. All work lands here.
- **Pushing:** **do not push** to `origin` during the migration. Everything stays local until the branch is maintainer-approved.
- **Commits:** each milestone in § Migration order is one commit (or a small cluster). Each commit must pass `go build ./...`; tests green at milestone boundaries.
- **Commit prefixes:** `baseline:`, `nuke:`, `transport:`, `options:`, `mcp-bridge:`, `unstable:`, `handlers:`, `errors:`, `otel:`, `examples:`, `docs:`.
- **Rebasing:** local rebases fine; `origin` is never force-pushed.

---

## Architecture

```
┌─────────────────────────────────────────────────┐
│            User Go application                  │
└────────────┬────────────────────────────────────┘
             │ opencodesdk.Query(...)
             │ opencodesdk.NewClient(...)
             │ opencodesdk.WithSDKTools(...)
             ▼
┌─────────────────────────────────────────────────┐
│  opencodesdk (this package)                     │
│  - subprocess lifecycle (spawn opencode acp)    │
│  - functional options → session/new params      │
│  - typed wrappers for opencode unstable methods │
│  - permission + fs/write handlers               │
│  - auth error surfacing                         │
│  - HTTP MCP bridge for WithSDKTools             │
│  - OTel observability                           │
└────────────┬────────────────────────────────────┘
             │ ClientSideConnection
             ▼
┌─────────────────────────────────────────────────┐
│  coder/acp-go-sdk                               │
│  - JSON-RPC dispatch + framing                  │
│  - schema-generated types                       │
│  - _meta propagation                            │
│  - $/cancel_request                             │
└────────────┬────────────────────────────────────┘
             │ stdio (newline-delimited JSON)
             ▼
┌─────────────────────────────────────────────────┐
│  opencode acp (child process)                   │
└─────────────────────────────────────────────────┘

                 ┌────────────────────────────────┐
                 │  loopback HTTP MCP server      │
                 │  (started by opencodesdk       │
                 │   when WithSDKTools is used;   │
                 │   declared in session/new's    │
                 │   mcpServers; reachable by     │
                 │   opencode over 127.0.0.1)     │
                 └────────────────────────────────┘
```

### What we own

- `opencodesdk` package (top-level): `Query`, `QueryStream`, `Client`, `NewClient`, `WithClient`, all `WithXxx` options, the `Tool` interface, error types.
- `internal/subprocess/`: spawn/lifecycle of `opencode acp`, stdin/stdout wiring into `coder/acp-go-sdk`.
- `internal/mcp/bridge/`: loopback HTTP MCP server for in-process tools.
- `internal/handlers/`: our implementations of `session/request_permission` and `fs/write_text_file` that fan out to user-provided callbacks.
- `internal/unstable/`: typed wrappers for `unstable_forkSession`, `unstable_resumeSession`, `unstable_setSessionModel`, `_meta.opencode.variant` parsing.
- `internal/observability/`: OTel spans/metrics.
- `examples/`, `integration/`, tests, docs.

### What we don't own

Everything `coder/acp-go-sdk` already provides:
- JSON-RPC request/response/notification types
- Method dispatch + handler registration API
- Schema-generated types for every stable + unstable method
- `_meta` propagation on all types
- `$/cancel_request` (generic) and `session/cancel` (turn-scoped) plumbing
- Protocol-version negotiation
- stdio framing (newline-delimited JSON via `bufio.Scanner`)

---

## Part 1: ACP protocol reference (source-of-truth)

### Transport framing

Newline-delimited JSON over stdio. Messages MUST NOT contain embedded newlines. No Content-Length headers (unlike LSP). Stderr is logging only — never parse it as messages. `coder/acp-go-sdk` handles all of this.

### Method catalog

**Client → Agent (requests we send):**

| Method | Status | coder SDK | opencode support |
|---|---|---|---|
| `initialize` | stable | yes | yes |
| `authenticate` | stable | yes | **unimplemented — throws** |
| `session/new` | stable | yes | yes |
| `session/load` | stable (cap: `loadSession`) | yes | yes, replays history |
| `session/list` | stable (cap: `list`) | yes | yes, scoped by cwd |
| `session/prompt` | stable | yes | yes |
| `session/cancel` | stable (**notification**) | yes | yes, but throws instead of returning `stopReason:"cancelled"` |
| `session/set_config_option` | stable | yes | yes (`configId: "model"\|"mode"`) |
| `session/set_mode` | stable | yes | yes |
| `session/set_model` | unstable | yes | as `unstable_setSessionModel` |
| `session/fork` | RFD, unstable | yes | as `unstable_forkSession` |
| `session/resume` | RFD, unstable | yes | as `unstable_resumeSession` |
| `session/close` | RFD | constants only | not implemented |
| `logout`, `nes/*`, `providers/*`, `document/*` | unstable | partial | **not implemented by opencode** |

**Agent → Client (requests we handle):**

| Method | coder SDK | opencode emits? |
|---|---|---|
| `session/update` (notification) | yes | yes — see § session/update variants |
| `session/request_permission` | yes | yes — only when rule evaluates to `ask` |
| `fs/read_text_file` | yes | **never called** |
| `fs/write_text_file` | yes | yes — after approved edit, to sync client buffer |
| `terminal/*` | yes | never |
| `elicitation/*` | yes | never |

### `session/update` variants

| `sessionUpdate` | Source | opencode emits? |
|---|---|---|
| `user_message_chunk` | spec | yes (only during `session/load`/`fork`/`resume` replay) |
| `agent_message_chunk` | spec | yes |
| `agent_thought_chunk` | spec | yes |
| `tool_call` | spec | yes |
| `tool_call_update` | spec | yes |
| `plan` | spec | yes (emitted when `todowrite` tool completes) |
| `available_commands_update` | spec | yes (once per session, ~1 tick after lifecycle response) |
| `current_mode_update` | spec | **never** |
| `config_option_update` | spec | **never** |
| `session_info_update` | spec | **never** |
| `usage_update` | spec (yes — it IS in the schema, confirmed) | yes (once per turn, not per-token) |

### Capabilities

**Client capabilities we declare:**
- `fs.readTextFile: true` (even though opencode never calls it — harmless)
- `fs.writeTextFile: true` (required — opencode emits this after approved edits)
- `terminal: false` (opencode never uses it)
- `_meta["terminal-auth"]: true` (opt-in to opencode's terminal-launch auth channel; SDK may choose to respect this if configured to spawn login)

**Agent capabilities opencode advertises:**
- `loadSession: true`
- `promptCapabilities: { embeddedContext: true, image: true }` (no `audio`)
- `mcpCapabilities: { http: true, sse: true }` (stdio works in code but isn't advertised)
- `sessionCapabilities: { fork: {}, list: {}, resume: {} }`
- `authMethods: [{ id: "opencode-login", name: "Login with opencode", description: "Run \`opencode auth login\` in the terminal" }]`
- `agentInfo: { name: "OpenCode", version: "1.14.20" }`

### Stop reasons

Authoritative set (prompt-turn.md + coder SDK):

- `end_turn` — normal completion
- `max_tokens` — hit model token limit
- `max_turn_requests` — hit request cap within the turn
- `refusal` — model refused
- `cancelled` — turn cancelled by client

**opencode only ever returns `end_turn`.** On cancellation it throws an error instead of returning `{stopReason: "cancelled"}` — our cancel handling must treat a JSON-RPC error response to `session/prompt` during an outstanding `session/cancel` notification as the cancel signal. coder SDK's `context.Canceled` semantics give us this plumbing for free if we feed it a cancellable context.

### Error codes

Standard JSON-RPC 2.0:

| Code | Meaning | When opencode uses it |
|---|---|---|
| `-32700` | parse error | framing |
| `-32600` | invalid request | malformed |
| `-32601` | method not found | unsupported method (incl. `session/cancel` sent as request) |
| `-32602` | invalid params | zod validation, unknown configId, session not found, bad value |
| `-32603` | internal error | handler exception (incl. `authenticate` — this is the bug) |
| `-32000` | authRequired | `LoadAPIKeyError` during `session/new`/`load`/`list`/`fork`/`resume` |
| `-32002` | resourceNotFound | not used by opencode |
| `-32800` | request cancelled | `$/cancel_request` response |

### Content block types (in `session/prompt` input)

- `text`: `{type:"text", text, annotations?:{audience?:["user"|"assistant"][]}}`
- `image`: `{type:"image", data?:base64, uri?, mimeType}` — **`uri` only accepts `http:` (not `https:`) per a probable opencode bug**; use data URIs
- `resource`: `{type:"resource", resource:{uri, mimeType?, text?|blob?}}`
- `resource_link`: `{type:"resource_link", uri, name?, mimeType?}`
- `audio`: silently dropped by opencode (`default: break` in the switch) despite being in ACP spec

### Session persistence

- opencode persists sessions in SQLite under `$XDG_DATA_HOME/opencode/opencode.db` (channel-aware; overridable via `OPENCODE_DB`, `OPENCODE_DISABLE_CHANNEL_DB`, `OPENCODE_TEST_HOME`).
- `session/list` filters by project (cwd). Cross-project listing is not exposed.
- Sessions survive `opencode acp` restarts. `session/load` rehydrates them.
- Cursor in `session/list` is an opaque string (millisecond timestamp internally). Treat as opaque.

### Authenticate flow (workaround)

opencode's `authenticate` handler is `throw new Error("Authentication not implemented")` → `-32603 internal error`. Do not call it.

Real flow:
1. User runs `opencode auth login` in their terminal out of band (writes credentials under XDG config).
2. Our SDK spawns `opencode acp`, sends `initialize`, calls `session/new`.
3. If credentials are missing, opencode returns `-32000 authRequired`.
4. Our SDK surfaces this as a typed error (`opencodesdk.ErrAuthRequired`) with a message instructing the user to run `opencode auth login`.

Optional sugar: if client declares `clientCapabilities._meta["terminal-auth"] = true`, opencode includes launch instructions in the auth method's `_meta`:

```json
{ "terminal-auth": { "command": "opencode", "args": ["auth", "login"], "label": "OpenCode Login" } }
```

A user of our SDK who wants an auto-launch UX can opt into this with `WithAutoLaunchLogin(true)`.

### Cancellation edge cases

- `session/cancel` after turn completes: no-op, silently ignored (idempotent).
- `session/cancel` with invalid `sessionId`: silently ignored.
- Cancel during pending `session/request_permission`: spec says agent should emit `$/cancel_request` for the outstanding permission request; client responds with `{outcome: "cancelled"}` (or `-32800`). coder SDK wires this up.
- Cancel mid-prompt: `session/prompt` throws (opencode bug) — our client treats this as the cancel signal when we know a cancel is outstanding.
- Back-to-back cancel + prompt: safe; notifications are serialized per session in coder SDK.

### `opencode acp` CLI flags worth noting

- `--cwd PATH` — working directory for the agent
- `--print-logs` — echo logs to stderr
- `--log-level DEBUG|INFO|WARN|ERROR`
- `--pure` — disable plugins
- `--hostname 127.0.0.1 --port 0` — important: `opencode acp` boots an internal HTTP server (not for ACP, but for opencode's own internal REST bridge). Pass explicit loopback + ephemeral port to avoid exposing it on LAN. `--mdns` should be off.
- `--cors` — additional CORS domains (irrelevant for stdio use)

---

## Part 2: opencode-specific extensions and gotchas

### Unstable methods opencode exposes

- `unstable_forkSession` — `{sessionId, cwd, mcpServers?}` → `{sessionId?, modes?, models?, configOptions?, _meta?}`. ACP maps this to `session/fork` once stable; we wrap it as `Client.ForkSession(ctx, sessionId, ...)`.
- `unstable_resumeSession` — `{sessionId, cwd, mcpServers?}` → like `session/load` without history replay. Wrap as `Client.ResumeSession(ctx, sessionId, ...)`.
- `unstable_setSessionModel` — `{sessionId, modelId}` → `{_meta: {opencode: {...}}}` only (no echoed configOptions). Prefer `session/set_config_option` with `configId:"model"` which returns updated options.

### `_meta` channels

- `_meta.opencode.variant` on `session/new`, `session/load`, `session/fork`, `session/resume`, `session/set_config_option`, `session/set_model` responses:
  ```json
  { "opencode": { "modelId": "provider/model", "variant": "high" | null, "availableVariants": ["low","high","maximum"] } }
  ```
  This is the only way to discover model variants — they're not exposed as a separate `configOption` entry.
- `_meta["terminal-auth"]` on `AuthMethod` — launch instructions (see auth flow above).
- `_meta.opencode.*` on `models` blocks — extra per-model metadata.

### `models` block on lifecycle responses

opencode adds a `models` field alongside `modes` and `configOptions` on `session/new`, `session/load`, `session/fork`, `session/resume`:

```json
{
  "models": {
    "currentModelId": "provider/model",
    "availableModels": [{"modelId":"...", "name":"..."}, ...]
  }
}
```

Not in standard ACP spec. We parse it and expose via `ModelInfo` / `Client.AvailableModels()`.

### configOptions surface

Only two options ever emitted by opencode:
- `model` (always) — select with `currentValue` + `options[]`
- `mode` (only if modes configured) — select across agents (`build`, `plan`, `general`, `explore`, `summarize`, etc.)

Variants live inside `model` options (e.g. `anthropic/claude-sonnet-4/high`) and in `_meta.opencode.variant`.

### Permission model defaults

Critical for getting consistent SDK behavior:

- Default agent: `build`. Its ruleset begins with `"*": "allow"` — **every tool permission resolves to allow, no prompts.** This is why naive probes don't trigger `session/request_permission`.
- Agent `plan` has `edit: {"*":"ask", <plansDir>:"allow"}` — edits outside the plans dir prompt.
- User config (`.config/opencode/config.json` `"permission"` key) can override, e.g. `{"permission": {"edit":"ask", "bash":{"git *":"allow"}}}`.
- Three fixed permission options opencode offers: `{"once","always","reject"}` mapped to ACP kinds `allow_once|allow_always|reject_once`. No `reject_always` is offered.
- `"always"` replies are in-memory only — not persisted across restarts (potential latent bug).

To see permission prompts during development: `WithAgent("plan")` or supply user config via the external opencode config file.

### Slash commands

Opencode parses `/name args` out of the concatenated text of the `session/prompt`. No dedicated RPC. `available_commands_update` is emitted once per session ~1 tick after the lifecycle response (via `setTimeout(...,0)`), so clients must be ready to receive notifications before the lifecycle response settles.

Unknown `/foo` silently falls through and returns `{stopReason:"end_turn"}` with no usage.

Our SDK surfaces this via `Client.AvailableCommands()` (snapshot) and a notification channel.

### MCP registration

- opencode accepts stdio, HTTP, and SSE MCP servers in `session/new`'s `mcpServers`, despite only advertising `http+sse` in `mcpCapabilities`.
- No URL validation at ACP layer. `127.0.0.1` loopback works.
- Plain HTTP (no TLS) accepted.
- Headers forwarded verbatim to the MCP client transport (used for auth).
- opencode tries HTTP streamable first, falls back to SSE on 404 — some known bugs around this (see github issues). Prefer HTTP streamable.
- MCP add failures at `session/new` are swallowed (logged but don't fail the call). **Start our HTTP MCP server BEFORE `session/new`** to avoid silent no-ops.

### Other opencode quirks

- opencode's ACP bridge talks to an internal opencode HTTP server over loopback — every action is a local TCP round-trip. Pass `--hostname 127.0.0.1 --port 0` when spawning.
- Streaming re-fetches the full message on every delta (`sdk.session.message`). Mild O(n²) server-side work for long outputs. Not our problem but informs timeout tuning.
- `bash` tool call completion carries full stdout in `content`. No truncation at ACP layer. Large outputs = large notifications.
- `session/list` uses ms-timestamp-as-cursor internally. Two sessions sharing a ms could be lost across pages (edge case; treat cursor as opaque anyway).
- `session/cancel` emitted mid-prompt can race the final `usage_update` — don't rely on seeing `usage_update` before the prompt response.
- Image content block `uri` check is `startsWith("http:")` — `https:` URIs are dropped. Probable bug. Use data URIs in our SDK.
- Audio content blocks are silently dropped.
- opencode ACP **never emits** `current_mode_update`, `config_option_update`, `session_info_update`.

---

## Part 3: What we build

### Public API surface

```go
package opencodesdk

// One-shot
func Query(ctx context.Context, prompt string, opts ...Option) (*Result, error)
func QueryStream(ctx context.Context, messages MessageStream, opts ...Option) (MessageIter, error)

// Stateful
func NewClient(opts ...Option) (Client, error)
func WithClient(ctx context.Context, fn func(Client) error, opts ...Option) error

type Client interface {
    Start(ctx context.Context) error
    Close() error

    // Stable ACP surface
    NewSession(ctx context.Context, opts ...SessionOption) (Session, error)
    LoadSession(ctx context.Context, id string, opts ...SessionOption) (Session, error)
    ListSessions(ctx context.Context, opts ...ListOption) ([]SessionInfo, error)

    // opencode unstable
    ForkSession(ctx context.Context, id string, opts ...SessionOption) (Session, error)
    ResumeSession(ctx context.Context, id string, opts ...SessionOption) (Session, error)
}

type Session interface {
    ID() string
    Prompt(ctx context.Context, blocks ...ContentBlock) (MessageIter, error)
    Cancel(ctx context.Context) error

    SetModel(ctx context.Context, modelID string) error
    SetMode(ctx context.Context, modeID string) error
    SetConfigOption(ctx context.Context, optionID, value string) error

    AvailableModels() []ModelInfo
    AvailableCommands() []SlashCommand
    CurrentVariant() (variant string, available []string)
}

// Options (selected)
func WithModel(id string) Option
func WithAgent(mode string) Option                     // "build", "plan", ...
func WithCwd(path string) Option
func WithMCPServers(servers ...MCPServer) Option       // user-defined external MCP
func WithSDKTools(tools ...Tool) Option                // in-process via HTTP bridge
func WithHooks(hooks ...Hook) Option
func WithCanUseTool(cb PermissionCallback) Option      // session/request_permission
func WithOnFsWrite(cb FsWriteCallback) Option          // fs/write_text_file
func WithLogger(log *slog.Logger) Option
func WithMeterProvider(mp metric.MeterProvider) Option
func WithTracerProvider(tp trace.TracerProvider) Option
func WithCLIPath(path string) Option
func WithCLIFlags(flags ...string) Option
func WithInitializeTimeout(d time.Duration) Option
func WithAutoLaunchLogin(enabled bool) Option          // uses _meta["terminal-auth"]

// Errors
var ErrAuthRequired = errors.New("opencode auth required; run `opencode auth login`")
var ErrCancelled = errors.New("prompt cancelled")
// + typed wrappers for -32601/-32602/-32603
```

### Subprocess lifecycle

`internal/subprocess/acp.go`:
1. Discover `opencode` binary (honor `WithCLIPath`, then `$PATH`).
2. Verify version via `opencode --version` ≥ 1.14.20 (skippable with option).
3. Spawn `opencode acp --hostname 127.0.0.1 --port 0` + user flags. Capture stderr into the logger.
4. Wire `os.Pipe`s into `coder/acp-go-sdk`'s `ClientSideConnection`.
5. Run `initialize`; store capabilities + `agentInfo`.
6. On `Client.Close`, send shutdown, wait for process exit with timeout, then SIGKILL.
7. Process group handling from codex fork (`process_group_unix.go`, `process_group_windows.go`) — keep as-is.

### Option → session/new translation

Options are accumulated on a builder that produces a `NewSessionRequest`:
- `WithModel(id)` → post-session `SetConfigOption("model", id)` (or pre-set via config before `session/new` if possible)
- `WithAgent("plan")` → post-session `SetConfigOption("mode", "plan")`
- `WithCwd(path)` → `NewSessionRequest.Cwd`
- `WithMCPServers(...)` → appended to `NewSessionRequest.McpServers`
- `WithSDKTools(...)` → HTTP bridge server added to `McpServers`

Since `session/new` creates the session at the agent's default model/mode, `WithModel`/`WithAgent` are applied via `set_config_option` immediately after create. This costs one extra RPC; acceptable.

### HTTP MCP bridge (`internal/mcp/bridge/`)

Keeps `WithSDKTools` working. Design:

1. On `Client.Start` (if any SDK tools registered):
   - Bind `net.Listen("tcp", "127.0.0.1:0")` — pick ephemeral port.
   - Generate 32-byte random bearer token via `crypto/rand`.
   - Start `http.Server` hosting `github.com/modelcontextprotocol/go-sdk/mcp.StreamableHTTPHandler` with a middleware that checks `Authorization: Bearer <token>`.
   - Populate MCP server's tool registry from `WithSDKTools` entries.
2. On every `session/new`:
   - Prepend `McpServerHttp{name:"__opencodesdk_inproc", url:"http://127.0.0.1:<port>/mcp", headers:[{name:"Authorization", value:"Bearer <token>"}]}` to `mcpServers`.
3. On `Client.Close`:
   - `http.Server.Shutdown(ctx)` with a 5s timeout.

Risks:
- opencode's MCP add can fail silently. Mitigation: health-check the bridge before `session/new`, and poll `/mcp/health` after to verify opencode connected.
- opencode may prefer SSE first. Mitigation: send `McpServerHttp`, not `McpServerSse`. The go-sdk's streamable HTTP handler should serve both endpoints correctly; verify with probe before shipping.
- CVE-2026-33252 style cross-site: binding to 127.0.0.1 + bearer token handles this.

### Permission handler

Registered with coder SDK's `ClientSideConnection` for `session/request_permission`:

```go
func (c *client) handlePermissionRequest(ctx context.Context, req acp.RequestPermissionParams) (acp.RequestPermissionResponse, error) {
    if c.opts.CanUseTool == nil {
        // auto-reject if no callback — opencode will mark tool failed
        return acp.RequestPermissionResponse{Outcome: acp.PermissionOutcomeSelected{OptionID: "reject"}}, nil
    }
    decision, err := c.opts.CanUseTool(ctx, req.ToolCall, req.Options)
    if err != nil {
        return {}, err
    }
    return acp.RequestPermissionResponse{Outcome: decision}, nil
}
```

Respect `ctx.Done()`: coder SDK sends `$/cancel_request` on cancel — we return `{outcome:"cancelled"}`.

### fs/write_text_file handler

Opencode emits this after an approved `edit` to sync client buffers. Default behavior: write to disk. Hook point:

```go
func (c *client) handleFsWrite(ctx context.Context, req acp.WriteTextFileParams) (acp.WriteTextFileResponse, error) {
    if c.opts.OnFsWrite != nil {
        if err := c.opts.OnFsWrite(ctx, req.Path, req.Content); err != nil {
            return {}, err
        }
        return {}, nil
    }
    // default: write through
    if !filepath.IsAbs(req.Path) { return {}, errors.New("path must be absolute") }
    return {}, os.WriteFile(req.Path, []byte(req.Content), 0o644)
}
```

Enforce absolute path. Optionally enforce cwd containment (opt-in via `WithStrictCwdBoundary(true)`).

### Auth error surfacing

Wrap coder SDK's error types. On any method returning `-32000`, return `ErrAuthRequired` with context (command to run).

### Unstable method wrappers

coder/acp-go-sdk exposes `connection.CallMethod(ctx, method, params, result)` for arbitrary methods. Our `Client.ForkSession`/`ResumeSession`/`SetModel` call these with typed params:

```go
func (c *client) ForkSession(ctx context.Context, id string, opts ...SessionOption) (Session, error) {
    params := buildForkParams(id, opts)
    var result forkSessionResult
    if err := c.conn.CallMethod(ctx, "unstable_forkSession", params, &result); err != nil {
        return nil, wrapErr(err)
    }
    return c.adoptSession(result), nil
}
```

Parse `_meta.opencode.variant` from responses into `Session.CurrentVariant()`.

---

## Part 4: Codex fork cleanup

Before building anything, delete the codex fork's code. What survives:

**Keep (in spirit, may rewrite):**
- `go.mod` module path (rename to opencode)
- `.github/`, `.golangci.yml`, `Makefile`, `scripts/`
- `.claude/rules/*` — update content, keep structure
- `LICENSE`
- `.gitignore` (already reasonable)
- Functional-options pattern from `options.go` (pattern, not content)
- `process_group_*.go` from `internal/subprocess/`

**Delete wholesale:**
- `internal/subprocess/appserver.go` + `appserver_adapter*.go` + `jsonrpc.go` (coder SDK replaces)
- `internal/protocol/` (coder SDK replaces)
- `internal/message/` (coder SDK schema replaces)
- `internal/session/` (opencode owns persistence)
- `internal/cli/` (rewrite for opencode discovery)
- `internal/config/capability.go` (no dual backend)
- All codex-specific internal packages: `elicitation/`, `userinput/`, `sandbox/`, `model/` (or rewrite minimal versions)
- All codex option dirs: `extended_thinking/`, `service_tier/`, `personality/`, `developer_instructions/`, `user_input_callback/`, `include_partial_messages/`, `memories/`, `memory/`
- Dead artifacts: `hello.txt`, `tmp/`, `cancellation_demo.txt`, `graceful_shutdown_demo.txt`, `coverage.out`, `options.md`
- Codex-specific root files: `sessions.go`, `session_stat.go`, `models.go`, `sdk_mcp_server.go` (rewrite for HTTP bridge)
- SQLite dep from `go.mod`
- codex observability module dep (replace with direct OTel)
- Most of `examples/` — keep only `quick_start` and rebuild

**Option constructors to delete:**
`WithSystemPromptPreset`, `WithPermissionMode` (opencode uses agents), `WithPermissionPromptToolName`, `WithEffort`, `WithServiceTier`, `WithPersonality`, `WithDeveloperInstructions`, `WithTools`/`WithAllowedTools`/`WithDisallowedTools` (opencode manages tools per-agent), `WithSandbox`, `WithImages`, `WithConfig` (codex `-c` flags), `WithOutputFormat`, `WithOutputSchema`, `WithCodexHome`, `WithOnUserInput`, `WithOnElicitation`, `WithIncludePartialMessages`, `WithContinueConversation` (ACP has no equivalent), `WithMaxTurns` (not ACP-native), `WithAddDirs`.

**Option constructors to keep or add:**
Keep: `WithLogger`, `WithCwd`, `WithCLIPath` (renamed from `WithCliPath`), `WithEnv`, `WithStderr`, `WithInitializeTimeout`, `WithMCPServers`, `WithSDKTools`, `WithHooks`, `WithCanUseTool`, `WithMeterProvider`, `WithTracerProvider`, `WithPrometheusRegisterer`, `WithResume`, `WithForkSession`, `WithSkipVersionCheck`.

Add: `WithModel`, `WithAgent`, `WithOnFsWrite`, `WithCLIFlags`, `WithAutoLaunchLogin`, `WithStrictCwdBoundary`.

---

## Part 5: Observability

New metric namespace: `opencodesdk.*`.

### Metrics (OTel)

- `opencodesdk.session.prompt.duration` — histogram, seconds, `{stop_reason, model, mode}`
- `opencodesdk.session.prompt.tokens` — histogram, `{direction: input|output|cached_read|cached_write|thought, model}`
- `opencodesdk.session.update.count` — counter, `{variant}` where variant is the sessionUpdate discriminator
- `opencodesdk.tool_call.duration` — histogram, `{tool_name, tool_kind, status}`
- `opencodesdk.tool_call.count` — counter, `{tool_name, status}`
- `opencodesdk.permission.request` — counter, `{outcome, tool_kind}`
- `opencodesdk.fs.delegated` — counter, `{op: read|write, outcome}` (only write expected from opencode)
- `opencodesdk.cost.usd` — counter, `{model}`, incremented from `usage_update`
- `opencodesdk.mcp_bridge.request` — counter, `{tool, status}`
- `opencodesdk.initialize.duration` — histogram
- `opencodesdk.cli.spawn` — counter, `{exit_code}`

### Spans (OTel)

- `opencodesdk.session.prompt` — one span per `session/prompt`, attrs include `{session.id, stop_reason, tokens.*, model, mode}`
- `opencodesdk.tool_call` — child span per tool invocation
- `opencodesdk.permission_request` — child span
- `opencodesdk.fs_delegation` — child span
- `opencodesdk.initialize` — handshake span
- `opencodesdk.subprocess` — root span covering the opencode acp process lifetime

No Prometheus bridge by default — rely on OTel → Prom via user-supplied bridge. Keep `WithPrometheusRegisterer` as sugar that creates an OTel MeterProvider from a registerer.

---

## Part 6: Migration order (commits on `feat/baseline`)

Milestone sizes revised for the thin-wrapper architecture.

**Milestone 0 — baseline commit (done-ish)**

- Commit current state (forked codex code) + this INIT.md as-is.
- Prefix: `baseline:`.
- Tree is the pre-migration "before" state.

**Milestone 1 — nuke codex code + module rename (0.5 day)**

- Delete every file listed in § Part 4 "Delete wholesale".
- Rename module to `github.com/ethpandaops/opencode-agent-sdk-go`, package `codexsdk` → `opencodesdk`.
- Add `github.com/coder/acp-go-sdk` dep.
- Drop SQLite dep.
- Result: a nearly empty repo with `go.mod`, `.gitignore`, `.github/`, `LICENSE`, INIT.md, and an empty `opencodesdk` package skeleton.
- Commit prefix: `nuke:`.

**Milestone 2 — subprocess + ACP client plumbing (1 day)**

- `internal/cli/`: opencode binary discovery, version check ≥ 1.14.20.
- `internal/subprocess/acp.go`: spawn `opencode acp --hostname 127.0.0.1 --port 0`, wire stdio into coder SDK's ClientSideConnection, initialize handshake.
- `client.go` + `client_impl.go`: `NewClient`, `Client.Start`, `Client.Close`.
- `errors.go`: `ErrAuthRequired`, `ErrCancelled`, error wrapping.
- End state: `Client.Start(ctx)` negotiates initialize and reports capabilities. `Client.Close()` cleanly shuts down.
- Commit prefix: `transport:`.

**Milestone 3 — sessions + prompt (1 day)**

- `Client.NewSession`, `Client.LoadSession`, `Client.ListSessions`.
- `Session.Prompt` returns a message iterator backed by `session/update` notifications.
- `Session.Cancel` sends notification + handles the thrown error via context cancel.
- Options: `WithCwd`, `WithModel`, `WithAgent`, `WithMCPServers` (external only for now).
- Parse `_meta.opencode.variant` + `models` block.
- End state: `examples/quick_start` works end-to-end.
- Commit prefix: `transport:` or `options:`.

**Milestone 4 — handlers (0.5 day)**

- Register `session/request_permission` handler → `WithCanUseTool`.
- Register `fs/write_text_file` handler → default-write + `WithOnFsWrite` hook.
- Register no-op `fs/read_text_file` for completeness (opencode never calls it).
- End state: Permission prompts surface to user code; edits sync through.
- Commit prefix: `handlers:`.

**Milestone 5 — auth error surfacing (0.25 day)**

- Map `-32000` from any RPC → `ErrAuthRequired` with message.
- Optional `WithAutoLaunchLogin` — spawn `opencode auth login` in a PTY if `_meta["terminal-auth"]` present and option enabled.
- End state: running against an unauthenticated opencode gives a clear error.
- Commit prefix: `errors:`.

**Milestone 6 — HTTP MCP bridge (2–3 days)**

- Probe first: verify opencode connects to a 127.0.0.1 HTTP MCP server with bearer auth. If no-go, iterate on transport (streamable vs SSE).
- `internal/mcp/bridge/`: http.Server + go-sdk MCP StreamableHTTPHandler + bearer middleware + port picker + graceful shutdown.
- `WithSDKTools` wires the bridge into `session/new`'s mcpServers.
- End state: `examples/sdk_tools` works end-to-end.
- Commit prefix: `mcp-bridge:`.

**Milestone 7 — unstable methods (0.5 day)**

- `Client.ForkSession`, `Client.ResumeSession`, `Session.SetModel` via coder SDK's extension method call.
- Typed param/result structs in `internal/unstable/`.
- End state: opencode-specific session ops available.
- Commit prefix: `unstable:`.

**Milestone 8 — observability (1 day)**

- OTel spans + metrics under `opencodesdk.*`.
- Wire session lifecycle, prompt spans, tool call spans.
- Accept external MeterProvider/TracerProvider/Prometheus registerer via options.
- End state: a Prom scrape shows the right names.
- Commit prefix: `otel:`.

**Milestone 9 — examples rebuild (1 day)**

- `quick_start` (already there)
- `sdk_tools` — in-process tool via bridge
- `external_mcp` — user-supplied MCP server
- `session_list` — enumerate + load prior session
- `permission_callback` — user approves/denies tool use
- `fs_intercept` — rewrite/reject opencode's writes
- `plan_mode` — `WithAgent("plan")`, demonstrates permission prompts
- Commit prefix: `examples:`.

**Milestone 10 — docs (0.5 day)**

- `README.md`, `doc.go`, `CLAUDE.md`, `.claude/rules/*`.
- Update `version.go`.
- Commit prefix: `docs:`.

Total: ~8–10 days, 10 commits. Materially smaller than the pre-adoption plan (which estimated 8–11 days doing far more work).

---

## Part 7: Open questions

Resolved during implementation, but flagged so we don't forget:

1. **Bridge health check shape.** Does go-sdk's `StreamableHTTPHandler` expose a liveness endpoint, or do we roll our own `/health`?
2. **opencode ↔ bridge negotiation.** Streamable vs SSE. Need empirical confirmation before Milestone 6 commits. See opencode issues [#6242](https://github.com/sst/opencode/issues/6242), [#8058](https://github.com/sst/opencode/issues/8058), [#16247](https://github.com/sst/opencode/issues/16247).
3. **`$/cancel_request` during `session/request_permission`.** coder SDK claims it wires this, but we should integration-test that a cancel during a user callback flows through correctly.
4. **`unstable_forkSession` response shape variance.** Source indicates it returns `{sessionId?}` — when is it absent? Same-session vs new-session fork semantics.
5. **`usage_update` derivation.** opencode computes `used = input + cached.read` and `size = context_window_from_provider`. If the provider doesn't advertise context window, opencode **skips** the update silently. Don't error on missing updates; treat as optional.
6. **Permission reply persistence.** `"always"` replies are in-memory only in opencode. If we need persistence, it has to be client-side (write to our own store, replay as user config). Punt for now.
7. **Protocol version**. opencode ignores client-sent version. If we ever need to detect opencode specifically vs other ACP agents, use `agentInfo.name === "OpenCode"`.
8. **Plan-mode slash exit.** `plan_exit` permission gates exiting plan mode; UX for this via our SDK needs thought.
9. **Stderr structured-log parsing.** opencode prints structured logs to stderr. Default: pass through to user-supplied `WithStderr` callback. Optional: parse as JSON and surface via logger if `--log-level` set.
10. **Image URI `http:` bug**. Avoid; use data URIs. File upstream issue against opencode.

---

## Part 8: Out of scope

- Wrapping `opencode run --format json` (lossy, one-shot).
- Wrapping `opencode serve` HTTP API (covered by `sst/opencode-sdk-go`; no type reuse).
- Supporting non-opencode ACP agents. coder/acp-go-sdk is generic, but our façade is opencode-shaped.
- Session persistence outside of what opencode already provides (no client-side session store).
- Auto-compaction orchestration (opencode has `/compact` slash command).
- Recovery / retry orchestrator.
- Migrating codex observability counters.

---

## Part 9: Reference URLs

- ACP spec: https://agentclientprotocol.com
- ACP typescript SDK: https://github.com/agentclientprotocol/typescript-sdk
- ACP Rust reference: https://github.com/agentclientprotocol/agent-client-protocol
- ACP Python SDK: https://github.com/agentclientprotocol/python-sdk
- **Go ACP SDK (our dep):** https://github.com/coder/acp-go-sdk
- opencode: https://github.com/sst/opencode
- opencode docs: https://opencode.ai/docs
- opencode HTTP SDK (not reused): https://github.com/sst/opencode-sdk-go
- MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk
- Zed ACP client: https://github.com/zed-industries/zed/tree/main/crates/agent_servers
- Community libraries page: https://agentclientprotocol.com/libraries/community.md
