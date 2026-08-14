-- gateway_virtual_keys 增加 agent_id 列。
-- "Grix中转"C端自助功能里，每个托管Agent配一把专属虚拟Key（虚拟Key明文只在创建那一刻能拿到，
-- 之后系统只存哈希，靠这个字段判断某个Agent是否已经配过Key，避免每次调用都重新发一把造成Key
-- 泛滥）。0=未关联具体Agent的通用Key（如管理员在塘主后台代发的那种），兼容现状。
-- 幂等：IF NOT EXISTS，与 model 定义保持一致（默认0，非空）。

ALTER TABLE gateway_virtual_keys
    ADD COLUMN IF NOT EXISTS agent_id BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_gateway_virtual_keys_agent_id ON gateway_virtual_keys (agent_id);
