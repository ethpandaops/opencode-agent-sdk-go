# Architecture

## Transport model

Single transport: `opencode acp` subprocess over stdio JSON-RPC. The
protocol layer (JSON-RPC framing, schema types, request/response
correlation, notification dispatch, generic cancellation) is provided
by [`github.com/coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk) —
we do **not** reimplement it.

## Backend routing

None. opencode has one ACP mode. `Query(...)`, `QueryStream(...)`, and
`NewClient(...)` all go through the same `opencode acp` subprocess.

## High-level components

- `internal/subprocess/`: opencode subprocess lifecycle + process-group
  handling, stdio wiring into coder/acp-go-sdk's `ClientSideConnection`
- `internal/cli/`: binary discovery + version check
- `internal/handlers/`: implementations of agent-initiated RPCs
  (`session/request_permission`, `fs/write_text_file`, cwd-boundary
  enforcement)
- `internal/mcp/bridge/`: loopback HTTP MCP server for `WithSDKTools`
- `internal/observability/`: OTel spans + metrics under `opencodesdk.*`

Typed wrappers for opencode's unstable methods
(`unstable_forkSession`, `unstable_resumeSession`,
`unstable_setSessionModel`) and `_meta.opencode.variant` parsing live
in the top-level `unstable.go`.

## Session persistence

opencode owns it. Sessions persist in `$XDG_DATA_HOME/opencode/opencode.db`
(SQLite) and survive `opencode acp` restarts. Listing is via
`session/list`; no client-side session metadata store.

## Change impact guidance

When changing options, transport, or session behavior:

- Update affected tests in the same commit.
- Align `README.md` and `doc.go` with new behavior.
