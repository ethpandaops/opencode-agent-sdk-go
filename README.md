# opencode-agent-sdk-go

> ⚠️ Work in progress. This repo is under active migration from a fork of
> `codex-agent-sdk-go`. Nothing here is usable yet. See
> [`INIT.md`](./INIT.md) for the full plan.

A Go SDK that spawns the [`opencode`](https://github.com/sst/opencode) CLI
in its Agent Client Protocol (ACP) mode and drives it over stdio JSON-RPC.

Built on top of [`coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk)
for the protocol layer. This package adds opencode subprocess
management, typed wrappers for opencode's unstable methods, in-process
MCP tool support (`WithSDKTools`), permission/filesystem callbacks, and
OpenTelemetry observability.

## Requirements

- Go 1.26+
- [`opencode`](https://github.com/sst/opencode) CLI v1.14.20+ in `$PATH`
- A working opencode auth (`opencode auth login`)

## Status

Under migration on branch `feat/baseline`. No commits are pushed to
`origin` until the branch is maintainer-approved.

## License

MIT — see [LICENSE](./LICENSE).
