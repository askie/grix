# 桌面端已下载更新的安装时机由客户端接管

## Context

macOS 客户端用 `auto_updater` 插件包装 Sparkle。Sparkle 自动下载完更新后会回调
`updater:willInstallUpdateOnQuit:immediateInstallationBlock:`。按 Sparkle 头文件的约定，
该回调返回 `YES` 表示**宿主接管安装时机**：Sparkle 会挂起当前更新周期，并且不再启动新的更新周期，
直到宿主调用 `immediateInstallHandler` 或应用退出。

上游 `auto_updater_macos` 返回 `YES` 却把 `immediateInstallHandler` 丢弃了，宿主因此既没有
安装入口，也没有任何状态可查。实际后果（3.2.6(885) 现网实例）：进程连续运行 6 天不退出，
3.2.6(886) 静默下载完成后更新周期永久挂起，服务端已发布到 3.2.7(902) 客户端完全感知不到；
用户点"检查更新"时 `checkForUpdates` 被原生层直接忽略，界面毫无反馈。

## Decision

vendored fork `frontend/plugins/auto_updater_macos`（沿用 `auto_updater_windows` 的既有做法，
经 `dependency_overrides` 引入），保留 `immediateInstallHandler` 并新增两个 method channel 方法：

- `getUpdateSessionStatus` → `sessionInProgress` / `canCheckForUpdates` / `hasPendingInstall` /
  `pendingVersion` / `pendingBuild` / `pendingSinceEpochMs`
- `installPendingUpdate` → 立即安装已下载的更新并重启应用

`DesktopAutoUpdaterService` 据此接管安装时机：手动检查前先查挂起状态，有挂起就弹出
"新版本已下载，重启后生效"并给出立即重启入口；另有 6 小时一次的轮询，挂起超过 3 天时
弹一次同样的提醒（两次提醒至少间隔 24 小时）。

Windows 的 WinSparkle 不存在"下载完成、长期挂起等退出"这个中间态（确认后直接跑安装程序），
所以这两个方法只在 macOS 上调用，Windows 行为不变。

## Alternatives

- **回调改返回 `NO`**：Sparkle 保留自己的更新周期与 gentle reminders，一行改动即可解决"感知不到新版本"。
  但按头文件说明，`immediateInstallHandler` 只有返回 `YES` 时可用，就拿不到"立即重启安装"的入口，
  满足不了"手动检查必须有可见反馈且能当场装上"的要求。
- **只在 Dart 侧监听 `before-quit-for-update` 事件推断状态**：不用 fork 原生插件，但仍然没有安装入口，
  且状态来源是事件时序而非原生真值，重复触发或漏事件时会和原生层不一致。

## Consequences

- 安装时机的责任从 Sparkle 转移到 Grix：Dart 侧的提醒逻辑若失效，挂起状态依然不会自愈，
  只能靠用户手动退出应用来安装。这是接管 `YES` 语义的代价，改回 `NO` 是可逆的退路。
- 多了一个需要跟随上游同步的 vendored fork（另一个是 `auto_updater_windows`）。
- 验签 fail-closed 门禁不变：`_updaterReady` 为 false 时，查询挂起状态和立即安装同样被拒绝。

## Verification

`frontend/test/data/providers/desktop_auto_updater_pending_install_test.dart`：挂起时手动检查必须弹窗
且不再调用原生 `checkForUpdates`、"立即重启"确实调用 `installPendingUpdate`、无挂起时照旧走
`checkForUpdates(inBackground: false)`、更新器未就绪时所有入口都不碰原生层。
`flutter build macos` 验证 vendored 插件可编译。
