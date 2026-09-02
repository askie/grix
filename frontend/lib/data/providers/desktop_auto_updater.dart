import 'dart:async';
import 'dart:io';

import 'package:auto_updater/auto_updater.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:grix/shared/utils/app_runtime_endpoints.dart';
import 'package:grix/shared/utils/toast_util.dart';
import 'package:grix/shared/widgets/app_dialog_style.dart';

/// 更新包 EdDSA (Ed25519) 验签公钥，base64。与 macOS Info.plist 的
/// SUPublicEDKey 是同一密钥对；私钥只存在于发布环境（SPARKLE_ED25519_PRIVATE_KEY_B64）。
const String _updateSigningPublicKey =
    'Ca9GXY8hidEBxf/TvJ5ETgU4xGBwgixPuHvhGgkLijg=';

/// 一个已经下载完成、等待应用重启后安装的更新。
///
/// 处在这个状态时 Sparkle 的更新周期是挂起的：新的 checkForUpdates（自动的和
/// 用户手动点的）都会被原生层直接忽略，服务端后续发布的版本也不会被发现。
/// 所以这个状态必须被显式呈现给用户，并给出重启入口。
class PendingDesktopUpdate {
  const PendingDesktopUpdate({
    required this.version,
    required this.build,
    required this.since,
  });

  /// 待安装版本的展示版本号，原生层没给出时为空串。
  final String version;

  /// 待安装版本的构建号，原生层没给出时为空串。
  final String build;

  /// 进入"等待重启安装"状态的时刻，原生层没给出时为 null。
  final DateTime? since;

  /// 已经挂起多久；[since] 未知时为 null。
  Duration? get age => since == null ? null : DateTime.now().difference(since!);

  /// 展示给用户的版本文案。构建号必须带上：热修版本的展示版本号可能和当前运行的
  /// 完全一样（如 3.2.6(885) -> 3.2.6(886)），只写 "3.2.6" 用户会以为提示错了。
  String get displayVersion {
    if (version.isEmpty) return build;
    if (build.isEmpty || build == version) return version;
    return '$version ($build)';
  }
}

/// Desktop auto-updater service using Sparkle (macOS) / WinSparkle (Windows).
/// Initialized alongside AppUpdateService for desktop platforms.
class DesktopAutoUpdaterService extends GetxService {
  static const _updaterChannel = MethodChannel(
    'dev.leanflutter.plugins/auto_updater',
  );

  /// 挂起多久之后开始提醒用户重启。设计成"几天"而不是几小时：更新已经装好了，
  /// 早一天晚一天无所谓，但一直不提醒会让长期不退出的进程彻底停在旧版本。
  static const _pendingReminderAfter = Duration(days: 3);

  /// 两次提醒之间的最小间隔，避免变成打扰。
  static const _pendingReminderInterval = Duration(hours: 24);

  /// 轮询挂起状态的间隔。原生层没有回调告诉我们"挂了多久"，只能定期问。
  static const _pendingReminderCheckInterval = Duration(hours: 6);

  /// fail-closed 的显式不变量：只有公钥门禁通过且 feed 已设置才为 true。
  /// 所有触发更新检查的入口都必须先过这道门，禁止绕过（否则 Windows 上
  /// 验签公钥没设置成功时仍可能拉起 WinSparkle 检查流程）。
  bool _updaterReady = false;

  Timer? _pendingReminderTimer;
  DateTime? _lastPendingReminderAt;

  String get _appcastUrl =>
      '${AppRuntimeEndpoints.apiBaseUrl}/app/appcast.xml?platform=${Platform.operatingSystem}';

  Future<DesktopAutoUpdaterService> init() async {
    if (kIsWeb || Platform.isAndroid || Platform.isIOS) return this;

    try {
      // macOS 由 Sparkle 读 Info.plist 的 SUPublicEDKey 验签；Windows 的
      // WinSparkle 必须在 init（发生在 setFeedURL 内）之前显式下发公钥。
      // 设不上就不初始化更新器：宁可不自动更新，也不接受未验签的更新包。
      if (Platform.isWindows) {
        final keyAccepted = await _updaterChannel.invokeMethod<bool>(
          'setEddsaPublicKey',
          {'publicKey': _updateSigningPublicKey},
        );
        if (keyAccepted != true) {
          debugPrint(
            'DesktopAutoUpdater: EdDSA public key rejected, updater disabled',
          );
          return this;
        }
      }
      await autoUpdater.setFeedURL(_appcastUrl);
      _updaterReady = true;
      await autoUpdater.setScheduledCheckInterval(86400); // daily
      await autoUpdater.checkForUpdates(inBackground: true);
      _startPendingReminder();
      debugPrint('DesktopAutoUpdater: initialized with feed $_appcastUrl');
    } catch (e) {
      debugPrint('DesktopAutoUpdater: init failed: $e');
    }
    return this;
  }

  @override
  void onClose() {
    _pendingReminderTimer?.cancel();
    _pendingReminderTimer = null;
    super.onClose();
  }

  /// 用户主动触发的检查（托盘菜单 / 关于页点版本号）。
  ///
  /// inBackground=false 是关键：后台模式在"已经是最新版"时**完全静默**，
  /// 用户点了菜单什么也没发生，会以为功能坏了。非后台模式下 Sparkle/WinSparkle
  /// 会自己弹窗，有更新提示更新、没更新明确告诉用户"已是最新"。
  ///
  /// 但还有一种原生层也不弹窗的情况：已经有一个更新下载完等着重启安装。这时
  /// Sparkle 的更新周期是挂起的，checkForUpdates 被直接忽略，用户点了同样毫无反应。
  /// 所以先查挂起状态，有就自己把话说清楚。
  Future<void> checkForUpdatesInteractive() async {
    if (kIsWeb || Platform.isAndroid || Platform.isIOS) return;
    if (!_updaterReady) {
      // 公钥门禁没过（或 init 失败）时更新器从未初始化，这里必须拒绝，
      // 不能试图补救 setFeedURL——那会绕过验签的 fail-closed 保证。
      debugPrint(
        'DesktopAutoUpdater: updater not ready, interactive check refused',
      );
      throw StateError('desktop updater not initialized');
    }

    final pending = await pendingUpdate();
    if (pending != null) {
      _showPendingUpdateDialog(pending);
      return;
    }

    try {
      await autoUpdater.checkForUpdates(inBackground: false);
    } catch (e) {
      debugPrint('DesktopAutoUpdater: interactive check failed: $e');
      rethrow;
    }
  }

  /// 查询原生层是否已有下载完成、等待重启安装的更新。没有则返回 null。
  Future<PendingDesktopUpdate?> pendingUpdate() async {
    // 只有 macOS 的 Sparkle 有"下载完成、挂起等退出"这个中间态。WinSparkle
    // 在用户确认后直接跑安装程序并退出应用，不存在长期挂起的安装会话。
    if (!_updaterReady || kIsWeb || !Platform.isMacOS) return null;
    try {
      final status = await _updaterChannel.invokeMapMethod<String, dynamic>(
        'getUpdateSessionStatus',
      );
      if (status == null || status['hasPendingInstall'] != true) return null;
      final sinceMs = status['pendingSinceEpochMs'];
      return PendingDesktopUpdate(
        version: status['pendingVersion'] as String? ?? '',
        build: status['pendingBuild'] as String? ?? '',
        since: sinceMs is int
            ? DateTime.fromMillisecondsSinceEpoch(sinceMs)
            : null,
      );
    } catch (e) {
      // 旧版本原生插件没有这个方法（MissingPluginException）时按"没有挂起"处理，
      // 交互式检查退回到原来的行为，不影响可用性。
      debugPrint('DesktopAutoUpdater: pending update query failed: $e');
      return null;
    }
  }

  /// 立即安装已下载的更新并重启应用。没有待安装更新或调用失败时返回 false。
  Future<bool> installPendingUpdateNow() async {
    if (!_updaterReady || kIsWeb || !Platform.isMacOS) return false;
    try {
      return await _updaterChannel.invokeMethod<bool>('installPendingUpdate') ??
          false;
    } catch (e) {
      debugPrint('DesktopAutoUpdater: install pending update failed: $e');
      return false;
    }
  }

  /// 长期不退出的进程保护：更新挂了几天还没装上时主动提醒一次。
  ///
  /// 不这么做的话，进程一直不退出 = 更新周期一直挂起 = 之后所有新版本都发现不了，
  /// 用户只能自己察觉不对再去重启。
  void _startPendingReminder() {
    _pendingReminderTimer?.cancel();
    _pendingReminderTimer = Timer.periodic(
      _pendingReminderCheckInterval,
      (_) => _remindPendingUpdateIfStale(),
    );
  }

  Future<void> _remindPendingUpdateIfStale() async {
    final pending = await pendingUpdate();
    final age = pending?.age;
    if (pending == null || age == null || age < _pendingReminderAfter) return;

    final last = _lastPendingReminderAt;
    if (last != null &&
        DateTime.now().difference(last) < _pendingReminderInterval) {
      return;
    }
    // 已经有弹窗开着时不插队，下一轮再说。
    if (Get.context == null || (Get.isDialogOpen ?? false)) return;

    _lastPendingReminderAt = DateTime.now();
    _showPendingUpdateDialog(pending);
  }

  void _showPendingUpdateDialog(PendingDesktopUpdate pending) {
    if (Get.context == null) return;
    showAppGetDialog(
      _PendingUpdateRestartDialog(pending: pending, service: this),
      barrierDismissible: true,
    );
  }
}

/// "新版本已下载，重启后生效"的提示弹窗，带立即重启入口。
class _PendingUpdateRestartDialog extends StatelessWidget {
  const _PendingUpdateRestartDialog({
    required this.pending,
    required this.service,
  });

  final PendingDesktopUpdate pending;
  final DesktopAutoUpdaterService service;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final displayVersion = pending.displayVersion;
    final body = displayVersion.isEmpty
        ? 'update_pending_restart_body_unknown_version'.tr
        : 'update_pending_restart_body'.trParams({'version': displayVersion});

    return AlertDialog(
      title: Text('update_pending_restart_title'.tr),
      content: SizedBox(
        width: 320,
        child: Text(body, style: theme.textTheme.bodyMedium),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: Text('update_later'.tr),
        ),
        FilledButton(
          onPressed: () => _restartAndInstall(context),
          child: Text('update_restart_now'.tr),
        ),
      ],
    );
  }

  Future<void> _restartAndInstall(BuildContext context) async {
    Navigator.of(context).pop();
    final started = await service.installPendingUpdateNow();
    if (!started) {
      CustomToast.show('update_restart_failed'.tr);
    }
  }
}
