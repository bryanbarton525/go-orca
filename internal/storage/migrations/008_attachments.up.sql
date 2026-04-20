-- 008_attachments: upload sessions, attachments, chunks, and workflow attachment columns.
--
-- upload_sessions — staged upload sessions that collect files before workflow creation.
-- attachments     — per-file metadata and processing state.
-- attachment_chunks — optional text chunks for large document retrieval.
-- workflows       — new columns for input documents, corpus summary, and attachment processing.

CREATE TABLE IF NOT EXISTS upload_sessions (
    id          TEXT    NOT NULL PRIMARY KEY,
    tenant_id   TEXT    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    scope_id    TEXT    NOT NULL REFERENCES scopes(id),
    status      TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open','consumed','expired','aborted')),
    workflow_id TEXT,
    expires_at  INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_upload_sessions_tenant ON upload_sessions(tenant_id, status);

CREATE TABLE IF NOT EXISTS attachments (
    id                TEXT    NOT NULL PRIMARY KEY,
    upload_session_id TEXT    NOT NULL REFERENCES upload_sessions(id) ON DELETE CASCADE,
    workflow_id       TEXT,
    tenant_id         TEXT    NOT NULL,
    scope_id          TEXT    NOT NULL,
    filename          TEXT    NOT NULL,
    content_type      TEXT    NOT NULL DEFAULT '',
    size_bytes        INTEGER NOT NULL DEFAULT 0,
    storage_path      TEXT    NOT NULL DEFAULT '',
    status            TEXT    NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','completed','failed')),
    summary           TEXT    NOT NULL DEFAULT '',
    chunk_count       INTEGER NOT NULL DEFAULT 0,
    error_message     TEXT    NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_attachments_session  ON attachments(upload_session_id);
CREATE INDEX IF NOT EXISTS idx_attachments_workflow ON attachments(workflow_id);

CREATE TABLE IF NOT EXISTS attachment_chunks (
    id            TEXT    NOT NULL PRIMARY KEY,
    attachment_id TEXT    NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    workflow_id   TEXT    NOT NULL,
    chunk_index   INTEGER NOT NULL,
    content       TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_chunks_attachment ON attachment_chunks(attachment_id, chunk_index);

-- Workflow columns for attachment snapshot
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS upload_session_id               TEXT NOT NULL DEFAULT '';
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS input_documents                 TEXT NOT NULL DEFAULT '[]';
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS input_document_corpus_summary   TEXT NOT NULL DEFAULT '';
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS attachment_processing           TEXT NOT NULL DEFAULT 'null';
