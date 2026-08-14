-- connector_upgrade_reports 增加 host_name / install_id：
-- 一台机器跑一个 connector 进程，但里面会接多个 agent，每个 agent 都各
-- 自上报一份升级结果。统计/灰度自动暂停时需要按机器维度去重，
-- 优先用 install_id（持久化 UUID，主机名变了也不丢），其次 host_name，
-- 兜底 agent_id。两列都允许 NULL，老记录走 agent_id 兜底语义。
ALTER TABLE connector_upgrade_reports
    ADD COLUMN IF NOT EXISTS host_name  VARCHAR(255),
    ADD COLUMN IF NOT EXISTS install_id VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_connector_upgrade_reports_install_version
    ON connector_upgrade_reports (install_id, to_version);
CREATE INDEX IF NOT EXISTS idx_connector_upgrade_reports_host_version
    ON connector_upgrade_reports (host_name, to_version);
