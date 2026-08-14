-- gateway_virtual_keys 增加 relay_model 列。
-- "Grix中转"启用时为该Agent选定的服务端模型（原生配置类型的CLI用这个名字发请求；
-- 空=未指定，走网关模型映射/兜底）。随每次签发写入、随Key生命周期存续，桌面端
-- "大模型设置"的Agent中转列表靠它回显上次选中的模型。
--
-- 补迁移背景：b6cb49cb 给 model.GatewayVirtualKey 加了 RelayModel 字段并已上线，
-- 但漏了这个文件。生产只跑本目录的 SQL 迁移，AutoMigrate 仅用于测试（见
-- store/migrate.go：MustInitSchema→ApplyMigrations，AutoMigrateWithDB 无生产调用方），
-- 于是 INSERT 带 relay_model 列 → 双区签发虚拟Key全部 42703 报错。
-- 幂等：IF NOT EXISTS，与 model 定义保持一致（varchar(128)，非空，默认空串）。

ALTER TABLE gateway_virtual_keys
    ADD COLUMN IF NOT EXISTS relay_model VARCHAR(128) NOT NULL DEFAULT '';
