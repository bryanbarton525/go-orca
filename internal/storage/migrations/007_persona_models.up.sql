-- 007_persona_models: persists Director-assigned persona model routing decisions
-- and the provider catalog snapshot taken at workflow start.
--
-- persona_models    — map of persona kind → model name as chosen by the Director.
--                     Stored as JSON so routing decisions survive server restart
--                     and are visible via GET /workflows/:id.
--
-- provider_catalogs — snapshot of each provider's available model inventory at
--                     the time the workflow started.  Captures which models were
--                     actually offered so routing decisions can be audited later
--                     even after the provider's catalog changes.

ALTER TABLE workflows
    ADD COLUMN IF NOT EXISTS persona_models TEXT NOT NULL DEFAULT '{}';

ALTER TABLE workflows
    ADD COLUMN IF NOT EXISTS provider_catalogs TEXT NOT NULL DEFAULT '{}';