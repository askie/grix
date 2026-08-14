import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/system/agent_client_toolbar_view.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

void main() {
  tearDown(Get.reset);

  testWidgets('renders detected type and opens name-only add dialog', (
    tester,
  ) async {
    final service = GrixConnectorService()
      ..isRunning.value = true
      ..probeResults.assignAll(const [
        AgentProbeResult(
          agentName: 'openclaw-1',
          clientType: 'openclaw',
          adapterType: 'openclaw',
          status: 'healthy',
        ),
      ])
      ..installedClients.assignAll(const [
        InstalledClientCommand(clientType: 'claude', installed: true),
      ]);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('zh', 'CN'),
        home: Scaffold(body: AgentClientToolbarView(service: service)),
      ),
    );

    expect(find.text('Agent 工具栏'), findsOneWidget);
    expect(find.text('OpenClaw'), findsOneWidget);
    expect(find.text('已部署 0 · 探测 1'), findsOneWidget);
    expect(find.text('Claude'), findsOneWidget);
    expect(find.text('命令已安装 · Agent 0'), findsOneWidget);

    await tester.tap(find.text('OpenClaw'));
    await tester.pumpAndSettle();

    expect(find.text('新增'), findsOneWidget);

    await tester.tap(find.text('新增'));
    await tester.pumpAndSettle();

    expect(find.text('新增 OpenClaw agent'), findsOneWidget);
    expect(find.byType(TextField), findsOneWidget);
    expect(find.text('名称'), findsOneWidget);
    expect(find.text('类型'), findsNothing);
    expect(find.text('大模型提供商'), findsNothing);
  });
}
