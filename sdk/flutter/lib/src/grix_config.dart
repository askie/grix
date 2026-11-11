import 'package:flutter/material.dart';

/// Optional visitor identity passed to `GrixWidget.configure`.
@immutable
class GrixUser {
  const GrixUser({this.name, this.email});

  final String? name;
  final String? email;
}

/// Configuration for the Grix Website Widget Flutter SDK.
@immutable
class GrixConfig {
  const GrixConfig({
    required this.siteKey,
    this.apiBase = defaultApiBase,
    this.locale,
    this.themeColor,
    this.title,
    this.user,
    this.appIdOverride,
  });

  static const String defaultApiBase = 'https://gb.grix.im';
  static const String sdkVersion = '1.0.0';

  final String siteKey;
  final String apiBase;
  final String? locale;
  final String? themeColor;
  final String? title;
  final GrixUser? user;

  /// Optional override for tests or white-label hosts. Normal hosts should
  /// leave this null so the host bundle id / package name is used.
  final String? appIdOverride;

  String get normalizedSiteKey => siteKey.trim();

  String get normalizedApiBase {
    final normalized = apiBase.trim().replaceFirst(RegExp(r'/+$'), '');
    return normalized.isEmpty ? defaultApiBase : normalized;
  }

  String resolveAppId(String hostPackageName) {
    final override = appIdOverride?.trim() ?? '';
    return override.isNotEmpty ? override : hostPackageName.trim();
  }

  Color get resolvedThemeColor {
    final raw = themeColor?.trim() ?? '';
    if (raw.isEmpty) return const Color(0xFF0F766E);
    final normalized = raw.startsWith('#') ? raw.substring(1) : raw;
    if (normalized.length == 6) {
      final value = int.tryParse(normalized, radix: 16);
      if (value != null) return Color(0xFF000000 | value);
    }
    if (normalized.length == 8) {
      final value = int.tryParse(normalized, radix: 16);
      if (value != null) return Color(value);
    }
    return const Color(0xFF0F766E);
  }
}
