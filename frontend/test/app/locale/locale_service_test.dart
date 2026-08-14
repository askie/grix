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
  });
}
