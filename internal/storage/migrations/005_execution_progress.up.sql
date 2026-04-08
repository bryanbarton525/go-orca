-- 005_execution_progress: adds live execution progress tracking to workflows.
--
-- execution — JSON object (state.Execution) written by the engine at every
--             persona/task transition so GET /workflows/:id shows real-time
--             progress without requiring clients to read the event stream.
--             Fields: current_persona, active_task_id, active_task_title,
--                     qa_cycle, remediation_attempt.

ALTER TABLE workflows
    ADD COLUMN IF NOT EXISTS execution JSONB NOT NULL DEFAULT '{}';
