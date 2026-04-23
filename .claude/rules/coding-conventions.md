# Coding Conventions

## Core Style

- Keep package and naming consistent with existing code (`opencodesdk`).
- Keep context as the first parameter for blocking operations.
- Use functional options (`WithXxx(...)`) for configurable behavior.
- Re-export public-facing types from root package when following existing pattern.

## Logging

- Use structured `slog` logging.
- Prefer context-aware logging calls when a relevant context is available in that path.
- Keep log messages concise and action-oriented.

## Errors

- Wrap errors with `%w` so callers can use `errors.Is`/`errors.As`.
- Prefer typed or sentinel errors from `errors.go` for stable checks.
- Do not suppress or ignore returned errors.

## API and Option Changes

- Keep public option constructors in `options.go`.
- Options that require subprocess behavior change (e.g. CLI flags) go
  through the subprocess layer, not the ACP layer.
- Options that map to `session/new` params or post-session
  `set_config_option` calls are translated at session-start time.

## Repo Structure

- Keep implementation details in `internal/`.
- Avoid introducing generic catch-all packages (`utils`, `helpers`, `common`).
- Follow nearby file patterns before introducing new structure.
