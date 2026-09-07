import 'app_version_info_impl.dart'
    if (dart.library.js_interop) 'app_version_info_web_impl.dart'
    as impl;

class AppVersionInfo {
  AppVersionInfo._();

  static const String unknownDisplayVersion = '--';

  static Future<String>? _displayVersionFuture;

  static Future<String> loadDisplayVersion() {
    return _displayVersionFuture ??= impl.loadDisplayVersion();
  }

  static String formatDisplayVersion({
    required String version,
    required String buildNumber,
  }) {
    final normalizedVersion = version.trim();
    final normalizedBuildNumber = buildNumber.trim();
    if (normalizedVersion.isEmpty) {
      return unknownDisplayVersion;
    }
    if (normalizedBuildNumber.isEmpty) {
      return normalizedVersion;
    }
    // 安卓曾经用 --split-per-abi，versionCode 会被加上 ABI 前缀（arm64 = 2000 +
    // 真实构建号），显示时得剥掉。现在 CI 出的是 universal APK，versionCode 就是
    // pubspec 构建号，不能再做任何换算——否则 3000 会被显示成 0。
    return '$normalizedVersion ($normalizedBuildNumber)';
  }
}
