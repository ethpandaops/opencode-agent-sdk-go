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
| `sdk_tools` | Register an in-process Go function as a tool via `WithSDKTools`; attach `ToolAnnotations` (read-only / non-destructive hints) via `WithToolAnnotations`; the SDK serves the tool to opencode through a loopback HTTP MCP bridge. |
| `external_mcp` | Attach an external stdio MCP server via `WithMCPServers`. Defaults to `npx -y @modelcontextprotocol/server-everything`; override with `EXTERNAL_MCP_COMMAND`. |
| `session_list` | Enumerate prior sessions scoped to the working directory. |
| `permission_callback` | Interactively approve/deny tool calls via stdin. Requires `"permission": {"edit": "ask"}` in `~/.config/opencode/config.json` to actually trigger prompts — the default `build` agent auto-allows everything. Runs in an isolated tempdir. |
| `fs_intercept` | Override `fs/write_text_file` delegations with `WithOnFsWrite` to capture writes in memory instead of hitting disk. Same permission prerequisite as above. Runs in an isolated tempdir. |
| `plan_mode` | Select opencode's plan mode via `WithInitialMode(ModePlan)` to demonstrate its read-only posture — plan's ruleset denies edits inline (it does NOT route through `session/request_permission`). Also lists `Session.AvailableModes()`. |
| `query_stream` | Run a list of prompts against a single long-lived session via `QueryStream` and iterate results with range-over-func. |
| `parallel_queries` | Fan out N independent one-shot `Query` calls concurrently, each in its own opencode subprocess. |
| `cancellation` | Cancel an in-flight turn via `Session.Cancel` (or `Client.CancelAll` to fan out across every live session); catch `ErrCancelled` on the pending `Prompt`. |
| `error_handling` | Trip every SDK sentinel error on purpose (`ErrCLINotFound`, `ErrClientClosed`, `ErrClientNotStarted`, `ErrClientAlreadyConnected`, …) and show how to match them via both sentinels and the `OpencodeSDKError` marker interface. |
| `stderr_callback` | Capture opencode's stderr line-by-line via `WithStderr`. |
| `custom_logger` | Route `opencodesdk`'s internal logs through a custom `slog.Handler`. |
| `multimodal_input` | Send a mixed text + image prompt via `QueryContent` + `ImageFileInput` + `Blocks`. |
| `query_stream_iter` | Drive `QueryStreamContent` with `PromptsFromChannel` to feed prompts dynamically. |
| `add_dirs` | Forward ACP's unstable `additionalDirectories` via `WithAddDirs`, with capability-probe fallback. |
| `custom_transport` | Inject a test-double `Transport` via `WithTransport`, bypassing the `opencode acp` subprocess entirely (useful for tests / embedded setups). |
| `resilient_query` | Wrap `Query` with retry-on-transient via `ResilientQuery` + `RetryPolicy`. |
| `pipeline` | Chain generate → evaluate → gate (Go) → refine on one session, using Go-side logic to gate on an LLM-scored threshold. |
| `typed_subscribers` | Register typed per-variant callbacks via `Session.Subscribe` + `WithOnTurnComplete` instead of demuxing raw `Session.Updates()`. |
| `iter_sessions` | Enumerate every session in the configured cwd via `Client.IterSessions`, which paginates through `session/list` transparently. |
| `fork_resume` | `Client.ForkSession` to branch a session (new id, inherited memory) and `Client.ResumeSession` to re-attach without replaying history. |
| `load_session` | `Client.LoadSession` to rehydrate a prior session by id; observe the full transcript replay that opencode emits on the wire. |
| `session_mutations` | `Session.SetModel` and `Session.SetMode` for intra-session config changes, with typed subscribers watching the resulting `SessionConfigOptionUpdate` / `CurrentModeUpdate` notifications. Enumerates `Session.AvailableModes()`. |
| `model_variant` | opencode-specific reasoning-effort variants: `Session.CurrentVariant`, `opencodesdk.OpencodeVariant`, and `Client.UnstableSetModel` for switching between e.g. `default` / `high` / `max`. |
| `effort` | Higher-level `WithEffort(EffortLow/Medium/High/Max)` that maps an abstract reasoning-depth enum to whatever variant the chosen model exposes (with sensible fallback). Sister to `model_variant` but doesn't require the caller to know the exact variant strings. |
| `max_turns` | `WithMaxTurns(n)` caps the number of assistant messages observed per session and calls `Session.Cancel` once the cap is crossed. Useful as a backstop against runaway agent loops. |
| `run_command` | `Session.RunCommand(name, args...)` invokes one of opencode's slash commands (advertised via `Session.AvailableCommands()`) as a prompt turn — sugar for sending the command as a leading-slash text block. |
| `prometheus_metrics` | Wire SDK metrics to a Prometheus registry via the built-in `WithPrometheusRegisterer` sugar and serve `/metrics` on `localhost:9090`. |
| `contrib_prometheus` | Build a Prometheus-backed OTel `MeterProvider` explicitly via `contrib/prometheus.NewMeterProvider` and pass it to `WithMeterProvider`. Use when you want to share the same provider with other OTel-instrumented code. Serves `/metrics` on `localhost:9091`. |
| `elicitation_callback` | Handle agent-initiated `elicitation/create` requests via `WithOnElicitation` and `WithOnElicitationComplete`. Forward-compatible: opencode 1.14.20 doesn't emit elicitation/create yet, so the callback is wired but dormant until a future ACP agent uses it. |
| `cost_tracker` | Feed a `CostTracker` from `UsageUpdate` notifications and persist the snapshot under `$XDG_DATA_HOME/opencode/sdk/session-costs/`. |
| `max_budget_usd` | Cap total USD spend with `WithMaxBudgetUSD`; the SDK auto-subscribes each session and calls `Session.Cancel` when the budget trips. `Client.BudgetTracker()` exposes the running snapshot. |
| `load_history` | Rehydrate a session with `Client.LoadSessionHistory` and inspect the replayed messages / raw notifications / final usage as a typed `SessionHistory`. |
| `set_config_option` | List `Session.InitialConfigOptions()` and drive `Session.SetConfigOption` / `SetConfigOptionBool` for arbitrary config ids beyond `model` / `mode`. |

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
