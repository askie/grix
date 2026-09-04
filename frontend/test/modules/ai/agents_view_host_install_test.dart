import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_category_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/feature_flag_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/ai/agents_view.dart';
import 'package:grix/modules/ai/controllers/agents_controller.dart';

class _FakeAuthService extends AuthService {
  @override
  String? get userId => 'test-owner';

  @override
  void attachAuthInterceptor(Dio dio) {}
}

class _FakeFeatureFlagService extends FeatureFlagService {
  @override
  bool isEnabled(String key) => false;
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}

  @override
  bool isOwnedByMe(AgentModel agent) => true;
}

class _FakeImService extends ImService {
  @override
  bool isAgentChannelOnline(String agentId) => false;

  @override
  bool hasAgentChannelState(String agentId) => false;

  @override
  void refreshAgentOnlineStates() {}
}

class _FakeAgentCategoryService extends AgentCategoryService {
  @override
  Future<void> restoreCachedCategories() async {}

  @override
  Future<void> syncCategoriesFromRemote() async {}

  @override
  Future<void> loadCategories() async {}
}

AgentModel _apiAgent({
  required String id,
  required String name,
  required String hostname,
  bool online = true,
  bool supportsConnectorAdmin = false,
}) {
  return AgentModel(
    id: id,
    agentName: name,
    providerType: 3,
    agentClientType: 'claude',
    sessionId: 'session-$id',
    hostname: hostname,
    online: online,
    supportsConnectorAdmin: supportsConnectorAdmin,
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAgentService agentService;
  late AgentsController controller;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    agentService = _FakeAgentService();
    Get.put<AuthService>(_FakeAuthService());
    Get.put<FeatureFlagService>(_FakeFeatureFlagService());
    Get.put<AgentService>(agentService);
    Get.put<AgentCategoryService>(_FakeAgentCategoryService());
    Get.put<ImService>(_FakeImService());
    controller = Get.put(AgentsController());
  });

  tearDown(Get.reset);

  Widget buildApp() {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      fallbackLocale: const Locale('en', 'US'),
      home: const AgentsView(),
      getPages: [
        GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
      ],
    );
  }

  Future<void> pumpHostView(WidgetTester tester) async {
    await tester.pumpWidget(buildApp());
    controller.viewMode.value = 1;
    await tester.pumpAndSettle();
  }

  // 归类守卫：有 host_name 的按机器分组，没上报过的（老 connector）归"未知主机"，
  // 而不是散在外面没有组头 —— 否则那台机器上就没有"安装 agent"入口。
  testWidgets('groups agents by host and buckets unreported ones', (
    tester,
  ) async {
    agentService.agents.assignAll([
      _apiAgent(id: 'a1', name: 'Alpha', hostname: 'gcf-mac'),
      _apiAgent(id: 'a2', name: 'Beta', hostname: ''),
    ]);

    await pumpHostView(tester);

    expect(find.text('gcf-mac'), findsOneWidget);
    expect(find.text('Unknown Host'), findsOneWidget);
    expect(find.byKey(const Key('host-install-gcf-mac')), findsOneWidget);
    expect(find.byKey(const Key('host-install-_unknown_')), findsOneWidget);
  });

  // 该主机没有在线的自有 agent 时按钮置灰：没有通道就发不出指令，
  // 让用户点了再报错不如直接看出不可用。
  testWidgets('install button is disabled when the host has no online agent', (
    tester,
  ) async {
    agentService.agents.assignAll([
      _apiAgent(
        id: 'a1',
        name: 'Alpha',
        hostname: 'gcf-mac',
        online: false,
        supportsConnectorAdmin: true,
      ),
    ]);

    await pumpHostView(tester);

    final button = tester.widget<IconButton>(
      find.byKey(const Key('host-install-gcf-mac')),
    );
    expect(button.onPressed, isNull);
  });

  // 有在线 agent 就可点：能不能真装取决于连接器是否声明 connector_admin，
  // 点下去才给出"请升级连接器"的具体提示。
  testWidgets('install button is enabled when the host has an online agent', (
    tester,
  ) async {
    agentService.agents.assignAll([
      _apiAgent(id: 'a1', name: 'Alpha', hostname: 'gcf-mac'),
    ]);

    await pumpHostView(tester);

    final button = tester.widget<IconButton>(
      find.byKey(const Key('host-install-gcf-mac')),
    );
    expect(button.onPressed, isNotNull);
  });
}
