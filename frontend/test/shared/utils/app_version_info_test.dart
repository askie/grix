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

    test('formatDisplayVersion keeps large build numbers verbatim', () {
      // 安卓改成 universal APK 后 versionCode 就是 pubspec 构建号（3000 起），
      // 不能再按 --split-per-abi 的 2000 偏移做 % 1000——那会显示成 (0)。
      final displayVersion = AppVersionInfo.formatDisplayVersion(
        version: '3.2.7',
        buildNumber: '3000',
      );

      expect(displayVersion, '3.2.7 (3000)');
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
