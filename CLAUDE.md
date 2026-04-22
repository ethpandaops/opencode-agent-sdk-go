# CLAUDE.md

Shared repository instructions for coding agents.

## Status

This repository is mid-migration from `codex-agent-sdk-go` to an
opencode-focused SDK built on top of
[`coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk). The migration
plan + protocol reference lives in **INIT.md** at the repository root —
read it before making changes.

The `feat/baseline` branch is the migration branch. Do not push to
`origin` during the migration.

## Repository Facts

- Module: `github.com/ethpandaops/opencode-agent-sdk-go`
- Primary package: `opencodesdk`
- Go: `1.26+`
- Minimum opencode CLI: `1.14.20+` (see `version.go`)
- ACP protocol version: `1`

## Core APIs (target; being built)

- One-shot: `Query(ctx, prompt, opts...)`
- Streaming input: `QueryStream(ctx, messages, opts...)`
- Stateful sessions: `NewClient(opts...)` + `Client` + `Session`
- Lifecycle helper: `WithClient(ctx, fn, opts...)`
- Session listing: `Client.ListSessions(ctx, opts...)`

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

- Read INIT.md before structural changes during migration.
- Follow nearby code patterns before introducing new patterns.
- Keep behavior changes covered by tests in the same commit.
- Keep docs aligned when public behavior changes (README.md, doc.go).

Ask first:

- Adding exported API surface.
- Adding new third-party dependencies beyond those listed in INIT.md.
- Changing behaviors documented in INIT.md's locked decisions.

Never:

- Ignore returned errors.
- Store `context.Context` in structs.
- Reintroduce codex-specific terminology or backend-selection logic.
- Push `feat/baseline` to `origin` during migration.

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
