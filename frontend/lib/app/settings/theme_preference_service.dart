import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

class ThemePreferenceService extends GetxService {
  static const String prefsKey = 'app_theme_mode';
  static bool _prefsUnavailableLogged = false;

  final Rx<ThemeMode> _themeMode = ThemeMode.system.obs;

  Future<ThemePreferenceService> init() async {
    final prefs = await _safeGetPrefs();
    if (prefs != null) {
      _themeMode.value = _parseThemeMode(prefs.getString(prefsKey));
    }
    return this;
  }

  ThemeMode get themeMode => _themeMode.value;

  /// Effective dark state: for [ThemeMode.system] it reflects the platform
  /// brightness; otherwise the fixed user choice.
  bool get isDarkMode {
    switch (_themeMode.value) {
      case ThemeMode.dark:
        return true;
      case ThemeMode.light:
        return false;
      case ThemeMode.system:
        return _platformBrightness == Brightness.dark;
    }
  }

  Brightness get _platformBrightness =>
      SchedulerBinding.instance.platformDispatcher.platformBrightness;

  Future<void> setThemeMode(ThemeMode mode) async {
    if (_themeMode.value == mode) return;
    _themeMode.value = mode;

    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    await prefs.setString(prefsKey, _serializeThemeMode(mode));
  }

  /// Pins the theme to the opposite of the currently effective brightness.
  Future<void> toggle() {
    return setThemeMode(isDarkMode ? ThemeMode.light : ThemeMode.dark);
  }

  ThemeMode _parseThemeMode(String? raw) {
    switch (raw) {
      case 'dark':
        return ThemeMode.dark;
      case 'light':
        return ThemeMode.light;
      default:
        return ThemeMode.system;
    }
  }

  String _serializeThemeMode(ThemeMode mode) {
    switch (mode) {
      case ThemeMode.dark:
        return 'dark';
      case ThemeMode.light:
        return 'light';
      case ThemeMode.system:
        return 'system';
    }
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
      'SharedPreferences unavailable, skip theme preference persistence: $error',
    );
  }
}
