import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

class ThemePreferenceService extends GetxService {
  static const String prefsKey = 'app_theme_mode';
  static bool _prefsUnavailableLogged = false;

  final Rx<ThemeMode> _themeMode = ThemeMode.light.obs;

  Future<ThemePreferenceService> init() async {
    final prefs = await _safeGetPrefs();
    if (prefs != null) {
      _themeMode.value = _parseThemeMode(prefs.getString(prefsKey));
    }
    return this;
  }

  ThemeMode get themeMode => _themeMode.value;

  bool get isDarkMode => _themeMode.value == ThemeMode.dark;

  Future<void> setDarkModeEnabled(bool enabled) async {
    final nextMode = enabled ? ThemeMode.dark : ThemeMode.light;
    if (_themeMode.value == nextMode) return;
    _themeMode.value = nextMode;

    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    await prefs.setString(prefsKey, _serializeThemeMode(nextMode));
  }

  Future<void> toggle() {
    return setDarkModeEnabled(!isDarkMode);
  }

  ThemeMode _parseThemeMode(String? raw) {
    return raw == 'dark' ? ThemeMode.dark : ThemeMode.light;
  }

  String _serializeThemeMode(ThemeMode mode) {
    return mode == ThemeMode.dark ? 'dark' : 'light';
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
