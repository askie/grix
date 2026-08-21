import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:get/get.dart';
import 'package:tray_manager/tray_manager.dart';

import '../../app/profile/instance_profile.dart';
import '../../data/providers/desktop_auto_updater.dart';
import '../../shared/utils/toast_util.dart';
import 'desktop_window_service.dart';

/// 系统托盘服务
/// 负责：托盘图标显示、右键菜单
class DesktopTrayService with TrayListener {
  Future<void> initialize() async {
    // 设置托盘图标
    final iconPath = Platform.isWindows
        ? 'assets/images/tray_icon.ico'
        : 'assets/images/tray_icon.png';

    await trayManager.setIcon(iconPath);
    await applyAccountTooltip();
    await refreshContextMenu();
    trayManager.addListener(this);
  }

  /// 菜单 label 是构建时取值的普通字符串：翻译注册进 GetX 之前构建会留下
  /// 原始 key，语言切换后也不会自动跟随。翻译就绪/语言变化后统一走这里重建。
  static Future<void> refreshContextMenuIfRegistered() async {
    if (Get.isRegistered<DesktopTrayService>()) {
      await Get.find<DesktopTrayService>().refreshContextMenu();
    }
  }

  /// 按当前登录账号更新托盘提示文字（鼠标悬停显示）。
  /// 未登录时显示 profile 名（非 default 实例），登录后显示账号昵称。
  Future<void> applyAccountTooltip({String? nickname}) async {
    var tooltip = 'Grix';
    final trimmed = nickname?.trim() ?? '';
    if (trimmed.isNotEmpty) {
      tooltip = 'Grix · $trimmed';
    } else if (!InstanceProfile.current.isDefault) {
      tooltip = 'Grix · ${InstanceProfile.current.name}';
    }
    try {
      await trayManager.setToolTip(tooltip);
    } catch (e) {
      debugPrint('设置托盘提示失败: $e');
    }
  }

  /// 按当前语言重建托盘右键菜单。初始化时调用一次即可，
  /// 之后由 refreshContextMenuIfRegistered 在翻译就绪/语言切换时触发。
  Future<void> refreshContextMenu() async {
    final menu = Menu(
      items: [
        MenuItem(key: 'show_window', label: 'desktop_tray_show_window'.tr),
        MenuItem(key: 'check_update', label: 'desktop_tray_check_update'.tr),
        MenuItem.separator(),
        MenuItem(key: 'quit', label: 'desktop_tray_quit'.tr),
      ],
    );
    await trayManager.setContextMenu(menu);
  }

  @override
  void onTrayIconMouseDown() {
    // 单击托盘图标显示窗口
    _showWindow();
  }

  @override
  void onTrayIconRightMouseDown() {
    trayManager.popUpContextMenu();
  }

  @override
  void onTrayMenuItemClick(MenuItem menuItem) {
    switch (menuItem.key) {
      case 'show_window':
        _showWindow();
        break;
      case 'check_update':
        _checkUpdate();
        break;
      case 'quit':
        _quitApp();
        break;
    }
  }

  /// 走 Sparkle/WinSparkle 的交互式检查：有更新提示更新，没更新也明确告诉用户，
  /// 不会点了菜单毫无动静。
  void _checkUpdate() {
    if (!Get.isRegistered<DesktopAutoUpdaterService>()) {
      // 正常走不到这里：托盘菜单要用户手动点，那时延迟初始化早已完成。
      // 真到了这一步说明启动流程被改坏了，别静默吞掉。
      debugPrint('DesktopTray: 更新服务尚未注册，检查更新被跳过');
      return;
    }
    Get.find<DesktopAutoUpdaterService>().checkForUpdatesInteractive().catchError((Object e) {
      // 更新器未就绪（如 Windows 验签公钥未设置成功）或检查失败，
      // 手动触发的操作必须给用户反馈，不能静默。
      debugPrint('DesktopTray: 检查更新失败: $e');
      CustomToast.show('update_check_failed'.tr);
    });
  }

  void _showWindow() {
    if (Get.isRegistered<DesktopWindowService>()) {
      Get.find<DesktopWindowService>().showAndFocus();
    }
  }

  void _quitApp() {
    if (Get.isRegistered<DesktopWindowService>()) {
      Get.find<DesktopWindowService>().forceQuit();
    }
  }

  void dispose() {
    trayManager.removeListener(this);
    trayManager.destroy();
  }
}
