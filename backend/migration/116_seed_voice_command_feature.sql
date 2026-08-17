-- 语音对话（App 内按住说话 → 文本 turn → TTS 播报）：默认关闭。
-- 两区独立数据库分别配置：国内区与全球区都先 seed 为 disabled，
-- 再由 aibot admin /admin/feature-gates 分别启用或配置白名单。
INSERT INTO feature_gates (key, display_name, status)
VALUES ('voice_command', '语音对话', 'disabled')
ON CONFLICT (key) DO NOTHING;
