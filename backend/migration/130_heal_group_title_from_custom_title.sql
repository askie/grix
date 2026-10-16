-- 130: 群聊标题统一以 group_name 为准。
-- 把历史上写在 custom_title 上的显示名回填到 group_name，再清空群成员 custom_title，
-- 避免列表（曾优先 custom_title）与本地 sync（优先 group_name）继续抖动。

UPDATE sessions s
SET group_name = sub.title,
    state_version = s.state_version + 1,
    updated_at = NOW()
FROM (
    SELECT DISTINCT ON (sm.session_id)
        sm.session_id,
        trim(both from sm.custom_title) AS title
    FROM session_members sm
    INNER JOIN sessions s2 ON s2.session_id = sm.session_id
    WHERE s2.session_type = 2
      AND s2.is_deleted = false
      AND sm.member_type = 1
      AND sm.custom_title IS NOT NULL
      AND trim(both from sm.custom_title) <> ''
    ORDER BY sm.session_id, sm.member_id ASC
) sub
WHERE s.session_id = sub.session_id
  AND s.session_type = 2
  AND s.is_deleted = false;

UPDATE session_members sm
SET custom_title = '',
    state_version = sm.state_version + 1
FROM sessions s
WHERE sm.session_id = s.session_id
  AND s.session_type = 2
  AND s.is_deleted = false
  AND sm.member_type = 1
  AND sm.custom_title IS NOT NULL
  AND trim(both from sm.custom_title) <> '';
