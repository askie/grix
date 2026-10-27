-- 131: 访客会话按接入网站分组。
-- widget_sites 记录该站点第一个被访客实际接入的域名（规范化 host：小写、不含协议和端口），
-- 只在首次访客初始化时写入一次，后续不再覆盖；客户端用它给访客会话分组。
--
-- 回填顺序：
--   1. 该站点最早一条 widget_sessions.last_page_url 的 host；
--   2. 否则取 allowed_origins 第一项的 host；
--   3. 都没有则留空。

ALTER TABLE widget_sites ADD COLUMN IF NOT EXISTS first_origin VARCHAR(255) NOT NULL DEFAULT '';

-- 迁移内临时的 host 规整函数：只服务于本次回填，随会话结束自动消失。
-- 规则：去掉协议、userinfo、端口、路径、query、fragment，转小写；IPv6 去掉方括号；
-- `*` 通配和解析不出的值一律返回空串（调用方据此跳过）。
CREATE OR REPLACE FUNCTION pg_temp.first_origin_host(raw text)
RETURNS text
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    value text := btrim(coalesce(raw, ''));
    host text;
BEGIN
    IF value = '' THEN
        RETURN '';
    END IF;
    -- 顺序要紧：先切 fragment、query，再剥协议和 userinfo，最后才切路径，
    -- 否则 userinfo 里的 @、query 里的 / 都会混进 host。
    value := split_part(value, '#', 1);
    value := split_part(value, '?', 1);
    value := regexp_replace(value, '^[A-Za-z][A-Za-z0-9+.-]*://', '');
    value := regexp_replace(value, '^[^/]*@', '');
    IF value LIKE '[%' THEN
        -- IPv6 字面量：[2001:DB8::1]:8443/path → 2001:db8::1
        host := btrim(split_part(value, ']', 1), '[]');
    ELSE
        host := split_part(value, '/', 1);
        host := regexp_replace(host, ':[0-9]+$', '');
        -- 去掉数字端口后还带冒号：非法端口或未加方括号的 IPv6，都按无效处理。
        IF host LIKE '%:%' THEN
            RETURN '';
        END IF;
    END IF;
    host := lower(btrim(host));
    IF host = ''
        OR host LIKE '%*%'
        OR host ~ '\s'
        OR host ~ '[/?#@]'
    THEN
        RETURN '';
    END IF;
    RETURN host;
END;
$$;

UPDATE widget_sites AS ws
SET first_origin = COALESCE(
    (
        SELECT pg_temp.first_origin_host(wsess.last_page_url)
        FROM widget_sessions AS wsess
        WHERE wsess.site_id = ws.id
          AND pg_temp.first_origin_host(wsess.last_page_url) <> ''
        ORDER BY wsess.created_at ASC, wsess.id ASC
        LIMIT 1
    ),
    (
        SELECT NULLIF(pg_temp.first_origin_host(ws.allowed_origins ->> 0), '')
        WHERE CASE
                  WHEN jsonb_typeof(ws.allowed_origins) = 'array'
                      THEN jsonb_array_length(ws.allowed_origins) > 0
                  ELSE FALSE
              END
    ),
    ''
)
WHERE ws.first_origin = '';
