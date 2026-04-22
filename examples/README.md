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
| `permission_callback` | Interactively approve/deny tool calls via stdin. Requires `"permission": {"edit": "ask"}` in `~/.config/opencode/config.json` to actually trigger prompts — the default `build` agent auto-allows everything. Runs in an isolated tempdir. |
| `fs_intercept` | Override `fs/write_text_file` delegations with `WithOnFsWrite` to capture writes in memory instead of hitting disk. Same permission prerequisite as above. Runs in an isolated tempdir. |
| `plan_mode` | Select opencode's `plan` agent via `WithAgent("plan")` to demonstrate its read-only posture — plan's ruleset denies edits inline (it does NOT route through `session/request_permission`). |
| `query_stream` | Run a list of prompts against a single long-lived session via `QueryStream` and iterate results with range-over-func. |
| `parallel_queries` | Fan out N independent one-shot `Query` calls concurrently, each in its own opencode subprocess. |
| `cancellation` | Cancel an in-flight turn via `Session.Cancel`; catch `ErrCancelled` on the pending `Prompt`. |
| `error_handling` | Trip every SDK sentinel error on purpose (`ErrCLINotFound`, `ErrClientClosed`, `ErrClientNotStarted`, …) and show how to match them. |
| `stderr_callback` | Capture opencode's stderr line-by-line via `WithStderr`. |
| `custom_logger` | Route `opencodesdk`'s internal logs through a custom `slog.Handler`. |
| `multimodal_input` | Send a mixed text + inline-image prompt using `acp.TextBlock` + `acp.ImageBlock`. |
| `pipeline` | Chain generate → evaluate → gate (Go) → refine on one session, using Go-side logic to gate on an LLM-scored threshold. |
| `typed_subscribers` | Register typed per-variant callbacks via `Session.Subscribe` + `WithOnTurnComplete` instead of demuxing raw `Session.Updates()`. |
| `iter_sessions` | Enumerate every session in the configured cwd via `Client.IterSessions`, which paginates through `session/list` transparently. |
| `fork_resume` | `Client.ForkSession` to branch a session (new id, inherited memory) and `Client.ResumeSession` to re-attach without replaying history. |
| `load_session` | `Client.LoadSession` to rehydrate a prior session by id; observe the full transcript replay that opencode emits on the wire. |
| `session_mutations` | `Session.SetModel` and `Session.SetMode` for intra-session config changes, with typed subscribers watching the resulting `SessionConfigOptionUpdate` / `CurrentModeUpdate` notifications. |
| `model_variant` | opencode-specific reasoning-effort variants: `Session.CurrentVariant`, `opencodesdk.OpencodeVariant`, and `Client.UnstableSetModel` for switching between e.g. `default` / `high` / `max`. |
| `prometheus_metrics` | Wire `WithMeterProvider` to a Prometheus registry and serve `/metrics` on `localhost:9090`. Lives in its own Go sub-module so Prometheus deps don't leak into the root module. |

For one-shot interactions, [`opencodesdk.Query`](../query.go) and
[`opencodesdk.WithClient`](../with_client.go) wrap the lifecycle shown
above into a single call — see the root README for the shorter form.

## Tips

- The SDK is silent by default. Pass `WithLogger(slog.New(...))` if
  you want to see what's happening on stderr.
- Streaming updates arrive on `Session.Updates()` in order; read that
  channel in a goroutine if you want to see the agent's thought /
  message deltas as they arrive.
- opencode's default permission ruleset is `"*": "allow"`, so
  `session/request_permission` is never emitted out of the box.
  Configure `"permission": {"edit": "ask"}` (or similar per-tool rules)
  in `~/.config/opencode/config.json` to exercise `WithCanUseTool` /
  `WithOnFsWrite`. The built-in `plan` agent does NOT use the ask path
  — it denies edits inline.
- Examples that may trigger file writes (`permission_callback`,
  `fs_intercept`, `plan_mode`) isolate themselves in a tempdir so an
  approved edit can't escape into your current directory.
