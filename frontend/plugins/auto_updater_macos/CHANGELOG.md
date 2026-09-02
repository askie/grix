## 1.0.0+grix.1

Vendored fork（源自 pub.dev auto_updater_macos 1.0.0，经 dependency_overrides 引入）：

* 保留 `updater:willInstallUpdateOnQuit:immediateInstallationBlock:` 传回的
  `immediateInstallHandler`。上游返回 `YES`（表示宿主接管安装时机）却把 handler 丢掉，
  Sparkle 因此永久挂起更新周期，长时间不退出的进程再也感知不到新版本，
  而且没有任何入口能把已下载的更新装上。
* 新增 method channel 方法 `getUpdateSessionStatus`，返回
  `sessionInProgress` / `canCheckForUpdates` / `hasPendingInstall` /
  `pendingVersion` / `pendingBuild` / `pendingSinceEpochMs`。
* 新增 method channel 方法 `installPendingUpdate`，立即安装已下载的更新并重启应用，
  没有待安装更新时返回 `false`。

## 1.0.0

* First major release.
