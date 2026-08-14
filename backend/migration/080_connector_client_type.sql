-- connector_releases / connector_upgrade_reports 增加 client_type 列，
-- 区分 grix-connector 与 grix-hermes 两类发布和上报。

ALTER TABLE connector_releases
    ADD COLUMN IF NOT EXISTS client_type VARCHAR(32) NOT NULL DEFAULT 'grix-connector';

ALTER TABLE connector_upgrade_reports
    ADD COLUMN IF NOT EXISTS client_type VARCHAR(32) NOT NULL DEFAULT 'grix-connector';

-- 原 UNIQUE(version, channel) 不含 client_type，同版本号不同类型会冲突；
-- 改为三列联合唯一。
ALTER TABLE connector_releases
    DROP CONSTRAINT IF EXISTS connector_releases_version_channel_key;
ALTER TABLE connector_releases
    ADD CONSTRAINT connector_releases_client_type_version_channel_key
    UNIQUE (client_type, version, channel);

-- node_version 列宽从 VARCHAR(16) 扩到 VARCHAR(32)，对齐 model。
ALTER TABLE connector_upgrade_reports
    ALTER COLUMN node_version TYPE VARCHAR(32);
