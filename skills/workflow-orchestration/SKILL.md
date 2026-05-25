---
name: workflow-orchestration
description: Model routing and structured-output rules for go-orca workflow personas (Director, Project Manager, Architect).
---

# Workflow Orchestration Skill

Use this skill when configuring or improving go-orca **workflow personas** — especially Director model routing and Project Manager constitution JSON.

## Director — persona model routing

The Director assigns `persona_models` for every downstream persona. Follow these rules when editing `prompts/personas/director.md` or tuning routing:

| Persona | Model guidance |
|---------|----------------|
| **project_manager** | **Never** assign sub-4B models (e.g. `llama3.2:1b`, `*:1b`, `*:3b`). PM output is strict JSON; small models emit invalid shapes (`audience` as object, spaced field names). Prefer the workflow default or **≥ 7B** models (`qwen3.5:9b`, `gpt-oss:20b`, etc.). |
| **architect** | Mid/large models (≥ 7B) for design + task graphs. |
| **pod**, **qa** | **Must** use `tools=yes` models only. |
| **matriarch** | Mid-size, `tools=yes` when available. |
| **finalizer** | Largest available model for synthesis. |

Do **not** document that PM should prefer sub-4B or “small models for planning” — that causes production failures on Ollama homelab catalogs.

## Project Manager — constitution JSON contract

PM responses must parse into `internal/state.Constitution` and `Requirements`. When editing `prompts/personas/project_manager.md`:

- **`constitution.audience`** — plain **string** only (e.g. `"general readers"`). Never `{"type":"general"}` or other objects.
- **Field names** — exact snake_case keys: `acceptance_criteria`, `output_medium`, `out_of_scope`. No spaces in keys.
- **`out_of_scope`** — JSON array of strings, not a single string (engine normalizes common mistakes).
- **`acceptance_criteria`** — array of strings, not objects, unless the engine flex parser applies.
- Keep **requirements** at the top level (`requirements.functional`, `requirements.non_functional`), not nested inside `constitution`.

The engine runs `normalizePMOutput` for common drifts; prompts should still target the correct schema to avoid retry loops and 90m handoff waits.

## Homelab / Ollama

When the default provider is Ollama, pin `provider` and `model` on workflow create (`qwen3.5:9b` or your cluster default). Director still chooses `persona_models`, but engine `normalizePersonaModels` swaps sub-4B models away from PM.
