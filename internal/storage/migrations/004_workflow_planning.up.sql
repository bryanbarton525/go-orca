-- 004_workflow_planning: adds Director-guided pipeline fields to workflows.
--
-- all_suggestions         — accumulated suggestion strings from all persona phases
--                           (fixes the AllSuggestions in-memory-only bug).
-- persona_prompt_snapshot — JSON map of prompt key → file content snapshotted at
--                           workflow start so resume/retry use identical prompts.
-- required_personas       — JSON array of PersonaKind strings chosen by Director;
--                           phases not listed are skipped by the engine.
-- finalizer_action        — delivery action chosen by Director, enforced in code
--                           by the Finalizer after LLM parse.

ALTER TABLE workflows
    ADD COLUMN IF NOT EXISTS all_suggestions         JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS persona_prompt_snapshot JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS required_personas       JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS finalizer_action        TEXT  NOT NULL DEFAULT '';
