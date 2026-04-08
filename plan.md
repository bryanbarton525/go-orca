# Persona Prompt Externalization and Director-Guided Pipeline Plan

## Goal

Move built-in persona system prompts out of Go string constants into runtime-loaded markdown files under `promos/personas`, ensure prompt changes affect only newly started workflows, and make the engine honor Director-selected persona inclusion/skipping and `finalizer_action` while preserving the current execution order.

## Confirmed Decisions

- [x] Prompt changes apply only to new workflows.
- [x] Base persona prompt root is `promos/personas`.
- [x] First dynamic pipeline step is inclusion/skipping only.
- [x] Missing required prompt files are hard errors.
- [x] Director-selected `finalizer_action` must be honored immediately when set.

## Review Corrections

- The current engine snapshots customization sources inside each `Run`, not persistently. An in-memory-only persona prompt snapshot would not satisfy "only new workflows" across pause/resume or retry. The persona prompt snapshot must be persisted on `WorkflowState` when first loaded.
- The current Director already emits `required_personas` and `finalizer_action`, but the engine ignores both and still runs a fixed sequence.
- The current Finalizer records an action in `FinalizationResult.Action` but does not execute a delivery action through `internal/finalizer/actions`. In this scope, "honor immediately" means the chosen action is stored and used in Finalizer output, not that real delivery action execution is added.
- `WorkflowState.AllSuggestions` currently exists in memory but does not round-trip through storage. Fix that while touching storage.

## Scope

### In Scope

- [ ] Externalize built-in persona prompts into markdown files under `promos/personas`.
- [ ] Load required persona prompt files at runtime.
- [ ] Persist a workflow-scoped persona prompt snapshot so existing workflows keep the same prompt content across resume and retry.
- [ ] Refactor personas and executor to use loaded prompt text instead of compiled constants.
- [ ] Persist Director planning fields on `WorkflowState`.
- [ ] Make the engine honor Director persona inclusion/skipping while preserving the current phase order.
- [ ] Make Finalizer honor a Director-selected `finalizer_action` immediately.
- [ ] Add storage, migration, test, and documentation coverage for the above.

### Out Of Scope

- Arbitrary ordered pipeline graphs.
- New persona types.
- Full delivery action execution through `internal/finalizer/actions`.
- Persisting the existing customization snapshot across resume and retry.

## Required Prompt Files

- `promos/personas/director.md`
- `promos/personas/project_manager.md`
- `promos/personas/architect.md`
- `promos/personas/implementer.md`
- `promos/personas/qa.md`
- `promos/personas/finalizer.md`
- `promos/personas/finalizer_refiner.md`
- `promos/personas/refiner.md`

## Implementation Tasks

### 1. Persona Prompt Catalog Package

- [ ] Add `internal/persona/prompts/catalog.go` with stable prompt keys, a loader that reads all required files from `promos/personas`, and a hard error if any are missing.
- [ ] Seed each markdown file with the current prompt body from the corresponding Go constant so behavior stays as close as possible to the current system on first rollout.

### 2. Executor Refactor

- [ ] Refactor `internal/persona/base/executor.go` so the executor no longer depends on a constructor-time system prompt. Pass `systemPrompt` into execution per call while keeping schema handling and overlay layering logic unchanged.
- [ ] Update each persona package to fetch its base prompt from the persisted snapshot instead of using a hard-coded constant.
- [ ] Use two separate prompt keys inside Finalizer: one for the main Finalizer phase and one for the inline Refiner retrospective phase.
- [ ] Keep `PromptsContext` and the existing customization overlays separate from the new base persona prompt files.

### 3. Workflow-Start Prompt Snapshot

- [ ] Add `PersonaPromptSnapshot map[string]string` to `state.WorkflowState`.
- [ ] In the engine, load and persist the persona prompt snapshot before the first persona phase runs, reusing it on resume or retry.

### 4. Director Planning Fields

- [ ] Add `RequiredPersonas []state.PersonaKind` and `FinalizerAction string` to `state.WorkflowState`.
- [ ] Add `FinalizerAction string` to `state.HandoffPacket`.
- [ ] Extend `engine.applyOutput` to normalize and persist `RequiredPersonas` and `FinalizerAction` from Director output.
- [ ] Validate and normalize both fields; fall back to defaults on invalid/empty Director output.

### 5. Storage Changes

- [ ] Persist `PersonaPromptSnapshot`, `RequiredPersonas`, `FinalizerAction`, and `AllSuggestions` in SQLite and Postgres create/save/load/list paths.
- [ ] Add Postgres migration `004_workflow_planning.up.sql` and `004_workflow_planning.down.sql`.
- [ ] Add idempotent `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` handling for SQLite DDL evolution.
- [ ] Update `internal/storage/sqlite/sqlite_test.go` to cover the new fields.

### 6. Engine Inclusion/Skipping Logic

- [ ] Keep `director` mandatory and always run it first.
- [ ] After Director runs, use `RequiredPersonas` as a filter over the current fixed order:
  `project_manager -> architect -> implementer -> qa -> finalizer`
- [ ] Skip implementer task loop entirely when `implementer` is not selected.
- [ ] Skip QA phase and retry loop entirely when `qa` is not selected.
- [ ] Skip Finalizer when `finalizer` is not selected.

### 7. Finalizer Honoring Director Action

- [ ] Pass `FinalizerAction` into Finalizer via `HandoffPacket`.
- [ ] Make Finalizer prefer the Director-selected action in code after parsing LLM response so it cannot drift.
- [ ] Update Finalizer prompt wording so the model understands the preferred action.

### 8. Tests

- [ ] Unit tests for persona prompt loader: success, aggregated missing-file errors, snapshot content shape.
- [ ] Engine tests for Director-selected persona inclusion/skipping.
- [ ] Engine/Finalizer tests verifying `FinalizerAction` from Director is enforced.
- [ ] Storage round-trip tests for all new fields.
- [ ] SQLite migration coverage for schema evolution on an existing database.
- [ ] Resume-style test that confirms existing workflows keep originally persisted prompt snapshot.

### 9. Docs

- [ ] Update or add docs distinguishing base persona prompts from additive customization overlays.
- [ ] Document `promos/personas` as the prompt source and snapshot semantics.
- [ ] Document Director inclusion/skipping behavior.

## Data Model Changes

| Field | Type | Location | Purpose |
|---|---|---|---|
| `PersonaPromptSnapshot` | `map[string]string` | `WorkflowState` | Persisted copy of prompt file content at workflow start |
| `RequiredPersonas` | `[]state.PersonaKind` | `WorkflowState` | Director-selected pipeline phases |
| `FinalizerAction` | `string` | `WorkflowState` | Director-selected delivery action |
| `FinalizerAction` | `string` | `HandoffPacket` | Forwarded to Finalizer at execution time |
| `AllSuggestions` | `[]string` | `WorkflowState` | Fix: was in memory only; must persist |

## Storage Columns

| Column | Type | Tables |
|---|---|---|
| `persona_prompt_snapshot` | JSON / TEXT | `workflows` |
| `required_personas` | JSON / TEXT | `workflows` |
| `finalizer_action` | TEXT | `workflows` |
| `all_suggestions` | JSON / TEXT | `workflows` |

## File Touchpoints

**New files:**
- `internal/persona/prompts/catalog.go`
- `internal/persona/prompts/catalog_test.go`
- `promos/personas/director.md`
- `promos/personas/project_manager.md`
- `promos/personas/architect.md`
- `promos/personas/implementer.md`
- `promos/personas/qa.md`
- `promos/personas/finalizer.md`
- `promos/personas/finalizer_refiner.md`
- `promos/personas/refiner.md`
- `internal/storage/migrations/004_workflow_planning.up.sql`
- `internal/storage/migrations/004_workflow_planning.down.sql`

**Modified files:**
- `internal/persona/base/executor.go`
- `internal/persona/director/director.go`
- `internal/persona/pm/pm.go`
- `internal/persona/architect/architect.go`
- `internal/persona/implementer/implementer.go`
- `internal/persona/qa/qa.go`
- `internal/persona/finalizer/finalizer.go`
- `internal/persona/refiner/refiner.go`
- `internal/workflow/engine/engine.go`
- `internal/state/state.go`
- `internal/storage/sqlite/sqlite.go`
- `internal/storage/postgres/postgres.go`
- `internal/storage/sqlite/sqlite_test.go`
- `docs/customization.md` (or new `docs/personas.md`)

## Acceptance Criteria

- [ ] No built-in persona package uses a hard-coded multi-line system prompt constant.
- [ ] Editing `promos/personas/*.md` changes prompt behavior for newly started workflows without recompiling.
- [ ] Existing paused, resumed, or retried workflows use the original prompt snapshot stored on their workflow record.
- [ ] Missing required prompt files fail clearly before persona execution begins.
- [ ] Director-selected `required_personas` are persisted and actually control which phases run.
- [ ] Director-selected `finalizer_action` is persisted and is the action stored by Finalizer when present.
- [ ] Current default behavior remains intact when Director output is empty or invalid.
- [ ] `AllSuggestions` round-trips through storage.
- [ ] SQLite and Postgres remain in parity for the new workflow fields.
- [ ] Docs clearly distinguish base persona prompts from additive customization overlays.

## Notes For The Implementation Agent

- Keep the refactor minimal. Do not redesign the executor or pipeline model more than necessary for this first pass.
- Preserve the current phase order and task model. This change is about externalized prompts and phase inclusion/skipping, not arbitrary orchestration.
- Treat the prompt snapshot as workflow state, not process state. That is the key requirement behind "only new workflows".
- Do not pull the new base persona prompt files into the existing customization registry. They serve a different purpose than `*.prompt.md` overlays.
- Do not add real finalizer action execution in this scope. The current system records the chosen action only.
- Prefer clear validation and explicit failure over silent fallbacks for missing prompt files.
- The prompt catalog root `promos/personas` should be overridable via a function parameter or option for test isolation.
