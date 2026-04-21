-- 009_attachment_relative_path: add relative_path column to preserve folder structure
--
-- When uploading folders via webkitdirectory, preserve the full relative path
-- (e.g., "subfolder/config.json") to maintain context and avoid name collisions.

ALTER TABLE attachments ADD COLUMN IF NOT EXISTS relative_path TEXT NOT NULL DEFAULT '';
