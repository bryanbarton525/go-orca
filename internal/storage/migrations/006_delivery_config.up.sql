-- 006_delivery_config: adds caller-supplied delivery action and config to workflows.
--
-- delivery_action — the delivery action key chosen by the API caller at workflow
--                   creation time (e.g. "github-pr", "webhook-dispatch").  When
--                   set this overrides the Director-selected finalizer_action so
--                   the caller retains full control over how artifacts are delivered.
--
-- delivery_config — non-secret action-specific configuration submitted with the
--                   workflow (target repo, base branch, webhook URL, etc.).
--                   Secrets (tokens, passwords) must come from environment
--                   variables and are NEVER stored here.

ALTER TABLE workflows
    ADD COLUMN IF NOT EXISTS delivery_action TEXT NOT NULL DEFAULT '';

ALTER TABLE workflows
    ADD COLUMN IF NOT EXISTS delivery_config JSONB NOT NULL DEFAULT 'null';
