-- 003_task_edges.up.sql
-- Adds a relational task_edges table for DAG dependency queries.
-- Each row represents a directed edge: (from_task_id) → (to_task_id),
-- meaning to_task_id must not begin until from_task_id has completed.
-- Tasks themselves remain stored as JSONB inside workflows.tasks; this table
-- provides queryable graph structure for scheduling and visualization.

CREATE TABLE IF NOT EXISTS task_edges (
    id           TEXT        NOT NULL PRIMARY KEY,
    workflow_id  TEXT        NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    tenant_id    TEXT        NOT NULL,
    from_task_id TEXT        NOT NULL,  -- prerequisite task
    to_task_id   TEXT        NOT NULL,  -- dependent task
    edge_kind    TEXT        NOT NULL DEFAULT 'depends_on'
                             CHECK (edge_kind IN ('depends_on', 'blocks', 'related')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, from_task_id, to_task_id, edge_kind)
);

CREATE INDEX IF NOT EXISTS idx_task_edges_workflow  ON task_edges(workflow_id);
CREATE INDEX IF NOT EXISTS idx_task_edges_from_task ON task_edges(workflow_id, from_task_id);
CREATE INDEX IF NOT EXISTS idx_task_edges_to_task   ON task_edges(workflow_id, to_task_id);
CREATE INDEX IF NOT EXISTS idx_task_edges_tenant    ON task_edges(tenant_id);
