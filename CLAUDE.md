# AGENTS.md

## Core Facts

- **Module**: `github.com/ethpandaops/opencode-agent-sdk-go`
- **Primary package**: `opencodesdk`
- **Go**: `1.26+`
- **Minimum opencode CLI**: `1.14.20+`
- **ACP protocol**: version 1

## Dev Commands

```bash
go build ./...
go test ./...
go test -race ./...
go test -v -run TestName ./...
golangci-lint run
```

Run in order: lint → typecheck → test → test -race.

## Integration Tests

`integration/` requires `opencode` CLI in `$PATH` and a working auth state (`opencode auth login`).

## Key APIs

- **One-shot**: `Query(ctx, prompt, opts...) (*QueryResult, error)`
- **Iterator**: `QueryStream(ctx, prompts, opts...)`
- **Stateful**: `NewClient(opts...)` → `Client` → `Session`
- **Lifecycle helper**: `WithClient(ctx, fn, opts...) error`
- **Client-less**: `StatSession(ctx, sessionID, opts...)` reads opencode's SQLite (`$XDG_DATA_HOME/opencode/opencode.db`)
- **Session listing**: `Client.ListSessions(ctx, cursor)`

## Architecture

- Single transport: `opencode acp` over stdio JSON-RPC via `github.com/coder/acp-go-sdk`
- No dual-backend routing
- In-process tools via loopback HTTP MCP bridge (`WithSDKTools`)
- opencode owns session persistence in `$XDG_DATA_HOME/opencode/opencode.db`

## Boundaries

- Ask before adding exported API or new dependencies
- Keep behavior changes covered by tests in the same commit
- Keep docs aligned when public behavior changes (README.md, doc.go)
- Never ignore returned errors
- Never store `context.Context` in structs

## Local Claude Rules

For detailed guidance, load:
- `.claude/rules/project-overview.md`
- `.claude/rules/build-and-test.md`
- `.claude/rules/architecture.md`
- `.claude/rules/coding-conventions.md`
- `.claude/rules/boundaries.md`