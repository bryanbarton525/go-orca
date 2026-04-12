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

## Testing

- **Test Isolation Rule**: Implementation and tests MUST be in separate files with matching package names.
- **Package Matching**: Test files (`*_test.go`) MUST share the same package declaration as their implementation file. Never use a different package name or a special `_test` package for Go code.
- Use the standard `testing` package; only add testify if it meaningfully reduces boilerplate.
- Mock interfaces at package boundaries; never mock unexported functions.
- Aim for ≥80% statement coverage on new packages.

### Consolidation Rule — CRITICAL

In remediation cycles, **never create new artifact versions for the same component**. If multiple artifacts exist for a requirement:

1. **Fix existing correct artifacts in-place** — preserve artifacts that already pass validation.
2. **Do not create new versions** — create new artifacts only when no valid version exists.
3. **Focus on remediation only** — apply minimal changes to address blocking issues.
4. **Prevent infinite loops** — consolidating prevents infinite remediation cycles where multiple versions of the same component are created and blocked.

This discipline ensures workflow progress by preventing artifact proliferation in remediation cycles.

## Go Testing Best Practices

- Implementation file: Contains only the public API and implementation code with exactly one `package <name>` declaration
- Test file: Contains only test code with the SAME `package <name>` declaration as the implementation
- Both files must compile independently with `go build` (implementation) and `go test` (tests)
- Never mix implementation and test code in a single file
- When fixing QA blocking issues, preserve existing correctly-structured artifacts and focus only on remediation

## Concurrency

- Prefer channels over shared memory for coordination between goroutines.
- Always pass `context.Context` as the first parameter to any function that may block.
- Use `sync.WaitGroup` or `errgroup.Group` for fan-out; document lifecycle clearly.

## Error Handling

- Sentinel errors (`var ErrFoo = errors.New("...")`) belong at the package level.
- Wrap errors with enough context to trace back to the call site without a stack trace.
- `log.Fatal` / `os.Exit` are only acceptable in `main()` after setup failures.

## Remediation Guidelines

When handling QA blocking issues:

1. **Read each issue carefully** — identify the exact requirement being violated
2. **Apply minimal changes** — fix only what's broken, don't refactor unrelated code
3. **Verify compilation** — ensure all imports are present
4. **Verify test correctness** — concurrent tests must use `wg.Wait()` and not hardcoded expectations
5. **Check test isolation** — no shared state between tests, proper cleanup
6. **Consistency** — use same patterns everywhere (e.g., always `UnixMilli()`)
7. **Apply Consolidation Rule** — never create new artifact versions for the same component when valid versions already exist

## References

- [go-idioms.md](references/go-idioms.md) — curated list of idiomatic patterns with before/after examples
