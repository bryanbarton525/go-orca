-- 002_scope_settings.down.sql
-- Removes per-scope configuration and policy tables.

DROP TABLE IF EXISTS scope_finalizer_policies;
DROP TABLE IF EXISTS scope_tool_policies;
DROP TABLE IF EXISTS scope_provider_policies;
DROP TABLE IF EXISTS scope_component_sources;
DROP TABLE IF EXISTS scope_settings;
