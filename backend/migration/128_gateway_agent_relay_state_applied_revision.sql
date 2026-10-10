-- 128_gateway_agent_relay_state_applied_revision.sql
-- applied_at 区分不了"回执确认"与"失败/超时/断连清理"（三者都写 applied=false +
-- 时间戳），关闭方向的"待生效"提示因此误判。补 applied_revision：记录 applied
-- 值所对应的 desired revision——只有回执（revision 门槛在条件 UPDATE 里）才推进它，
-- desired 写入与断连清理都清 0。关闭已确认 ⇔ applied_revision >= revision。
ALTER TABLE gateway_agent_relay_state
    ADD COLUMN IF NOT EXISTS applied_revision BIGINT NOT NULL DEFAULT 0;
