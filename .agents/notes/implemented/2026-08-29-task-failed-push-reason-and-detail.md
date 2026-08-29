# task_failed 推送：判定用 code，展示用 detail

## Context

`AgentNotificationEvent.Summary` 同时承担两个互斥角色：

- 判定输入。`userInitiatedStopReasons`、`suppressedFailureNotifyReasons`、
  `deferredCleanupNotifyReasons`（2026-07-10 生产误报审计的治理手段）以及
  `pushTitle` 的 unknown 分支，全部是对协议 code 做 map 查表。
- 展示输入。`pushBody` 需要一句能告诉用户"为什么失败"的话。

连接器绝大多数终态结果只带 `msg` 不带 `code`（`bridge.ts` 里 20 多处
`sendEventResultWithCleanup` 调用），而 `terminalRunFromRecord` 把
`firstNonEmpty(code, msg, 默认码)` 合成一个字符串同时喂给两边，于是：

- 自由文本匹配不上任何 map，用户按停止（`("canceled", msg="canceled")`，无 code）
  仍收到"任务意外停止"；code-less 失败绕过 30 分钟陈旧窗口，ws 重启批量清理旧
  pending 事件时每条都推。
- pending 路径相反：`status.Code` 已被填成默认码，`firstNonEmpty(Code, Msg)`
  永远取到 code，连接器的原始 `msg` 被丢弃，推送只剩"消息处理出错"。

## Decision

把两个角色拆成两个字段。

- `Summary` 只放机器 code。终态路径新增 `terminalNotifyReason(payload)`，只看
  `payload.Code`，为空时按 status 回落到 `canceled` / `processing_failed` 码；
  所有判定和 `pushTitle` 走它。
- 新增可选 `Detail`（`json:"detail,omitempty"`）只放 agent 的自由文本，
  终态路径传 `payload.Msg`，pending 路径经 `MarkRunFailedNotify` 传原始
  `payload.Msg`（不是已被占位符覆盖的 `status.Msg`）。
- `pushBody` 的 task_failed 优先级：unknown 专用文案 → `Detail` → `Summary`
  的本地化映射 → 通用兜底。`Detail` 排在映射之前，与会话内失败提示
  （`buildAgentDeliveryFailureMessageContent`）一致：code 多半是后端补的通用兜
  底码，其映射文案等于没说；代价是 `Detail` 是 agent 发什么语言就显示什么语言，
  而用户在会话内看到的本来就是同一段文本。
- 存储侧完全不动。`run.StopReason`、`agent_output_status.stop_reason`、
  `chat_states.stop_reason` 继续保留 `firstNonEmpty(code, msg)` 的原文，审计信
  息不降级。

## Alternatives

- **只修判定，不加字段。** 判定改用 code 后，终态路径的 `Summary` 从自由文本变
  成通用码，推送反而从"有原因"退化成"消息处理出错"，等于修好误报的同时制造了
  信息缺失。
- **`Detail` 只在没有映射时兜底（映射优先）。** 保住了文案的本地化质量，但
  code-less 失败会被回落成 `processing_failed`——一个有映射的码——于是 `Detail`
  永远轮不到渲染，正好挡住最需要它的那条路径。
- **用 `MarkRunFailedNotify` 的 `notifyReason` 承载文本。** 一个字段仍然要同时
  服务判定和展示，只是把冲突挪个位置。
- **让连接器给每处结果补 code。** 需要跨仓库改 20 多处并等发布，且第三方 agent
  不受控；后端仍必须处理无 code 的输入。

## Consequences

- 协议是加法式变更，`omitempty`。滚动发布两个方向都安全：老 ws 发的载荷没有
  `detail`，新 push 服务回落到从 `Summary` 里读自由文本（该分支保留）；新 ws 发
  的 `detail` 被老 push 服务忽略，行为回到修复前。JetStream 重投的旧消息同理。
- 失败推送量会合理下降：用户主动停止不再推，ws 重启批量清理不再逐条推。这是
  2026-07-10 治理本来就该产生的效果，不要误判为推送链路故障。
- `pushTitle` 的 unknown 分支现在能稳定命中 ack_timeout 码（此前自由文本永远匹
  配不上，一律显示"任务失败"）。
- 前端不受影响：`stop_reason` 的唯一消费点
  （`im_service_agent_state.dart`）只存不显示。

## Verification

- `backend/internal/ws/agentapi/terminal_notify_reason_test.go`：
  `terminalNotifyReason` 的取值表，以及两条回归锁——code-less cancel 必须命中
  `userInitiatedStopReasons`，code-less 失败必须落进 `deferredCleanupNotifyReasons`。
- `backend/internal/notification/i18n_test.go`：`Detail` 与映射的优先级、机器码
  形态的 `Detail` 被拦截、unknown 文案高于 `Detail`、`Summary` 里的自由文本仍能
  渲染（滚动发布兼容）、全语言 title != body。
