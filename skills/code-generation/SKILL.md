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

## Module Path & Cross-File Contracts — CRITICAL

When tasks are executed in isolation, the single most common failure mode is **contract drift**:
files that should share a module path, type, or signature end up disagreeing because each file
was generated without seeing the others. Follow these rules every time you emit Go code:

### Module path discipline

- Every internal import MUST be prefixed by the **exact** string declared in `go.mod` (the value
  of the `module` directive). Do not shorten, abbreviate, or substitute an alternative domain.
- If you do not have `go.mod` in context, read the canonical module path from the task
  description. It is typically stated as `github.com/<org>/<project>` or similar.
- Do not mix prefixes across files in the same project. `example.com/foo`, `foo`, and
  `github.com/example/foo` are three different modules as far as the Go toolchain is concerned;
  choosing inconsistently across files guarantees compilation failure.
- Never invent a placeholder module path (`example.com/...`) when a real one is specified.

### Shared type and signature discipline

- A shared type (struct, interface, sentinel error, constants) MUST be defined in exactly ONE
  package. Every other package that uses it must import it, not redeclare it.
- When you are generating a file that CONSUMES a type or function from another package, and the
  task description gives you the canonical signature or struct fields, reproduce them verbatim.
  Do not add, rename, or drop fields. Do not reorder parameters. Do not change types.
- If a task description is silent on the exact signature of a dependency, state your assumption
  explicitly in the summary and keep the signature minimal and obvious (e.g. `ctx` first, a
  single ID parameter, a single typed return plus `error`).
- Never create parallel "v2" or "_fixed" files for the same component. Fix in place.

### Pre-emit checklist (run mentally before writing any .go file)

1. What is the canonical module path? Write it down.
2. List every `import` line — does each internal one start with that exact module path?
3. For every exported identifier you reference from another package in this project, do you have
   its signature/struct definition stated in the task description? If not, flag it in `summary`.
4. For every exported identifier you define in this file, is this the single owning location?
5. Does every public function accept `context.Context` as its first parameter where appropriate?
6. Is the test file (if any) in the same package as the implementation file?

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
3. **Verify compilation** — ensure all imports are present and all import prefixes match the module path
4. **Verify test correctness** — concurrent tests must use `wg.Wait()` and not hardcoded expectations
5. **Check test isolation** — no shared state between tests, proper cleanup
6. **Consistency** — use same patterns everywhere (e.g., always `UnixMilli()`)
7. **Apply Consolidation Rule** — never create new artifact versions for the same component when valid versions already exist
8. **Shared contract fixes must be atomic** — when a blocking issue names multiple files that share a type, signature, or import path, fix ALL of them in a single artifact set with one canonical definition used consistently across every file.

## References

- [go-idioms.md](references/go-idioms.md) — curated list of idiomatic patterns with before/after examples
