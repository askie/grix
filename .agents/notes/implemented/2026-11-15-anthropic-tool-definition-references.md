# DeepSeek Anthropic 历史内联工具定义转换为引用

## Context

Claude Code 在 resume 长会话时会重放 `tool_addition.tool_definition`，内联携带工具完整 schema。DeepSeek 的 Anthropic 兼容端点拒绝这种历史块，要求带 `name` 的 `tool_reference`，导致 422；新建会话没有这段历史，因此不受影响。Claude 2.1.321 与 2.1.325 发出的结构相同，回退 CLI 无效。

## Decision

仅在 DeepSeek 的 Anthropic 适配器启用兼容转换，同时覆盖 messages 与 count_tokens：

- 将内联 `definition` 按工具名去重后加入顶层 `tools`，并设置 `defer_loading:true`，避免新增工具从会话开始即可见；已有同名顶层工具保持不变，新增定义以最后一次更新为准。
- 将原历史块替换为 `{"type":"tool_reference","name":<name>}`，其余消息内容不变。
- 原请求必须已有至少一个非延迟工具，否则不转换，避免制造 DeepSeek 的 all-deferred 400。
- 不含该结构的请求保持原样；其他 Anthropic 上游不默认启用。
- 不修改 Claude transcript。

## Alternatives

- 回退 Claude CLI：两个版本请求一致，不能解决。
- 清理 transcript：CLI 会重建这些块，且容易破坏会话历史，不采用。
- 直接删除历史工具块：请求可恢复，但会丢失延迟工具能力，优先保留带 defer_loading 的定义。
- 使用 `tool_name`：DeepSeek 实测返回 422 `missing field name`，协议字段必须是 `name`。

## Consequences

这是有损兼容，不是纯透传：提升定义会改变 tools 前缀及 prompt cache，同名历史 schema 只能保留最新版本。保留的是后续工具调用能力，不保证历史每个时点的 schema 完全语义等价。该变化位于网关上游请求适配层，不修改 Grix 会话协议、connector 配置或 Hermes 消息语义。未来接入 Anthropic 官方端点时不得默认开启此转换。

## Verification

- `go test ./internal/gateway/upstream ./internal/gateway/httpapi`
- 用真实老会话生成的 2 MB 级请求通过 `AnthropicAdapter` 发往本机假上游：共 2678 条 messages，`messages[1700]` 的内联定义改为引用，顶层 tools 包含 Artifact 与 Workflow。
- 用虚拟 Key 向真实 DeepSeek 中转发送最小请求：`name + defer_loading:true` 返回 200；`tool_name` 返回 422 `missing field name`。
- 未部署，未将完整老会话请求发送到真实上游。
