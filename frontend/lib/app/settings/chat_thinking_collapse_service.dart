import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 思考气泡 App 级折叠开关服务。
/// 维护全局唯一的折叠状态，所有思考气泡共享并实时跟随，状态持久化到本地。
class ChatThinkingCollapseService extends GetxService {
  static const String prefsKey = 'chat_thinking_collapsed';
  static bool _prefsUnavailableLogged = false;

  final RxBool _collapsed = false.obs;

  Future<ChatThinkingCollapseService> init() async {
    final prefs = await _safeGetPrefs();
    _collapsed.value = prefs?.getBool(prefsKey) ?? false;
    return this;
  }

  bool get collapsed => _collapsed.value;
  RxBool get collapsedRx => _collapsed;

  Future<void> toggle() => setCollapsed(!_collapsed.value);

  Future<void> setCollapsed(bool next) async {
    if (_collapsed.value == next) return;
    _collapsed.value = next;

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
      'SharedPreferences unavailable, skip thinking collapse persistence: $error',
    );
  }
}
