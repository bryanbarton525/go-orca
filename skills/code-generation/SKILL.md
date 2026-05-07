---
name: code-generation
description: Use this skill when implementing code for any module in this repository.
---
# Code Generation Skill

Use this skill when implementing code for any module in this repository.

## Language Layout Profiles — CRITICAL

When creating a new project or major module, select the matching profile below and keep file paths consistent with it. Do not invent ad-hoc directory trees.

### Go (service/app) — project-layout style

Use this as default for production Go services. It follows the common Go community layout style (cmd + internal + pkg) while keeping it pragmatic.

```
.
├── cmd/
│   └── <app-name>/
│       └── main.go
├── internal/
│   ├── app/            # wiring/bootstrap
│   ├── domain/         # core business types/rules
│   ├── service/        # use-cases
│   ├── transport/      # http/grpc handlers
│   └── store/          # db/repository adapters
├── pkg/                # optional reusable public packages
├── api/                # OpenAPI/proto (optional)
├── migrations/         # db migrations (optional)
├── test/               # integration/e2e harness (optional)
├── go.mod
└── README.md
```

Rules:
- `cmd/<app-name>/main.go` is the only process entrypoint.
- Keep business logic in `internal/`; handlers call services, not raw SQL.
- Put `go.mod` at repo root (single-module default).
- For small libraries (not services), use a simpler root package layout and omit `cmd/`.

#### Module Path Alignment — CRITICAL

Before writing any Go source file, verify that the `module` directive in `go.mod` exactly matches
the import prefix used throughout the codebase.

- If internal packages are imported as `linear-sync/internal/config`, then `go.mod` **must** declare `module linear-sync`.
- A mismatch (e.g. `module workflow/some-uuid` while code uses `linear-sync/...`) causes the Go
  compiler to treat those import paths as standard-library lookups, producing `package X is not in std` errors.
- When attaching to an existing repository, **read `go.mod` first**. If the module path does not
  match the intended import prefix, correct it before writing any source files.
- Keep the `go` version directive unchanged when correcting the module path.

### Python (package/service)

```
.
├── pyproject.toml
├── src/
│   └── <package_name>/
│       ├── __init__.py
│       ├── app.py
│       ├── domain/
│       ├── services/
│       └── adapters/
├── tests/
├── scripts/            # optional operational scripts
└── README.md
```

Rules:
- Use `src/` layout for import safety.
- Keep tests outside package under `tests/`.
- Framework bootstrapping (FastAPI/Flask/CLI) stays in `app.py` or `main.py`, not mixed into domain code.

### TypeScript / Node (backend service)

```
.
├── package.json
├── tsconfig.json
├── src/
│   ├── index.ts        # startup/bootstrap
│   ├── domain/
│   ├── services/
│   ├── routes/
│   ├── middleware/
│   └── infra/
├── test/               # unit/integration tests
├── dist/               # build output (generated)
└── README.md
```

Rules:
- Source only in `src/`; never mix generated output into source paths.
- Keep runtime config/env parsing in a dedicated module (for example `src/infra/config.ts`).

### Rust (service/CLI)

```
.
├── Cargo.toml
├── src/
│   ├── main.rs         # binary entrypoint
│   ├── lib.rs          # optional reusable crate API
│   ├── domain/
│   ├── service/
│   └── adapters/
├── tests/              # integration tests
└── README.md
```

Rules:
- Prefer `src/lib.rs` + thin `main.rs` for testability.
- Group modules by responsibility, not by file type.

### Java (Maven/Gradle service)

```
.
├── pom.xml | build.gradle
├── src/
│   ├── main/
│   │   ├── java/<base_package>/
│   │   └── resources/
│   └── test/
│       ├── java/<base_package>/
│       └── resources/
└── README.md
```

Rules:
- Mirror package structure in test tree.
- Keep configuration in `resources/` and business logic in package modules.

### Layout Selection Heuristics

- New service/application: use the full profile for that language.
- Small single-purpose utility: allow reduced layout, but still keep idiomatic entrypoint + tests.
- Remediation cycle: keep existing valid layout unless the blocker is directly caused by layout.

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
- Follow the selected language layout profile above for directory structure and file placement.

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
