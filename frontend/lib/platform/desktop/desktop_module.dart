import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../data/providers/auth_service.dart';
import '../../modules/system/grix_connector_service.dart';
import '../platform_capability.dart';
import 'desktop_window_service.dart';
import 'desktop_tray_service.dart';
import 'desktop_autostart_service.dart';

/// 桌面端模块统一入口
/// 仅在桌面平台（macOS / Windows / Linux）执行初始化
class DesktopModule {
  DesktopModule._();

  static Future<void> initialize() async {
    if (!PlatformCapability.isDesktop) return;

    debugPrint('🖥️ 桌面模块初始化开始');

    final windowService = DesktopWindowService();
    await windowService.initialize();
    Get.put(windowService);

    final trayService = DesktopTrayService();
    await trayService.initialize();
    Get.put(trayService);

    // 多实例时靠窗口标题区分账号；托盘提示同步显示登录用户名。
    // 登录态/账号资料变化即刷新两处。
    if (Get.isRegistered<AuthService>()) {
      final authService = Get.find<AuthService>();
      ever(authService.userRx, (user) {
        windowService.applyAccountTitle(nickname: user?.nickname);
        trayService.applyAccountTooltip(nickname: user?.nickname);
      });
      await windowService.applyAccountTitle(
        nickname: authService.user?.nickname,
      );
      await trayService.applyAccountTooltip(
        nickname: authService.user?.nickname,
      );
    }

    final autostartService = DesktopAutostartService();
    await autostartService.initialize();
    Get.put(autostartService);

    // Agent connector 服务（首页工具栏需要全局可用）
    Get.put(GrixConnectorService());

    debugPrint('🖥️ 桌面模块初始化完成');
  }
}
