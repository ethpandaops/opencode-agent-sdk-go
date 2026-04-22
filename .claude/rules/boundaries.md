# Boundaries

## Always

- Follow existing patterns in neighboring code before introducing new patterns.
- Keep behavior changes covered by tests in the same commit.
- Run relevant tests for changed code; run `go test ./...` for substantial changes.
- Keep user-facing and agent-facing docs aligned when public behavior changes.
- Respect INIT.md's locked decisions; if one must change, update INIT.md in the same commit.

## Ask First

- Adding exported API surface (new public functions/types/options).
- Changing transport or protocol semantics beyond what INIT.md describes.
- Adding third-party dependencies beyond those listed in INIT.md.
- Making breaking behavior changes to existing option semantics.

## Never

- Leave errors unchecked.
- Store `context.Context` in structs.
- Reintroduce codex-specific terminology, dual-backend routing, or the
  SQLite session store.
- Reimplement ACP JSON-RPC framing or schema types — those belong to
  `coder/acp-go-sdk`.
- Push `feat/baseline` to `origin` during the migration.
