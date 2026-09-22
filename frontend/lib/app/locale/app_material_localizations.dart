import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'locale_service.dart';

/// Shared Material localization wiring for [GrixApp] and the Android
/// first-launch privacy gate [MaterialApp]. Keep both in sync via this file.
class AppMaterialLocalizations {
  AppMaterialLocalizations._();

  static const List<LocalizationsDelegate<dynamic>> delegates =
      <LocalizationsDelegate<dynamic>>[
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ];

  static List<Locale> get supportedLocales => LocaleService.supportedLocales
      .map((entry) => entry.locale)
      .toList(growable: false);
}
