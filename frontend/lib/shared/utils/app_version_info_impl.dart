import 'package:package_info_plus/package_info_plus.dart';

import 'app_version_info.dart';

Future<String> loadDisplayVersion() async {
  try {
    final packageInfo = await PackageInfo.fromPlatform();
    return AppVersionInfo.formatDisplayVersion(
      version: packageInfo.version,
      buildNumber: packageInfo.buildNumber,
    );
  } catch (_) {
    return AppVersionInfo.unknownDisplayVersion;
  }
}
