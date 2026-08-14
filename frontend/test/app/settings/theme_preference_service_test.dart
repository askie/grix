import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/settings/theme_preference_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  group('ThemePreferenceService', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('defaults to light mode when no saved preference exists', () async {
      final service = await ThemePreferenceService().init();

      expect(service.themeMode, ThemeMode.light);
      expect(service.isDarkMode, isFalse);
    });

    test('restores saved dark mode preference from local storage', () async {
      SharedPreferences.setMockInitialValues({
        ThemePreferenceService.prefsKey: 'dark',
      });

      final service = await ThemePreferenceService().init();

      expect(service.themeMode, ThemeMode.dark);
      expect(service.isDarkMode, isTrue);
    });

    test('persists user-selected theme mode locally', () async {
      final service = await ThemePreferenceService().init();

      await service.setDarkModeEnabled(true);

      expect(service.themeMode, ThemeMode.dark);

      final prefs = await SharedPreferences.getInstance();
      expect(
        prefs.getString(ThemePreferenceService.prefsKey),
        'dark',
      );
    });
  });
}
