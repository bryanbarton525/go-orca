# QA Validation Skill

Use this skill when reviewing code or infrastructure changes for correctness, safety, and completeness.

## The QA Mindset

QA is about **finding failure modes before they hit production**, not rubber-stamping a diff. Ask:

- What could go wrong at scale?
- What happens at the boundary (empty list, zero, nil, max int)?
- What is the failure mode if this external dependency is unavailable?

## Remediation Limits

**When remediation cycles stall or repeat**, the workflow risks entering an indefinite loop. Use these guidelines to decide when to escalate:

- **Two-cycle rule**: If the same blocking issue is raised in consecutive remediation cycles without meaningful progress, escalate to "QA exhausted".
- **Same fix, different wording**: If QA is flagging the exact same error in successive cycles with only minor rephrasing, the Implementer likely isn't reading or addressing the feedback correctly. Escalate after the second occurrence.
- **Convergence check**: Before the third cycle, verify that the fix is complete (compile, run, pass all checks). If not, escalate immediately.
- **Cross-issue dependency**: If multiple blocking issues prevent convergence, list them explicitly and require a consolidated fix that addresses all at once.

**Escalation to "QA exhausted"**:
- Trigger: Two remediation cycles without convergence on the same issue.
- Condition: The same issue has been addressed but remains blocking.
- Action: Set `passed` to `false` with an explanation that the workflow requires escalation or a process review.

## Code Review Checklist

### Correctness

- [ ] All error paths handled; no silently swallowed errors
- [ ] No off-by-one errors in loops or slice indexing
- [ ] Context propagated to all blocking calls
- [ ] Goroutine leaks avoided (every goroutine has a clear exit condition)

### Security (OWASP Top 10)

- [ ] No SQL/command injection (parameterized queries, `exec.Command` with separate args)
- [ ] Secrets not logged or returned in error messages
- [ ] TLS not skipped in production paths (`tls_skip_verify` only in dev)
- [ ] Input validated at system boundaries

### Concurrency Safety (CRITICAL)

- [ ] NO mixing of `sync.Mutex` and `sync/atomic` on the same state (data race hazard)
- [ ] All shared state protected by a single synchronization primitive (prefer mutex for compound operations)
- [ ] `context.Context` passed to all blocking calls and checked with `select` on `ctx.Done()`
- [ ] No goroutine leaks: every `wg.Add()` has corresponding `wg.Done()`
- [ ] `time.Now().UnixMilli()` used consistently; never mix `UnixNano()` and `UnixMilli()`

### Observability

- [ ] New code paths emit structured log lines at appropriate levels
- [ ] Errors include enough context to diagnose without a stack trace
- [ ] Metrics or tracing spans added for latency-sensitive paths

### Tests

- [ ] Happy path covered
- [ ] At least one sad path per error return
- [ ] Table-driven where there are ≥3 cases with the same structure
- [ ] No sleeping or time-dependent assertions
- [ ] **ALL test artifacts use `httptest.NewServer()` with `defer ts.Close()`**
- [ ] **ALL tests create fresh `http.ServeMux` (never use `http.DefaultServeMux`)**
- [ ] Concurrent tests verify via `wg.Wait()` + race detector, not hardcoded expected values
- [ ] Proper imports present (`context`, `fmt`, `sync`, `time`, `net/http`, `testing`)

## API Review Checklist

- [ ] Request and response types validated (not raw `map[string]any`)
- [ ] HTTP status codes match semantics (201 for create, 204 for no-content delete, etc.)
- [ ] Pagination for list endpoints
- [ ] Breaking changes documented in changelog

## Infrastructure Review Checklist

See [qa-checklist.md](references/qa-checklist.md) for the full Kubernetes / GitOps checklist.

- [ ] No plaintext secrets in manifests (use Sealed Secrets / external secret store)
- [ ] Resource requests and limits set on all containers
- [ ] Health/readiness probes defined
- [ ] Image tag is pinned (not `:latest`)

## Common Failure Patterns to Catch

### Pattern 1: Context Cancellation Not Checked

```go
// BAD: No context cancellation check during sleep
func allow(ctx context.Context, bucket *Bucket) bool {
    time.Sleep(replenishTime) // <-- ctx ignored!
}

// GOOD: Check context while waiting
func allow(ctx context.Context, bucket *Bucket) bool {
    for {
        now := time.Now().UnixMilli()
        elapsed := now - bucket.lastReplenish
        tokensToAdd := elapsed * bucket.rate / 1000
        bucket.mutex.Lock()
        bucket.tokens = min(bucket.burst, bucket.tokens+tokensToAdd)
        bucket.lastReplenish = now
        bucket.mutex.Unlock()
        
        if bucket.tokens >= 1 {
            return true
        }
        
        select {
        case <-ctx.Done():
            return false
        default:
            time.Sleep(time.Millisecond)
        }
    }
}
```

### Pattern 2: Atomic+Mutex Mixing (Data Race Hazard)

```go
// BAD: Mixing atomic with mutex protects partial state
bucket.mutex.Lock()
atomic.AddInt64(&bucket.tokenCount, -1) // <-- UNSAFE: atomic while mutex held
bucket.mutex.Unlock()

// GOOD: Only use mutex for compound operations
bucket.mutex.Lock()
bucket.tokens--
if bucket.tokens < 0 {
    bucket.tokens = bucket.burst // or handle empty bucket
}
bucket.mutex.Unlock()
```

### Pattern 3: Test Server Cleanup Omitted

```go
// BAD: Server never closed
ts := httptest.NewServer(handler)
defer ts.Close() // <-- missing!
// test logic...
// Server leaks goroutines

// GOOD: Always close test servers
ts := httptest.NewServer(handler)
defer ts.Close()
```

### Pattern 4: Test Using Default Mux

```go
// BAD: Polluting http.DefaultServeMux
func TestSomething(t *testing.T) {
    http.HandleFunc("/test", handler) // <-- leaks into default mux
}

// GOOD: Use httptest.Mux
mux := http.NewServeMux()
mux.HandleFunc("/test", handler)
ts := httptest.NewServer(mux)
defer ts.Close()
```

## Remediation Guidelines

When handling QA blocking issues:

1. **Read each issue carefully** — identify the exact requirement being violated
2. **Apply minimal changes** — fix only what's broken, don't refactor unrelated code
3. **Verify compilation** — ensure all imports are present
4. **Verify test correctness** — concurrent tests must use `wg.Wait()` and not hardcoded expectations
5. **Check test isolation** — no shared state between tests, proper cleanup
6. **Consistency** — use same patterns everywhere (e.g., always `UnixMilli()`)

## Common Go False Positives — Do NOT Report as Issues

The following are valid idiomatic Go. Reporting them as blocking issues wastes a full
Architect → Implementer → QA remediation cycle and must be avoided:

| Pattern | Why it is valid |
|---|---|
| `append(dst, src...)` | Core Go spec §Built-in functions; `...` spreads any slice expression |
| `append(dst, fn()...)` | Method or function call returning a slice is a valid slice expression |
| `fmt.Errorf("msg: %w", err)` | `%w` is the standard error wrapping verb since Go 1.13 |
| `var _ Iface = (*T)(nil)` | Compile-time interface satisfaction check; not dead code |
| `//go:embed ...` | Standard Go 1.16+ embed directive |
| Named returns in `defer` | Idiomatic error-capture in deferred cleanup |
| `errors.Is` / `errors.As` on wrapped error chains | Correct unwrapping API; do not replace with `==` |

When in doubt, cross-reference `skills/code-generation/references/go-idioms.md` before
raising a blocking syntax issue.

## Common Go False Positives — Do NOT Report as Issues

The following are valid idiomatic Go. Reporting them as blocking issues wastes a full
Architect → Implementer → QA remediation cycle and must be avoided:

| Pattern | Why it is valid |
|---|---|
| `append(dst, src...)` | Core Go spec §Built-in functions; `...` spreads any slice expression |
| `append(dst, fn()...)` | Method or function call returning a slice is a valid slice expression |
| `fmt.Errorf("msg: %w", err)` | `%w` is the standard error wrapping verb since Go 1.13 |
| `var _ Iface = (*T)(nil)` | Compile-time interface satisfaction check; not dead code |
| `//go:embed ...` | Standard Go 1.16+ embed directive |
| Named returns in `defer` | Idiomatic error-capture in deferred cleanup |
| `errors.Is` / `errors.As` on wrapped error chains | Correct unwrapping API; do not replace with `==` |

When in doubt, cross-reference `skills/code-generation/references/go-idioms.md` before
raising a blocking syntax issue.
