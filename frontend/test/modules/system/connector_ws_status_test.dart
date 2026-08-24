import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/system/connector_status_view.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

/// 连接器活着 ≠ agent 可达：WS 断服和升级事务必须在状态页如实亮出来。
void main() {
  tearDown(Get.reset);

  GrixConnectorService putService() {
    final service = GrixConnectorService()
      ..isInstalled.value = true
      ..isRunning.value = true
      ..installedVersion.value = '4.2.0'
      ..latestVersion.value = '4.2.0';
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

  testWidgets('部分 agent 断服：显示连接比例和警示，不显示成一切正常', (tester) async {
    putService()
      ..wsConnected.value = 1
      ..wsTotal.value = 3;

    await pumpView(tester);

    expect(find.text('1/3'), findsOneWidget);
    expect(find.textContaining('其余重连中'), findsOneWidget);
  });

  testWidgets('全部在线：只显示比例，不出警示', (tester) async {
    putService()
      ..wsConnected.value = 3
      ..wsTotal.value = 3;

    await pumpView(tester);

    expect(find.text('3/3'), findsOneWidget);
    expect(find.textContaining('其余重连中'), findsNothing);
  });

  testWidgets('老版本连接器没有 ws 字段：不显示连接行', (tester) async {
    putService();

    await pumpView(tester);

    expect(find.text('服务端连接'), findsNothing);
  });

  testWidgets('升级事务进行中：显示升级提示', (tester) async {
    putService()
      ..upgradeInProgress.value = true
      ..upgradePhase.value = 'activating';

    await pumpView(tester);

    expect(find.textContaining('升级进行中'), findsOneWidget);
    expect(find.textContaining('activating'), findsOneWidget);
  });
}
