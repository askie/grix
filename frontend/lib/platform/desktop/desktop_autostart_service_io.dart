import 'dart:io';

import 'package:flutter/widgets.dart';
import 'package:launch_at_startup/launch_at_startup.dart';
import 'package:package_info_plus/package_info_plus.dart';

/// 开机自启管理服务
class DesktopAutostartService {
  bool _isEnabled = false;
  bool get isEnabled => _isEnabled;

  Future<void> initialize() async {
    try {
      final packageInfo = await PackageInfo.fromPlatform();
      launchAtStartup.setup(
        appName: packageInfo.appName,
        appPath: Platform.resolvedExecutable,
        packageName: packageInfo.packageName,
      );
      _isEnabled = await launchAtStartup.isEnabled();
    } catch (e) {
      debugPrint('开机自启初始化失败: $e');
    }
  }

  Future<void> setEnabled(bool enabled) async {
    try {
      if (enabled) {
        await launchAtStartup.enable();
      } else {
        await launchAtStartup.disable();
      }
      _isEnabled = enabled;
    } catch (e) {
      debugPrint('设置开机自启失败: $e');
    }
  }
}
