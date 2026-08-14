import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 桌面端首页 Agent 工具栏显隐状态持久化服务。
///
/// 状态默认展示，切换后持久化到本地 SharedPreferences，跨启动保持一致。
class AgentToolbarVisibilityService extends GetxService {
  static const String prefsKey = 'home_agent_toolbar_visible';
  static bool _prefsUnavailableLogged = false;

  final RxBool _visible = true.obs;

  Future<AgentToolbarVisibilityService> init() async {
    final prefs = await _safeGetPrefs();
    _visible.value = prefs?.getBool(prefsKey) ?? true;
    return this;
  }

  bool get visible => _visible.value;
  RxBool get visibleRx => _visible;

  Future<void> toggle() => setVisible(!_visible.value);

  Future<void> setVisible(bool next) async {
    if (_visible.value == next) return;
    _visible.value = next;

    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    await prefs.setBool(prefsKey, next);
  }

  Future<SharedPreferences?> _safeGetPrefs() async {
    try {
      return await SharedPreferences.getInstance();
    } on MissingPluginException catch (e) {
      _logPrefsUnavailable(e);
      return null;
    } on PlatformException catch (e) {
      _logPrefsUnavailable(e);
      return null;
    }
  }

  void _logPrefsUnavailable(Object error) {
    if (_prefsUnavailableLogged) return;
    _prefsUnavailableLogged = true;
    debugPrint(
      'SharedPreferences unavailable, skip agent toolbar visibility persistence: $error',
    );
  }
}
