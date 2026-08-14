// ignore_for_file: must_call_super

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/modules/ai/agent_scope_view.dart';
import 'package:grix/modules/ai/controllers/agent_scope_controller.dart';

class _FakeAgentService extends AgentService {}

class _TestAgentScopeController extends AgentScopeController {
  @override
  void onInit() {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    Get.put<AgentService>(_FakeAgentService());
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('renders translated labels for agent category scopes', (
    WidgetTester tester,
  ) async {
    final controller = Get.put<AgentScopeController>(
      _TestAgentScopeController(),
    );
    controller.agentId.value = '9992';
    controller.agentName.value = '分类助手';
    controller.providerType.value = 3;
    controller.availableScopes.value = const [
      'agent.category.list',
      'agent.category.create',
      'agent.category.update',
      'agent.category.assign',
    ];
    controller.selectedScopes.value = const ['agent.category.assign'];

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('en', 'US'),
        home: const AgentScopeView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('查看 Agent 分类'), findsOneWidget);
    expect(find.text('创建 Agent 分类'), findsOneWidget);
    expect(find.text('修改 Agent 分类'), findsOneWidget);
    expect(find.text('设置 Agent 分类'), findsOneWidget);

    expect(find.text('允许查看当前账号下的 Agent 分类列表。'), findsOneWidget);
    expect(find.text('允许创建新的 Agent 分类。'), findsOneWidget);
    expect(find.text('允许修改已有 Agent 分类。'), findsOneWidget);
    expect(find.text('允许为 Agent 设置或清空分类。'), findsOneWidget);

    expect(find.text('agent.category.list'), findsNothing);
    expect(find.text('agent.category.create'), findsNothing);
    expect(find.text('agent.category.update'), findsNothing);
    expect(find.text('agent.category.assign'), findsNothing);
  });
}
