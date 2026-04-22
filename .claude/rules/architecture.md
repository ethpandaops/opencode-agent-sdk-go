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

## High-level components (target layout)

- `internal/subprocess/acp.go`: opencode subprocess lifecycle, stdio
  wiring into coder/acp-go-sdk's `ClientSideConnection`
- `internal/cli/`: binary discovery + version check
- `internal/handlers/`: implementations of agent-initiated RPCs
  (`session/request_permission`, `fs/write_text_file`)
- `internal/mcp/bridge/`: loopback HTTP MCP server for `WithSDKTools`
- `internal/unstable/`: typed wrappers for opencode's unstable methods
  (`unstable_forkSession`, `unstable_resumeSession`, `unstable_setSessionModel`)
  and `_meta.opencode.variant` parsing
- `internal/observability/`: OTel spans + metrics under `opencodesdk.*`

## Session persistence

opencode owns it. Sessions persist in `$XDG_DATA_HOME/opencode/opencode.db`
(SQLite) and survive `opencode acp` restarts. Listing is via
`session/list`; no client-side session metadata store.

## Change impact guidance

When changing options, transport, or session behavior:

- Update INIT.md locked decisions if the change affects a decision there.
- Update affected tests in the same commit.
- Align `README.md` and `doc.go` with new behavior.
- Do not push the branch.
