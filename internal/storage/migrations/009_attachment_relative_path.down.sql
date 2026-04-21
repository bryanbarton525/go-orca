-- 009_attachment_relative_path: rollback

ALTER TABLE attachments DROP COLUMN IF EXISTS relative_path;
