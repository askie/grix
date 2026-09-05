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
import 'package:grix/data/providers/session_service.dart';
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
  final sent = <({String sessionId, String content})>[];

  @override
  bool isAgentChannelOnline(String agentId) => false;

  @override
  bool hasAgentChannelState(String agentId) => false;

  @override
  void refreshAgentOnlineStates() {}

  /// 通道候选存在时点按钮会打开安装弹窗，它一进来就拉可安装列表。
  /// 这里给个空列表，免得测试打到真实网络。
  @override
  Future<dynamic> requestConnectorAdmin({
    required String agentId,
    required String op,
    Map<String, dynamic>? args,
  }) async {
    if (op == 'list_installable') {
      return {'platform': 'darwin', 'agents': <dynamic>[]};
    }
    return null;
  }

  @override
  Future<void> sendMessage(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) async {
    sent.add((sessionId: sessionId, content: content));
  }
}

class _FakeSessionService extends SessionService {
  final opened = <String>[];

  @override
  Future<String?> openLatestSession(String peerId, int peerType) async {
    opened.add('$peerId:$peerType');
    return 'session-$peerId';
  }
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
  String clientType = 'claude',
}) {
  return AgentModel(
    id: id,
    agentName: name,
    providerType: 3,
    agentClientType: clientType,
    sessionId: 'session-$id',
    hostname: hostname,
    online: online,
    supportsConnectorAdmin: supportsConnectorAdmin,
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAgentService agentService;
  late _FakeImService imService;
  late _FakeSessionService sessionService;
  late AgentsController controller;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    agentService = _FakeAgentService();
    imService = _FakeImService();
    sessionService = _FakeSessionService();
    Get.put<AuthService>(_FakeAuthService());
    Get.put<FeatureFlagService>(_FakeFeatureFlagService());
    Get.put<AgentService>(agentService);
    Get.put<AgentCategoryService>(_FakeAgentCategoryService());
    Get.put<ImService>(imService);
    Get.put<SessionService>(sessionService);
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

  String hostButtonTooltip(WidgetTester tester, String host) {
    return tester
        .widget<Tooltip>(
          find.ancestor(
            of: find.byKey(Key('host-install-$host')),
            matching: find.byType(Tooltip),
          ),
        )
        .message!;
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
    // 提示要说清是"没装连接器/连接器离线"，不能再让用户去升级一个不存在的连接器。
    expect(
      hostButtonTooltip(tester, 'gcf-mac'),
      'No Grix connector on this host, or the connector is offline',
    );
  });

  // 有在线的连接器 agent 但都没声明 connector_admin：这才是真的"连接器太老"，
  // 按钮可点，点下去给出升级提示 —— 这条语义在 hermes 分支加进来后必须保留。
  testWidgets('an online connector agent without the capability says upgrade', (
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
    expect(hostButtonTooltip(tester, 'gcf-mac'), 'Install agent');

    await tester.tap(find.byKey(const Key('host-install-gcf-mac')));
    // toast 是 post-frame 才插进 overlay 的，得多走一帧才看得见。
    await tester.pump();
    await tester.pump();

    expect(
      find.text(
        'The connector on this host is too old. '
        'Please upgrade it and try again.',
      ),
      findsOne,
    );
    // 不是求助分支：既没开会话也没发消息。
    expect(sessionService.opened, isEmpty);
    expect(imService.sent, isEmpty);

    // toast 自带 3s 定时器，排空再收尾。
    await tester.pump(const Duration(seconds: 4));
  });

  // 分支一：有声明了 connector_admin 的在线自有 agent，仍走原来的远程安装弹窗。
  testWidgets('a connector_admin candidate still opens the install sheet', (
    tester,
  ) async {
    agentService.agents.assignAll([
      _apiAgent(
        id: 'a1',
        name: 'Alpha',
        hostname: 'gcf-mac',
        supportsConnectorAdmin: true,
      ),
      _apiAgent(
        id: 'h1',
        name: 'Hermes One',
        hostname: 'gcf-mac',
        clientType: 'hermes',
      ),
    ]);

    await pumpHostView(tester);

    expect(hostButtonTooltip(tester, 'gcf-mac'), 'Install agent');
    await tester.tap(find.byKey(const Key('host-install-gcf-mac')));
    await tester.pumpAndSettle();

    expect(find.text('Install on gcf-mac'), findsOne);
    // 走的是安装弹窗，不是求助 hermes：既没开会话也没发消息。
    expect(sessionService.opened, isEmpty);
    expect(imService.sent, isEmpty);
  });

  // 分支二：这台机器上只有在线 hermes，说明它根本没装连接器。
  // 让 hermes 自己去装，比提示用户"升级连接器"实在。
  testWidgets('only an online hermes hands the connector install to it', (
    tester,
  ) async {
    agentService.agents.assignAll([
      _apiAgent(
        id: 'h1',
        name: 'Hermes One',
        hostname: 'gcf-mac',
        clientType: 'hermes',
      ),
    ]);

    await pumpHostView(tester);

    expect(
      hostButtonTooltip(tester, 'gcf-mac'),
      'Ask Hermes One to install the connector',
    );

    await tester.tap(find.byKey(const Key('host-install-gcf-mac')));
    await tester.pumpAndSettle();
    expect(find.text('Install the Grix connector'), findsOne);

    await tester.tap(find.byKey(const Key('host-install-hermes-confirm')));
    await tester.pumpAndSettle();

    expect(sessionService.opened, ['h1:2']);
    expect(imService.sent.single.sessionId, 'session-h1');
    final text = imService.sent.single.content;
    expect(text, contains('gcf-mac'));
    expect(text, contains('grix-connector-bootstrap'));
  });

  // 多个 hermes 在线时取第一个：都在同一台机器上，谁装都一样。
  testWidgets('the first online hermes is picked when several are online', (
    tester,
  ) async {
    agentService.agents.assignAll([
      _apiAgent(
        id: 'h1',
        name: 'Hermes One',
        hostname: 'gcf-mac',
        clientType: 'hermes',
      ),
      _apiAgent(
        id: 'h2',
        name: 'Hermes Two',
        hostname: 'gcf-mac',
        clientType: 'hermes',
      ),
    ]);

    await pumpHostView(tester);

    expect(
      hostButtonTooltip(tester, 'gcf-mac'),
      'Ask Hermes One to install the connector',
    );
    await tester.tap(find.byKey(const Key('host-install-gcf-mac')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('host-install-hermes-confirm')));
    await tester.pumpAndSettle();

    expect(sessionService.opened, ['h1:2']);
  });

  testWidgets('declining the hermes prompt neither opens nor sends anything', (
    tester,
  ) async {
    agentService.agents.assignAll([
      _apiAgent(
        id: 'h1',
        name: 'Hermes One',
        hostname: 'gcf-mac',
        clientType: 'hermes',
      ),
    ]);

    await pumpHostView(tester);
    await tester.tap(find.byKey(const Key('host-install-gcf-mac')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();

    expect(sessionService.opened, isEmpty);
    expect(imService.sent, isEmpty);
  });

  // 布局守卫：安装按钮进胶囊后，IconButton 默认的 padded 热区会把胶囊撑到 40 高，
  // 压住下面第一排瓦片左上角的类型标签和在线圆点。按钮盒必须收在 24，
  // 胶囊下沿也必须留在瓦片标签上方。
  testWidgets('the host capsule stays short enough to clear the tile label', (
    tester,
  ) async {
    agentService.agents.assignAll([
      _apiAgent(id: 'a1', name: 'Alpha', hostname: 'gcf-mac'),
    ]);

    await pumpHostView(tester);

    final buttonRect = tester.getRect(
      find.byKey(const Key('host-install-gcf-mac')),
    );
    expect(buttonRect.height, lessThanOrEqualTo(26));

    // 胶囊整体（安装按钮所在的那个 Row 的父 Container）不能比 26 高。
    final capsule = find.ancestor(
      of: find.byKey(const Key('host-install-gcf-mac')),
      matching: find.byType(Container),
    );
    expect(tester.getSize(capsule.first).height, lessThanOrEqualTo(26));

    // 瓦片左上角那个 8 号字的类型标签必须整个落在胶囊下沿之下。
    final providerLabel = find.byWidgetPredicate(
      (widget) => widget is Text && widget.style?.fontSize == 8,
    );
    expect(providerLabel, findsOneWidget);
    final labelBox = find.ancestor(
      of: providerLabel,
      matching: find.byType(Container),
    );
    expect(
      tester.getRect(labelBox.first).top,
      greaterThanOrEqualTo(tester.getRect(capsule.first).bottom + 2),
    );
  });
}
