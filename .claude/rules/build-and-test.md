# Build and Test

## Default Commands

```bash
go build ./...
go test ./...
go test -race ./...
go test -v -run TestName ./...
go test -tags=integration ./integration/...
golangci-lint run
go run ./examples/quick_start
```

## Command Usage

- Use targeted tests first while iterating (single package or `-run` pattern).
- Before finishing substantial code changes, run `go test ./...`.
- Run `go test -race ./...` for concurrency-sensitive changes.
- Run `golangci-lint run` before finalizing when Go files changed.

## Integration Test Notes

- Integration tests require the `opencode` CLI (≥ 1.14.20) in `$PATH`
  and a working opencode auth state (`opencode auth login`).
- If integration tests are not runnable in the current environment,
  call that out explicitly rather than skipping silently.

## Commit Hygiene

- Every commit must pass `go build ./...`.
- Do not land commits with red tests without calling it out explicitly
  in the commit message.
