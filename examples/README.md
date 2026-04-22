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
| `session_list` | Enumerate prior sessions scoped to the working directory. |
| `permission_callback` | Interactively approve/deny tool calls via stdin. Requires `"permission": {"edit": "ask"}` in `~/.config/opencode/config.json` to actually trigger prompts — the default `build` agent auto-allows everything. |
| `fs_intercept` | Override `fs/write_text_file` delegations with `WithOnFsWrite` to capture writes in memory instead of hitting disk. Same permission prerequisite as above. |

## Tips

- The SDK is silent by default. Pass `WithLogger(slog.New(...))` if
  you want to see what's happening on stderr.
- Streaming updates arrive on `Session.Updates()` in order; read that
  channel in a goroutine if you want to see the agent's thought /
  message deltas as they arrive.
- opencode's default `build` agent is configured `"*": "allow"`, so
  `session/request_permission` is never emitted without extra
  config. The `plan` agent is where the ask-path actually fires.
