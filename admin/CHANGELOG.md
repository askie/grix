# Admin iOS 发布日志

> 按版本记录每次发布的更新内容。

## 2.4.0+24（当前版本 — 首次记录）

>- 初始版本基线

## 2.4.0+25（2026-06-24）

>- feat(link-safety): 链接黑名单管理模块
>- feat(phone-sms): 短信配置管理页面
>- feat(connector): connector 控制更新（push 返回广播节点数）

## 2.4.0+26（2026-06-25）

>- fix(link-blocklist): 移动端适配 — 窄屏"新建规则"改为加号图标、工具栏紧凑布局

## 2.4.0+28（2026-06-25）

>- fix(connector): 修复 connector 页右上角三点菜单点击无效

## 2.4.0+29（2026-06-25）

>- feat(phone-sms): 塘主管理新增"短信设置"页（阿里云 + AWS SNS + 区号白名单 + 测试发送）
>- feat(user-mgmt): 用户管理增加手机号字段（列表显示 / 搜索 / 解绑）
>- feat(auth): 手机号短信登录注册三处跟进缺失

## 2.4.0+33（2026-06-26）

>- feat(connector): 版本卡片增加 Connector 升级推送按钮，修复 wsToHttp URL 拼接 bug

## 2.4.0+34（2026-06-26）

>- build: 版本号递增 +34（发布维护）

## 2.4.0+35（2026-06-27）

>- feat(settings): 语音模型清单增加预定义音色，C端音色改下拉选择+自定义

## 2.4.0+36（2026-06-28）

>- fix(connector): 修复连接器升级报告 status 字段解析错误，解决整页崩溃

## 2.4.0+37（2026-06-28）

>- feat(connector): 连接器升级统计页改为从版本记录下拉选择版本号，无需手输

## 2.4.0+38（2026-06-28）

>- feat(admin): UI 文案优化 — Connector 页标题改「插件」、升级 Tab 改「版本」、Connector 分段改「连接器」
>- feat(admin): App 统计页版本输入框改为下拉选择已有版本
>- fix(link-blocklist): 链接黑名单 severity 徽章内联、卡片全宽贴边布局
>- fix(admin): 底部菜单「链接黑名单」缩短为「链接」

## 2.4.0+40（2026-06-29）

>- fix(admin): APP 版本发布校验增强 — Appcast 通道隔离 + 版本格式校验 + 唯一性去重

## 2.4.0+41（2026-07-02）

>- feat(gateway): 大模型计费网关管理页面（钱包/对账/计价）
>- feat(gateway): 上游厂商密钥管理（塘主后台动态增删密钥）
>- fix(gateway): 审查整改 — 坏Key隔离、缺凭据日志、吊销窗口提示
>- feat(push): 离线推送通道分通道开关（iOS/FCM/WebPush/jPush）

---

## 2.4.0+42（2026-07-02）

>- 重新构建发布

## 2.4.0+43（2026-07-02）

>- fix(admin): 计费网关弹窗移动端自适应宽度，缩小两侧留白
>- fix(admin): 修改密码/链接黑名单弹窗改用统一 DialogContentBox 宽度组件
>- fix(admin): 黑名单弹窗窄屏残留溢出收尾修复
>- test(admin): 黑名单弹窗窄屏溢出三档回归测试
>- fix(admin): 黑名单/改密弹窗补 scrollable 防软键盘弹起纵向溢出

## 2.4.0+44（2026-07-07）

>- feat(admin): 用户ID统一渲染为可点击用户名片(UserRef)+详情操作卡
>- test(admin): UserRef 组件渲染 widget 测试
>- fix(admin): UserDirectory 批量失败退避自动限次重试
>- test(admin): 弹窗scrollable抗软键盘溢出回归测试

## 2.4.0+47（2026-07-09）

>- feat(admin): 虾蛋市场新增置顶/取消置顶操作，置顶虾蛋显示图钉+置顶徽标并在用户端排最前

## 2.4.0+46（2026-07-08）

>- feat(admin): 塘主后台新增数据看板(Dashboard)模块 — 首页统计聚合用户/会话/消息核心指标
>- feat(backend): 新增 /admin/api/dashboard/stats 聚合 API

## 2.4.0+50（2026-07-10）

>- feat(reach): 版本发布公告改草稿制——后台可编辑文案、手动发送
>- fix(reach): 审查P1修复——发送校验口径统一、邮件可编辑字段转义

## 2.4.0+49（2026-07-09）

>- fix(reach): 定时任务可取消+模板在用禁删+失败状态兜底+指定用户试发

## 2.4.0+52（2026-07-27）

>- feat(admin): 虚拟Key生成弹窗增加 CC Switch 配置帮助

## 2.4.0+53（2026-07-27）

>- feat(admin): 在线用户列表 — 查看当前在线用户、强制下线

## 2.4.0+54（2026-08-11）

>- feat(admin): 访客封禁管理 — 新增访客封禁列表、封禁/解封操作与导航入口

---

*注：CHANGELOG 从 2.4.0+24 开始记录。*
## 2.4.0+55（2026-08-11）

>- fix(ios): 最低系统版本 13.0 → 15.0，修复 App Store Connect ITMS-90068 警告（Podfile platform + IPHONEOS_DEPLOYMENT_TARGET）
