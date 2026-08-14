import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:image_cropper/image_cropper.dart';

class AvatarCropStrings {
  const AvatarCropStrings({
    required this.title,
    required this.hint,
    required this.zoomLabel,
    required this.zoomOutLabel,
    required this.zoomInLabel,
    required this.cancelLabel,
    required this.saveLabel,
    required this.rotateLeftTooltip,
    required this.rotateRightTooltip,
  });

  final String title;
  final String hint;
  final String zoomLabel;
  final String zoomOutLabel;
  final String zoomInLabel;
  final String cancelLabel;
  final String saveLabel;
  final String rotateLeftTooltip;
  final String rotateRightTooltip;

  WebTranslations toWebTranslations() {
    return WebTranslations(
      title: title,
      rotateLeftTooltip: rotateLeftTooltip,
      rotateRightTooltip: rotateRightTooltip,
      cancelButton: cancelLabel,
      cropButton: saveLabel,
    );
  }
}

class AvatarCropLocalizations {
  AvatarCropLocalizations._();

  static const String _defaultLocaleKey = 'en_US';
  static const String _zhLocaleKey = 'zh_CN';

  static AvatarCropStrings resolve({Locale? locale}) {
    final localeKey = _localeKeyFor(locale);
    final translations = Get.translations;
    final localizedMap = translations[localeKey] ?? const <String, String>{};
    final fallbackMap =
        translations[_defaultLocaleKey] ?? const <String, String>{};

    String text(String key) => localizedMap[key] ?? fallbackMap[key] ?? key;

    return AvatarCropStrings(
      title: text('profile_avatar_crop_title'),
      hint: text('profile_avatar_crop_hint'),
      zoomLabel: text('profile_avatar_crop_zoom'),
      zoomOutLabel: text('profile_avatar_crop_zoom_out'),
      zoomInLabel: text('profile_avatar_crop_zoom_in'),
      cancelLabel: text('common_cancel'),
      saveLabel: text('common_save'),
      rotateLeftTooltip: text('profile_avatar_crop_rotate_left'),
      rotateRightTooltip: text('profile_avatar_crop_rotate_right'),
    );
  }

  static String _localeKeyFor(Locale? locale) {
    if (locale == null) return _defaultLocaleKey;
    final lang = locale.languageCode.toLowerCase();
    if (lang.startsWith('zh')) return _zhLocaleKey;
    final country = locale.countryCode?.toUpperCase() ?? '';
    if (country.isNotEmpty) return '${lang}_$country';
    return lang;
  }
}
