import 'dart:ui' show Locale;

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/application_switcher_title.dart';

void main() {
  group('resolveApplicationSwitcherTitle', () {
    test('zh and zh_TW match values-zh app_name', () {
      expect(
        resolveApplicationSwitcherTitle(const Locale('zh')),
        '虾塘',
      );
      expect(
        resolveApplicationSwitcherTitle(const Locale('zh', 'TW')),
        '虾塘',
      );
    });

    test('non-zh locales match default app_name', () {
      expect(
        resolveApplicationSwitcherTitle(const Locale('en')),
        'Grix',
      );
      expect(
        resolveApplicationSwitcherTitle(const Locale('ja')),
        'Grix',
      );
    });

    test('non-Android platforms keep Grix', () {
      debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
      addTearDown(() => debugDefaultTargetPlatformOverride = null);
      expect(
        resolveApplicationSwitcherTitle(const Locale('zh')),
        'Grix',
      );
    });
  });
}
