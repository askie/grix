import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 工具栏技能弹窗的“最近使用”记录服务（仅前端持久化）。
///
/// 记录用户在技能弹窗中选用过的技能，按最近使用时间倒序保存。
/// 下次打开弹窗时用它对技能列表做稳定排序：用过的排在前面。
class ChatSkillUsageService extends GetxService {
  static const String prefsKey = 'chat_skill_recent_used';
  static const int _maxEntries = 50;
  static bool _prefsUnavailableLogged = false;

  /// 最近使用的技能 key，最新的在最前面。
  final List<String> _recent = <String>[];

  Future<ChatSkillUsageService> init() async {
    final prefs = await _safeGetPrefs();
    final stored = prefs?.getStringList(prefsKey);
    if (stored != null) {
      _recent
        ..clear()
        ..addAll(stored);
    }
    return this;
  }

  /// 在已有技能列表中的“最近使用”名次，越小越靠前；未用过返回较大值。
  int rankOf(String key) {
    if (key.isEmpty) return _maxEntries + 1;
    final index = _recent.indexOf(key);
    return index < 0 ? _maxEntries + 1 : index;
  }

  /// 记录一次技能使用，置顶并持久化。
  Future<void> record(String key) async {
    if (key.isEmpty) return;
    _recent.remove(key);
    _recent.insert(0, key);
    if (_recent.length > _maxEntries) {
      _recent.removeRange(_maxEntries, _recent.length);
    }
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    await prefs.setStringList(prefsKey, _recent);
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
      'SharedPreferences unavailable, skip skill usage persistence: $error',
    );
  }
}
