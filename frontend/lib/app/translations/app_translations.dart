import 'dart:convert';

import 'package:flutter/services.dart';
import 'package:get/get.dart';

class AppTranslations extends Translations {
  /// 测试环境由 flutter_test_config.dart 预加载后写入此字段，
  /// 使 AppTranslations() 无参构造能返回真实 en_US 翻译。
  static Map<String, Map<String, String>> _testKeys = const {};
  // ignore: use_setters_to_change_properties
  static set testKeys(Map<String, Map<String, String>> v) => _testKeys = v;

  AppTranslations([Map<String, Map<String, String>>? keys])
    : _keys = keys ?? _testKeys;

  final Map<String, Map<String, String>> _keys;

  @override
  Map<String, Map<String, String>> get keys => _keys;

  /// 支持的 locale → asset 文件名映射
  static const Map<String, String> _localeFiles = {
    'en_US': 'assets/i18n/en_US.json',
    'zh_CN': 'assets/i18n/zh_CN.json',
    'ja_JP': 'assets/i18n/ja_JP.json',
    'ko_KR': 'assets/i18n/ko_KR.json',
    'de_DE': 'assets/i18n/de_DE.json',
    'fr_FR': 'assets/i18n/fr_FR.json',
    'es_ES': 'assets/i18n/es_ES.json',
    'pt_BR': 'assets/i18n/pt_BR.json',
    'ru_RU': 'assets/i18n/ru_RU.json',
    'ar': 'assets/i18n/ar.json',
    'hi_IN': 'assets/i18n/hi_IN.json',
  };

  static Future<AppTranslations> load() async {
    final result = <String, Map<String, String>>{};

    for (final entry in _localeFiles.entries) {
      try {
        final raw = await rootBundle.loadString(entry.value);
        final decoded = json.decode(raw) as Map<String, dynamic>;
        result[entry.key] = decoded.map((k, v) => MapEntry(k, v.toString()));
      } catch (e) {
        // 加载失败时跳过，不影响其他语言
        assert(() {
          // ignore: avoid_print
          print('AppTranslations: failed to load ${entry.value}: $e');
          return true;
        }());
      }
    }

    // 语言别名：确保 language-only locale 也能命中
    if (result.containsKey('en_US')) result['en'] = result['en_US']!;
    if (result.containsKey('zh_CN')) result['zh'] = result['zh_CN']!;
    if (result.containsKey('ja_JP')) result['ja'] = result['ja_JP']!;
    if (result.containsKey('ko_KR')) result['ko'] = result['ko_KR']!;
    if (result.containsKey('de_DE')) result['de'] = result['de_DE']!;
    if (result.containsKey('fr_FR')) result['fr'] = result['fr_FR']!;
    if (result.containsKey('es_ES')) result['es'] = result['es_ES']!;
    if (result.containsKey('pt_BR')) result['pt'] = result['pt_BR']!;
    if (result.containsKey('ru_RU')) result['ru'] = result['ru_RU']!;
    if (result.containsKey('hi_IN')) result['hi'] = result['hi_IN']!;

    return AppTranslations(result);
  }
}
