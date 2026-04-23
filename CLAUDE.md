# CLAUDE.md

Shared repository instructions for coding agents.

## Status

Opencode-focused Go SDK built on top of
[`coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk).

## Repository Facts

- Module: `github.com/ethpandaops/opencode-agent-sdk-go`
- Primary package: `opencodesdk`
- Go: `1.26+`
- Minimum opencode CLI: `1.14.20+` (see `version.go`)
- ACP protocol version: `1`

## Core APIs

- One-shot: `Query(ctx, prompt, opts...) (*QueryResult, error)`
- Multi-prompt iterator: `QueryStream(ctx, prompts, opts...)`
- Stateful sessions: `NewClient(opts...)` + `Client` + `Session`
- Lifecycle helper: `WithClient(ctx, fn, opts...) error`
- Session listing: `Client.ListSessions(ctx, cursor)`
- Client-less session stat: `StatSession(ctx, sessionID, opts...)`
  reads opencode's local SQLite (`$XDG_DATA_HOME/opencode/opencode.db`)
- opencode unstable: `Client.ForkSession`, `Client.ResumeSession`,
  `Client.UnstableSetModel`

## Canonical Commands

```bash
go build ./...
go test ./...
go test -race ./...
golangci-lint run
```

Integration tests live under `integration/` and require `opencode` in `$PATH`.

## Architecture Facts

- Only one transport: `opencode acp` over stdio JSON-RPC via
  `github.com/coder/acp-go-sdk`. No dual-backend routing.
- `Client.Start(...)` spawns the `opencode acp` subprocess and runs
  the ACP initialize handshake.
- In-process `WithSDKTools` is served by a loopback HTTP MCP bridge
  declared in `session/new`'s `mcpServers`.
- `authenticate` is opencode-unimplemented; auth errors surface as
  `ErrAuthRequired` on `session/new`/`session/load`.

## Boundaries

Always:

- Follow nearby code patterns before introducing new patterns.
- Keep behavior changes covered by tests in the same commit.
- Keep docs aligned when public behavior changes (README.md, doc.go).

Ask first:

- Adding exported API surface.
- Adding new third-party dependencies beyond those already in `go.mod`.
- Changing transport or protocol semantics.

Never:

- Ignore returned errors.
- Store `context.Context` in structs.
- Reintroduce dual-backend routing or a client-side session store.

## Claude Modules

For Claude Code, load and follow these detailed modules:

@.claude/rules/project-overview.md
@.claude/rules/build-and-test.md
@.claude/rules/architecture.md
@.claude/rules/coding-conventions.md
@.claude/rules/boundaries.md

If guidance appears to conflict, prioritize:
1. `boundaries.md`
2. `coding-conventions.md`
3. the remaining modules
