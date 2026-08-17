# iOS 发布日志

> 按版本记录每次发布的更新内容。

## 3.2.4+856（2026-08-18）

> - feat: DeepSeek 会话新增技能开关（opt-in）
> - fix: DeepSeek 技能开关状态同步——等待技能运行时重建、丢弃过期开关、被拒开关收敛、断开时工具栏 ack 归位
> - feat: macOS 语音命令（M0）
> - fix: 补充 iOS 语音识别权限描述，满足 App Store 审核

## 3.2.4+852（2026-08-18）

> - feat: 消息列表长按「关闭消息通知」改为对端级静音，新建会话线程自动继承静音
> - fix: 对端静音后新线程保持静音；群聊/线程选择器仍为会话级静音

## 3.2.4+851（2026-08-18）

> - feat: DeepSeek 工具栏新增思维链开关控制
> - fix: 关闭会话通知时静音所有线程
> - fix: eggs 安装模式由后端能力驱动，支持 deepseek skill 安装与主 agent 孵化
> - fix: DeepSeek 安装说明改用官方 npm 包并补 pnpm
> - fix: dispatch 等待 session 绑定 60s，避免留下空会话
> - fix: Sparkle 更新时允许真正退出并钉死 macOS 签名 Team ID
> - fix(security): voicebridge/NATS 鉴权与敏感日志收敛；ACP 审批卡片按权限路由；gin 收敛可信代理边界防伪造 XFF；客户端平台低危安全项修复与后端安全加固

## 3.2.4+849（2026-08-17）

> - fix: 启动引导期避免 GetX root，修复启动稳定性
> - fix(web): 入口加载前版本化 Flutter entrypoint；Service Worker 只缓存 200 响应，修复坏缓存导致启动卡 loading

## 3.2.4+848（2026-08-17）

> - feat: DeepSeek 工具栏支持 Profile 选择与新建
> - fix: DeepSeek Profile 打磨：隐藏默认 profile 文本、可从工具栏创建、profile chip 值直显不走 badge、设置 pending 卡死增加超时自愈、修复工具栏审查隐患
> - fix: 安全修复 Top5：pay 管理面鉴权、退款 TOCTOU、widget 指纹会话、local endpoint SSRF、上传类型校验
> - fix: 支持安全微博详情深链
> - fix: voicebridge 锁 openai<3 并显式依赖 httpx，修复镜像启动崩溃
> - fix: 大量硬编码中文文案改走 i18n：工具栏弹窗、插件弹窗、chat 工具栏、文件选择器、语音卡片、流程图缩放、音视频卡片、需求图文案、本地搜索、Agent 安装页、启动失败页、桌面托盘菜单、MCP 工具描述等
> - fix: 等消息历史加载完再显示快捷绑定目录

## 3.2.4+847（2026-08-16）

> - feat: 安装指南中 DeepSeek Harness 排首位
> - fix: DeepSeek 工具栏打磨：供应商/模式/模型主 chip 展示，去掉分类标签，供应商名不进 chip，模型目录跟随供应商，隐藏锁定场景选择器，精简权限与供应商文案
> - fix: DeepSeek 设置失败态去掉文字后缀改 warning 叹号图标，去掉「待生效」徽标，preset chip 值直显
> - fix: 无 catalog 时隐藏 DeepSeek 供应商入口
> - fix: 底部栏英文 Agent 改为复数 Agents，补齐工具栏英文端翻译缺口
> - fix: 本地已保存语言与服务端不一致时反向同步 preferred_language

## 3.2.4+846（2026-08-15）

> - feat: DeepSeek 工具栏新增供应商选择，切换后刷新模型列表
> - feat: DeepSeek 工具栏新增场景选择器并持久化 set_preset
> - feat: DeepSeek 工具栏新增插件开关列表
> - feat: 新增 DeepSeek 客户端 logo 与类型元数据
> - feat: DeepSeek 工具栏标签与状态文案本地化
> - fix: DeepSeek 场景持久化到工具栏绑定
> - fix: 配额查询失败不再渲染叹号错误按钮，静默等待下次刷新
> - fix: stop_output 按钮改为纯图标
> - fix: 本地化配额余额提示
> - fix: normalize DeepSeek sessionBinding into open-session card

## 3.2.4+845（2026-08-14）

> - feat: 新增 DeepSeek Harness 模型适配器与工具栏
> - fix: agent profile 不可用时 fail auth（服务端，影响 App 使用）

## 3.2.4+844（2026-08-13）

> - fix: 连续 agent 气泡隐藏昵称
> - fix: agent 工具栏支持鼠标拖拽滚动（desktop/web）
> - fix(agentapi): no_reply 投递可靠静默（可靠性加固）

## 3.2.4+842（2026-08-13）

> - fix: 连续 agent 气泡显示时间
> - fix(agentapi): hermes 兜底审批投递失败回补卡片失败态
> - fix(agentapi): hermes 兜底审批改写为纯文本回传
> - fix(agentapi): session_send 去除 agent 成员护栏（回调一律主人身份）
> - fix(agentapi): no_reply 静默 ack 协议

## 3.2.4+841（2026-08-12）

> - fix: 线程弹层置顶后即时重排（置顶修复审查收尾：mounted 防护/重排只刷操作行/pinnedAt tiebreak）

## 3.2.4+840（2026-08-12）

> - fix: 恢复系列页/线程弹层单会话级置顶（修复置顶全量联动回归）

## 3.2.4+839（2026-08-12）

> - fix: 修复 agent 会话列表置顶显示（基于 friend 作用域状态）

## 3.2.4+837（2026-08-12）

> - fix: 虾蛋孵化弹窗支持 Cursor 类型主 Agent（移除 OpenClaw/Hermes 老旧硬编码筛选，按 API Agent + active + isMain + agentClientType 语义筛选）
> - fix(customercoach): 活跃支持会话进行中跳过主动引导

## 3.2.4+836（2026-08-12）

> - fix: 修复 agent 终态丢失导致 composing 指示器永久不消失
> - fix(frontend): 多线程未读面板对齐，避免列表抖动
> - fix(frontend): 会话列表可见变化立即刷新
> - fix(customercoach): 禁止向用户叙述推理过程
> - fix(customercoach): 预派发门控确定性，避免模型沉默卡住
> - fix(agentapi): 接受 hermes event_result canceled 状态
> - fix: record_only 镜像台账行不再阻断 pending dispatch 栅栏
> - fix(agentapi): owner 级技能同步至同 owner 的 peer，新 peer 库技能保持项目范围不可用

## 3.2.4+834（2026-08-11）

> - fix: 修复会话插入通知风暴（会话列表置顶/插入排序优化）

## 3.2.4+833（2026-08-11）

> - fix: 会话列表 CPU 优化（削减 sync/pin 写放大）
> - fix: 优化私聊打开延迟
> - fix: 优化会话列表最后消息查找
> - fix: 修复 sync cursor 实时更新持久化
> - fix: 修复 reset 重试与批量 upsert 边界场景

## 3.2.4+832（2026-08-11）

> - feat(customercoach): 主动引导支持功能门控控制（feature gate）
> - fix(customercoach): 引导流程完成后跳过主动派发
> - fix: 任务队列清空时清除 agent composing 状态
> - fix: 代理激活时丢弃过期 event_id 的工具卡片
> - fix: 加固宿主代理工具卡片抑制逻辑
> - fix(frontend): 替换已创建会话时不带过渡动画

## 3.2.4+831（2026-08-11）

> - feat(reach): 新增直达用户入口（direct user reach entrypoint）
> - fix(chat): 已运营技能优先展示
> - fix(frontend): 简化 agent 提供商选择器
> - fix(egg): 技能安装根目录对齐，技能包按项目技能安装

## 3.2.3+828（2026-08-09）

> - fix: 创建会话页 Loading 改为省略号状态展示（替代弧形 spinner）

## 3.2.3+827（2026-08-09）

> - fix: 修复创建会话页 Loading 停在四分之一弧（spinner 冻结不旋转）
> - fix: 消除私聊会话路由快照窗口期

## 3.2.3+826（2026-08-09）

> - fix(chat): 结构化 text 块拼接补换行，修复多块文本渲染成一行
> - fix(gateway): responsesbridge 多文本块合并补换行
> - fix: 私聊会话 Loading 保持旋转（ticker 不中断）

## 3.2.3+825（2026-08-09）

> - fix: 创建会话页 Loading 改用显式 ticker 旋转并去掉入场快照（修复 Loading 不旋转）
> - fix: 访客会话工具栏不显示对话审计开关
> - fix: 裁剪图片编辑器画布保持在底部工具栏之上
> - fix(toolbar): Cursor M/API 用量按钮补齐时间内环
> - fix: 缩短 agent 输入中 TTL，队列清空即清除
> - build: 启用 ccache 加速 pod 编译，并修复 Xcode 26 下 ccache 编译失败（CC 改走 wrapper 脚本）

## 3.2.3+823（2026-08-08）

> - fix: 创建会话页 Loading 改用显式 ticker 旋转并去掉入场快照（修复 Loading 不旋转）
> - fix: 访客会话工具栏不显示对话审计开关
> - fix: 裁剪图片编辑器画布保持在底部工具栏之上
> - fix(toolbar): Cursor M/API 用量按钮补齐时间内环
> - fix: 缩短 agent 输入中 TTL，队列清空即清除

## 3.2.3+822（2026-08-08）

> - fix: 创建会话期间会话仍可继续使用，不再卡在「创建中」页

## 3.2.3+821（2026-08-08）

> - fix: 创建私聊会话卡顿——先导航跳转再创建会话，避免卡在创建视图

## 3.2.3+820（2026-08-08）

> - fix: 资料页「+会话」按钮不再禁用，避免掐断红色 splash
> - fix: 会话线程弹窗加号建会话时先 loading 再跳转

## 3.2.3+819（2026-08-08）

> - fix(chat): dispatch-result 成功结果气泡着色为绿色
> - fix(cursor): API 用量为 0 时隐藏进度工具栏项

## 3.2.3+818（2026-08-08）

> - feat: [dispatch-result] 消息以着色 markdown 气泡展示
> - test: 覆盖 dispatch-result unwrap 外层 trim

## 3.2.3+817（2026-08-08）

> - fix: 固定艾特条背景改为透明
> - fix: 固定艾特唯一可访问 agent 时同步弹出群工具栏
> - fix: 缩小固定艾特 Chip 字号并明确横向滑动
> - fix(ios): 最低系统版本提升至 15.0（App Store 2027 春季新规）

## 3.2.3+816（2026-08-08）

> - feat: 群聊支持固定艾特条并在发送时自动注入
> - fix: rate-limit sheet 异步间隔后的 context 使用防护
> - fix: 技能命令弹窗去掉标题，仅保留 Tab
> - fix: get_rate_limits 工具栏项点击时上报 click 事件
> - fix: 资料页创建会话跳转卡顿 — 路由跳转与标题绑定改后台执行

## 3.2.3+815（2026-08-07）

> - feat: agent 范围标签支持本地化
> - feat: 桌面端持久化 agent toolbar 可见性
> - fix: 首次视频下载可靠性
> - fix: 前端 toolbar 状态竞态
> - fix: 预览对话框最大缩放倍率 6x→10x

## 3.2.2+811（2026-08-05）

> - fix: mermaid 预览缩放控件移至顶栏，支持单级缩放
> - fix: mermaid 视口 z 轴等比例缩放，补充缩放回归测试
> - fix: 访客封禁/关闭成功提示前置，刷新异常不再吞掉反馈
> - fix: 审计选择器显示 agent 名称

## 3.2.2+809（2026-08-04）

> - fix: 优化大聊天窗口加载流畅度

## 3.2.2+808（2026-08-04）

> - feat: PayPal 充值渠道（功能开关控制）
> - fix: 会话线程面板未读角标与服务器同步
> - fix: 持久化群头像成员缓存，避免头像串图

## 3.2.2+805（2026-08-03）

> - fix: 消息流指示器帧率 60fps → 3.3fps，降低 CPU 占用

## 3.2.2+804（2026-08-02）

> - fix: 群聊中保留 agent 接收模式显示

## 3.2.2+803（2026-08-02）

> - fix: 修复头像图片缓存 key，头像不再串图
> - fix: 优化发送消息弹窗布局

## 3.2.2+801（2026-08-02）

> - fix: 统一瞬态通知为顶部 toast 展示
> - fix: 保持 agent 消息输入框 outline 可见

## 3.2.2+800（2026-08-02）

> - feat: 桌面端中转开关真值切换为服务端（M3，配合后端 v1.2.28 + connector 3.19.0）
> - feat: 移动端「模型设置」页面（M4，默认模型 + Agent 模型设置）
> - fix: conversation-audit 发送通道改位置参数，规避 dart2wasm wasm-opt 编译失败

## 3.2.2+798（2026-08-01）

> - feat: 转发目标选择器增加 send-to-agent 动作

## 3.2.2+797（2026-07-31）

> - fix: 保持消息活动排序单调性

## 3.2.2+795（2026-07-31）

> - fix: 工具栏 action ack 后清除 loading 状态

## 3.2.2+793（2026-07-31）

> - feat: 发送审计数据到 agent 会话
> - feat: 记住 agent 消息接收人
> - feat: 工具栏显示选中 agent 头像
> - fix: agent 元数据绑定后再发送审计
> - fix: cached agent 提升稳定性（非阻塞恢复、移除冗余断言）
> - refactor: 抽取复用 agent 消息对话框

## 3.2.1+789（2026-07-29）

> - version: 升级至 3.2.1

## 3.2.0+787（2026-07-28）

> - feat: 工具栏显示 currency provider_quota 余额按钮
> - feat: 审计令牌时间线图表
> - fix: 群工具栏在 sharedAgents 加载后刷新
> - fix: shared users 可加载 agent toolbar
> - fix: 审计切换跟随 agent toolbar 可见性
> - fix: 审计输出条变绿
> - fix: 审计头部显示真实客户端标签
> - fix: 审计时长标签更清晰
> - fix: 连接器 agent 生命周期一致性加固

## 3.2.0+785（2026-07-28）

> - fix: load audit content in span details
> - fix(ci): fix 3 blocking frontend tests

## 3.2.0+782（2026-07-28）

> - feat: persist agent audit preferences (turn-level + scoped replay)
> - feat: explain audit toggle and copy audit id
> - fix(chat): cover persistent audit send paths
> - fix(chat): strip source audit on forward
> - fix: localize conversation audit detail UI
> - fix(frontend): freeze previews at whitespace boundary
> - fix(frontend): bound streaming conversation previews
> - fix: repair conversation audit timeline layout

## 3.2.0+778（2026-07-27）

> - feat: redesign conversation audit detail page (auto-load timeline, full JSON view/copy)
> - feat: move audit toggle into agent toolbar
> - feat(admin): add online users list

## 3.2.0+775（2026-07-27）

> - feat: add turn-level conversation audit
> - fix: harden conversation audit replay
> - fix(frontend): keep oversized chats responsive
> - fix(frontend): show toast without waiting for navigation frame
> - fix(frontend): re-arm composing idle timer on raw keystrokes
> - fix(frontend): end human composing after 60s input idle

## 3.2.0+772（2026-07-25）

> - fix: 接通 agent_skill_refresh_resp 即时回执路径
> - fix: 渲染 binding meta 中的 provider_quota 限额条（Pi 工具栏 5H/周限额）

## 3.2.0+769（2026-07-25）

> - feat: 技能弹窗两个 Tab 支持下拉刷新（agent_skill_refresh）
> - fix: 替换硬编码中文为i18n key + 采纳审查员建议

## 3.2.0+768（2026-07-25）

> - feat: 技能库新增「技能库」Tab，支持软链启用/卸载本机 Agent
> - feat: Agent 客户端 240x240 PNG 图标支持

## 3.2.0+766（2026-07-25）

> - feat: Agent 介绍支持全屏编辑，对齐聊天输入体验；插入联系人时显示昵称、保存为 @id

## 3.2.0+765（2026-07-25）

> - feat: Widget 嵌入脚本支持 data-locale，强制指定访客聊天界面语言
> - feat: App「网站接入」详情弹窗支持选择嵌入语言
> - fix: 西班牙语访客工具栏关闭按钮标签文案修正
> - fix: 访客聊天工具栏按钮支持国际化

## 3.2.0+764（2026-07-24）

> - fix: 访客聊天页面支持国际化
> - fix: 文本文档关闭确认改用 showAppContentDialog
> - fix: 系统状态页和代理工具栏支持国际化
> - fix: 网关中继面板（AI Model Settings）支持国际化
> - fix: 群聊新成员不可见入群前历史，消息可见性全口径叠加 joined_at 过滤

## 3.2.0+763（2026-07-24）

> - fix: 私聊直达路径不再在入队成功时误报 agent 离线
> - fix: 去掉发消息 3s 离线启发式
> - fix: Cursor 模式本地化翻译及长模型列表滚动支持
> - feat: Cursor 用量弹层 i18n（rate_limit_monthly_usage / rate_limit_api_usage）

## 3.2.0+760（2026-07-22）

> - feat: Agent 介绍限制提升至 3072 字符
> - feat: Agent 介绍中支持插入联系人或 Agent ID
> - fix: Agent 操作面板标签自适应缩放防换行

## 3.2.0+759（2026-07-22）

> - feat(frontend): 移动端文本文件预览和编辑
> - feat(frontend): 预览 Tailnet 文本文件

## 3.2.0+758（2026-07-22）

> - fix(frontend): 队列拖拽高亮互斥，避免多任务同时高亮
> - fix: 调整 Agent 快捷设置引导排序

## 3.1.10+757（2026-07-22）

> - fix(chat): 队列拖拽优化——反馈行偏移避免右手遮挡；整行长按拖动代替独立手柄；落点稳定高亮；边缘/间隙排序与中心合并；修复真机边缘→中心合并不触发
> - fix(frontend): 改进队列拖拽排序与合并
> - fix: 移除队列拖拽 Tooltip，增加长按手柄回归测试

## 3.1.8+754（2026-07-22）

> - fix(chat): 队列整行拖拽修复——反馈行位置避免右手遮挡；落目标任务稳定高亮；修复真机连续从边缘进入目标中心时合并不触发；更新中英文提示；移除独立手柄改长按整行拖动

## 3.1.8+752（2026-07-22）

> - fix(chat): 移除独立拖动手柄，改为长按任务整行拖动；拖动反馈显示完整任务行；落到目标中心合并、边缘/间隙排序；修复真机连续从边缘进入目标中心时合并不触发；更新中英文提示

## 3.1.8+751（2026-07-22）

> - fix(chat): 移除队列拖拽手柄 Tooltip；增加长按手柄回归测试（静态检查通过）

## 3.1.8+750（2026-07-21）

> - feat(chat): 队列弹窗统一拖动手势，落点区分排序与合并（审查通过）

## 3.1.7+748（2026-07-21）

> - fix(chat): 队列弹窗按钮对齐并恢复手柄拖动排序（审查通过）

## 3.1.7+747（2026-07-20）

> - 同 3.1.7+743 代码，重新构建上传

## 3.1.7+743（2026-07-20）

> - feat(frontend): AI 空态精简为极速接入入口，消息列表补齐同款入口
> - feat(frontend): 队列任务编辑+暂停 UI（event_hold / queue_edit）
> - feat(frontend): 会话列表新增"草稿"标记
> - feat(frontend): Agent 极速接入向导——一问建号、贴任务自动检测、上线即聊
> - fix(frontend): 按审查意见修正宽度断言、清理未用参数
> - fix(frontend): 老服务端降级 toast 独立文案（审查建议项）
> - fix(frontend): 极速接入向导落实审查建议两处
> - fix(frontend): 会话摘要API路径下@提及高亮已读后永久残留

## 3.1.7+742（2026-07-19）

> - feat(chat): 工具栏技能弹窗一键上传按钮

## 3.1.7+741（2026-07-19）

> - feat(frontend): 自定义技能多机同步（grix-hermes 1.8.5）
> - 同 3.1.7+740 代码，重新构建上传

## 3.1.7+740（2026-07-18）

> - feat(frontend): 设置页接入技能库入口——打开管理弹层，整条对话+界面管理链路可用
> - feat(frontend): 自定义技能库管理界面——SkillLibraryService(REST)+管理弹层(列表/新建/编辑/删除)+11语i18n+service测试
> - fix(skill): 技能同步审查修复——唯一冲突识别pg原生错误+同名遮蔽去重+内置只读UI+e2e补齐+文档对齐
> - fix(chat): 队列弹窗 running 项补灰色禁用态拖动手柄占位，对齐各行删除按钮
> - style(chat): 占位手柄注释挪至 else 分支上方（审查意见）

## 3.1.7+729（2026-07-18）

>- fix(push): iOS 图标角标虚数修复——推送角标口径对齐可见会话，回前台补 pull_sync 对账

## 3.1.7+728（2026-07-17）

>- feat(agenttoolbar): Kimi 工具栏用量条移到工作区图标之后
>- feat(agenttoolbar): kimi 模型改为会话级可切换下拉
>- fix(frontend): 未读占位会话从落库起带对端身份，补拉续跑不等整刷
>- fix(agentslashcmd): Kimi 斜杠命令清单改为 ACP 内置命令表
>- fix(agenttoolbar): 删除 Codex 工具栏工作空间菜单中的「速率限制」选项
>- fix(agenttoolbar): kimi 模式审查修复——auto 徽标中文展示+模式 id 规范化+空交集日志
>- fix(agenttoolbar): kimi 模式下拉收口为默认/计划/自动三档并中文本地化
>- fix(frontend): 冷启动优化复审修复——单次相关子查询 + 空批停链必刷
>- fix(chat): 思考卡与工具分组卡统一撑满宽度(上限360)
>- fix(chat): 思考卡片右侧的展开/箭头贴到卡片右缘
>- fix(frontend): 删除 batchApplySessionDeltas 写不存在列 raw_unread_inc 的死代码

## 3.1.7+726（2026-07-17）

>- perf(frontend): 聊天冷启动加载优化——会话最后消息查询逐会话索引化 + 补拉期间列表刷新节流
>- fix(frontend): 删除 batchApplySessionDeltas 写不存在列 raw_unread_inc 的死代码

## 3.1.7+724（2026-07-17）

>- feat(frontend): 移动端设置页多账号切换
>- fix(im): 修复桌面端休眠唤醒后 WS 重连状态机卡死

## 3.1.7+723（2026-07-17）

>- feat(notification): agent 上线/离线推送
>- fix(前端): 会话列表未读角标与底部栏偶发不一致

## 3.1.7+717（2026-07-16）

>- feat(kimi): 服务端补齐 Kimi Code CLI agent 类型接入
>- feat(gateway): Agent 中转列表支持选择模型，启用后保存并回显
>- feat(gateway): 新增中转凭证直签API，支撑桌面端直连本地Connector
>- feat(frontend): 会话预览显示流式响应
>- fix(gateway): 换模型失败路径如实处理——回滚显示、提示中转已中断
>- fix(gateway): 中转设置改按 agent_id 对齐本机agent
>- fix(gateway): 中转Key下发失败后可补发重试
>- fix(gateway): 中转设置tab首次渲染错位
>- fix(session-list): 会话列表时间对齐「最后一条可见消息」
>- fix(chat): 粘贴附件失败不再吞掉文本粘贴回退
>- fix(auth): 服务端故障不再伪装成凭证失效
>- fix(frontend): refresh gateway settings data on open

## 3.1.7+702（2026-07-14）

>- fix(gateway): 中转设置Agent列表——过滤已删除agent、收窄到本机
>- fix(connector): 升级按钮改为下发指令给 connector，由其等空闲后自升
>- fix(connector): 可用版本改问 connector，与灰度规则同源
>- fix(connector): 补掉升级链路上六个会退化成"点了没反应"的失败面
>- fix(connector): 测试不再依赖本机 connector；查版本合流；标注 available:false 的二义性
>- style(connector): 升级结果提示改用项目统一的 CustomToast

## 3.1.6+699（2026-07-14）

>- fix(frontend): 会话摘要不再被卡片消息冲成"..."——纯卡片消息不改变摘要，继续显示上一条可读文本
>- fix(frontend): 只有撤回才允许清空会话摘要，编辑等刷新保留已有摘要
>- fix(frontend): 会话快照摘要恢复无条件落库，pull_sync 撤回补摘要刷新

## 3.1.6+698（2026-07-13）

>- fix(ai): Agent 接入页——切类型任务不跟着变 + 类型选择改底部 sheet + 去掉重复的刷新入口
>- fix(ai): 按审查整改——同款 LayoutBuilder 坑再修一处 + 切类型正文回顶 + 在线后仍能重测连接

## 3.1.6+697（2026-07-13）

>- feat: Agent 接入指南改为后端下发的可执行任务书，覆盖 15 种客户端
>- feat: 手动检查更新入口——关于页点版本号 + 桌面托盘菜单
>- fix(auth): 区号选择去掉国旗图标，港澳台统一加中国前缀
>- fix(im): 修复连接横幅上的"重试"按钮点了没反应
>- fix(gateway): 充值下单必然 401——鉴权拦截器挂错了生命周期钩子

## 3.1.4+693（2026-07-10）

>- 同 +692 代码，重新构建上传

## 3.1.4+690（2026-07-10）

- feat: 华为推送通道打通 Phase 1b（HMS SDK 接入 + token 缓存加固）
- feat: 国产厂商推送通道直连 Phase 1a（华为/小米 provider + 原生通道解析器）
- feat: 桌面版多账号多开（per-profile 实例隔离）
- feat: 大模型配置改名 + 11 语言翻译
- feat: 中转站用户自助充值
- fix(desktop): 托盘退出无反应
- fix(desktop): 测试环境回落非桌面路径

## 3.1.2+686（2026-07-09）

- feat: 跨区登录/扫码识别与多语言引导提示
- feat: 登录错误状态码语义化（禁用 403/锁定 429/不可用 503）
- feat: agent 连接安全页
- feat: 收藏头像组件
- fix: macOS 本地库路径锚定+图标规范边距

## 3.1.2+684（2026-07-08）

- fix: 会话摘要排除工具卡片消息(grix://card)
- fix(markdown): 修复链接鼠标悬停光标不变手型的问题

## 3.1.2+683（2026-07-08）

- feat(remote-file-picker): 流量目录列表显示最后更新时间
- 同 +682 代码，修复 timeout 后重新构建上传

## 3.1.1+673（2026-07-07）

- feat(agent): 前端连接安全页——登录历史+IP黑名单

## 3.1.0+668（2026-07-04）

- feat(widget,voice): widget访客欢迎语与语音开场白多语言化，语言与前端i18n对齐
- feat(chat): 提问卡倒计时展示+超时禁提交
- feat(chat): 三点菜单/长按消息菜单/会话列表长按菜单防重复点击
- fix(chat): 补齐切模型/审批/问答/配对/上下文压缩/用量查询卡片文案国际化
- fix(chat): 绑定卡/会话控制卡文案本地化，补齐 11 语言翻译
- fix(chat): 视频预览播放条隐藏时点击画面只唤出播放条不暂停播放
- fix(chat): 图片预览弹窗豁免标记移到 showDialog 同行，修复弹窗守卫测试
- fix(chat): 会话状态卡不再一刀切隐藏成功详情，恢复 where/status 查询详情展示
- fix(chat): 快捷绑定组件加揭示延迟，消除进会话时闪现
- fix(chat): 目录绑定消息与状态卡瘦身

## 3.1.0+665（2026-07-03）

- feat(chat): 空白agent聊天页快捷绑定目录组件
- feat(widget-sites): 主题色改预设色板点选，不再手填色值
- fix(chat): 快捷绑定目录缓存按机器隔离，跨agent补位仅限同机器
- fix(chat): 输入框附件八宫格弹层间距收紧
- fix(chat): 附件八宫格标签关闭系统字体缩放防溢出

## 3.1.0+664（2026-07-03）

- feat(chat): 输入框全屏编辑器（长文本展开按钮、全屏编辑、与小输入框实时同步、@提及支持、直接发送）
- i18n: 11 语言补 3 键全屏编辑器相关文案

## 3.1.0+663（2026-07-03）

- fix: 电脑端接管后外放没声修复（call_controller 回写 isSpeakerOn）
- fix: 视频预览顶栏下载/关闭按钮加半透明圆形底

## 3.1.0+659（2026-07-03）

- fix: 资料页简介只展示首个空行前内容且限两行
- feat: 孵化选Agent底部sheet补审查建议
- feat: 虾蛋孵化选Agent改为底部sheet弹窗

## 3.0.0+656（2026-07-02）

- fix(chat): 隐藏消息回复的锁标记流式期即渲染且不被后续覆盖冲掉

## 3.0.0+655（2026-07-02）

> - refactor: 简化虾蛋孵化弹窗流程，自动从 agent 类型识别路径

## 3.0.0+654（2026-07-02）

> - feat: add media.upload scope gating agent file upload presign

## 3.0.0+653（2026-07-01）

> - fix: 工具栏消息队列改倒序展示（最新消息置顶，正在执行的 running 置底）

## 3.0.0+651（2026-07-01）

> - feat: 升主版本 2.11.0→3.0.0
> - feat: 私聊三点菜单新增"转换为群聊"功能，二次确认后同一会话无缝切换为群聊
> - fix: Agent 回复模式"有问必答"(ModeAll)从"仅@触发"分支独立展示，文案覆盖11国语言

## 2.11.0+650（2026-07-01）

## 2.10.0+648（2026-06-30）

> - feat: 聊天页右上角三点菜单新增收藏/取消收藏入口（私聊和群聊均有，位置在"重命名"之后）

## 2.10.0+647（2026-06-30）

> - fix: 会话消息列表长按菜单增加收藏/取消收藏入口
> - fix: 收藏页长按菜单增加取消收藏功能
> - fix: 收藏/取消收藏后主列表书签图标实时同步

## 2.10.0+646（2026-06-30）

> - feat: 会话收藏功能（收藏/取消收藏、收藏列表、书签入口）
> - i18n: 补充11个语言的收藏功能文案

## 2.10.0+645（2026-06-30）

> - fix: 通话浮层冷启动时订阅不上通话状态，拨号弹窗不显示
> - fix: 安卓12+启动屏统一为白底红字Grix，去掉红色图标方块
> - fix: 补回 dio 5.10.0 的 transformTimeout 分支
> - fix: 修复网页拖链接/文字误触上传覆盖层且松手卡住
> - fix: 网页版选文件/粘贴文件都能进待发送池预览

## 2.10.0+642（2026-06-30）

> - fix: 图片预览单击关闭；视频弹窗下载/关闭按钮左右对调
> - fix: 修复安卓12+冷启动启动屏logo被拉伸成窄条

## 2.10.0+641（2026-06-29）

> - fix: 黑名单链接点击改为静默不响应，不再弹拦截中间页；可疑链接提示弹窗保留

## 2.10.0+640（2026-06-29）

> - fix: 聊天消息链接点击收口到黑名单校验入口（修复消息里点链接绕过黑名单的漏洞）

## 2.10.0+639（2026-06-29）

> - fix: 修复聊天里多个流程图混排时后面的流程图渲染不出来的问题

## 2.10.0+638（2026-06-29）

> - fix: 修复聊天里 mermaid 流程图分组框重叠/塌缩的渲染问题

## 2.10.0+637（2026-06-29）

> - fix: 修复 Mermaid 流程图边连接 subgraph 分组框时布局塌缩

## 2.10.0+636（2026-06-29）

> - fix: 绑定手机号接口补带登录态 Authorization，修复「missing or invalid authorization header」报错

## 2.10.0+635（2026-06-29）

> - fix: 用户资料页用户ID复制改为点击触发

## 2.10.0+631（2026-06-28）

> 后端：fix Apple 登录 RS256 签名算法，fix phone_e164 空字符串索引。前端同 +630 代码，重新构建上传。

## 2.10.0+630（2026-06-28）

> 回滚两阶段启动等改动，重新构建上传（疑为编译缓存脏导致白屏，非代码问题）
> - 恢复两阶段启动——冷启动秒开首页
> - fix: 迁移 iOS UIScene lifecycle，修复 iOS 26 白屏
> - fix: Apple Sign-In delegate 被 ARC 释放导致回调丢失
> - fix: OAuth登录超时计时范围修正
> - fix: 冷启动头像不更新
> - fix: 分组弹窗线程列表改用本地数据
> - perf: 全平台 release 构建启用 --split-debug-info + --obfuscate

## 2.10.0+622（2026-06-28）

> 同 +621 代码，重新构建上传

## 2.10.0+621（2026-06-28）

> - fix: 迁移 iOS UIScene lifecycle，修复 iOS 26 白屏

## 2.10.0+619（2026-06-28）

> - fix: Apple Sign-In delegate 被 ARC 释放导致回调丢失

## 2.10.0+615（2026-06-28）

> - fix: OAuth 登录（Apple/Google）超时计时范围修正，SDK 授权不再占用网络请求超时

## 2.10.0+613（2026-06-28）

> - fix: 分组弹窗线程列表改用本地数据，置顶口径与资料页一致

## 2.10.0+609（2026-06-28）

>- fix: 冷启动后头像不更新——AgentService/SessionService/FeatureFlagService 移回关键初始化阶段

## 2.10.0+608（2026-06-28）

>- perf: 全平台构建启用 --split-debug-info + --obfuscate，IPA 47.8MB
>- feat: 两阶段启动——冷启动秒开首页

## 2.10.0+607（2026-06-28）

- fix: 切号后离线消息错投/点开空白会话——推送带 recipient_id + 登录失效他账号设备绑定
- perf: 统一新建私聊入口,去掉资料页跳转前的阻塞核对

## 2.10.0+606（2026-06-27）

### Apple & Google Sign-In 全端上线
- feat(ios): Debug/Profile/Release.xcconfig 填入 GIDClientID/GIDServerClientID
- feat(web): Makefile 注入 GOOGLE_WEB_CLIENT_ID
- feat(android): CI(release-public.yml) 注入 AIBOT_ANDROID_GOOGLE_WEB_CLIENT_ID
- released: Web TKE / iOS 2.10.0+606 ASC / Android APK 2.10.0+605

## 2.10.0+603（2026-06-26）

>- 回滚 Composing 指示器移出消息流的改动，重新构建上传

## 2.10.0+602（2026-06-26）

>- 同 +599 代码，重新构建上传

## 2.10.0+601（2026-06-26）

>- 同 +599 代码，重新构建上传

## 2.10.0+599（2026-06-26）

- fix: Web端 Agent Composing 状态不显示
- fix: 消除空消息列表时 composing 指示器重复显示

## 2.10.0+598（2026-06-26）

>- 同 +597 代码，重新构建上传

## 2.10.0+597（2026-06-26）

- feat(flowchart): 去掉头部缩放 +/- 按钮，下载改为小眼睛预览，点击流程图全屏查看

## 2.10.0+596（2026-06-26）

- perf: 会话列表预览加缓存+超大输入截断，消除返回列表主线程卡死(ANR)

## 2.10.0+595（2026-06-26）

### Bug 修复
- fix: 流式消息透传 quoted_message_id，agent 回复的引用在流式期可见

## 2.10.0+594（2026-06-26）

### Bug 修复
- fix: 私聊引用自己消息时引用预览不显示

## 2.10.0+593（2026-06-26）

### Bug 修复
- fix: 移除消息分组长按菜单中的"删除全部会话"选项

## 2.10.0+592（2026-06-25）

- feat: 工具栏技能弹窗支持搜索+最近使用置顶

## 2.9.0+581（2026-06-24）

> 同 +580 代码，重新构建上传

## 2.9.0+580（2026-06-24）

### Agent 共享及稳定性
- fix: auth_ack 与 manager.register 顺序 race
- fix: 共享 agent 进入托管选择器与气泡昵称
- i18n: 共享 agent 弹窗及被共享提示走 i18n
- test: 补共享 agent 进入托管选择器的守卫/单元测试
- test: 修 CI 8 个失效测试

## 2.9.0+579（2026-06-24）

### 文件选择器优化
- feat: 底部"已选 N 个"并入清除按钮，显示"清除(N)"
- chore: 删除已弃用的 i18n 键 remote_file_picker_selected_count

## 2.9.0+578（2026-06-24）

### Markdown 表格修复
- fix: 表格单元格内时间冒号不再被切成有序列表
- fix: 表格行整行 bypass inline 有序列表切分（普适收口）
- fix: 表格 cell 切分识别反引号 inline code 与 `$...$` 行内数学保护区
- fix: GFM §4.10 合规：单 dash delimiter + 短对齐符 `:--`/`--:`/`:-:` 识别

## 2.9.0+576（2026-06-23）

- feat(remote-file-picker): 上传按钮支持选择来源(相册/浏览文件)
- fix(agent-share): 加共享对象存在性校验 + 主人/被共享者两端边界提示
- fix(agent-share): 对抗审查二轮 A+B+C 档 owner 隔离修复 + 全套守卫测试
- test: 对抗审查二轮 C 档 3 项 + 全套守卫测试(14 个)

## 2.9.0+573（2026-06-23）

- fix: 会话列表预览把 @用户ID 替换成 @显示名
- fix: 工具消息让会话上浮但不动 preview

## 2.9.0+572（2026-06-23）

- fix: 置顶分组会话排序口径统一，消除动画反复横跳

## 2.9.0+571（2026-06-23）

### Agent 共享一期
- feat: 前端共享管理 + 分享给我的列表（一期第6步）
- feat: 共享按钮接入 feature gate（默认关闭）
- fix: 对抗审查 - 被共享agent禁止拖动排序

### 语音通话
- feat: 语音通话采集健壮性修复 + 完善诊断上报
- fix: 语音桥从房间消失时15s兜底结束通话
- fix: P1 拆房时清除待应用参与档

### 其他修复
- fix: 会话列表实时重排节流+去抖，消除轮流跳动
- fix: 空文件上传给明确提示
- feat: 资料页滚动到消息列表时显示对端昵称

## 2.9.0+570（2026-06-23）

- fix: 未读角标自适应定位，中心锚定头像右上角

## 2.9.0+582（2026-06-24）

- feat: 工具栏 5H/7D 用量圆环加入绿色时间进度内环
- fix: 新建语音 agent 时移除写死的 provider/model 默认值

## 2.9.0+583（2026-06-24）

- fix: tailnet 自签 HTTPS 视频/音频走 loopback 反代播放

## 2.10.0+584（2026-06-24）

### 视频播放
- fix: 视频预览进度条松手即播 + 拖动命中区域加大到 28px

### 链接安全防护
- feat: 链接安全防护 P0 打开时拦截 + 塘主黑名单管理
- fix: P0 后审 5 项高优修复 + 三轮审查 6 项
- fix: P1 中优收尾 7 项
- feat: P2 规模化优化（命中聚合 + CSV 导入 + 拦截统计）

### 手机号短信登录
- feat: 前端手机号短信登录注册 + 老用户绑定引导（PR3）

## 2.10.0+587（2026-06-25）

### 手机号短信登录
- feat: /v1/auth/methods 匿名能力开关 + C 端入口联动塘主开关
- fix: 手机号短信登录注册三处跟进缺失

## 2.10.0+589（2026-06-25）

### tailnet 代理稳定性
- fix: 消除 proxy verify 和 ensureBase 竞态
- fix: tailnet loopback proxy 应用生命周期自愈
- fix: 切换播放速度时重触发播放

## 2.10.0+590（2026-06-25）

- fix: 绑定手机号提示前检查 phoneLoginEnabled 开关

## 2.10.0+591（2026-06-25）

### 消息搜索性能优化
- feat: 消息列表搜索改用本地数据库查询替代内存过滤
- test: 补会话列表搜索改 DB 查询的守卫测试
