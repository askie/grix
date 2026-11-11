-- 132: widget_sites 新增 allowed_apps 字段，支持移动端 App SDK 渠道白名单校验。
-- 存放 iOS bundle id 与 Android 包名（反向域名格式，如 com.example.app），默认空数组 []。

ALTER TABLE widget_sites
  ADD COLUMN IF NOT EXISTS allowed_apps JSONB NOT NULL DEFAULT '[]'::jsonb;
