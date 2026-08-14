-- 语音大脑（owner 主动呼出的语音通道）
-- 与 voice_auto_delegate_agent_id（widget 访客来电自动接单）完全分开，各存各的。
-- voice_brain_agent_id 指向一个 type=4 语音大模型，作为"我↔文字 agent 私聊"的语音外呼通道。
ALTER TABLE user_settings
    ADD COLUMN IF NOT EXISTS voice_brain_agent_id BIGINT;

-- 新增独立功能开关：语音大脑（白名单控制设置项与六宫格按钮展示）。
-- 默认 disabled，由塘主后台 /admin/feature-gates 单独配白名单。
INSERT INTO feature_gates (key, display_name, status) VALUES
    ('voice_brain', '语音大脑', 'disabled')
ON CONFLICT (key) DO NOTHING;
