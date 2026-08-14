# Admin 后台 Flutter 重构方案

## 背景

原 Admin 后台是后端用 Go 模板（Tabler UI）做的服务端渲染页面，仅支持浏览器访问。本次重构将其改为 Flutter 跨平台 App，覆盖 iOS / Android / iPad / macOS / Windows / Web 六端，一套代码。

## 架构对比

| 维度 | 重构前（Go 模板 SSR） | 重构后（Flutter App） |
|------|----------------------|---------------------|
| 渲染方式 | 后端拼 HTML 返回浏览器 | 客户端原生渲染 |
| 数据交互 | 表单 POST + 页面跳转 | JSON API + Bearer Token |
| 认证方式 | Cookie + CSRF Token | Bearer Token（sessionID） |
| 支持平台 | 仅浏览器 | iOS / Android / iPad / macOS / Windows / Web |
| 离线能力 | 无 | Token 本地持久化，断网友好提示 |
| 布局适配 | 固定桌面宽屏 | 响应式：宽屏侧边栏 / 窄屏底部导航栏 |
| 技术栈 | Go + html/template + Tabler CSS + jQuery | Flutter + GetX + Dio + Material 3 |
| 状态管理 | 无（每次刷页面） | GetX 响应式 |
| 后端改动 | — | 仅新增 JSON 出口层，不改业务逻辑和数据库 |
| 原后台 | — | 保留并存，互不影响 |

## 后端改动范围

新增 `internal/admin/router_api*.go` 系列文件，挂载在 `/admin/api` 路径下：

```
router_api.go            — 注册入口 + 登录/登出/当前管理员
router_api_users.go      — 用户管理
router_api_reports.go    — 举报管理
router_api_moderation.go — 内容审查
router_api_admins.go     — 管理员管理
router_api_settings.go   — 系统设置
router_api_featuregates.go — Feature Gates
router_api_app.go        — App 版本发布
router_api_connector.go  — Connector 升级
router_api_eggs.go       — 彩蛋管理
```

设计原则：
- 每个文件仅做"JSON 序列化 + 参数校验 + 调用已有 service"，不含任何业务逻辑
- 认证复用已有的 `RequireAPIAuth()` 中间件（支持 `Authorization: Bearer <token>`）
- 统一响应信封 `{code: 0, msg: "success", data: {...}}`
- 与原 SSR 网页后台并存，注册在同一个 Gin Engine 上，互不干扰

## Flutter 前端架构

```
admin/lib/
├── main.dart                    — 入口：初始化 token / 注入 AuthService / 启动 App
├── core/
│   ├── config/app_config.dart   — 后端地址配置（支持编译时覆盖）
│   ├── network/
│   │   ├── api_client.dart      — Dio 单例：Bearer 注入 / 信封拆解 / 401 处理
│   │   ├── api_exception.dart   — 统一异常类型
│   │   └── page_result.dart     — 通用分页模型 PageResult<T>
│   └── storage/token_store.dart — Token 本地持久化
├── app/
│   ├── theme/
│   │   ├── app_palette.dart     — 集中色板（品牌色 + 中性面 + 语义色）
│   │   └── app_theme.dart       — 全局 Material 3 主题
│   └── routes/app_routes.dart   — 路由表 + 页面注册
├── shared/
│   ├── controllers/
│   │   └── paged_list_controller.dart — 列表分页基类
│   ├── navigation/
│   │   ├── nav_items.dart       — 导航菜单定义
│   │   └── auth_guard.dart      — 路由鉴权守卫
│   └── widgets/                 — 通用 UI 组件
│       ├── admin_scaffold.dart  — 响应式骨架（侧栏 / 底栏切换）
│       ├── async_view.dart      — 加载 / 错误 / 空状态
│       ├── paginator.dart       — 分页控制条
│       └── confirm_dialog.dart  — 确认弹窗 + Toast
└── modules/                     — 各业务模块（每模块独立目录）
    ├── auth/                    — 登录
    ├── users/                   — 用户管理
    ├── reports/                 — 举报管理
    ├── moderation/              — 内容审查
    ├── admins/                  — 管理员管理
    ├── settings/                — 系统设置
    ├── feature_gates/           — Feature Gates
    ├── app_releases/            — App 版本发布
    ├── connector/               — Connector 升级
    └── eggs/                    — 彩蛋管理
```

## 模块开发约定

每个业务模块遵循统一结构：

```
modules/<模块名>/
├── models.dart      — 数据模型（fromJson 工厂）
├── service.dart     — API 调用（返回 PageResult<T> 或具体模型）
├── controller.dart  — 控制器（列表类继承 PagedListController<T>）
├── binding.dart     — GetX 依赖注入
└── view.dart        — 页面 UI
```

## 响应式布局策略

- 宽度 ≥ 900px（桌面 / iPad 横屏）：左侧固定侧边栏 240px + 右侧内容区
- 宽度 < 900px（手机 / iPad 竖屏）：顶部 AppBar + 底部导航栏（4 主入口 + "更多"弹层）
- 窗口大小变化时自动切换，无需手动操作

## 设计色板

对齐 Grix 产品品牌视觉：
- 品牌主色：龙虾红 `#E63946`
- 背景面：暖色奶油底 `#FBF7EE`
- 中性文本：深暖灰 `#221C12` / `#6F6149` / `#9A8A6E`
- 语义色：成功绿 `#1F9D63` / 警告黄 `#C8881A` / 危险红 `#D64949` / 信息蓝 `#3B6FE0`
- 每个语义色配柔和底（用于状态标签），全应用统一

## 功能覆盖

| 模块 | 功能点 |
|------|--------|
| 登录 | 账号密码 → Token → 自动进入后台 |
| 用户管理 | 列表 / 搜索 / 状态筛选 / 分页 / 封号 / 解封 / 解除审查禁言 / 解除登录锁定 |
| 举报管理 | 列表（多维筛选）/ 详情（附件查看）/ 处理（驳回/不处理/重复/封禁用户/封禁群组） |
| 内容审查 | 事件列表 / 仅看禁言 / 解除禁言 / 审查设置（开关/阈值/关键词） |
| 管理员管理 | 列表 / 创建 / 启用 / 禁用 / 删除（含本人保护） |
| 系统设置 | 认证开关组 / 群组邀请阈值 / 修改本人密码（改后强制重登） |
| Feature Gates | 开关列表 / 创建 / 切换状态 / 白名单用户增删 |
| App 版本发布 | 列表 / 创建 / 发布 / 暂停 / 恢复 / 撤销 / 删除 / 灰度规则 / 下载统计 |
| Connector 升级 | 列表 / 创建 / 发布操作 / 推送升级 / 灰度规则 / 升级报告 / 统计 |
| 彩蛋管理 | 分类管理 / 彩蛋增改详情 / 上下架 / 版本管理 / 预签名上传 |

## 测试与质量

- 静态分析：`dart analyze` 零问题
- 单元测试：7 个测试覆盖分页模型、用户模型、网络层（伪适配器端到端）、登录页渲染
- 后端验证：用真实数据库 curl 逐接口验证（登录 / 用户列表 / 管理员 / 设置 / Feature Gates / 审查设置 / 举报）
- 构建验证：Web ✅ / macOS ✅ / iOS ✅ / Windows 配置就绪 / Android 配置就绪

## 运行方式

```bash
# 1. 重启后端（使用新代码，包含 /admin/api 路由）
cd backend && go run ./cmd/api config.yaml

# 2. 运行 Flutter（任选平台）
cd admin
flutter run -d macos                    # macOS 桌面
flutter run -d chrome                   # Web 浏览器
flutter run -d <ios_device_id>          # iOS 设备

# 指定后端地址（生产环境等）
flutter run --dart-define=ADMIN_API_BASE_URL=https://your-api.example.com
```

登录账号：使用后端 admin_bootstrap 创建的管理员账号。

## 未来扩展

- 暗色主题：色板已预留结构，补一套暗色映射即可
- 国际化：内部系统当前中文硬编码，如需多语言可接入 GetX 国际化
- 推送通知：可对接 FCM/APNs 实现举报/审核事件实时推送
- 生物识别：移动端可加 Face ID / 指纹解锁
