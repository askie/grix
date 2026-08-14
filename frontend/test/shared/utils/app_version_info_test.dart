import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/app_version_info.dart';

void main() {
  group('AppVersionInfo', () {
    test('formatDisplayVersion joins version and build number', () {
      final displayVersion = AppVersionInfo.formatDisplayVersion(
        version: '1.0.3',
        buildNumber: '4',
      );

      expect(displayVersion, '1.0.3 (4)');
    });

    test('formatDisplayVersion trims whitespace', () {
      final displayVersion = AppVersionInfo.formatDisplayVersion(
        version: ' 1.0.3 ',
        buildNumber: ' 4 ',
      );

      expect(displayVersion, '1.0.3 (4)');
    });

    test('formatDisplayVersion returns version only when build is empty', () {
      final displayVersion = AppVersionInfo.formatDisplayVersion(
        version: '1.0.3',
        buildNumber: '',
      );

      expect(displayVersion, '1.0.3');
    });

    test('formatDisplayVersion returns placeholder when version is empty', () {
      final displayVersion = AppVersionInfo.formatDisplayVersion(
        version: ' ',
        buildNumber: '4',
      );

      expect(displayVersion, '--');
    });
  });
}
