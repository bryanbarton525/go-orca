-- 001_initial_schema.up.sql
-- Initial gorca schema: tenants, scopes, workflows, events, tasks, artifacts.

-- ─── Tenants ──────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS tenants (
    id         TEXT        NOT NULL PRIMARY KEY,
    slug       TEXT        NOT NULL UNIQUE,
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─── Scopes ───────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS scopes (
    id              TEXT        NOT NULL PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    kind            TEXT        NOT NULL CHECK (kind IN ('global','org','team')),
    name            TEXT        NOT NULL,
    slug            TEXT        NOT NULL,
    parent_scope_id TEXT        REFERENCES scopes(id) ON DELETE RESTRICT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, slug)
);
CREATE INDEX IF NOT EXISTS idx_scopes_tenant ON scopes(tenant_id);

-- ─── Workflows ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS workflows (
    id              TEXT        NOT NULL PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    scope_id        TEXT        NOT NULL REFERENCES scopes(id),
    status          TEXT        NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending','running','paused','completed','failed','cancelled')),
    mode            TEXT        NOT NULL DEFAULT 'software',
    title           TEXT        NOT NULL DEFAULT '',
    request         TEXT        NOT NULL,
    provider_name   TEXT,
    model_name      TEXT,
    error_message   TEXT,
    -- JSONB blobs for structured phase outputs
    constitution    JSONB,
    requirements    JSONB,
    design          JSONB,
    tasks           JSONB       NOT NULL DEFAULT '[]',
    artifacts       JSONB       NOT NULL DEFAULT '[]',
    finalization    JSONB,
    summaries       JSONB       NOT NULL DEFAULT '{}',
    blocking_issues JSONB       NOT NULL DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_workflows_tenant  ON workflows(tenant_id);
CREATE INDEX IF NOT EXISTS idx_workflows_scope   ON workflows(scope_id);
CREATE INDEX IF NOT EXISTS idx_workflows_status  ON workflows(status);

-- ─── Workflow events (append-only journal) ────────────────────────────────────
CREATE TABLE IF NOT EXISTS workflow_events (
    id          TEXT        NOT NULL PRIMARY KEY,
    workflow_id TEXT        NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    tenant_id   TEXT        NOT NULL,
    scope_id    TEXT        NOT NULL,
    type        TEXT        NOT NULL,
    persona     TEXT,
    payload     JSONB,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_events_workflow    ON workflow_events(workflow_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_events_tenant_time ON workflow_events(tenant_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_events_type        ON workflow_events(type);
