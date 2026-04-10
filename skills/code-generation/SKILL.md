---
name: code-generation
description: Best practices for writing idiomatic, production-quality Go code.
---

# Code Generation Skill

Use this skill when implementing Go code for any module in this repository.

## Idioms

- Prefer `errors.New` / `fmt.Errorf("%w", ...)` for error wrapping; never swallow errors silently.
- Use named return values only when they materially improve clarity (e.g., `(n int, err error)` is idiomatic).
- Return early on errors rather than deeply nesting success paths.
- Avoid `interface{}` / `any` in public API surface; use concrete or constrained generic types.
- Table-driven tests with `t.Run` are the default test structure.

## Package Layout

- One package per directory; avoid cyclic imports.
- Keep `internal/` packages unexported to the module boundary; expose stable API via top-level packages.
- Use `cmd/` only for `main` packages.

## Concurrency

- Prefer channels over shared memory for coordination between goroutines.
- Always pass `context.Context` as the first parameter to any function that may block.
- Use `sync.WaitGroup` or `errgroup.Group` for fan-out; document lifecycle clearly.

## Error Handling

- Sentinel errors (`var ErrFoo = errors.New("...")`) belong at the package level.
- Wrap errors with enough context to trace back to the call site without a stack trace.
- `log.Fatal` / `os.Exit` are only acceptable in `main()` after setup failures.

## Testing

- Use the standard `testing` package; only add testify if it meaningfully reduces boilerplate.
- Mock interfaces at package boundaries; never mock unexported functions.
- Aim for ≥80% statement coverage on new packages.

## References

- [go-idioms.md](references/go-idioms.md) — curated list of idiomatic patterns with before/after examples
