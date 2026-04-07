-- 003_task_edges.down.sql
DROP INDEX IF EXISTS idx_task_edges_tenant;
DROP INDEX IF EXISTS idx_task_edges_to_task;
DROP INDEX IF EXISTS idx_task_edges_from_task;
DROP INDEX IF EXISTS idx_task_edges_workflow;
DROP TABLE IF EXISTS task_edges;
