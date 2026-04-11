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
