import 'dart:io';

import 'package:auto_updater/auto_updater.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:grix/shared/utils/app_runtime_endpoints.dart';

/// 更新包 EdDSA (Ed25519) 验签公钥，base64。与 macOS Info.plist 的
/// SUPublicEDKey 是同一密钥对；私钥只存在于发布环境（SPARKLE_ED25519_PRIVATE_KEY_B64）。
const String _updateSigningPublicKey = 'Ca9GXY8hidEBxf/TvJ5ETgU4xGBwgixPuHvhGgkLijg=';

/// Desktop auto-updater service using Sparkle (macOS) / WinSparkle (Windows).
/// Initialized alongside AppUpdateService for desktop platforms.
class DesktopAutoUpdaterService extends GetxService {
  static const _updaterChannel = MethodChannel('dev.leanflutter.plugins/auto_updater');

  /// fail-closed 的显式不变量：只有公钥门禁通过且 feed 已设置才为 true。
  /// 所有触发更新检查的入口都必须先过这道门，禁止绕过（否则 Windows 上
  /// 验签公钥没设置成功时仍可能拉起 WinSparkle 检查流程）。
  bool _updaterReady = false;

  String get _appcastUrl => '${AppRuntimeEndpoints.apiBaseUrl}/app/appcast.xml?platform=${Platform.operatingSystem}';

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
          debugPrint('DesktopAutoUpdater: EdDSA public key rejected, updater disabled');
          return this;
        }
      }
      await autoUpdater.setFeedURL(_appcastUrl);
      _updaterReady = true;
      await autoUpdater.setScheduledCheckInterval(86400); // daily
      await autoUpdater.checkForUpdates(inBackground: true);
      debugPrint('DesktopAutoUpdater: initialized with feed $_appcastUrl');
    } catch (e) {
      debugPrint('DesktopAutoUpdater: init failed: $e');
    }
    return this;
  }

  /// 用户主动触发的检查（托盘菜单 / 关于页点版本号）。
  ///
  /// inBackground=false 是关键：后台模式在"已经是最新版"时**完全静默**，
  /// 用户点了菜单什么也没发生，会以为功能坏了。非后台模式下 Sparkle/WinSparkle
  /// 会自己弹窗，有更新提示更新、没更新明确告诉用户"已是最新"。
  Future<void> checkForUpdatesInteractive() async {
    if (kIsWeb || Platform.isAndroid || Platform.isIOS) return;
    if (!_updaterReady) {
      // 公钥门禁没过（或 init 失败）时更新器从未初始化，这里必须拒绝，
      // 不能试图补救 setFeedURL——那会绕过验签的 fail-closed 保证。
      debugPrint('DesktopAutoUpdater: updater not ready, interactive check refused');
      throw StateError('desktop updater not initialized');
    }
    try {
      await autoUpdater.checkForUpdates(inBackground: false);
    } catch (e) {
      debugPrint('DesktopAutoUpdater: interactive check failed: $e');
      rethrow;
    }
  }
}
