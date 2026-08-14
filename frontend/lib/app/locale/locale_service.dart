import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

class LocaleService {
  LocaleService._();

  static const String _localeKey = 'app_locale';
  static bool _prefsUnavailableLogged = false;

  /// 所有支持的语言，顺序即 Settings 列表顺序
  static const List<({Locale locale, String label, String nativeLabel})>
  supportedLocales = [
    (locale: Locale('en', 'US'), label: 'English', nativeLabel: 'English'),
    (locale: Locale('zh', 'CN'), label: '中文', nativeLabel: '中文'),
    (locale: Locale('ja', 'JP'), label: 'Japanese', nativeLabel: '日本語'),
    (locale: Locale('ko', 'KR'), label: 'Korean', nativeLabel: '한국어'),
    (locale: Locale('de', 'DE'), label: 'German', nativeLabel: 'Deutsch'),
    (locale: Locale('fr', 'FR'), label: 'French', nativeLabel: 'Français'),
    (locale: Locale('es', 'ES'), label: 'Spanish', nativeLabel: 'Español'),
    (locale: Locale('pt', 'BR'), label: 'Portuguese', nativeLabel: 'Português'),
    (locale: Locale('ru', 'RU'), label: 'Russian', nativeLabel: 'Русский'),
    (locale: Locale('ar'), label: 'Arabic', nativeLabel: 'العربية'),
    (locale: Locale('hi', 'IN'), label: 'Hindi', nativeLabel: 'हिन्दी'),
  ];

  static Future<Locale?> loadSavedLocale() async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return null;
    final raw = prefs.getString(_localeKey);
    return _parseLocale(raw);
  }

  static Future<void> saveLocale(Locale locale) async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    await prefs.setString(_localeKey, _serializeLocale(locale));
  }

  /// 返回当前 locale 对应的 nativeLabel，找不到时返回 'English'
  static String currentNativeLabel(Locale? locale) {
    if (locale == null) return 'English';
    final match = supportedLocales.where(
      (e) =>
          e.locale.languageCode == locale.languageCode &&
          (e.locale.countryCode == null ||
              e.locale.countryCode == locale.countryCode),
    );
    return match.isNotEmpty ? match.first.nativeLabel : 'English';
  }

  static Locale? _parseLocale(String? raw) {
    if (raw == null || raw.isEmpty) return null;
    final normalized = raw.replaceAll('-', '_').toLowerCase();
    for (final entry in supportedLocales) {
      final lang = entry.locale.languageCode.toLowerCase();
      final country = entry.locale.countryCode?.toLowerCase() ?? '';
      if (country.isNotEmpty && normalized == '${lang}_$country') {
        return entry.locale;
      }
      if (normalized == lang) {
        return entry.locale;
      }
    }
    // 前缀匹配（如 zh-Hans-CN → zh_CN）
    for (final entry in supportedLocales) {
      if (normalized.startsWith(entry.locale.languageCode.toLowerCase())) {
        return entry.locale;
      }
    }
    return null;
  }

  static String _serializeLocale(Locale locale) {
    final countryCode = locale.countryCode;
    if (countryCode == null || countryCode.isEmpty) {
      return locale.languageCode;
    }
    return '${locale.languageCode}_$countryCode';
  }

  static Future<SharedPreferences?> _safeGetPrefs() async {
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

  static void _logPrefsUnavailable(Object error) {
    if (_prefsUnavailableLogged) return;
    _prefsUnavailableLogged = true;
    debugPrint(
      'SharedPreferences unavailable, skip locale persistence: $error',
    );
  }
}
