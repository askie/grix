import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

class LocaleService {
  LocaleService._();

  static const String _localeKey = 'app_locale';
  static const Locale fallbackLocale = Locale('en', 'US');
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
    return parseLocale(raw);
  }

  static Future<void> saveLocale(Locale locale) async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    await prefs.setString(_localeKey, _serializeLocale(locale));
  }

  /// 有手动保存的语言偏好时返回 true（跟随系统时不会写入该键）。
  static Future<bool> hasSavedLocale() async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return false;
    final raw = prefs.getString(_localeKey);
    return raw != null && raw.isNotEmpty;
  }

  /// 解析生效语言：有保存值用保存值；否则按系统 locales 优先顺序匹配。
  ///
  /// 跟随系统时**不会**写入 SharedPreferences。
  static Future<Locale> resolveEffectiveLocale({
    List<Locale>? systemLocales,
  }) async {
    final saved = await loadSavedLocale();
    return resolveEffectiveLocaleSync(
      saved: saved,
      systemLocales:
          systemLocales ?? PlatformDispatcher.instance.locales,
    );
  }

  /// 纯同步解析，便于单元测试注入 saved / systemLocales。
  static Locale resolveEffectiveLocaleSync({
    Locale? saved,
    List<Locale> systemLocales = const <Locale>[],
  }) {
    if (saved != null) {
      return saved;
    }
    return resolveFromSystemLocales(systemLocales);
  }

  /// 按系统语言优先顺序取第一个能映射到 [supportedLocales] 的项；都匹配不上则
  /// 回退 [fallbackLocale]。
  static Locale resolveFromSystemLocales(List<Locale> systemLocales) {
    for (final candidate in systemLocales) {
      final matched = matchSupportedLocale(candidate);
      if (matched != null) {
        return matched;
      }
    }
    return fallbackLocale;
  }

  /// 将任意 Locale（含 script，如 zh-Hans-CN）映射到支持的语言；无法映射返回 null。
  static Locale? matchSupportedLocale(Locale locale) {
    return parseLocale(_localeToRaw(locale));
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

  static bool isSameLocale(Locale a, Locale b) {
    return a.languageCode == b.languageCode &&
        (a.countryCode ?? '') == (b.countryCode ?? '');
  }

  /// 公开解析入口（与持久化字符串、系统 locale tag 共用）。
  static Locale? parseLocale(String? raw) {
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

  static String _localeToRaw(Locale locale) {
    final parts = <String>[locale.languageCode];
    final scriptCode = locale.scriptCode;
    if (scriptCode != null && scriptCode.isNotEmpty) {
      parts.add(scriptCode);
    }
    final countryCode = locale.countryCode;
    if (countryCode != null && countryCode.isNotEmpty) {
      parts.add(countryCode);
    }
    return parts.join('_');
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
