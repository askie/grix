-- widget 站点欢迎语从单一字符串升级为多语言 map（key 为 locale 代码，见 pkg/locale.Supported）。
-- 存量 display_config.welcome 为字符串，新代码按 map 反序列化会报错，导致访客配置接口 /
-- 塘主站点列表接口失败。此迁移把老字符串就地转成 {"en_US": "原文"}，空串则移除该字段。
-- 幂等：仅处理 welcome 为 string 的行；已是 object 或缺省的行不动。

-- 非空字符串 → {"en_US": 原文}
UPDATE widget_sites
SET display_config = jsonb_set(
        display_config,
        '{welcome}',
        jsonb_build_object('en_US', display_config->>'welcome')
    )
WHERE jsonb_typeof(display_config -> 'welcome') = 'string'
  AND COALESCE(display_config ->> 'welcome', '') <> '';

-- 空字符串 → 移除 welcome 字段（等价于未配置欢迎语）
UPDATE widget_sites
SET display_config = display_config - 'welcome'
WHERE jsonb_typeof(display_config -> 'welcome') = 'string'
  AND COALESCE(display_config ->> 'welcome', '') = '';
