# Project Overview

## Identity

- Repository: `github.com/ethpandaops/opencode-agent-sdk-go`
- Primary package: `opencodesdk`
- Go version: `1.26+`
- Minimum opencode CLI version: `1.14.20` (see `version.go`)
- ACP protocol version: `1`
- Protocol dep: `github.com/coder/acp-go-sdk` v0.12.0+

## Status

Stable public API. Opencode-focused Go SDK built on
`github.com/coder/acp-go-sdk`.

## What This SDK Exposes

- One-shot: `Query(ctx, prompt, opts...) (*QueryResult, error)`
- Multi-prompt iterator: `QueryStream(ctx, prompts, opts...)`
- Stateful client: `NewClient(opts...) (Client, error)` + `Session`
- Lifecycle helper: `WithClient(ctx, fn, opts...) error`
- Session enumeration: `Client.ListSessions(ctx, cursor)`
- opencode unstable: `Client.ForkSession`, `Client.ResumeSession`,
  `Client.UnstableSetModel`, `Session.CurrentVariant`

## Primary Public Surface

- `options.go`: all `WithXxx(...)` constructors
- `client.go` + `client_impl.go`: `Client` interface + implementation
- `session.go` + `session_impl.go`: `Session` interface + implementation
- `query.go`: top-level `Query` / `QueryStream` + `QueryResult`
- `with_client.go`: lifecycle helper
- `mcp.go`: `Tool` interface + `NewTool` + `WithSDKTools`
- `unstable.go`: opencode-specific RPC wrappers + `OpencodeVariant`
- `permissions.go`: `PermissionCallback`, helpers (`AllowOnce` etc.)
- `auth.go`: `TerminalAuthInstructions` + `TerminalAuthLaunch`
- `errors.go`: typed and sentinel errors (incl. `ErrAuthRequired`,
  `ErrCancelled`, `ErrCLINotFound`)

## Documentation Sync Expectations

When public APIs or options change, update in the same commit:

- `README.md` (user-facing)
- `doc.go` (package docs)
- `CLAUDE.md` / `.claude/rules/*` if agent guidance changed
