---
name: qa-validation
description: Checklists and patterns for systematic QA review of code, APIs, and infrastructure changes.
---

# QA Validation Skill

Use this skill when reviewing code or infrastructure changes for correctness, safety, and completeness.

## The QA Mindset

QA is about **finding failure modes before they hit production**, not rubber-stamping a diff. Ask:

- What could go wrong at scale?
- What happens at the boundary (empty list, zero, nil, max int)?
- What is the failure mode if this external dependency is unavailable?

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

### Observability

- [ ] New code paths emit structured log lines at appropriate levels
- [ ] Errors include enough context to diagnose without a stack trace
- [ ] Metrics or tracing spans added for latency-sensitive paths

### Tests

- [ ] Happy path covered
- [ ] At least one sad path per error return
- [ ] Table-driven where there are ≥3 cases with the same structure
- [ ] No sleeping or time-dependent assertions

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
