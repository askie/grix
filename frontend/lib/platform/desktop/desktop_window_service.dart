import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:window_manager/window_manager.dart';

import '../../app/profile/instance_profile.dart';

/// 桌面窗口管理服务
/// 负责：关闭时最小化到托盘、窗口尺寸/位置记忆
class DesktopWindowService with WindowListener {
  static const _kWindowX = 'desktop_window_x';
  static const _kWindowY = 'desktop_window_y';
  static const _kWindowWidth = 'desktop_window_width';
  static const _kWindowHeight = 'desktop_window_height';
  static const _kCloseToTray = 'desktop_close_to_tray';

  static const Size _defaultSize = Size(1280, 800);
  static const Size _minSize = Size(400, 600);
  static const Duration _boundsSaveDelay = Duration(milliseconds: 250);

  Timer? _boundsSaveTimer;
  Future<void> _boundsSaveChain = Future<void>.value();

  /// 关闭窗口时是否最小化到托盘（默认 true）
  bool _closeToTray = true;
  bool get closeToTray => _closeToTray;

  set closeToTray(bool value) {
    _closeToTray = value;
    SharedPreferences.getInstance().then((prefs) {
      prefs.setBool(_kCloseToTray, value);
    });
  }

  Future<void> initialize() async {
    await windowManager.ensureInitialized();

    final prefs = await SharedPreferences.getInstance();
    _closeToTray = prefs.getBool(_kCloseToTray) ?? true;

    final savedWidth = prefs.getDouble(_kWindowWidth);
    final savedHeight = prefs.getDouble(_kWindowHeight);
    final savedX = prefs.getDouble(_kWindowX);
    final savedY = prefs.getDouble(_kWindowY);

    final size = sanitizeRestoredSize(savedWidth, savedHeight);
    final hasSavedPosition =
        savedX != null && savedY != null && savedX.isFinite && savedY.isFinite;

    final WindowOptions options = WindowOptions(
      size: size,
      minimumSize: _minSize,
      center: !hasSavedPosition,
      title: 'Grix',
    );

    await windowManager.waitUntilReadyToShow(options, () async {
      windowManager.addListener(this);
      await windowManager.setPreventClose(true);
      if (hasSavedPosition) {
        await windowManager.setPosition(Offset(savedX, savedY));
      }
      await windowManager.show();
      await windowManager.focus();
    });
  }

  /// 保存当前窗口位置和尺寸
  Future<void> _saveWindowBounds() async {
    try {
      final position = await windowManager.getPosition();
      final size = await windowManager.getSize();
      if (!isPersistableSize(size)) {
        debugPrint('忽略无效窗口尺寸: ${size.width}x${size.height}');
        return;
      }
      final prefs = await SharedPreferences.getInstance();
      if (position.dx.isFinite && position.dy.isFinite) {
        await prefs.setDouble(_kWindowX, position.dx);
        await prefs.setDouble(_kWindowY, position.dy);
      }
      await prefs.setDouble(_kWindowWidth, size.width);
      await prefs.setDouble(_kWindowHeight, size.height);
    } catch (e) {
      debugPrint('保存窗口位置失败: $e');
    }
  }

  void _scheduleWindowBoundsSave() {
    _boundsSaveTimer?.cancel();
    _boundsSaveTimer = Timer(_boundsSaveDelay, () {
      _boundsSaveTimer = null;
      _boundsSaveChain = _boundsSaveChain.then((_) => _saveWindowBounds());
    });
  }

  Future<void> _flushWindowBounds() async {
    _boundsSaveTimer?.cancel();
    _boundsSaveTimer = null;
    await _boundsSaveChain;
    await _saveWindowBounds();
  }

  @visibleForTesting
  static Size sanitizeRestoredSize(double? width, double? height) {
    if (width == null ||
        height == null ||
        !width.isFinite ||
        !height.isFinite ||
        width <= 0 ||
        height <= 0) {
      return _defaultSize;
    }
    return Size(
      width < _minSize.width ? _minSize.width : width,
      height < _minSize.height ? _minSize.height : height,
    );
  }

  @visibleForTesting
  static bool isPersistableSize(Size size) {
    return size.width.isFinite &&
        size.height.isFinite &&
        size.width >= _minSize.width &&
        size.height >= _minSize.height;
  }

  @override
  void onWindowClose() async {
    await _flushWindowBounds();
    if (_closeToTray) {
      await windowManager.hide();
    } else {
      await windowManager.setPreventClose(false);
      await windowManager.close();
    }
  }

  @override
  void onWindowMoved() {
    _scheduleWindowBoundsSave();
  }

  @override
  void onWindowResized() {
    _scheduleWindowBoundsSave();
  }

  /// 按当前登录账号更新窗口标题（多实例时靠标题区分窗口）。
  /// 未登录时显示 profile 名（非 default 实例），登录后显示账号昵称。
  Future<void> applyAccountTitle({String? nickname}) async {
    var title = 'Grix';
    final trimmed = nickname?.trim() ?? '';
    if (trimmed.isNotEmpty) {
      title = 'Grix · $trimmed';
    } else if (!InstanceProfile.current.isDefault) {
      title = 'Grix · ${InstanceProfile.current.name}';
    }
    try {
      await windowManager.setTitle(title);
    } catch (e) {
      debugPrint('设置窗口标题失败: $e');
    }
  }

  /// 显示窗口并聚焦（从托盘恢复时调用）
  Future<void> showAndFocus() async {
    await windowManager.show();
    await windowManager.focus();
  }

  /// 真正退出应用
  ///
  /// 必须用 destroy 而非 close：macOS 的 AppDelegate 返回
  /// applicationShouldTerminateAfterLastWindowClosed = false（为支持关窗到托盘），
  /// close 只关窗口不终止进程；destroy 在三平台都是真正结束进程。
  Future<void> forceQuit() async {
    await _flushWindowBounds();
    await windowManager.setPreventClose(false);
    await windowManager.destroy();
  }

  void dispose() {
    _boundsSaveTimer?.cancel();
    windowManager.removeListener(this);
  }
}
