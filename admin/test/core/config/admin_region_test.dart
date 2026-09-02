// 默认 `flutter test` 跑在 Dart VM 上（kIsWeb 恒为 false），覆盖原生端分支和
// AdminRegionStore 的"未选择/已选择"状态语义。
// Web 端专属分支（同源相对路径、按当前域名推断展示区域、区域选择器展示条件）
// 只有在 `flutter test --platform chrome test/core/config/admin_region_test.dart`
// 下才会真正执行——这是 kIsWeb 恒为 true 的唯一测试环境。
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/core/config/admin_region.dart';
import 'package:grix_admin/core/config/app_config.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // AdminRegionStore 是纯静态状态，每个 test 跑前重置本地存储 + 重新 load，
  // 避免上一个 test 的显式选择通过 SharedPreferences 持久化污染下一个 test。
  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    await AdminRegionStore.load();
  });

  test('AdminRegionStore: 未选择过区域时不应被当成"已选择 CN"', () {
    expect(AdminRegionStore.hasExplicitChoice, isFalse);
  });

  test('AdminRegionStore: 显式选择会被记住，且可来回切换', () async {
    await AdminRegionStore.save(AdminRegion.cn);
    expect(AdminRegionStore.hasExplicitChoice, isTrue);
    expect(AdminRegionStore.current, AdminRegion.cn);

    await AdminRegionStore.save(AdminRegion.global);
    expect(AdminRegionStore.current, AdminRegion.global);
  });

  test('AppConfig.apiRootForRegion 是纯函数：只看传入的 region，返回对应绝对域名', () {
    expect(
      AppConfig.apiRootForRegion(AdminRegion.cn),
      '$kCnAdminApiBase${AppConfig.adminApiPrefix}',
    );
    expect(
      AppConfig.apiRootForRegion(AdminRegion.global),
      '$kGlobalAdminApiBase${AppConfig.adminApiPrefix}',
    );
  });

  if (!kIsWeb) {
    test('原生端 publicOrigin：未选区默认 CN 绝对域名，显式选全球后跟着切', () async {
      expect(AppConfig.publicOrigin, kCnAdminApiBase);

      await AdminRegionStore.save(AdminRegion.global);
      expect(AppConfig.publicOrigin, kGlobalAdminApiBase);
    });
  }

  // regionForHost / showSelectorForHost 不依赖 kIsWeb，直接喂域名字符串即可在 VM
  // 上把 Web 端"按当前域名推断"这条修复本身的正负分支全部覆盖，不用依赖 Chrome。
  group('按host推断区域/是否展示选择器（本次修复的核心分支）', () {
    test('全球区官方域名 → 展示全球，且展示选择器', () {
      expect(AdminRegionStore.regionForHost('gb.grix.im'), AdminRegion.global);
      expect(AdminRegionStore.showSelectorForHost('gb.grix.im'), isTrue);
    });

    test('CN官方域名 → 展示CN，且展示选择器', () {
      expect(AdminRegionStore.regionForHost('grix.dhf.pub'), AdminRegion.cn);
      expect(AdminRegionStore.showSelectorForHost('grix.dhf.pub'), isTrue);
    });

    test('非官方域名(本地/自建部署) → 展示CN兜底，但不展示选择器', () {
      expect(AdminRegionStore.regionForHost('127.0.0.1'), AdminRegion.cn);
      expect(AdminRegionStore.showSelectorForHost('127.0.0.1'), isFalse);
      expect(AdminRegionStore.showSelectorForHost('localhost'), isFalse);
    });
  });

  if (!kIsWeb) {
    test('原生端（本测试所在的 VM 环境）：apiRoot 不管有没有显式选择过区域，始终打绝对域名', () async {
      expect(AppConfig.apiRoot, '$kCnAdminApiBase${AppConfig.adminApiPrefix}');

      await AdminRegionStore.save(AdminRegion.global);
      expect(
        AppConfig.apiRoot,
        '$kGlobalAdminApiBase${AppConfig.adminApiPrefix}',
      );
    });
  }

  if (kIsWeb) {
    test('Web 端：未显式选择区域时走同源相对路径，不打任何绝对域名', () {
      expect(AppConfig.apiRoot, AppConfig.adminApiPrefix);
    });

    test('Web 端：一旦显式选择过区域，就按选择打对应绝对域名（跨域）', () async {
      await AdminRegionStore.save(AdminRegion.global);
      expect(
        AppConfig.apiRoot,
        '$kGlobalAdminApiBase${AppConfig.adminApiPrefix}',
      );

      await AdminRegionStore.save(AdminRegion.cn);
      expect(AppConfig.apiRoot, '$kCnAdminApiBase${AppConfig.adminApiPrefix}');
    });

    test('Web 端 shouldShowSelector：只在命中官方两个域名时才为 true（此测试跑在测试域名下，应为 false）', () {
      // flutter test --platform chrome 里 Uri.base.host 不是 grix.dhf.pub / gb.grix.im，
      // 所以这里断言的是"非官方域名不展示选择器"这条分支。
      expect(AdminRegionStore.shouldShowSelector, isFalse);
    });
  }
}
