import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';

/// 横向条（工具栏、chip 条等）的滚动行为。
///
/// 全局 [AppScrollBehavior] 在桌面/Web 上故意不把鼠标纳入 dragDevices，
/// 以免干扰文字选中；但横向条几乎只能靠拖动滚动，因此这里单独放开鼠标与触控板。
class HorizontalDragScrollBehavior extends MaterialScrollBehavior {
  const HorizontalDragScrollBehavior();

  @override
  Set<PointerDeviceKind> get dragDevices => const <PointerDeviceKind>{
    PointerDeviceKind.touch,
    PointerDeviceKind.stylus,
    PointerDeviceKind.invertedStylus,
    PointerDeviceKind.mouse,
    PointerDeviceKind.trackpad,
  };
}
