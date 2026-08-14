import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:package_info_plus/package_info_plus.dart';

import 'package:grix/data/providers/app_update_service.dart';
import 'package:grix/modules/profile/about_view.dart';

/// 关于页的版本号是手动检查更新的唯一入口（桌面端另有托盘菜单）。
/// 这里锁住的是：点得动、且点下去真的会去查更新——而不是一段死文本。
class _FakeAppUpdateService extends AppUpdateService {
  int interactiveCalls = 0;
  final _gate = Completer<void>();

  @override
  Future<void> checkForUpdateInteractive() async {
    interactiveCalls++;
    await _gate.future; // 停在"检查中"，便于断言 loading 态与防连点
  }

  void finish() {
    if (!_gate.isCompleted) _gate.complete();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  PackageInfo.setMockInitialValues(
    appName: 'Grix',
    packageName: 'pub.dhf.grix',
    version: '3.1.4',
    buildNumber: '694',
    buildSignature: '',
  );

  late _FakeAppUpdateService service;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    service = _FakeAppUpdateService();
    Get.put<AppUpdateService>(service);
  });

  tearDown(() {
    service.finish();
    Get.reset();
  });

  testWidgets('点关于页的版本号会触发检查更新，检查期间显示进度且不重复触发', (tester) async {
    await tester.pumpWidget(GetMaterialApp(home: const AboutView()));
    await tester.pump();

    expect(service.interactiveCalls, 0);

    // 版本号旁边的刷新图标就是"可点"的提示
    expect(find.byIcon(Icons.refresh_rounded), findsOneWidget);

    await tester.tap(find.byIcon(Icons.refresh_rounded));
    await tester.pump();

    expect(
      service.interactiveCalls,
      1,
      reason: '点版本号必须真的去查更新，不能是个装饰',
    );

    // 检查中：进度圈取代刷新图标
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    expect(find.byIcon(Icons.refresh_rounded), findsNothing);

    // 检查中连点不应叠加请求，否则会弹出多个更新对话框
    await tester.tap(
      find.byType(CircularProgressIndicator),
      warnIfMissed: false,
    );
    await tester.pump();
    expect(service.interactiveCalls, 1, reason: '检查进行中不应重复触发');

    // 检查结束后，入口恢复可点。
    // 不能用 pumpAndSettle：进度圈是无限循环动画，永远等不到静止。
    await tester.runAsync(() async {
      service.finish();
      await Future<void>.delayed(Duration.zero); // 放行 await 的续体
    });
    await tester.pump();
    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(find.byIcon(Icons.refresh_rounded), findsOneWidget);
  });
}
