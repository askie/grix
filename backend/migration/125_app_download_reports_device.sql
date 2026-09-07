-- 应用内更新可观测：光有「下载完成」不够，安卓侧的失败绝大多数发生在下载之后
-- （安装权限被拦、国产 ROM 纯净模式、包被提前删掉），从下载记录里完全看不出来。
-- stage 区分 download / install 两个阶段，device_model + os_version + abi 用于
-- 定位「哪些机型装不上」。四列都是 NOT NULL DEFAULT，老行按默认值补齐
-- （stage='download'，其余空串），不需要回填，客户端也可以不传。
ALTER TABLE app_download_reports ADD COLUMN IF NOT EXISTS stage VARCHAR(16) NOT NULL DEFAULT 'download';
ALTER TABLE app_download_reports ADD COLUMN IF NOT EXISTS device_model VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE app_download_reports ADD COLUMN IF NOT EXISTS os_version VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE app_download_reports ADD COLUMN IF NOT EXISTS abi VARCHAR(32) NOT NULL DEFAULT '';

-- 统计按 (release_id, stage) 聚合安装成功数与机型失败数。
CREATE INDEX IF NOT EXISTS idx_app_download_reports_release_stage
    ON app_download_reports (release_id, stage);
