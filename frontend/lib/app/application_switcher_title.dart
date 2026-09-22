import 'dart:ui' show Locale, PlatformDispatcher;

import 'package:flutter/foundation.dart';

/// Android recent-tasks / app-switcher label.
///
/// Must match `android/.../values/strings.xml` (`Grix`) and
/// `values-zh/strings.xml` (`虾塘`) — keyed off the **system** language, not
/// the in-app locale preference, so the label stays aligned with the launcher
/// icon name under review. Other platforms (web tab title etc.) keep `Grix`.
String resolveApplicationSwitcherTitle([Locale? locale]) {
  if (kIsWeb || defaultTargetPlatform != TargetPlatform.android) {
    return 'Grix';
  }
  final languageCode =
      (locale ?? PlatformDispatcher.instance.locale).languageCode;
  return languageCode == 'zh' ? '虾塘' : 'Grix';
}
