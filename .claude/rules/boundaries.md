# Boundaries

## Always

- Follow existing patterns in neighboring code before introducing new patterns.
- Keep behavior changes covered by tests in the same commit.
- Run relevant tests for changed code; run `go test ./...` for substantial changes.
- Keep user-facing and agent-facing docs aligned when public behavior changes.

## Ask First

- Adding exported API surface (new public functions/types/options).
- Changing transport or protocol semantics.
- Adding third-party dependencies beyond those in `go.mod`.
- Making breaking behavior changes to existing option semantics.

## Never

- Leave errors unchecked.
- Store `context.Context` in structs.
- Reintroduce dual-backend routing or a client-side SQLite session
  store — opencode owns persistence.
- Reimplement ACP JSON-RPC framing or schema types — those belong to
  `coder/acp-go-sdk`.
