import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/settings/theme_preference_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('ThemePreferenceService', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test(
      'defaults to following the system when no preference exists',
      () async {
        final service = await ThemePreferenceService().init();

        expect(service.themeMode, ThemeMode.system);
      },
    );

    test('treats unknown stored values as system', () async {
      SharedPreferences.setMockInitialValues({
        ThemePreferenceService.prefsKey: 'bogus',
      });

      final service = await ThemePreferenceService().init();

      expect(service.themeMode, ThemeMode.system);
    });

    test('restores saved dark mode preference from local storage', () async {
      SharedPreferences.setMockInitialValues({
        ThemePreferenceService.prefsKey: 'dark',
      });

      final service = await ThemePreferenceService().init();

      expect(service.themeMode, ThemeMode.dark);
      expect(service.isDarkMode, isTrue);
    });

    test('restores saved light mode preference from local storage', () async {
      SharedPreferences.setMockInitialValues({
        ThemePreferenceService.prefsKey: 'light',
      });

      final service = await ThemePreferenceService().init();

      expect(service.themeMode, ThemeMode.light);
      expect(service.isDarkMode, isFalse);
    });

    test('persists user-selected theme mode locally', () async {
      final service = await ThemePreferenceService().init();

      await service.setThemeMode(ThemeMode.dark);
      expect(service.themeMode, ThemeMode.dark);

      var prefs = await SharedPreferences.getInstance();
      expect(prefs.getString(ThemePreferenceService.prefsKey), 'dark');

      await service.setThemeMode(ThemeMode.system);
      prefs = await SharedPreferences.getInstance();
      expect(prefs.getString(ThemePreferenceService.prefsKey), 'system');
    });

    test('toggle pins the opposite of the effective brightness', () async {
      final service = await ThemePreferenceService().init();
      final wasDark = service.isDarkMode;

      await service.toggle();

      expect(service.themeMode, wasDark ? ThemeMode.light : ThemeMode.dark);
    });
  });
}
