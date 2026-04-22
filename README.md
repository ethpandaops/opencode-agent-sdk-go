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
- `session/request_permission` and `fs/write_text_file` callbacks
- **in-process Go tools** via a loopback HTTP MCP bridge
  (`WithSDKTools`) — no separate MCP server to run
- opencode's `terminal-auth` auth-flow hint extraction
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

    c, err := opencodesdk.NewClient(opencodesdk.WithCwd(cwd))
    if err != nil {
        panic(err)
    }
    defer c.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    if err := c.Start(ctx); err != nil {
        panic(err)
    }

    sess, err := c.NewSession(ctx)
    if err != nil {
        panic(err)
    }

    // Drain streaming updates in a goroutine.
    go func() {
        for n := range sess.Updates() {
            if n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.Content.Text != nil {
                fmt.Print(n.Update.AgentMessageChunk.Content.Text.Text)
            }
        }
    }()

    res, err := sess.Prompt(ctx, acp.TextBlock("Say hello in three words."))
    if err != nil {
        panic(err)
    }

    fmt.Printf("\nstop: %s\n", res.StopReason)
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
| `WithEnv(map)` | overlay on inherited env |
| `WithStderr(fn)` | stderr callback |
| `WithInitializeTimeout(d)` | handshake timeout (default 60s) |
| `WithSkipVersionCheck(bool)` | skip the ≥1.14.20 assertion |
| `WithModel(id)` | applied via `session/set_config_option` |
| `WithAgent(name)` | sets the opencode mode (`build`, `plan`, ...) |
| `WithMCPServers(servers...)` | external MCP servers |
| `WithSDKTools(tools...)` | in-process tools via the bridge |
| `WithCanUseTool(cb)` | permission-prompt callback |
| `WithOnFsWrite(cb)` | intercept `fs/write_text_file` |
| `WithUpdatesBuffer(n)` | per-session update channel size |
| `WithTerminalAuthCapability(bool)` | opt into opencode's `terminal-auth` launch hints |
| `WithMeterProvider(mp)` | OTel MeterProvider |
| `WithTracerProvider(tp)` | OTel TracerProvider |

## Examples

See [`examples/`](./examples/) for five working programs:

- `quick_start` — minimal round-trip
- `sdk_tools` — in-process tool via the bridge
- `session_list` — list prior sessions with pagination
- `permission_callback` — interactive permission UX
- `fs_intercept` — capture writes in memory instead of on disk

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
reimplement the ACP types or the JSON-RPC transport. See
[`INIT.md`](./INIT.md) for the migration log and the live-probed
protocol reference used to design this.

## License

MIT — see [LICENSE](./LICENSE).
