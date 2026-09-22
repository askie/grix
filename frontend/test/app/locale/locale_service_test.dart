import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/locale/locale_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('LocaleService', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('loadSavedLocale returns null when no preference exists', () async {
      final locale = await LocaleService.loadSavedLocale();
      expect(locale, isNull);
    });

    test('saveLocale persists and loadSavedLocale restores locale', () async {
      await LocaleService.saveLocale(const Locale('zh', 'CN'));

      final locale = await LocaleService.loadSavedLocale();
      expect(locale, const Locale('zh', 'CN'));
    });

    test('resolveEffectiveLocaleSync prefers saved locale over system', () {
      final resolved = LocaleService.resolveEffectiveLocaleSync(
        saved: const Locale('ja', 'JP'),
        systemLocales: const [Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hans', countryCode: 'CN')],
      );
      expect(resolved, const Locale('ja', 'JP'));
    });

    test('maps zh-Hans-CN system locale to zh_CN', () {
      final resolved = LocaleService.resolveEffectiveLocaleSync(
        systemLocales: const [
          Locale.fromSubtags(
            languageCode: 'zh',
            scriptCode: 'Hans',
            countryCode: 'CN',
          ),
        ],
      );
      expect(resolved, const Locale('zh', 'CN'));
    });

    test('maps zh-TW system locale to zh_CN', () {
      final resolved = LocaleService.resolveEffectiveLocaleSync(
        systemLocales: const [Locale('zh', 'TW')],
      );
      expect(resolved, const Locale('zh', 'CN'));
    });

    test('maps zh-HK system locale to zh_CN', () {
      final resolved = LocaleService.resolveEffectiveLocaleSync(
        systemLocales: const [Locale('zh', 'HK')],
      );
      expect(resolved, const Locale('zh', 'CN'));
    });

    test('maps ja-JP system locale to ja_JP', () {
      final resolved = LocaleService.resolveEffectiveLocaleSync(
        systemLocales: const [Locale('ja', 'JP')],
      );
      expect(resolved, const Locale('ja', 'JP'));
    });

    test('unsupported system locale falls back to en_US', () {
      final resolved = LocaleService.resolveEffectiveLocaleSync(
        systemLocales: const [Locale('sv', 'SE')],
      );
      expect(resolved, LocaleService.fallbackLocale);
    });

    test('picks first matching system locale by priority order', () {
      final resolved = LocaleService.resolveEffectiveLocaleSync(
        systemLocales: const [
          Locale('sv', 'SE'),
          Locale('ja', 'JP'),
          Locale('zh', 'CN'),
        ],
      );
      expect(resolved, const Locale('ja', 'JP'));
    });

    test('resolveEffectiveLocale does not persist system locale', () async {
      final before = await LocaleService.loadSavedLocale();
      expect(before, isNull);

      final resolved = await LocaleService.resolveEffectiveLocale(
        systemLocales: const [Locale('zh', 'CN')],
      );
      expect(resolved, const Locale('zh', 'CN'));

      final after = await LocaleService.loadSavedLocale();
      expect(after, isNull);
      expect(await LocaleService.hasSavedLocale(), isFalse);
    });
  });
}
