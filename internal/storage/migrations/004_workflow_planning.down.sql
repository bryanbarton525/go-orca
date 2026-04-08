ALTER TABLE workflows
    DROP COLUMN IF EXISTS all_suggestions,
    DROP COLUMN IF EXISTS persona_prompt_snapshot,
    DROP COLUMN IF EXISTS required_personas,
    DROP COLUMN IF EXISTS finalizer_action;
