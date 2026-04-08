-- 006_delivery_config (down): removes caller-supplied delivery fields.

ALTER TABLE workflows DROP COLUMN IF EXISTS delivery_config;
ALTER TABLE workflows DROP COLUMN IF EXISTS delivery_action;
