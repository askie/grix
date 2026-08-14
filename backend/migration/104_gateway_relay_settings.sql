-- 104_gateway_relay_settings.sql
-- "Grix中转"的用户级模型设置：默认(兜底)模型 + 模型映射表。
--
-- 为什么是用户级(按钱包)而不是按Agent：connector 的 MITM 代理是机器级共享的，
-- per-Agent 的模型设置在"同机多Agent"场景下物理上做不到，做了展示出来也是假的。
--
-- 网关每次请求按这张表解析模型：映射命中→用映射值；未命中但请求的模型本身后端就支持→用它；
-- 都不是→用 default_model 兜底。兜底保证 Claude/Codex 发布任何新模型都不会打挂链路。
CREATE TABLE IF NOT EXISTS gateway_relay_settings (
    wallet_id     BIGINT       PRIMARY KEY REFERENCES gateway_wallets(id),
    -- 兜底模型：所有没被映射命中、且本身不是后端支持模型的请求都落到它。必填。
    default_model VARCHAR(64)  NOT NULL,
    -- 模型映射表：{"客户端侧模型名": "后端支持的模型名"}。
    -- key 由用户自定义(Claude/Codex 的任意模型名)，value 写入时校验必须在当前价目表里有基准价。
    model_map     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);
