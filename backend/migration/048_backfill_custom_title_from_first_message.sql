-- 048: 回填 custom_title —— 对所有 custom_title 为空的人类成员，
-- 用该会话中第一条人类发送的消息内容截取前 24 个字符填充。

UPDATE session_members sm
SET custom_title = sub.title
FROM (
    SELECT ranked.session_id, LEFT(
        regexp_replace(trim(both from ranked.content), '\s+', ' ', 'g'),
        24
    ) AS title
    FROM (
        SELECT
            m.session_id,
            m.content,
            ROW_NUMBER() OVER (
                PARTITION BY m.session_id
                ORDER BY m.created_at ASC, m.msg_id ASC
            ) AS rn
        FROM messages m
        WHERE m.is_deleted = false
          AND m.sender_type = 1
          AND m.msg_type = 1
          AND trim(both from m.content) <> ''
    ) AS ranked
    WHERE ranked.rn = 1
) sub
WHERE sm.session_id = sub.session_id
  AND sm.member_type = 1
  AND (sm.custom_title IS NULL OR trim(both from sm.custom_title) = '');
