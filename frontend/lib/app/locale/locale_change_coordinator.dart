import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/auth_service.dart';
import '../../data/providers/user_settings_service.dart';
import 'locale_service.dart';

class LocaleChangeCoordinator {
  LocaleChangeCoordinator._();

  static Future<bool> changeLocale(Locale newLocale) async {
    final previousLocale = Get.locale ?? const Locale('en', 'US');

    Get.updateLocale(newLocale);
    await LocaleService.saveLocale(newLocale);

    if (!_shouldSyncServer()) {
      return true;
    }

    final synced = await Get.find<UserSettingsService>().updatePreferredLanguage(
      _localeTag(newLocale),
    );
    if (synced) {
      return true;
    }

    Get.updateLocale(previousLocale);
    await LocaleService.saveLocale(previousLocale);
    return false;
  }

  static bool _shouldSyncServer() {
    if (!Get.isRegistered<AuthService>() ||
        !Get.isRegistered<UserSettingsService>()) {
      return false;
    }
    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    return userId.isNotEmpty;
  }

  static String _localeTag(Locale locale) {
    final countryCode = locale.countryCode?.trim() ?? '';
    if (countryCode.isEmpty) {
      return locale.languageCode;
    }
    return '${locale.languageCode}-$countryCode';
  }
}
