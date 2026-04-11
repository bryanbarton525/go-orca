# Workflow Engine

The workflow engine (`internal/workflow/engine`) drives a single workflow run from start to finish through a fixed persona pipeline. The scheduler (`internal/workflow/scheduler`) manages concurrent execution across a bounded worker pool.

## Persona Pipeline

Every workflow passes through six phases in order:

```
Director
   │
   ▼
Project Manager
   │
   ▼
Architect
   │
   ▼
Implementer  ◄──────────────────────────┐
   │                                     │  Implementer executes new tasks
   ▼                                     │  from Architect's remediation plan
QA ──── blocking issues? ──► Architect ──┘
   │                         (re-plans)
   │ (passes or max QA cycles exhausted)
   ▼
Finalizer
```

### Phase Descriptions

| Phase | Persona Kind | Purpose | Primary Output |
|---|---|---|---|
| 1 | `director` | Selects provider, model, workflow mode, and title from the request | `ProviderName`, `ModelName`, `Mode`, `Title` on `WorkflowState` |
| 2 | `project_manager` | Produces a `Constitution` (vision, goals, constraints) and structured `Requirements` | `Constitution`, `Requirements` |
| 3 | `architect` | Designs the solution: components, decisions, tech stack, and a `Task` graph | `Design`, `Tasks[]` |
| 4 | `implementer` | Executes once per ready/pending task; produces `Artifacts` | `Artifacts[]` appended per task |
| 5 | `qa` | Reviews artifacts; raises `BlockingIssues` and `Suggestions` | `BlockingIssues`, `AllSuggestions` |
| 6 | `finalizer` | Selects and runs a delivery action; produces `FinalizationResult` | `Finalization` |

The `refiner` persona exists in the registry but is invoked by the Finalizer internally for a retrospective pass — it is not a separate top-level phase.

## Model Routing

Model routing runs in four steps at the start of every workflow and again after the Director completes.

### 1. Catalog Discovery

Before the Director runs, `ensureProviderCatalogs` calls `provider.Models()` on every registered provider and snapshots the result into `WorkflowState.ProviderCatalogs`. The snapshot is persisted immediately so resumed workflows use the same catalog that was in place when they started.

If `provider.Models()` fails or times out (default: 10 s, configurable via `ModelDiscoveryTimeout`), the engine falls back to a synthetic single-model catalog built from the configured `default_model`. The catalog is marked `Degraded: true` and workflow execution continues — discovery failure is never fatal.

Models listed in `excluded_models` config are filtered out during discovery and never appear in the catalog. Any routing decision that selects an excluded model is silently replaced.

### 2. Director Routing

Before the Director runs, every model in the provider catalog is surfaced to it as a hint line:

```
qwen3.5:9b [family=qwen3, params=9.4B]
qwen3:1.7b [family=qwen3, params=1.7B]
qwen2.5-coder:14b [family=qwen2.5-coder, params=14.8B]
```

The `params` value comes directly from the provider's metadata (`parameter_size` on Ollama). The Director uses this to make capacity-aware routing decisions:

- Synthesis-heavy personas (`implementer`, `finalizer`) are steered toward larger-parameter models so they have enough context to produce complete, untruncated artifacts.
- Classification and planning personas (`director`, `project_manager`) can use smaller models where output is compact.

The Director returns a JSON object with three routing fields:

```json
{
  "provider":       "ollama",
  "model":          "qwen3.5:9b",
  "persona_models": {
    "project_manager": "qwen3.5:9b",
    "architect":       "qwen3.5:9b",
    "implementer":     "qwen2.5-coder:14b",
    "qa":              "qwen3.5:9b",
    "finalizer":       "qwen2.5-coder:14b"
  }
}
```

The engine normalizes all three against the live catalog:

- **`provider`** — validated against the registered provider registry; falls back to the engine `DefaultProvider` if the name is unknown or the catalog is empty.
- **`model`** — validated against that provider's catalog; falls back to the catalog's `DefaultModel` if the requested model is absent or excluded.
- **`persona_models`** — each entry validated individually; any missing or excluded entry falls back to the workflow-level `model`.

### 3. Workflow-Level Override

Setting `model` on `POST /workflows` pins `WorkflowState.ModelName` regardless of what the Director returns. The Director still runs on that pinned model. The Director **can** still suggest different per-persona models via `persona_models` — those are normalized against the catalog and may differ from the pinned model.

Setting `provider` on `POST /workflows` pins `WorkflowState.ProviderName` and the Director cannot override it.

### 4. Per-Persona Resolution

`buildPacket` calls `resolvePersonaModel(ws, kind, provider)` for every persona before calling `Execute`:

1. For downstream personas (`project_manager`, `architect`, `implementer`, `qa`, `finalizer`), check `ws.PersonaModels[kind]` — use it if it passes the catalog allow-list.
2. Fall back to `ws.ModelName` (the workflow-level model).
3. If that is also absent or excluded, fall back to the catalog's `DefaultModel`.

The Director always receives `ws.ModelName` directly (step 2), not a persona-model assignment.

### Routing Behaviour Summary

| Scenario | Director model | Downstream persona models |
|---|---|---|
| No override | `DefaultModel` from config | Director's `persona_models`, each falling back to `DefaultModel` |
| `model` on POST | The requested model (catalog-validated) | Director's `persona_models`, falling back to the requested model |
| `provider` on POST | `DefaultModel` for that provider | Same |
| Both `provider` + `model` | Requested model on requested provider | Same |
| Requested model excluded | `DefaultModel` | Same |

`persona.started` SSE events carry `provider_name` and `model_name` so you can observe per-persona routing in real time.

## QA / Remediation Loop

The QA phase runs inside a loop capped at `MaxQARetries` (default: 2). When QA raises blocking issues, the **Architect** leads remediation — not the Implementer directly:

```
for qaCycle = 1 to MaxQARetries+1:
  run QA persona
  ws.Execution.QACycle = qaCycle

  if ws.BlockingIssues is empty → break (QA passed)
  if qaCycle == MaxQARetries+1  → fail workflow ("N blocking issues after K QA cycles")

  // Architect remediation
  run Architect with IsRemediation=true
    → Architect reads ## QA Blocking Issues and produces targeted new tasks
    → Engine validates: only tasks with assigned_to=implementer accepted
    → New tasks appended to ws.Tasks with RemediationSource="qa_remediation"
    → EventTaskCreated emitted per new task
  ws.Execution.RemediationAttempt++

  // Implementer re-run on new tasks only
  run Implementer (skips all tasks not assigned_to=implementer)

  clear ws.BlockingIssues
  check pause
```

Completed tasks from prior iterations are preserved for audit — only new Pending tasks from the Architect remediation pass are executed.

If `MaxQARetries` QA cycles complete with blocking issues still present, the engine transitions the workflow to `failed`.

## Role Enforcement (applyOutput)

`applyOutput` enforces strict output contracts after every persona invocation:

| Output field | Allowed persona | Violation handling |
|---|---|---|
| `Design`, `Tasks[]` | `architect` only | Output silently discarded; warning appended to `AllSuggestions` |
| `Artifacts[]` | `implementer` only | Output silently discarded; warning appended to `AllSuggestions` |
| `Constitution`, `Requirements` | `project_manager` only | Output silently discarded; warning appended to `AllSuggestions` |

Warnings follow the pattern: `role-enforcement: persona <kind> produced <field> which is not permitted; output discarded`.

## Task Ownership

Every `Task` carries an `AssignedTo` field (a `PersonaKind`). `runImplementerPhase` skips any task whose `AssignedTo` is not `implementer`. This prevents the Implementer from executing tasks intended for other phases and ensures QA-remediation tasks created by the Architect are correctly routed.

## Execution Progress

`WorkflowState.Execution` is updated at every persona and task boundary and persisted to storage immediately:

```go
type Execution struct {
    CurrentPersona     PersonaKind // most-recently active persona
    ActiveTaskID       string      // task currently running under Implementer
    ActiveTaskTitle    string
    QACycle            int         // current QA pass (1-based)
    RemediationAttempt int         // Architect remediation passes so far
}
```

Poll `GET /workflows/:id` to read in-flight progress without subscribing to the SSE stream.

## HandoffPacket

Every persona receives a `HandoffPacket` — a self-contained context snapshot built from the current `WorkflowState`:

```go
type HandoffPacket struct {
    WorkflowID string
    TenantID   string
    ScopeID    string
    Mode       WorkflowMode
    Request    string

    // Accumulated phase outputs from prior personas
    Constitution *Constitution
    Requirements *Requirements
    Design       *Design
    Tasks        []Task
    Artifacts    []Artifact
    Summaries    map[PersonaKind]string

    // Active execution context
    CurrentPersona PersonaKind
    ProviderName   string
    ModelName      string

    // Workflow-start snapshot of all base persona prompt file contents.
    // Personas read their system prompt from here so disk edits cannot
    // affect an in-flight workflow.
    PersonaPromptSnapshot map[string]string

    // Delivery action chosen by the Director, forwarded to the Finalizer
    // so it is enforced in code rather than inferred by the LLM.
    FinalizerAction string

    // Directory where the Refiner may write improvement files after the run.
    ImprovementsPath string

    // Customization context (snapshotted at workflow start)
    CustomAgentMD  string
    SkillsContext  string
    PromptsContext string

    // Issues and suggestions accumulated across prior phases
    BlockingIssues []string
    AllSuggestions []string

    // QA/remediation loop context — populated during Phase 5
    QACycle            int  // current QA pass (1-based)
    RemediationAttempt int  // number of Architect remediation passes so far
    IsRemediation      bool // true when this Architect invocation is a remediation pass
}
```

The packet ensures personas never need to read global state — everything they need is passed in.

## PersonaOutput

Each persona returns a `PersonaOutput`:

```go
type PersonaOutput struct {
    Persona        PersonaKind
    Summary        string
    RawContent     string
    BlockingIssues []string
    Suggestions    []string

    // Typed outputs (only the relevant field is set per persona)
    Constitution   *Constitution
    Requirements   *Requirements
    Design         *Design
    Tasks          []Task
    Artifacts      []Artifact
    Finalization   *FinalizationResult

    CompletedAt    time.Time
}
```

The engine merges this output back into `WorkflowState` after each phase via `applyOutput`.

## Customization Snapshot

At the start of `runPhases`, the engine calls `CustomizationRegistry.Snapshot(ws.ScopeID)` once. This snapshot is immutable for the duration of the workflow — live source changes do not affect a running pipeline.

The snapshot's three context strings (`SkillsContext`, `AgentsContext`, `PromptsContext`) are injected into every `HandoffPacket`.

## Pause and Resume

Between each phase the engine calls `checkPause`:

```go
if PauseFunc != nil && PauseFunc() {
    transition workflow to status=paused
    return ErrPaused
}
```

`ErrPaused` is a non-fatal sentinel. The scheduler recognises it and does **not** treat the workflow as failed. The workflow is persisted in `paused` status and can be resumed:

```
POST /workflows/:id/resume
```

Resume re-enqueues the workflow ID. The engine reloads the saved `WorkflowState` and continues from the last completed phase.

## State Transitions

```
pending
   │  (Scheduler.Enqueue + Engine.Run)
   ▼
running
   │           │              │
   ▼           ▼              ▼
completed   failed         paused
                              │
                    POST /workflows/:id/resume
                              │
                              ▼
                           running
                              │
                       ┌──────┴──────┐
                       ▼             ▼
                   completed      failed
```

The `cancelled` status is set by `POST /workflows/:id/cancel`. A cancelled workflow is terminal and cannot be resumed.

## WorkflowStatus Values

| Value | Meaning |
|---|---|
| `pending` | Created, not yet picked up by the scheduler |
| `running` | Actively executing in the engine |
| `paused` | Execution suspended; awaiting `POST /resume` |
| `completed` | All phases finished successfully |
| `failed` | A phase returned an unrecoverable error |
| `cancelled` | Cancelled via API before or during execution |

## WorkflowMode Values

| Value | Typical Use |
|---|---|
| `software` | Code generation, APIs, CLIs |
| `content` | Articles, documentation, copy |
| `docs` | Technical documentation |
| `research` | Analysis, literature review |
| `ops` | Infrastructure, runbooks |
| `mixed` | Multi-domain tasks |

The Director persona selects the mode from the user's request; it can also be overridden in the `POST /workflows` body.

## Implementer: Per-Task Execution

The Implementer phase iterates over all tasks with status `pending` or `ready`, but **only executes tasks whose `assigned_to` field is `implementer`**. Tasks assigned to other personas are skipped without modification.

```go
for i := range ws.Tasks {
    t := &ws.Tasks[i]
    if t.AssignedTo != PersonaImplementer {
        continue // skip tasks not owned by Implementer
    }
    if t.Status != TaskStatusReady && t.Status != TaskStatusPending {
        continue
    }
    // Update ws.Execution.ActiveTaskID / ActiveTaskTitle
    // Build a HandoffPacket with packet.Tasks = []Task{*t}
    // Execute Implementer persona
    // Mark task completed, append artifacts
}
```

Each task gets its own isolated `HandoffPacket` containing only that single task. `ws.Execution.ActiveTaskID` and `ActiveTaskTitle` are updated before each LLM call so `GET /workflows/:id` reflects which task is running. Summaries are appended per task: `[taskID] summary`.

During QA remediation, the Architect appends new tasks with `RemediationSource: "qa_remediation"` and `assigned_to: implementer`. `runImplementerPhase` naturally picks these up on the next Implementer pass without special handling.

## Scheduler

The scheduler (`internal/workflow/scheduler`) manages the worker pool:

| Option | Default | Description |
|---|---|---|
| `Concurrency` | 4 | Maximum parallel workflow runs |
| `RetryDelay` | 5s | Wait before re-enqueuing a failed workflow |
| `MaxRetries` | 0 | Number of automatic retries on failure (0 = none) |

Internal queue capacity = `Concurrency × 4`. `Enqueue` returns an error immediately if the queue is full.

`Scheduler.Shutdown(ctx)` cancels the worker context, waits for all in-flight jobs to finish, and returns an error if the provided context deadline is exceeded.

## Event Types

| Event Type | Trigger |
|---|---|
| `state.transition` | Any workflow status change |
| `persona.started` | Before `persona.Execute` is called |
| `persona.completed` | After `persona.Execute` returns successfully |
| `persona.failed` | After `persona.Execute` returns an error |
| `task.started` | Before Implementer executes a single task |
| `task.completed` | After Implementer finishes a single task |
| `task.failed` | After Implementer returns an error for a single task |
| `task.created` | When Architect appends a new remediation task during QA |
| `artifact.produced` | After each artifact is committed from Implementer output |
| `refiner.suggestion` | After the inline Refiner produces an improvement recommendation |
