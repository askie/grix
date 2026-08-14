import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/system/connector_status_view.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

/// 连接器升级按钮：升级指令只下发一次，且下发状态不能残留到下一个版本。
void main() {
  tearDown(Get.reset);

  GrixConnectorService putService({
    required String installed,
    required String latest,
  }) {
    final service = GrixConnectorService()
      ..isInstalled.value = true
      ..isRunning.value = true
      ..installedVersion.value = installed
      ..latestVersion.value = latest;
    Get.put<GrixConnectorService>(service);
    return service;
  }

  Future<void> pumpView(WidgetTester tester) => tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          fallbackLocale: const Locale('zh', 'CN'),
          home: const Scaffold(body: ConnectorStatusView()),
        ),
      );

  testWidgets('有新版且未下发过：显示升级按钮', (tester) async {
    putService(installed: '3.4.0', latest: '3.5.0');

    await pumpView(tester);

    expect(find.text('更新到 3.5.0'), findsOneWidget);
    expect(find.textContaining('无需等待'), findsNothing);
  });

  testWidgets('指令已下发：按钮换成等待说明，不让用户重复点', (tester) async {
    final service = putService(installed: '3.4.0', latest: '3.5.0');
    service.upgradeQueuedVersion.value = '3.5.0';

    await pumpView(tester);

    expect(find.text('更新到 3.5.0'), findsNothing);
    expect(find.textContaining('已通知后台升级到 3.5.0'), findsOneWidget);
    expect(find.textContaining('无需等待'), findsOneWidget);
  });

  // 下发状态若记成一个 bool，升完这一版就再也不会复位：等下一个版本出来时，
  // 按钮会一上来就显示成"已通知升级"，用户根本点不动。
  testWidgets('升完一版后又出新版：升级按钮必须回来，而不是残留等待说明', (tester) async {
    final service = putService(installed: '3.5.0', latest: '3.6.0');
    service.upgradeQueuedVersion.value = '3.5.0'; // 上一轮升级留下的状态

    await pumpView(tester);

    expect(find.text('更新到 3.6.0'), findsOneWidget);
    expect(find.textContaining('已通知后台升级'), findsNothing);
  });
}
