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
    var normalizedBuildNumber = buildNumber.trim();
    if (normalizedVersion.isEmpty) {
      return unknownDisplayVersion;
    }
    if (normalizedBuildNumber.isEmpty) {
      return normalizedVersion;
    }
    // Android --split-per-abi adds ABI prefix (arm64 = 2000 + real build).
    final buildNum = int.tryParse(normalizedBuildNumber);
    if (buildNum != null && buildNum >= 1000) {
      normalizedBuildNumber = '${buildNum % 1000}';
    }
    return '$normalizedVersion ($normalizedBuildNumber)';
  }
}
