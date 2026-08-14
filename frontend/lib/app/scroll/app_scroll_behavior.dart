import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';

import '../../platform/platform_capability.dart';

/// 全局滚动行为
///
/// 移动端：允许鼠标和触控板拖动滚动（与触摸一致）
/// 桌面端/Web：仅保留默认设备（触摸），鼠标不参与拖动，以支持文字选中
class AppScrollBehavior extends MaterialScrollBehavior {
  const AppScrollBehavior();

  @override
  Set<PointerDeviceKind> get dragDevices {
    if (PlatformCapability.isMobile) {
      return <PointerDeviceKind>{
        ...super.dragDevices,
        PointerDeviceKind.mouse,
        PointerDeviceKind.trackpad,
      };
    }
    return super.dragDevices;
  }
}
