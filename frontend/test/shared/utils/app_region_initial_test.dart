import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:grix/shared/utils/app_region_config.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('resolveInitialRegion', () {
    test('记住的区域为 cn 时优先沿用', () async {
      SharedPreferences.setMockInitialValues({'app_region': 'cn'});
      expect(await resolveInitialRegion(), AppRegion.cn);
    });

    test('记住的区域为 global 时优先沿用', () async {
      SharedPreferences.setMockInitialValues({'app_region': 'global'});
      expect(await resolveInitialRegion(), AppRegion.global);
    });

    test('无记录时回退到按系统语言推断', () async {
      SharedPreferences.setMockInitialValues(const {});
      expect(await resolveInitialRegion(), detectRegionFromLocale());
    });

    test('记录为空串时也回退到按系统语言推断', () async {
      SharedPreferences.setMockInitialValues({'app_region': ''});
      expect(await resolveInitialRegion(), detectRegionFromLocale());
    });
  });

  group('resolveInitialRegion (web)', () {
    test('Web 端 CN 域名(grix.dhf.pub)忽略存储，识别为 cn', () async {
      SharedPreferences.setMockInitialValues({'app_region': 'global'});
      final region = await resolveInitialRegion(
        isWeb: true,
        baseUri: Uri.parse('https://grix.dhf.pub/'),
      );
      expect(region, AppRegion.cn);
    });

    test('Web 端全球区域名(gb.grix.im)忽略存储，识别为 global', () async {
      SharedPreferences.setMockInitialValues({'app_region': 'cn'});
      final region = await resolveInitialRegion(
        isWeb: true,
        baseUri: Uri.parse('https://gb.grix.im/'),
      );
      expect(region, AppRegion.global);
    });

    test('Web 端本地开发域名回退按语言推断，不读存储', () async {
      SharedPreferences.setMockInitialValues({'app_region': 'cn'});
      final region = await resolveInitialRegion(
        isWeb: true,
        baseUri: Uri.parse('http://localhost:3000/'),
      );
      // 不读 prefs，而是走 detectRegionFromLocale()
      expect(region, detectRegionFromLocale());
    });
  });
}
