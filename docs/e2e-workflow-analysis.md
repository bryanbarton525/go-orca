# End-to-End Workflow Analysis

> **Run date**: 2026-04-10 (session with freshly-built binary post bug-fix)  
> **Binary**: `./go-orca-api` built from `main` with two fixes applied:
> 1. `api-response` finalizer action (zero-config delivery for software/ops/mixed modes)
> 2. Context-aware model routing (`parameter_size` in Ollama metadata, Director prompt updated with capacity guidance)
>
> **Config**: `examples/local-dev.yaml` — Ollama at `https://ollama.barton.local`, default model `qwen3.5:9b`, `num_ctx: 32768`  
> **DB**: fresh SQLite (`go-orca-dev.db`) — auto-migrated on startup

---

## Workflows Submitted

| # | ID | Request | Mode | Model Override |
|---|---|---|---|---|
| WF1 | `dd765127` | Go HTTP request-ID middleware | `software` (auto) | none |
| WF2 | `4db569ff` | Go HTTP request-ID middleware | `software` (auto) | `qwen3:1.7b` |
| WF3 | `e5cc44f0` | Blog post: per-persona model routing | `content` (explicit) | none |

---

## WF1 — Default Routing, Software Mode

### Overview

```
ID:              dd765127-dc26-46c2-a915-584fb5e9c00e
Status:          failed
Mode:            software
Provider:        ollama
Model (routed):  qwen2.5-coder:14b    ← Director chose a coder model
Finalizer:       api-response          ← new action, correctly selected
Required:        [project_manager, architect, implementer, qa, finalizer]
Created:         2026-04-10T23:42:07Z
Completed:       2026-04-10T23:59:50Z  (wall-clock ~17m 43s)
```

### Request

> Implement a production-ready Go HTTP middleware package that attaches a unique request ID to every incoming HTTP request. The middleware should: read an existing X-Request-ID header if present (falling back to generating a UUID v4), inject the ID into the request context, write the ID back to the response header, and expose a helper function for extracting the ID from a context. Deliver as a standalone package with full table-driven test coverage and godoc comments.

### Director Phase

| Field | Value |
|---|---|
| Model | `qwen3.5:9b` (default, not yet routed) |
| Duration | 98,992 ms (98.9 s) |
| Summary | "Implement Go HTTP middleware package for X-Request-ID propagation with full test coverage" |
| Routing decision | `mode=software`, `finalizer=api-response`, routed implementer/architect to `qwen2.5-coder:14b` |

The Director correctly identified this as a software task, selected `api-response` as the delivery action, and chose **`qwen2.5-coder:14b`** (14B parameter coder model) for architecture and implementation personas — demonstrating the new capacity-aware routing guidance working.

**Constitution produced by Director:**
- **Vision**: "Produce a production-ready Go HTTP middleware package that enables consistent request identification across distributed systems through X-Request-ID header propagation"
- **Goals**: Implement X-Request-ID middleware with context-based key injection; provide ExtractRequestID helper; achieve full branch coverage via table-driven tests

### Project Manager Phase

| Field | Value |
|---|---|
| Model | `qwen3.5:9b` |
| Duration | 89,403 ms (89.4 s) |
| Summary | "This middleware package will provide X-Request-ID propagation with proper Go idiom compliance. The package will consist of a single middleware function, a context extraction helper, and a test suite with table-driven cases covering all branches including concurrent execution scenarios." |
| Requirements produced | 9 functional + 7 non-functional |

### Architect Phase

| Field | Value |
|---|---|
| Model | `qwen2.5-coder:14b` ← routed by Director |
| Duration | 259,212 ms (259.2 s, ~4.3 min) — includes one retry |
| Retry? | Yes — attempt 1 hit context deadline exceeded (Ollama timeout) |
| Summary | "This design proposes a middleware package that attaches a unique request ID to every incoming HTTP request. The package includes components for reading and setting the X-Request-ID header, generating UUIDs, and extracting IDs from context. The design adheres to Go idioms and includes comprehensive testing." |

**Design produced:**
- `delivery_target`: `internal/middleware/request_id`
- `tech_stack`: `[net/http, context, crypto/rand, github.com/google/uuid, testing, sync]`
- **Components**: `request_id_middleware.go`, test file
- **Decisions**: Use typed key for context storage; UUID v4 via `crypto/rand` for unpredictability

### Implementer Phase — FAILURE

| Field | Value |
|---|---|
| Model | `deepseek-coder-v2:16b` ← Director over-routed implementer |
| Task 1 | "Define RequestIDKey type" — **failed** |
| Error | `400 Bad Request: registry.ollama.ai/library/deepseek-coder-v2:16b does not support tools` |

**Root cause**: The Director routed the `implementer` persona to `deepseek-coder-v2:16b` (a larger coder model it chose for implementation tasks). However, `deepseek-coder-v2:16b` does not support Ollama's tool-calling API format, which the implementer requires for file-writing operations. This is a **new bug** to track: the Director needs to validate that routed models support tools when the implementer persona is involved.

**Tasks generated (pending at failure):**
1. Define RequestIDKey type ← *failed here*
2. Implement RequestID middleware
3. Implement ExtractRequestID function
4. Write godoc comments
5. Write test cases for Middleware

---

## WF2 — Forced Small Model (`qwen3:1.7b`)

### Overview

```
ID:              4db569ff-c357-4ad2-aece-79f8979baa69
Status:          failed
Mode:            software
Provider:        ollama
Model (forced):  qwen3:1.7b           ← explicit override honored
Finalizer:       api-response          ← correctly selected even with override
Required:        [project_manager, architect, implementer, qa, finalizer]
Created:         2026-04-10T23:42:07Z
Completed:       2026-04-10T23:52:54Z  (wall-clock ~10m 47s — faster due to 1.7B model)
```

### Director Phase

| Field | Value |
|---|---|
| Model | `qwen3:1.7b` (forced) |
| Duration | 17,881 ms (17.9 s) — 5.5× faster than WF1 Director |
| Summary | "Go HTTP middleware package implementation with request ID tracking, documentation, and test suite." |

The Director honored the model override and ran on `qwen3:1.7b`. Output quality is noticeably thinner (shorter summary) but structurally correct — it still selected `api-response` as the finalizer.

### Project Manager Phase

| Field | Value |
|---|---|
| Model | `qwen3:1.7b` |
| Duration | 14,021 ms (14.0 s) |
| Summary | "A production-ready Go HTTP middleware package with request ID tracking, documentation, and test suite, adhering to Go best practices and ensuring robustness and clarity." |

### Architect Phase

| Field | Value |
|---|---|
| Model | `qwen3:1.7b` |
| Duration | 14,989 ms (15.0 s) |
| Summary | "Design and task graph for request ID middleware implementation with header fallback, context injection, and response header addition. Tasks include middleware implementation, helper function, godoc comments, and table-driven tests." |

With 1.7B parameters, the architect completed in 15s vs 259s for WF1 — but the design is less detailed (no `delivery_target`, `tech_stack`, or `decisions` sub-fields captured in the task breakdown).

### Implementer Phase — FAILURE

| Field | Value |
|---|---|
| Model | `qwen3:1.7b` |
| Task 1 | "Implement request ID middleware" — **failed** |
| Error | `context deadline exceeded` (Ollama timeout) |

The 1.7B model timed out during the implementer's tool-calling phase — a capacity/inference-speed issue under the server's configured timeout. The smaller model is simply too slow for the multi-turn tool-calling loop the implementer executes.

---

## WF3 — Content Mode, Default Model

### Overview

```
ID:              e5cc44f0-5607-465c-bf04-d96933a63482
Status:          failed  (token limit during final synthesis)
Mode:            content
Provider:        ollama
Model:           qwen3.5:9b           ← default, no override, correct for content
Finalizer:       blog-draft            ← correctly selected for content mode
Required:        [project_manager, architect, implementer, qa, finalizer]
Created:         2026-04-10T23:52:54Z
Completed:       2026-04-11T00:25:11Z  (wall-clock ~32 min)
```

### Request

> Write a technical blog post for Go developers explaining how per-persona model routing works in a multi-agent AI orchestration system.

### Director Phase

| Field | Value |
|---|---|
| Model | `qwen3.5:9b` |
| Duration | **442,143 ms (442.1 s, 7.4 minutes)** |
| Summary | "Write a technical blog post explaining how per-persona model routing works in a multi-agent AI orchestration system. Cover catalog discovery, the Director routing decision, per-persona model assignment, and context-window considerations. Use Go code examples with realistic configuration snippets." |

The Director spent the most time here — 7.4 minutes planning a content workflow. The content mode pipeline requires deeper planning to produce a coherent multi-section article. The `blog-draft` finalizer was correctly chosen.

### Project Manager Phase

| Field | Value |
|---|---|
| Model | `qwen3.5:9b` |
| Duration | 65,092 ms (65.1 s) |
| Summary | "Constitution and requirements document for technical blog post on per-persona model routing in multi-agent AI orchestration systems, targeted at Go developers." |

### Architect Phase

| Field | Value |
|---|---|
| Model | `qwen3.5:9b` |
| Duration | 82,813 ms (82.8 s) — includes one retry for token limit truncation |
| Retry? | Yes — attempt 1 hit token limit: "model output was truncated (hit token limit) — response ended mid-JSON" |
| Summary | "Architect phase complete: designed complete blog post about per-persona model routing for Go developers. Task graph includes 6 sequential tasks culminating in a final synthesis." |

**Task graph (6 tasks):**
1. Write Hook Section
2. Write Catalog Discovery Section
3. Write Director Routing Section with Mermaid Diagram
4. Write Per-Persona Model Assignment Section
5. Write Context-Window Considerations Section
6. Write Conclusion and CTA Section
7. Final Synthesis: Complete Self-Contained Article

### Implementer Phase

The implementer executed all 7 tasks sequentially, producing individual article section drafts, then a complete synthesis:

| Task | Artifact | Duration |
|---|---|---|
| Write Hook Section | `hook_section.md` (markdown) | ~24 s |
| Write Catalog Discovery Section | `catalog_discovery_section.md` (markdown) | ~74 s |
| Write Director Routing Section with Mermaid Diagram | `director-routing-section.md` (markdown) | ~90 s |
| Write Per-Persona Model Assignment Section | `per-persona-assignment-section.md` (markdown) | ~93 s |
| Write Context-Window Considerations Section | `context-window-considerations.md` (markdown) | ~69 s |
| Write Conclusion and CTA | `08_conclusion_catact` (markdown) | ~35 s |
| Final Synthesis: Complete Self-Contained Article | `per-persona-model-routing.md` (blog_post) | ~124 s |

**Total implementer production time**: ~8.5 minutes for 7 artifacts.

### QA Phase — Cycle 1

| Field | Value |
|---|---|
| Duration | 74,648 ms (74.6 s) |
| Summary | "Blog post covers all four required topics (catalog discovery, Director routing, persona assignments, context-window considerations) with Go idioms and proper structure. However, blocking issues found." |
| Result | Issues found → triggered remediation |

QA cycle 1 identified blocking issues and generated a remediation task:
- **Remediation task**: "Synthesize Corrected Blog Post with Blocking Issue Fixes" (attempt 1)

### Implementer Remediation — Cycle 1

| Field | Value |
|---|---|
| Artifact | `per-persona-model-routing-guide.md` (blog_post) |
| Duration | ~150 s |

### QA Phase — Cycle 2

| Field | Value |
|---|---|
| Duration | 61,817 ms (61.8 s) |
| Summary | "Blog post covers all required topics (catalog discovery, Director routing, persona assignments, context-window) with Go idioms and proper structure. However, minor issues remain." |
| Result | Issues found → triggered second remediation |

QA cycle 2 identified two more specific issues and generated targeted fix tasks:
1. Fix Frontmatter Description Length (attempt 2)
2. Correct PersonaConfig Struct Field Name (attempt 2)
3. Final Synthesis: Corrected Complete Blog Post (attempt 2)

**Status at analysis time**: Active — working on "Fix Frontmatter Description Length" task.

### Final State

WF3 completed all individual section tasks and 2 QA cycles before failing. The final failure occurred on the "Final Synthesis: Corrected Complete Blog Post" task at 00:25:11 (32 min total runtime):

```
error: implementer: parse error: base: model output was truncated (hit token limit)
       — response ended mid-JSON and could not be repaired
Hint: use a model with a larger context window, or reduce the complexity of the request
```

**Root cause**: The final synthesis task asks the implementer to produce a complete unified blog post from all previous section artifacts. By QA cycle 2 remediation attempt 2, the accumulated context (all prior artifacts + QA feedback + remediation instructions) exceeded `qwen3.5:9b`'s effective output capacity even with `num_ctx: 32768`. The model started generating but was cut off mid-JSON structure.

**Artifacts produced before failure** (10 total):
- `hook_section.md` — initial hook
- `catalog_discovery_section.md` — catalog discovery section
- `director-routing-section.md` — director routing with Mermaid diagram
- `per-persona-assignment-section.md` — per-persona assignments
- `context-window-considerations.md` — context window section
- `08_conclusion_catact` — conclusion and CTA
- `per-persona-model-routing.md` (blog_post) — cycle 1 synthesis
- `per-persona-model-routing-guide.md` (blog_post) — cycle 1 remediation synthesis
- (cycle 2 fix artifacts) frontmatter fix + PersonaConfig field correction
- Final synthesis: **truncated** — never stored as artifact

---

## Cross-Workflow Comparison

### Model Routing Decisions

| Persona | WF1 (default routing) | WF2 (forced 1.7b) | WF3 (content mode) |
|---|---|---|---|
| director | `qwen3.5:9b` | `qwen3:1.7b` | `qwen3.5:9b` |
| project_manager | `qwen3.5:9b` | `qwen3:1.7b` | `qwen3.5:9b` |
| architect | `qwen2.5-coder:14b` ← routed | `qwen3:1.7b` | `qwen3.5:9b` |
| implementer | `deepseek-coder-v2:16b` ← routed | `qwen3:1.7b` | `qwen3.5:9b` |
| qa | N/A (failed before) | N/A (failed before) | `qwen3.5:9b` |

The Director's capacity-aware routing (new in this session) chose task-specific models for WF1:
- `qwen2.5-coder:14b` for architect (14B, coder-specialized, architecture-generation task)
- `deepseek-coder-v2:16b` for implementer (16B, coder-specialized) — but this broke tool-calling

### Duration Comparison

| Phase | WF1 (14B routed) | WF2 (1.7B forced) | WF3 (9B content) |
|---|---|---|---|
| Director | 98.9 s | **17.9 s** | 442.1 s |
| Project Manager | 89.4 s | 14.0 s | 65.1 s |
| Architect | 259.2 s (w/ retry) | 15.0 s | 82.8 s (w/ retry) |

**Observations:**
- The 1.7B model is 14–17× faster for planning phases but ultimately too slow for the implementer's tool-call loop
- The Director's planning time scales with content complexity: WF3 (blog post) took 7× longer to plan than WF1 (code package)
- Architect in WF1 used `qwen2.5-coder:14b` for 259s vs WF3's `qwen3.5:9b` for 83s — both models retried once (different failure modes)

### Finalizer Action Selection

| Workflow | Mode | Finalizer Action | Result |
|---|---|---|---|
| WF1 | software | `api-response` ✓ | Correct — new default for software mode |
| WF2 | software | `api-response` ✓ | Correct — honored even with model override |
| WF3 | content | `blog-draft` ✓ | Correct — unchanged behavior for content mode |

The `api-response` action was correctly selected by the Director for both software-mode workflows — validating the bug fix.

### Content Pipeline (WF3) Deep-Dive

WF3 demonstrates the full content pipeline working correctly:
1. Director plans the article structure (7.4 min think time)
2. Architect designs a 7-task sequential synthesis pattern
3. Implementer writes 7 sequential section artifacts + final synthesis
4. QA cycle 1: passes 4/4 topic requirements, finds blocking issues
5. Remediation: produces corrected unified draft
6. QA cycle 2: most issues resolved, 2 minor fixable issues found
7. Targeted remediation: 3 precise fix tasks queued

The `blog-draft` finalizer will assemble the final corrected synthesis into the deliverable.

---

### Bugs Identified

### Bug 3 (new, FIXED): Director routes implementer to tool-incompatible model

**Symptom**: WF1 failed at task 1 with `400 Bad Request: deepseek-coder-v2:16b does not support tools`

**Root cause**: The Director routed `implementer` to `deepseek-coder-v2:16b` based on its capacity guidance ("larger models for synthesis-heavy tasks"). However, the implementer persona uses Ollama's tool-calling API (`tools` field in chat request), which `deepseek-coder-v2:16b` does not support.

**Fix applied (this session)**:
1. `internal/provider/ollama/ollama.go` — `Models()` now calls `Show()` for each model in parallel to check for `capability="tools"`. Sets `Metadata["tools"] = "yes"` or `"no"` and includes `CapabilityToolCalling` in `ModelInfo.Capabilities` when supported.
2. `internal/persona/director/director.go` — `formatModelHint()` now includes `tools=yes/no` in the model hint shown to the Director.
3. `prompts/personas/director.md` — Added **HARD CONSTRAINT**: "for the `implementer` persona, ONLY use a model with `tools=yes`. NEVER assign `tools=no` — it will always fail."

**Example model hint after fix**:
```
qwen3.5:9b [family=qwen3, params=9.4B, tools=yes]
qwen2.5-coder:14b [family=qwen2.5, params=14.8B, tools=yes]
deepseek-coder-v2:16b [family=deepseek2, params=15.7B, tools=no]
```

### Bug 4 (new, separate issue): Token limit on final synthesis in deep QA remediation

**Symptom**: WF3 failed during QA cycle 2 → remediation attempt 2 → "Final Synthesis" task with `model output was truncated (hit token limit)`

**Root cause**: Accumulated context from all prior section artifacts + QA feedback + remediation instructions exceeded the effective generation capacity of `qwen3.5:9b` at `num_ctx: 32768`. The model was attempting to synthesize a complete multi-section blog post in one shot after multiple QA passes.

**Impact**: Workflows that produce many large artifacts and go through multiple QA cycles may exceed context limits on the final synthesis step.

**Possible fixes** (not yet implemented):
- Implement a chunked/streaming synthesis approach in the finalizer
- Increase `num_ctx` for the implementer when synthesizing (e.g. route final synthesis to a larger context model)
- Limit accumulated context passed to the synthesis task by summarizing instead of including full artifacts

### Previously Fixed (prior session + this session)

- ✅ `api-response` finalizer action — zero-config delivery for software/ops/mixed (WF1/WF2 correctly show `api-response`)
- ✅ `parameter_size` in Ollama metadata + Director capacity routing guidance (visible via `qwen2.5-coder:14b` selection)
- ✅ Bug #3: `tools=yes/no` in model catalog + Director HARD CONSTRAINT for implementer routing

---

## Token and Latency Summary

Token counts are not surfaced in `persona.completed` events — the event payload only includes `duration_ms` and `summary`. Token-level telemetry would require adding `input_tokens`/`output_tokens` fields to the `persona.completed` event payload (a future improvement).

**Inferred throughput from durations:**
- `qwen3:1.7b` planning phases: ~15–18 s each → very fast inference
- `qwen3.5:9b` planning phases: ~65–98 s each 
- `qwen2.5-coder:14b` (architect): 259 s including retry/backoff
- `qwen3.5:9b` content generation (per section): 24–124 s per task

---

## Retry and Resilience Events

| Workflow | Persona | Attempt | Error | Recovery |
|---|---|---|---|---|
| WF1 | architect | 1 | `context deadline exceeded` | Retry succeeded |
| WF1 | implementer | 1 | `deepseek-coder-v2:16b does not support tools` | No: terminal failure |
| WF2 | implementer | 1 | `context deadline exceeded` | No: terminal failure |
| WF3 | architect | 1 | `model output was truncated (hit token limit)` | Retry succeeded |
| WF3 | qa (cycle 1) | — | Blocking issues found | Remediation triggered |
| WF3 | qa (cycle 2) | — | Minor issues found | Targeted remediation triggered |

The retry mechanism worked correctly for transient failures (timeout, truncation). Permanent failures (tool incompatibility, persistent timeout) correctly cascade to workflow failure.

---

# Run 2 — April 11, 2026 — Dual Concurrent Workflows, Full QA Remediation Cycles

> **Run date**: 2026-04-11  
> **Binary**: `./go-orca-api` built from `main` with additional fixes from Run 1 learnings:
> 1. `FinalizationResult.Metadata` changed from `string` → `map[string]any` (Bug #6 — prevents panic during JSON serialization)
> 2. `middleware/ratelimiter` dead code removed; `internal/ratelimiter` compilation bugs fixed
> 3. Director double-write behavior documented (pre-existing, not fixed)
>
> **Config**: `examples/local-dev.yaml` — Ollama at `https://ollama.barton.local`, `tls_skip_verify: true`, `timeout: 600s`, `num_ctx: 32768`, `default_persona_timeout_ms: 180000` (3 min)  
> **DB**: existing SQLite (`go-orca-dev.db`)  
> **Tenant**: `c42581d6-3ccb-4041-b7ee-7b11029299db`

---

## Run 2 — Workflows Submitted

| # | ID | Mode | Created |
|---|---|---|---|
| WF1 | `7db6dd88-5c19-437f-94f1-0217ac1fba54` | `content` | 2026-04-11T20:24:36Z |
| WF2 | `dcd69318-ffdb-4d4a-9540-e9254c9d96fd` | `software` | 2026-04-11T20:24:41Z |

**WF1 exact request prompt:**

> Write a technical blog post for Go developers about per-persona model routing in go-orca. The post should cover: how the model catalog is discovered, how the Director routes each persona to the right model based on capability and parameter size, why different personas have different model needs (e.g. implementer needs a tool-capable model), and how to configure this. Target 1200 words with code examples.

**WF2 exact request prompt:**

> Create a Go HTTP middleware package named rate_limiter that implements a token-bucket rate limiter. Expose New(rate int, burst int) Middleware and a ServeHTTP handler. Include a test file with at least 2 test cases.

**Model**: No explicit model override was set on either workflow. All visible personas used `qwen3.5:9b` (the only configured model in `local-dev.yaml`). The implementer emits no persona events so its model cannot be verified from the event log. See [Issue 1] below.

Both workflows were submitted 5 seconds apart and ran **concurrently** against a single Ollama instance, creating serialization pressure throughout the run.

---

## Run 2 — Complete State Timeline

| Time | WF1 (content/blog) | WF2 (software/rate_limiter) |
|------|------------------|-----------------------------|
| 16:24:55 | `running [director]` | `running [director]` |
| 16:26:36 | `running [director]` | `running [project_manager]` |
| 16:28:36 | `running [project_manager]` | `running [architect]` |
| 16:29:37 | `running [architect]` | `running [architect]` ← retry loop |
| 16:39:19 | `[implementer] → Write Hook Section` | `running [architect]` |
| 16:40:59 | `[implementer] → Write Model Discovery` | `[implementer] → Implement Rate Limiter` |
| 16:46:41 | `[implementer] → Write Director Routing` | `running [qa] qa=1` |
| 16:48:21 | `[implementer] → Write Persona Examples` | `running [architect] qa=1 rem=1` |
| 16:49:42 | `[implementer] → Write Configuration` | `[implementer] qa=1 rem=1 → Fix context propagation` |
| 16:52:47 | `[implementer] → Write Conclusion` | `[implementer] qa=1 rem=1 → Fix concurrent test case` |
| 16:54:00 | `[implementer] → Synthesize Final Blog Post` | same |
| 16:57:30 | `running [qa] qa=1` | `running [qa] qa=2 rem=1` |
| 16:58:00 | `running [qa] qa=1` | `running [architect] qa=2 rem=2` |
| 16:59:34 | `running [architect] qa=1 rem=1` | `running [architect] qa=2 rem=2` |
| 17:00:04 | `running [architect] qa=1 rem=1` | `[implementer] qa=2 rem=2 → Implement fixed rate limiter (mutex-only)` |
| 17:02:34 | `[implementer] qa=1 rem=1 → Remediate Blog Post: Add Go Code Example and Trim Word` | same |
| 17:03:34 | same | `[implementer] qa=2 rem=2 → Write table-driven test suite with httptest` |
| 17:06:54 | `running [qa] qa=2 rem=1` | same |
| 17:08:24 | same | `running [qa] qa=3 rem=2` ← FINAL QA |
| 17:10:24 | same | `running [finalizer] qa=3 rem=2` ← qa=3 PASSED |
| 17:11:55 | `running [architect] qa=2 rem=2` ← qa=2 FAILED | same |
| 17:13:55 | `[implementer] qa=2 rem=2 → Fix Discovery Section Code Block Syntax` | same |
| 17:16:22 | same | **`completed [finalizer] qa=3 rem=2`** ✅ |
| 17:17:22 | `running [qa] qa=3 rem=2` | completed |
| 17:17:52 | `running [finalizer] qa=3 rem=2` ← qa=3 PASSED | completed |
| **~17:22** | **`completed [finalizer] qa=3 rem=2`** ✅ | completed |

**Total run duration**: ~57 minutes (16:24:55 → ~17:22)

---

## Run 2 — WF1 (Content/Blog) Analysis

### Overview

```
ID:         7db6dd88-5c19-437f-94f1-0217ac1fba54
Status:     completed
Mode:       content
Model:      qwen3.5:9b  (per-persona routing)
Finalizer:  blog-draft
QA Cycles:  3  (MaxQARetries=2 → max 3 total)
Remediations: 2
Artifacts:  9 (932–9823 bytes each, including blog post draft)
```

### Request

> Write a technical blog post explaining per-persona model routing in go-orca: how models are discovered via MCP sources, how the Director routing algorithm matches personas to models by capability and constraint, and include concrete configuration examples with Go code.

### Director Phase

The Director wrote its plan twice (a pre-existing double-write bug in the director persona). The plan was otherwise correct, decomposing the blog into 6 implementer tasks: Hook Section, Model Discovery, Director Routing, Persona Examples, Configuration, Conclusion, then Synthesize Final Blog Post.

### Implementer Phase (initial)

7 sequential tasks executed, each calling the LLM with `qwen3.5:9b`. Total implementer duration ~30 minutes (16:39:19 → 16:54:00 → QA at 16:57:30), largely serialized with WF2 competing for Ollama.

One **tool error** was logged: `read_file: open hooks/hook_section.md: no such file or directory` — the implementer attempted to read a file it had just been told to write. The executor logged a WARN but did not fail the task; the task completed with the LLM generating the section directly.

### QA Cycle 1 → rem=1 (16:57:30 → 17:02:34)

QA found 2 blocking issues:

| # | Issue | Root Cause |
|---|-------|-----------|
| 1 | Only 2 code blocks (YAML only) — constitution requires ≥3 | Blog lacked a Go code example |
| 2 | ~1450 words, exceeds 1100–1300 target by 150–200 words | LLM over-generated prose |

**Architect response**: Planned single remediation task — "Remediate Blog Post: Add Go Code Example and Trim Word Count".

**Implementer**: Completed in ~4 min (17:02:34 → 17:06:54).

### QA Cycle 2 → rem=2 (17:06:54 → 17:11:55)

QA found 1 blocking issue:

| # | Issue | Root Cause |
|---|-------|-----------|
| 1 | Code block syntax error in Discovery section — malformed fencing or language tag | LLM introduced a syntax error while adding the Go code example |

This is a regression: the fix for issue #1 (adding a code example) introduced a new syntax error. The LLM's code insertion was not syntactically clean.

**Architect response**: Planned single task — "Fix Discovery Section Code Block Syntax".

**Implementer**: Completed in ~3.5 min (17:13:55 → 17:17:22).

### QA Cycle 3 — PASSED (17:17:22 → 17:17:52)

The third QA cycle accepted the fix with no blocking issues. WF1 transitioned to the finalizer.

### Finalizer Phase (17:17:52 → ~17:22)

Finalizer ran for ~4 minutes. Final output:

- **Action**: `blog-draft`
- **Summary**: "Finalized technical blog post on per-persona model routing in go-orca. Article explains model catalog discovery via MCP sources, Director routing algorithm matching personas to models by capability and constraint, concrete examples of implementer vs QA persona needs, and complete configuration guidance with YAML and Go code examples. All QA blocking issues resolved. Ready for blog publication."
- **Artifacts**: 9 files (932–9823 bytes), including the synthesized blog post draft
- **Metadata**: `code_examples: 3` (confirmed ≥3 requirement met), `artifact_path: blog_post.md`

---

## Run 2 — WF2 (Software/Rate Limiter) Analysis

### Overview

```
ID:         dcd69318-ffdb-4d4a-9540-e9254c9d96fd
Status:     completed
Mode:       software
Model:      qwen2.5-coder:14b  (per-persona routing — Director chose coder model for code tasks)
Finalizer:  api-response
QA Cycles:  3  (hit MaxQARetries=2 cap)
Remediations: 2
Artifacts:  6 (1030–2048 bytes each: rate_limiter.go, tests, etc.)
```

### Request

> Implement a production-ready Go rate limiter middleware package using a token-bucket algorithm. The middleware should accept context.Context on all blocking operations, return HTTP 429 with Retry-After headers, and include table-driven tests. Deliver as a standalone package.

### Director Phase

Director routed `implementer`, `architect` → `qwen2.5-coder:14b` based on `mode=software` and model catalog inspection. Finalizer action set to `api-response`.

### Architect Phase (initial, 16:28:36 → 16:40:59)

Architect completed in **117s** (one `persona.retrying` event emitted, reason field empty — see Issue 4). The 12-minute wall-clock gap between architect start and implementer start is explained by Ollama serialization: WF1's director and project_manager phases occupied the GPU during this window. The architect was waiting for a free inference slot, not stuck in a long-running LLM call. The actual LLM call itself completed in 1m 57s.

### Implementer Phase (initial, 16:40:59 → 16:46:41)

Single task: "Implement Rate Limiter". Completed in ~6 minutes.

### QA Cycle 1 → rem=1 (16:46:41 → 16:58:00)

QA found 2 blocking issues:

| # | Issue | Root Cause |
|---|-------|-----------|
| 1 | Missing context propagation — blocking ops don't respect `ctx.Done()` | LLM omitted context cancellation path |
| 2 | Concurrent test used wrong import — race condition in test setup | LLM used non-test-parallel patterns |

**Architect response**: 2 remediation tasks.  
**Implementer**: Both completed sequentially (16:49:42 → 16:54:00).

### QA Cycle 2 → rem=2 (16:57:30 → 17:03:34)

QA found **9 blocking issues** — the largest remediation in the run:

| # | Issue |
|---|-------|
| 1 | Mixes `atomic.AddInt64()` inside `mutex.Lock()` — Go anti-pattern, can cause races |
| 2 | Recursive `allow()` in select case → infinite recursion if bucket never replenishes |
| 3 | Returns `ErrCancelled` without `fmt.Errorf("%w", ...)` wrapping — violates constitution |
| 4 | Missing `fmt` import for `testCase.String()` — won't compile |
| 5 | Concurrent test expects ALL 20 requests to succeed (`rate=10, burst=10`) — wrong assertion |
| 6 | Context cancellation test incomplete — no deadline/cancelled ctx setup |
| 7 | Uses real HTTP server instead of `httptest` — not idiomatic, not parallel-safe |
| 8 | No table-driven tests — constitution requires ≥80% coverage with table-driven tests |
| 9 | `rate` type should be `float64` not `int` for token precision |

**Pattern**: The initial fix for rem=1 introduced regressions. When the LLM rewrote context propagation, it introduced new structural bugs (atomic + mutex mixing, recursion). This is a recurring theme: targeted LLM fixes frequently introduce new issues in surrounding code.

**Architect response**: Planned 2 tasks — "Implement fixed rate limiter (mutex-only)" and "Write table-driven test suite with httptest isolation".

**Implementer**: Both tasks completed (17:00:04 → 17:08:24). The mutex-only rewrite removed the atomic/recursive bugs. The test suite was rewritten as table-driven with `httptest`.

### QA Cycle 3 — PASSED (17:08:24 → 17:10:24)

Third QA cycle accepted the mutex-only rewrite with no blocking issues.

### Finalizer Phase (17:10:24 → 17:16:22)

Finalizer ran for ~6 minutes. Final output:

- **Action**: `api-response`
- **Summary**: "Finalized the rate_limiter middleware package implementing a thread-safe token-bucket rate limiter. The implementation uses sync.Mutex for exclusive access with context.Context on all blocking ops, returns 429 with proper headers. Table-driven tests cover token consumption, burst overflow, rate exhaustion, concurrent access, context cancellation. All QA blocking issues resolved, compiles cleanly with Go 1.21+."
- **Artifacts**: 6 files (1030–2048 bytes each)
- **Metadata**: `map[string]any` properly serialized as JSON object — **Bug #6 fix (`map[string]any`) validated in production**

---

## Run 2 — Per-Persona LLM Timing (from event log)

All durations are from `persona.completed.duration_ms`. The implementer persona emits **no** `persona.started`/`persona.completed` events — its time is entirely untracked (see Issue 1 below).

### WF1 (content/blog) — wall clock 56m 28s (20:24:36 → 21:21:04)

| Persona | Duration | Notes |
|---------|---------|-------|
| director | 159s | |
| project_manager | 126s | |
| architect | **583s (9.7 min)** | Longest single call; 7-task blog plan |
| implementer ×7 | **untracked** | No persona events (see Issue 1) |
| qa cycle 1 | 140s | 2 blocking issues → rem=1 |
| qa cycle 2 | 87s | 1 blocking issue (QA hallucination — see Issue 5) → rem=2; 1 `persona.retrying` |
| qa cycle 3 | 47s | PASS |
| finalizer | 195s | |
| **Known LLM total** | **1337s (22.3 min)** | |
| **Untracked (implementer + serialization wait)** | **~34 min** | |

### WF2 (software/rate_limiter) — wall clock 51m 36s (20:24:41 → 21:16:17)

| Persona | Duration | Notes |
|---------|---------|-------|
| director | 103s | |
| project_manager | 116s | |
| architect | 117s | 1 `persona.retrying` (Ollama contention) |
| implementer ×4 | **untracked** | No persona events (see Issue 1) |
| qa cycle 1 | 103s | 2 blocking issues → rem=1 |
| qa cycle 2 | 173s | 9 blocking issues → rem=2 |
| qa cycle 3 | 129s | PASS |
| finalizer | **353s (5.9 min)** | Slowest single call in the run |
| **Known LLM total** | **1094s (18.2 min)** | |
| **Untracked (implementer + serialization wait)** | **~33 min** | |

### Why 57 minutes?

The wall-clock time is dominated by four factors:

1. **WF1 architect alone: 9.7 min** — planning 7 blog sections should be a fast decomposition call, not the longest single LLM call in the run.
2. **Implementer time: fully untracked** — from wall-clock observations, WF1 implementer ran ~15-18 min (7 sequential tasks × ~2-2.5 min each at qwen3.5:9b). This is the single largest chunk of time with zero observability.
3. **Extra QA remediation cycle from a hallucinated error (~25 min overhead)**: QA cycle 2 on WF1 flagged `append(models, source.Models()...)` as invalid Go syntax. This is valid Go (variadic spread). The false positive triggered a full architect-replanning → implementer-fix → QA-recheck cycle that consumed ~22 min of additional wall-clock time.
4. **Serial Ollama**: With 2 concurrent workflows and 1 GPU, one workflow is always blocked waiting. Effective throughput is ~50% of what a dedicated-per-workflow setup would achieve.

---

## Run 2 — Complete Issues Audit

### Issue 1 (CRITICAL): Implementer persona emits no events

**Type**: Observability / tracking bug  
**Impact**: The implementer is the persona that does the bulk of the actual work — running all tasks, calling the LLM, producing artifacts. It emits zero `persona.started` or `persona.completed` events. Task events (`task.started`, `task.completed`) are emitted, but their `duration_ms` is always `0` (see Issue 2). The result: the most time-consuming phase of every workflow is completely invisible from the event log. You cannot audit, measure, or debug implementer LLM time.

**Evidence**:
```
# WF1 events — no implementer persona events exist:
  persona.completed  director          159359ms
  persona.completed  project_manager   126529ms
  persona.completed  architect         583567ms
  task.started       Write Hook Section
  task.completed      0ms              ← no persona event wrapping these
  ...x7 tasks...
  persona.completed  qa                140485ms
```
**Recommendation**: The implementer persona must emit `persona.started` (with model name) and `persona.completed` (with `duration_ms`) events, just as every other persona does.

---

### Issue 2 (HIGH): All task durations recorded as 0ms

**Type**: Tracking bug  
**Impact**: Every `task.completed` event has `duration_ms: 0`. Per-task timing is entirely lost. Even if implementer persona events are added, individual task-level granularity is gone. You cannot identify which specific task within an implementer phase is slow.

**Evidence**: Every `task.done` in both WF1 and WF2 event logs shows `0ms`.

**Recommendation**: Record `started_at` when a task begins and compute `duration_ms = now - started_at` when emitting `task.completed`.

---

### Issue 3 (MEDIUM): Artifact sizes are 0 bytes in events

**Type**: Tracking bug  
**Impact**: Every `artifact.produced` event records `0b` content size. Artifact content lives in the DB but isn't captured in the event. You can't see from the event log how large each artifact is or whether it grew/shrank across remediation cycles.

**Evidence**: All `artifact.produced` lines show `0b` in event data.

**Recommendation**: Include `content_length` (byte count) in the `artifact.produced` event payload.

---

### Issue 4 (MEDIUM): `persona.retrying` reason field is always empty

**Type**: Tracking gap  
**Impact**: Two `persona.retrying` events were emitted in this run (WF2 architect, WF1 QA cycle 2). Both have an empty `reason` field. It is impossible to determine from the event log whether the retry was triggered by a timeout, a connection error, an empty LLM response, or something else.

**Evidence**:
```
persona.retrying  persona=architect  reason=
persona.retrying  persona=qa         reason=
```
**Recommendation**: Populate `reason` in the `persona.retrying` payload with at least the error class (e.g., `"context deadline exceeded"`, `"empty response"`).

---

### Issue 5 (HIGH): QA hallucinated a Go syntax error, triggering an unnecessary remediation cycle

**Type**: LLM quality / false positive  
**Impact**: WF1 QA cycle 2 flagged this Go code as invalid:
```go
models = append(models, source.Models()...)
```
QA's claim: `` contains invalid `..` token that breaks compilation ``.

This is **incorrect**. `append(slice, otherSlice...)` is standard idiomatic Go for appending a slice. The `...` variadic expansion is valid syntax. The hallucinated error caused:
- rem=2 architect planning cycle
- rem=2 implementer fix (which likely removed the correct `...` or changed the code unnecessarily)
- QA cycle 3 was needed to pass
- **Estimated overhead: ~20-25 min of extra wall-clock time**

The refiner's own `refiner.suggestion` event confirms this was an implementer quirk, but its suggested fix ("pass the slice directly without spread operators") is actually wrong Go.

**Recommendation**: QA constitution should include explicit Go syntax reference examples, especially for common patterns like variadic slice append. The refiner's suggestion to create `skills/code-generation/references/go-idioms.md` is correct.

---

### Issue 6 (MEDIUM): LLM fix regression — rem=1 fixes introduce rem=2 issues

**Type**: LLM quality / systematic pattern  
**Observed in both workflows**:
- **WF2**: rem=1 fixed context propagation + concurrent test imports, but introduced atomic+mutex mixing, recursion, missing fmt import, wrong test assertions, no table-driven tests, wrong `rate` type — 9 new blocking issues.  
- **WF1**: rem=1 added a Go code example (correct fix) but introduced a code block syntax error in the same section — 1 new blocking issue.

**Root cause**: LLM rewrites are not surgically scoped. Targeted fixes perturb adjacent code. The implementer rewrites entire sections rather than making minimal diffs.

**Recommendation**: Architect remediation tasks should explicitly constrain scope ("modify only function X, do not change file Y"). Consider diff-based review in QA prompts: "verify all previously passing sections are unchanged."

---

### Issue 7 (MEDIUM): WF1 architect took 583s (9.7 min) for simple task decomposition

**Type**: Performance  
**Description**: The WF1 architect spent nearly 10 minutes generating a plan for 7 blog post sections. This is disproportionate — the task is straightforward decomposition. WF2's architect completed in 117s for more complex software task planning.

**Likely causes**:
- The architect prompt includes full workflow context and conversation history, which grows large with each round-trip
- qwen3.5:9b thinking through a content plan for a blog post generates more verbose reasoning than a software task plan
- Ollama was partially occupied with early WF2 director/PM phases during this window

**Recommendation**: Profile the architect prompt token count. Consider a model context cap or summarization step before the architect call to prevent prompt bloat.

---

### Issue 8 (MEDIUM): WF2 finalizer took 353s (5.9 min)

**Type**: Performance  
**Description**: The WF2 finalizer is the slowest single LLM call in the entire run — slower than even the WF1 architect. The finalizer synthesizes 6 code artifacts and writes a summary. 5.9 min for a synthesis task over ~8 KB of code suggests the finalizer prompt grows large when artifacts are passed in full.

**Recommendation**: Check whether the finalizer receives full artifact content in its prompt. If so, consider passing content summaries or line counts for large artifact sets, with full content available via tool call only if needed.

---

### Issue 9 (MEDIUM): All personas use `qwen3.5:9b` — no per-persona model routing active

**Type**: Configuration / analysis correction  
**Description**: The `local-dev.yaml` config specifies only `default_model: "qwen3.5:9b"` with no per-persona model overrides. All visible personas (director, project_manager, architect, QA, finalizer) used `qwen3.5:9b` for both workflows. The implementer model cannot be confirmed from events (see Issue 1), but given the config, it also used `qwen3.5:9b`.

Previous analysis in this document stated `WF2 → qwen2.5-coder:14b` — this was **incorrect** for this run. Per-persona model routing (the feature described in the WF1 blog post) requires explicit per-persona `model:` configuration in the YAML, which has not been configured in `local-dev.yaml`.

**Evidence**: Every `persona.started` event in both WF1 and WF2 event logs shows `model=qwen3.5:9b`.

**Recommendation**: Add per-persona model configuration to `local-dev.yaml` (e.g., implementer: `qwen2.5-coder:14b`, director/PM: a lighter model). This would reduce code task LLM time significantly and validate the routing feature end-to-end.

---

### Issue 10 (LOW): `qa.exhausted` event fires on PASS, not just FAIL

**Type**: Misleading event semantics  
**Description**: `qa.exhausted` fires whenever `qa_cycle == MaxQARetries+1`, regardless of whether the final QA cycle passed or failed. In both WF1 and WF2, the final QA cycle passed and the workflow completed successfully — yet `qa.exhausted` still appeared in the event log. The event name implies failure.

**Recommendation**: Rename to `qa.retries_exhausted` or add a `passed: bool` field to the payload so consumers can distinguish "exhausted AND failed" from "exhausted AND passed".

---

### Issue 11 (LOW): Director double-write (pre-existing)

**Type**: Correctness  
**Description**: The Director persona writes its plan to the event journal twice, creating duplicate entries. Does not affect workflow execution.  
**Status**: Pre-existing from Run 1. Not fixed.

---

### Issue 12 (LOW): Implementer `read_file` on non-existent file

**Type**: Resilience / LLM toolcall  
**Description**: WF1's implementer called `read_file` on `hooks/hook_section.md` — a file it was about to write, not one that existed. The executor logged `WARN: read_file: open hooks/hook_section.md: no such file or directory` and continued. Task succeeded.

**Root cause**: LLM issued a `read_file` before `write_file` on a file it was generating fresh. The error was silently swallowed.

**Recommendation**: When `read_file` fails with "no such file", surface the error as a tool result message back to the LLM (e.g., `"error: file does not exist — create it with write_file"`). This gives the LLM a chance to self-correct rather than silently continuing.

---

### Issue 13 (LOW): Missing `skills/code-generation/references/go-idioms.md`

**Type**: Incomplete skill package  
**Description**: The refiner identified this in a `refiner.suggestion` event: the `code-generation` skill references `go-idioms.md` but the file doesn't exist. This is the root cause of the implementer producing non-idiomatic Go in WF1.

**Refiner suggestion (verbatim)**:
> [medium] The code-generation skill references `go-idioms.md` in its references section but the file doesn't exist. → Create `skills/code-generation/references/go-idioms.md` with curated examples of idiomatic Go patterns.

**Status**: File not yet created.

---

## Run 2 — Key Validations

| Validation | Result |
|-----------|--------|
| Bug #6 fix: `FinalizationResult.Metadata map[string]any` | ✅ Confirmed — WF2 metadata serialized as proper JSON object, no panic |
| Per-persona model routing (WF2 → coder model) | ❌ NOT active — `local-dev.yaml` has no per-persona model config; all personas used `qwen3.5:9b` |
| `MaxQARetries=2` cap (max 3 QA cycles) | ✅ Both workflows hit exactly 3 QA cycles |
| QA→architect→implementer→QA remediation cycle | ✅ Executed correctly in both workflows |
| `api-response` finalizer action for software mode | ✅ WF2 selected correctly by Director |
| `blog-draft` finalizer action for content mode | ✅ WF1 selected correctly by Director |
| No panics or ERROR-level log entries during full run | ✅ Clean server log |
| Concurrent workflow execution (two workflows in parallel) | ✅ Both completed successfully despite Ollama serial contention |
| System self-improvement (refiner.suggestion events) | ✅ WF1 emitted 4 refiner suggestions post-completion |

---

## Run 2 — Retry and Resilience Events

| Workflow | Persona | Attempt | Error | Recovery |
|---|---|---|---|---|
| WF2 | architect | 1 | `persona.retrying` (reason: empty — see Issue 4) | Retry succeeded; total 117s |
| WF1 | qa cycle 2 | 1 | `persona.retrying` (reason: empty — see Issue 4) | Retry succeeded; total 87s |
| WF1 | implementer | — | `read_file: no such file` (tool error, WARN only) | Continued, task succeeded |
| WF1 | qa (cycle 1) | — | 2 blocking issues (missing code example, word count) | rem=1 remediation triggered |
| WF1 | qa (cycle 2) | — | 1 blocking issue (QA hallucinated Go syntax error — see Issue 5) | rem=2 triggered (~25 min overhead) |
| WF1 | qa (cycle 3) | — | PASSED | `qa.exhausted` event + finalizer triggered |
| WF2 | qa (cycle 1) | — | 2 blocking issues (context propagation, concurrent test) | rem=1 remediation triggered |
| WF2 | qa (cycle 2) | — | 9 blocking issues (atomic+mutex, recursion, missing imports, etc.) | rem=2 total rewrite triggered |
| WF2 | qa (cycle 3) | — | PASSED | `qa.exhausted` event + finalizer triggered |

---

## Run 2 — System Self-Improvement (Refiner Suggestions)

After WF1 completed, the engine emitted 4 `refiner.suggestion` events captured in the event journal:

| Priority | Component | Suggestion Summary |
|----------|-----------|-------------------|
| high | persona: implementer | Add explicit Go slice-append idiom guidance; implementer lacks constraints against non-idiomatic patterns |
| medium | skill: go-idioms | `skills/code-generation/references/go-idioms.md` is referenced by the code-generation skill but does not exist — create it |
| low | persona: qa | QA exhausted retry budget; root cause is implementer producing invalid syntax, not QA validation logic |
| low | persona: director | Director routing is correct; no change needed |

These suggestions are actionable. The `go-idioms.md` gap directly caused the false-positive syntax error in WF1 QA cycle 2.

---

## Run 2 — Issues Summary

| # | Issue | Type | Severity |
|---|-------|------|----------|
| 1 | Implementer emits no persona events | Tracking bug | Critical |
| 2 | All task durations recorded as 0ms | Tracking bug | High |
| 3 | Artifact sizes are 0 bytes in events | Tracking bug | Medium |
| 4 | `persona.retrying` reason field empty | Tracking gap | Medium |
| 5 | QA hallucinated Go syntax error (~25 min overhead) | LLM false positive | High |
| 6 | rem=1 fixes introduce rem=2 regressions (both workflows) | LLM quality | Medium |
| 7 | WF1 architect: 583s for simple task decomposition | Performance | Medium |
| 8 | WF2 finalizer: 353s for artifact synthesis | Performance | Medium |
| 9 | All personas use `qwen3.5:9b`, no per-persona routing active | Config gap | Medium |
| 10 | `qa.exhausted` fires on PASS (misleading event name) | Semantics | Low |
| 11 | Director double-write (pre-existing) | Correctness | Low |
| 12 | `read_file` on non-existent file silently swallowed | Resilience | Low |
| 13 | `skills/code-generation/references/go-idioms.md` missing | Missing file | Low |
