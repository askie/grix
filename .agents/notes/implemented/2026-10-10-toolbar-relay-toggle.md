# 工具栏"中转开关"两态按钮

- 状态：implemented
- 日期：2026-10-10

## Context

"Grix中转"开关原本只在用户设置页（REST `POST /v1/gateway/agents/:id/relay`）。要在聊天输入框上方的 Agent 工具栏直接放两态按钮（中转/官方），同时保证：多设备同时点击不会把状态反向翻转、共享使用者不能动主人的中转配置、余额归零后用户仍能关闭中转。

## Decision

- 按钮由 core 统一注入（`agenttoolbar/core/relay_toggle.go`），不分散到 17 个 agent 包；前端零改动（通用渲染已有 button/selected/badge 契约），只加了 `swap` 图标映射。
- 按钮 ActionID 版本化：`gateway_relay_toggle:<enable|disable>:<relay revision>`。core 动作校验要求 ActionID 与当前最新快照完全相等，旧快照点击拒绝（`relay_state_stale`）并强推新快照；revision 透传为 `expectedRevision` 走 state 表乐观锁。两层防护，绝不对最新状态取反。
- 仅 agent 主人可见可操作（resolver 判定 requester==owner；写路径 `GatewaySetAgentRelay` 归属复核不变）。
- 显隐口径：余额 > 0 或已开启时显示。余额不足只限制开启，不限制关闭。这只是 UI 策略；真实扣费放行仍由 wallet PreflightCheck 控制。
- 模型语义：工具栏开启恒传空模型，投影的 RelayModel 只作展示（BadgeText 预览）——若当显式值写入，会把 MITM"空模型=走映射/兜底"固化成具体模型，并让原生类型的回填值绕过 need_model 引导。开启时原生配置类型空模型按"state 已选 → 最新活跃 Key 快照 → 钱包兜底"回填，回填值不可用报 need_model；MITM 类型空模型保持"走映射/兜底"旧语义。关闭不校验模型可用性，且空模型走原子关闭（`SetGatewayAgentRelayStateDisabled` 单语句 UPDATE 不动 relay_model），避免"读旧模型再写回"覆盖并发新选择。
- 投影查询错误（Key 列表/中转设置/系统默认模型）传播为 error 而不是折叠成兜底值：故障≠没有值，调用方收到 error 时不注入按钮。系统默认模型用 `relay.SystemDefaultModelStrict`——计费主链路的宽松版（故障折叠为内置常量）保持不变。
- 状态展示：Selected 只反映 desired；tooltip 用 applied/applied_revision/state_known 在两个方向都表达"待生效/未知"。"当前 desired 已获回执确认"以 `applied_revision >= revision` 判定（migration 128 新增列）——applied_at 不可信：失败/超时/断连清理都写时间戳。离线可保存 desired 等上线 sync。
- 快照刷新统一在 `GatewaySetAgentRelay` 写成功后经 AgentToolbarRefresher 触发；`RefreshByAgent` 改为逐会话 `RefreshSessionForAgent`（群聊 resolver 必须带目标 agent）。
- 存量竞态修复：actual 回执的 revision 新鲜度判定并入条件 UPDATE（`SetGatewayAgentRelayStateAppliedIfRevision`），消除"旧回执通过检查后污染新 desired"的交错。

## Alternatives

- 前端本地按钮 + REST（仿对话审计开关）：状态同步与权限判断散到前端，与 desired 服务端权威相悖，未选。
- 各 agent 包自行注入：钱包/权限/名单/文案逻辑复制 17 份，未选。
- 两选项 select 代替两态 button：多一次点击，留作将来"中转+临时换模型"的扩展位。

## Consequences

- 工具栏每次构建对主人+支持类型的 agent 多一次钱包/state 只读查询（含 Redis 能力探测）；查询失败只是不出现按钮，不影响工具栏其它项。
- 设置页"关闭"不再清空 relay_model（改为保留），重开沿用原模型。
- flag `gateway.relay_state_enabled` 关闭时按钮完全不出现，行为与现状一致。

## Verification

- `internal/agenttoolbar/core/relay_toggle_test.go`：注入显隐规则、两态渲染、陈旧点击拒绝+强推、core 拦截不派发给 agent 包。
- `internal/api/service/gateway_relay_toolbar_test.go`：只读投影不落库、主人限定、模型兜底链、模型下架后仍可关闭并保留模型。
- `internal/store/gateway_agent_relay_state_store_test.go`：条件 UPDATE 拒绝迟到的旧回执。
