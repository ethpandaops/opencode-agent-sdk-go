# Project Overview

## Identity

- Repository: `github.com/ethpandaops/opencode-agent-sdk-go`
- Primary package: `opencodesdk`
- Go version: `1.26+`
- Minimum opencode CLI version: `1.14.20` (see `version.go`)
- ACP protocol version: `1`
- Protocol dep: `github.com/coder/acp-go-sdk` v0.12.0+

## Status

Under active migration on branch `feat/baseline`. Read `INIT.md` at the
repo root for the complete plan, milestone list, and protocol reference.

## What This SDK Exposes (target)

- One-shot query API: `Query(ctx, prompt, opts...)`
- Streaming input query API: `QueryStream(ctx, messages, opts...)`
- Stateful client API: `NewClient(opts...) (Client, error)` + `Session`
- Lifecycle helper: `WithClient(ctx, fn, opts...) error`
- Session enumeration: `Client.ListSessions(ctx, opts...)`
- opencode unstable wrappers: `Client.ForkSession`, `Client.ResumeSession`, `Session.SetModel`

## Primary Public Surface Areas (target layout)

- `options.go`: all `WithXxx(...)` constructors
- `client.go`: `Client` and `Session` interfaces
- `query.go`: top-level `Query` / `QueryStream`
- `with_client.go`: lifecycle helper
- `mcp.go`: `Tool` interface + `NewTool` + `WithSDKTools`
- `types.go`: re-exported ACP message/content/config types
- `errors.go`: typed and sentinel errors (incl. `ErrAuthRequired`, `ErrCancelled`)

## Documentation Sync Expectations

When public APIs or options change, update in the same commit:

- `README.md` (user-facing)
- `doc.go` (package docs)
- `CLAUDE.md` / `.claude/rules/*` if agent guidance changed
- `INIT.md` if any locked decision changes
