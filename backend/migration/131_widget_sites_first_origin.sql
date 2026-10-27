-- 131: 访客会话按接入网站分组。
-- widget_sites 记录该站点第一个被访客实际接入的域名（规范化 host：小写、不含协议和端口），
-- 只在首次访客初始化时写入一次，后续不再覆盖；客户端用它给访客会话分组。
--
-- 回填顺序：
--   1. 该站点最早一条 widget_sessions.last_page_url 的 host；
--   2. 否则取 allowed_origins 第一项的 host；
--   3. 都没有则留空。

ALTER TABLE widget_sites ADD COLUMN IF NOT EXISTS first_origin VARCHAR(255) NOT NULL DEFAULT '';

UPDATE widget_sites AS ws
SET first_origin = COALESCE(
    (
        SELECT NULLIF(
                   lower(trim(both '[]' FROM regexp_replace(
                       split_part(split_part(wsess.last_page_url, '://', 2), '/', 1),
                       ':[0-9]+$', ''))),
                   '')
        FROM widget_sessions AS wsess
        WHERE wsess.site_id = ws.id
          AND wsess.last_page_url LIKE '%://%'
        ORDER BY wsess.created_at ASC, wsess.id ASC
        LIMIT 1
    ),
    (
        SELECT NULLIF(
                   lower(trim(both '[]' FROM regexp_replace(
                       split_part(split_part(ws.allowed_origins ->> 0, '://', 2), '/', 1),
                       ':[0-9]+$', ''))),
                   '')
        WHERE CASE
                  WHEN jsonb_typeof(ws.allowed_origins) = 'array'
                      THEN jsonb_array_length(ws.allowed_origins) > 0
                  ELSE FALSE
              END
    ),
    ''
)
WHERE ws.first_origin = '';
