# claude/codex 中转缺 model 时回填兜底模型

## Context

claude/codex 的中转期望态 model 历来可空（MITM 时代口径：空 = 走网关映射兜底），
`GatewaySetAgentRelay` 的必填校验和 `GatewayRelayStateSync` 的缺 model 回填都只覆盖
原生配置类型。但 direct_relay capability 的 `supported=true` 要求 relay model 非空且
厂商在原生协议白名单内——空 model 的 claude/codex agent 永远拿到 `supported=false`，
connector 按设计回退 MITM，direct-first 对这两类形同虚设。

## Decision

缺 model 的回填口径从原生类型扩展到 claude/codex，三个落点共用同一 helper
（`backfillRelayModelForAgent`：最新活跃 Key 的 relay_model → 钱包 default_model）：

1. sync 建行分支：首报 enabled 且缺 model 时回填（与原生类型一致）。
2. sync 存量行修复：行已存在、enabled 且 model 为空时回填并落库（乐观锁冲突放弃，
   下一轮 sync 重试），回填后 desired 与最新活跃 Key 不一致会自然触发顺带重签。
3. 签发层 `GatewayIssueAgentRelayCredential`：claude/codex 空 model 时先回填再签；
   回填值必须过可服务校验，回填不到或已下线则维持空 model 旧行为（不阻断签发，
   capability 保持 supported=false）。

`GatewaySetAgentRelay` 对 claude/codex 的 model 仍然可空（不改为必填），桌面端
既有"不选模型开中转"的流程不变，由 sync/签发路径补齐。

## Alternatives

- 把 claude/codex 加进 `gatewayNativeProviderClientTypes`（开中转强制选模型）：
  拒绝。会破坏桌面端既有流程，且 MITM 路径本身不需要 model。
- 只在签发层回填、不动 sync 期望态：存量 agent 没有触发签发的时机
  （key model 与 desired 均为空不会重签），永远停在 MITM。
- connector 侧自行猜模型：违反 D1（capability 由后端集中裁决）。

## Consequences

- 存量 enabled 且空 model 的 claude/codex agent 在 connector 下一次 sync 时
  desired model 被回填为兜底模型（default_model），并重签一把绑定该模型的虚拟 Key，
  capability supported=true（模型为 deepseek 系时），connector 自动切 direct。
- 回填后桌面端 GET /agents 会看到该 agent 带上了模型；sync 应答
  `ModelAutoFilled=true` 供桌面端提示"已自动选用默认模型"。
- default_model 不是 deepseek 系时 capability 仍 supported=false，维持 MITM，
  行为不劣化。回填值已下线（不在可服务清单）时签发维持空 model 旧行为。
- 虚拟 Key 的 relay_model 从空变为具体模型：Key 的模型绑定语义本就以此字段为准，
  空 = 未绑定是 MITM 时代遗留，绑定后期望态与计费口径更明确。

## Verification

- `TestGatewayRelayStateSync_FirstReportSeedsInitialDesired`（claude 首报回填）
- `TestGatewayRelayStateSync_ClaudeExistingRowEmptyModelBackfillHeals`（存量行修复 +
  顺带重签带 supported=true 的 Claude capability）
- `TestGatewayIssueAgentRelayCredential_ClaudeEmptyModelBackfills`（签发层回填）
- `TestGatewayIssueAgentRelayCredential_ClaudeBackfillUnservableKeepsEmpty`（回填值
  不可服务时维持空 model、不阻断签发的保守边界）
