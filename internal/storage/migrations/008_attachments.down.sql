-- 008_attachments: rollback
DROP TABLE IF EXISTS attachment_chunks;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS upload_sessions;

-- SQLite does not support DROP COLUMN; these are no-ops on SQLite but work on Postgres.
-- For SQLite, a full table rebuild is needed to remove columns — skipped for rollback simplicity.
