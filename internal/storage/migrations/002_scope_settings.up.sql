-- 002_scope_settings.up.sql
-- Adds per-scope configuration and policy tables.

-- ─── Scope settings (arbitrary KV per scope) ─────────────────────────────────
CREATE TABLE IF NOT EXISTS scope_settings (
    id         TEXT        NOT NULL PRIMARY KEY,
    scope_id   TEXT        NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    tenant_id  TEXT        NOT NULL,
    key        TEXT        NOT NULL,
    value      TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope_id, key)
);
CREATE INDEX IF NOT EXISTS idx_scope_settings_scope ON scope_settings(scope_id);

-- ─── Scope component sources (customization discovery config per scope) ───────
CREATE TABLE IF NOT EXISTS scope_component_sources (
    id              TEXT        NOT NULL PRIMARY KEY,
    scope_id        TEXT        NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    tenant_id       TEXT        NOT NULL,
    name            TEXT        NOT NULL,
    source_type     TEXT        NOT NULL, -- filesystem | repo | git-mirror | builtin
    root            TEXT        NOT NULL,
    precedence      INTEGER     NOT NULL DEFAULT 50,
    enabled_types   JSONB       NOT NULL DEFAULT '[]', -- ["skill","agent","prompt"]
    refresh_seconds INTEGER     NOT NULL DEFAULT 300,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope_id, name)
);
CREATE INDEX IF NOT EXISTS idx_scope_comp_sources_scope ON scope_component_sources(scope_id);

-- ─── Scope provider policies (allowed providers/models per scope) ─────────────
CREATE TABLE IF NOT EXISTS scope_provider_policies (
    id               TEXT        NOT NULL PRIMARY KEY,
    scope_id         TEXT        NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    tenant_id        TEXT        NOT NULL,
    allowed_providers JSONB      NOT NULL DEFAULT '[]', -- ["openai","ollama","copilot"]
    default_provider TEXT,
    default_model    TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope_id)
);

-- ─── Scope tool policies (allowed tool packs per scope) ───────────────────────
CREATE TABLE IF NOT EXISTS scope_tool_policies (
    id             TEXT        NOT NULL PRIMARY KEY,
    scope_id       TEXT        NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    tenant_id      TEXT        NOT NULL,
    allowed_tools  JSONB       NOT NULL DEFAULT '[]', -- tool pack names
    denied_tools   JSONB       NOT NULL DEFAULT '[]',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope_id)
);

-- ─── Scope finalizer policies (allowed delivery targets per scope) ────────────
CREATE TABLE IF NOT EXISTS scope_finalizer_policies (
    id                TEXT        NOT NULL PRIMARY KEY,
    scope_id          TEXT        NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    tenant_id         TEXT        NOT NULL,
    allowed_actions   JSONB       NOT NULL DEFAULT '[]', -- finalizer action names
    default_action    TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope_id)
);
