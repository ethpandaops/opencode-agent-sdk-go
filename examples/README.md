# Examples

Runnable examples for `opencode-agent-sdk-go`. Each example is a
standalone `main` package you can run with `go run`:

```bash
go run ./examples/quick_start
```

All examples assume `opencode` (≥ 1.14.20) is on `$PATH` and you've
run `opencode auth login` out of band.

| Example | Shows |
| --- | --- |
| `quick_start` | Spawn opencode, create a session, prompt it, stream the response. |
| `sdk_tools` | Register an in-process Go function as a tool via `WithSDKTools`; the SDK serves it to opencode through a loopback HTTP MCP bridge. |
| `external_mcp` | Attach an external stdio MCP server via `WithMCPServers`. Defaults to `npx -y @modelcontextprotocol/server-everything`; override with `EXTERNAL_MCP_COMMAND`. |
| `session_list` | Enumerate prior sessions scoped to the working directory. |
| `permission_callback` | Interactively approve/deny tool calls via stdin. Requires `"permission": {"edit": "ask"}` in `~/.config/opencode/config.json` to actually trigger prompts — the default `build` agent auto-allows everything. |
| `fs_intercept` | Override `fs/write_text_file` delegations with `WithOnFsWrite` to capture writes in memory instead of hitting disk. Same permission prerequisite as above. |
| `plan_mode` | Select opencode's `plan` agent via `WithAgent("plan")` so `session/request_permission` actually fires; pairs with an auto-approving callback. |
| `query_stream` | Run a list of prompts against a single long-lived session via `QueryStream` and iterate results with range-over-func. |
| `parallel_queries` | Fan out N independent one-shot `Query` calls concurrently, each in its own opencode subprocess. |
| `cancellation` | Cancel an in-flight turn via `Session.Cancel`; catch `ErrCancelled` on the pending `Prompt`. |
| `error_handling` | Trip every SDK sentinel error on purpose (`ErrCLINotFound`, `ErrClientClosed`, `ErrClientNotStarted`, …) and show how to match them. |
| `stderr_callback` | Capture opencode's stderr line-by-line via `WithStderr`. |
| `custom_logger` | Route `opencodesdk`'s internal logs through a custom `slog.Handler`. |
| `multimodal_input` | Send a mixed text + inline-image prompt using `acp.TextBlock` + `acp.ImageBlock`. |
| `pipeline` | Chain generate → evaluate → gate (Go) → refine on one session, using Go-side logic to gate on an LLM-scored threshold. |

For one-shot interactions, [`opencodesdk.Query`](../query.go) and
[`opencodesdk.WithClient`](../with_client.go) wrap the lifecycle shown
above into a single call — see the root README for the shorter form.

## Tips

- The SDK is silent by default. Pass `WithLogger(slog.New(...))` if
  you want to see what's happening on stderr.
- Streaming updates arrive on `Session.Updates()` in order; read that
  channel in a goroutine if you want to see the agent's thought /
  message deltas as they arrive.
- opencode's default `build` agent is configured `"*": "allow"`, so
  `session/request_permission` is never emitted without extra
  config. The `plan` agent is where the ask-path actually fires.
