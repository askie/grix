# App Store 截图：Apple Watch Ultra 档（422×514）

iOS 3.2.7+919 内置的 watchOS 伴侣 App，Apple Watch Ultra 3 (49mm) 模拟器，watchOS 26.2，状态栏 21:45。用于 App Store Connect 手工上传，对应 Connect 当前必填的 Apple Watch Ultra 尺寸档，其他表款由 Connect 自动缩放。

| 文件 | 页面 |
|---|---|
| 01-inbox.png | 待处理：待审批 + 待回答的会话 |
| 02-agents.png | Agent 一览：在线/离线状态与会话数 |
| 03-quick-send.png | 快速发送：按会话列出的最近活动 |
| 04-conversation.png | 发送页：最近对话 |
| 05-sending.png | 发送页：已发送、等待新回复、朗读 |
| 06-approval.png | 处置页：批准 / 拒绝 |

说明：

- 全部为模拟器原图，PNG，无 alpha 通道，未缩放或裁剪。
- 列表内容为演示样本，不是审核账号的真实数据：手表列表只读取 agent 运行时由 ws 服务写入的 chat_states，审核账号名下 agent 全部离线，无法用真实数据填满。界面为真实运行界面。
- 未包含 Live Activity / Smart Stack 截图：无头模拟器对 watchOS 没有输入注入，需在有图形会话的 Mac 上补拍。该项在 Connect 中非必填。
- watchOS 模拟器不支持 `simctl status_bar override`，时间无法钉到 9:41，改为同一分钟内连拍保持一致。
