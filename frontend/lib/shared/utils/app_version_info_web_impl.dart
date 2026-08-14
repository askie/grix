import 'dart:convert';

import 'package:http/http.dart' as http;

import 'app_version_info.dart';

Future<String> loadDisplayVersion() async {
  try {
    final versionUrl = Uri.parse('/version.json');
    final response = await http.get(
      versionUrl,
      headers: {'cache-control': 'no-cache'},
    );
    if (response.statusCode != 200) {
      return AppVersionInfo.unknownDisplayVersion;
    }
    final decoded = jsonDecode(response.body);
    if (decoded is! Map<String, dynamic>) {
      return AppVersionInfo.unknownDisplayVersion;
    }

    final version = (decoded['version'] ?? '').toString();
    final buildNumber = (decoded['build_number'] ?? '').toString();
    return AppVersionInfo.formatDisplayVersion(
      version: version,
      buildNumber: buildNumber,
    );
  } catch (_) {
    return AppVersionInfo.unknownDisplayVersion;
  }
}
