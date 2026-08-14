// ignore_for_file: must_call_super

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/modules/ai/agent_conn_security_view.dart';
import 'package:grix/modules/ai/controllers/agent_conn_security_controller.dart';
import 'package:grix/modules/ai/models/agent_conn_security_model.dart';

class _FakeAgentService extends AgentService {}

/// 覆写 onInit 跳过参数解析与网络加载，直接由测试塞入 obs 数据。
class _TestConnSecurityController extends AgentConnSecurityController {
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

  Future<void> pumpView(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('en', 'US'),
        home: const AgentConnSecurityView(),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('renders login history with online/offline/geo badges and ban actions', (
    WidgetTester tester,
  ) async {
    final controller = Get.put<AgentConnSecurityController>(
      _TestConnSecurityController(),
    );
    controller.agentId.value = '9992';
    controller.agentName.value = '测试助手';
    controller.providerType.value = 3;
    controller.logs.value = [
      AgentConnectionLogEntry(
        id: '1',
        clientType: 'claude',
        clientIP: '1.1.1.1',
        ipLocation: '北京',
        isPrimary: true,
        geoChanged: false,
        allowlistMiss: false,
        disconnectReason: '',
        connectedAt: DateTime(2026, 7, 6, 10, 30),
        disconnectedAt: null,
      ),
      AgentConnectionLogEntry(
        id: '2',
        clientType: 'codex',
        clientIP: '2.2.2.2',
        ipLocation: '上海',
        isPrimary: false,
        geoChanged: true,
        allowlistMiss: false,
        disconnectReason: 'closed',
        connectedAt: DateTime(2026, 7, 6, 9, 0),
        disconnectedAt: DateTime(2026, 7, 6, 9, 30),
      ),
    ];
    // 2.2.2.2 已在黑名单：历史里应显示「已封禁」而非「加入黑名单」。
    controller.ipRules.value = [
      const AgentIPRuleEntry(
        id: '100',
        ruleType: 'ban',
        ipCidr: '2.2.2.2',
        remark: '异常登录',
      ),
    ];

    await pumpView(tester);

    // 徽标
    expect(find.text('在线'), findsOneWidget);
    expect(find.text('已断开'), findsOneWidget);
    expect(find.text('异地'), findsOneWidget);

    // 未封禁的 IP 显示「加入黑名单」按钮；已封禁的显示「已封禁」
    expect(find.text('加入黑名单'), findsOneWidget);
    expect(find.text('已封禁'), findsOneWidget);

    // 黑名单区列出该规则（2.2.2.2 同时出现在历史与黑名单区）
    expect(find.text('2.2.2.2'), findsNWidgets(2));
    expect(find.text('异常登录'), findsOneWidget);
    expect(find.text('移除'), findsOneWidget);
  });

  testWidgets('shows empty hints when there is no history or blocklist', (
    WidgetTester tester,
  ) async {
    final controller = Get.put<AgentConnSecurityController>(
      _TestConnSecurityController(),
    );
    controller.agentId.value = '9992';
    controller.providerType.value = 3;
    controller.logs.value = const [];
    controller.ipRules.value = const [];

    await pumpView(tester);

    expect(find.text('还没有黑名单 IP。'), findsOneWidget);
    expect(find.text('还没有连接记录。'), findsOneWidget);
  });

  testWidgets('shows unsupported hint for non-API agents', (
    WidgetTester tester,
  ) async {
    final controller = Get.put<AgentConnSecurityController>(
      _TestConnSecurityController(),
    );
    controller.agentId.value = '9992';
    controller.providerType.value = 1; // 非 API 接入类

    await pumpView(tester);

    expect(find.text('只有 API 接入类 Agent 才有连接记录。'), findsOneWidget);
  });
}
