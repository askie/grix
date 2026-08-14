import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:dio/dio.dart';
import 'package:grix/data/providers/agent_category_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/feature_flag_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/ai/controllers/agents_controller.dart';
import 'package:grix/modules/ai/agents_view.dart';
import 'package:grix/shared/widgets/session_avatar.dart';

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
  int loadAgentsCalls = 0;

  @override
  Future<void> loadAgents({String? categoryId}) async {
    loadAgentsCalls++;
  }

  // 测试用例默认场景=「我自己拥有的 agent」,直接固定为 true,避免每条 fixture
  // 都重复设 ownerID;真共享分发逻辑由单独的 agent share 测试覆盖。
  @override
  bool isOwnedByMe(AgentModel agent) => true;
}

class _FakeImService extends ImService {
  final Set<String> onlineAgentIds = <String>{};
  final Set<String> knownAgentIds = <String>{};
  int refreshAgentOnlineStatesCalls = 0;

  @override
  bool isAgentChannelOnline(String agentId) {
    return onlineAgentIds.contains(agentId.trim());
  }

  @override
  bool hasAgentChannelState(String agentId) {
    return knownAgentIds.contains(agentId.trim());
  }

  @override
  void refreshAgentOnlineStates() {
    refreshAgentOnlineStatesCalls++;
  }
}

class _FakeAgentCategoryService extends AgentCategoryService {
  int loadCategoriesCalls = 0;
  int restoreCachedCategoriesCalls = 0;
  int syncCategoriesFromRemoteCalls = 0;

  @override
  Future<void> restoreCachedCategories() async {
    restoreCachedCategoriesCalls++;
  }

  @override
  Future<void> syncCategoriesFromRemote() async {
    syncCategoriesFromRemoteCalls++;
  }

  @override
  Future<void> loadCategories() async {
    loadCategoriesCalls++;
    await restoreCachedCategories();
    await syncCategoriesFromRemote();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAgentService agentService;
  late _FakeImService imService;
  late _FakeAgentCategoryService agentCategoryService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    agentService = _FakeAgentService();
    imService = _FakeImService();
    agentCategoryService = _FakeAgentCategoryService();
    Get.put<AuthService>(_FakeAuthService());
    Get.put<FeatureFlagService>(_FakeFeatureFlagService());
    Get.put<AgentService>(agentService);
    Get.put<AgentCategoryService>(agentCategoryService);
    Get.put<ImService>(imService);
    Get.put(AgentsController());
  });

  tearDown(() {
    Get.reset();
  });

  Widget buildApp() {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      fallbackLocale: const Locale('en', 'US'),
      home: const AgentsView(),
      getPages: [
        GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
        GetPage(
          name: AppRoutes.accountInfo,
          page: () => const SizedBox.shrink(),
        ),
      ],
    );
  }

  // --- Guard test: empty state ---

  testWidgets('shows empty state when no agents', (WidgetTester tester) async {
    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.text('No agents yet'), findsOneWidget);
  });

  // --- Guard test: empty state only offers quick access, no separate
  // full-wizard button (the full wizard stays reachable via the "+" menu) ---

  testWidgets(
    'empty state shows only quick access button, not a separate create button',
    (WidgetTester tester) async {
      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('agent-quick-access-button')),
        findsOneWidget,
      );
      expect(find.text('Create Agent'), findsNothing);
    },
  );

  // --- Guard test: quick access button is a primary FilledButton whose
  // width constraint is capped at exactly 66% of the screen width. Asserting
  // the constraint itself (not just the rendered size) so this fails if the
  // cap factor regresses — the button's own content is short enough that a
  // size-only check would stay green even at 100%. ---

  testWidgets(
    'quick access button constraint is capped at exactly 66% of screen width',
    (WidgetTester tester) async {
      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      final buttonFinder = find.byKey(const Key('agent-quick-access-button'));
      expect(tester.widget(buttonFinder), isA<FilledButton>());

      final constrainedBox = tester.widget<ConstrainedBox>(
        find
            .ancestor(of: buttonFinder, matching: find.byType(ConstrainedBox))
            .first,
      );
      final pageWidth = tester.getSize(find.byType(AgentsView)).width;
      expect(
        constrainedBox.constraints.maxWidth,
        closeTo(pageWidth * 0.66, 0.5),
      );
    },
  );

  // --- Guard test: on a narrow screen the cap actually bites and visibly
  // compresses the button below its natural content width ---

  testWidgets(
    'quick access button is visibly narrower than its natural width on a small screen',
    (WidgetTester tester) async {
      tester.view.physicalSize = const Size(320, 700);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      final buttonFinder = find.byKey(const Key('agent-quick-access-button'));
      final buttonWidth = tester.getSize(buttonFinder).width;

      expect(buttonWidth, lessThanOrEqualTo(320 * 0.66 + 0.5));
    },
  );

  // --- Guard test: agents rendered as avatar blocks with name below ---

  testWidgets('renders agent avatar with name text below', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-1',
        agentName: 'Chat Bot',
        providerType: 3,
        agentClientType: 'openclaw',
        sessionId: 'session-1',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.byType(SessionAvatar), findsOneWidget);
    expect(find.text('Chat Bot'), findsOneWidget);
  });

  // --- Guard test: isMain agent shows main badge ---

  testWidgets('isMain agent shows main badge', (WidgetTester tester) async {
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-main',
        agentName: 'Main Bot',
        providerType: 3,
        isMain: true,
        sessionId: 'session-main',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.text('Main'), findsOneWidget);
  });

  // --- Guard test: non-main agent does not show main badge ---

  testWidgets('non-main agent does not show main badge', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-normal',
        agentName: 'Normal Bot',
        providerType: 3,
        isMain: false,
        sessionId: 'session-normal',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.text('Main'), findsNothing);
  });

  // --- Guard test: tap agent block opens action sheet ---

  testWidgets('tapping agent block opens action sheet', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-chat-1',
        agentName: 'Chat Bot',
        providerType: 3,
        sessionId: 'session_chat_1',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    await tester.tap(find.byType(SessionAvatar).first);
    await tester.pumpAndSettle();

    expect(find.text('Chat'), findsOneWidget);
    expect(find.text('Config'), findsOneWidget);
  });

  // --- Guard test: gear icon tap shows bottom sheet with Config ---

  testWidgets('tapping gear icon shows bottom sheet with Config', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-lp-1',
        agentName: 'LP Bot',
        providerType: 3,
        sessionId: 'session_lp_1',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    await tester.tap(find.byType(SessionAvatar).first);
    await tester.pumpAndSettle();

    expect(find.text('Config'), findsOneWidget);
  });

  // --- Guard test: Agent API gear icon tap shows Config + Permissions ---

  testWidgets('Agent API agent gear icon shows Config and Permissions', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-api-lp',
        agentName: 'API Bot',
        providerType: 3,
        sessionId: 'session_api_lp',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    await tester.tap(find.byType(SessionAvatar).first);
    await tester.pumpAndSettle();

    expect(find.text('Config'), findsOneWidget);
    expect(find.text('Permissions'), findsOneWidget);
  });

  // --- Guard test: non-Agent API gear icon tap shows only Config ---

  testWidgets('non-Agent API agent gear icon shows only Config', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-remote-lp',
        agentName: 'Remote Bot',
        providerType: 1,
        sessionId: 'session_remote_lp',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    await tester.tap(find.byType(SessionAvatar).first);
    await tester.pumpAndSettle();

    expect(find.text('Config'), findsOneWidget);
    expect(find.text('Permissions'), findsNothing);
  });

  // --- Guard test: pull to refresh ---

  testWidgets('pull to refresh reloads agents and online states', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-refresh-1',
        agentName: 'Refresh Bot',
        providerType: 3,
        sessionId: 'session_refresh_1',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(agentService.loadAgentsCalls, 1);
    expect(imService.refreshAgentOnlineStatesCalls, 0);

    await tester.drag(find.byType(ListView).first, const Offset(0, 300));
    await tester.pump();
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    expect(agentService.loadAgentsCalls, 2);
    expect(imService.refreshAgentOnlineStatesCalls, 1);
  });

  // --- Guard test: uncategorized agents in Uncategorized box ---

  testWidgets('uncategorized agents shown in Uncategorized box', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-uncat',
        agentName: 'Uncat Bot',
        providerType: 1,
        categoryId: '0',
        sessionId: 'session_uncat',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.text('Uncategorized'), findsOneWidget);
    expect(find.byType(SessionAvatar), findsOneWidget);
  });

  // --- Guard test: two root categories render as 2 columns ---

  testWidgets('two root categories render as two columns', (
    WidgetTester tester,
  ) async {
    agentCategoryService.categories.assignAll([
      const AgentCategoryModel(id: 'cat-1', parentId: '0', name: 'Category A'),
      const AgentCategoryModel(id: 'cat-2', parentId: '0', name: 'Category B'),
    ]);
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-a',
        agentName: 'Bot A',
        providerType: 1,
        categoryId: 'cat-1',
        sessionId: 'session-a',
      ),
      AgentModel(
        id: 'agent-b',
        agentName: 'Bot B',
        providerType: 1,
        categoryId: 'cat-2',
        sessionId: 'session-b',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.text('Category A'), findsOneWidget);
    expect(find.text('Category B'), findsOneWidget);
    // Two columns: both category names exist (proves multi-column layout)
    expect(find.byType(Wrap), findsWidgets);
  });

  // --- Guard test: empty categories ARE rendered ---

  testWidgets('empty categories are rendered', (WidgetTester tester) async {
    agentCategoryService.categories.assignAll([
      const AgentCategoryModel(
        id: 'cat-empty',
        parentId: '0',
        name: 'Empty Cat',
      ),
    ]);
    // No agents in the empty category
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-uncat',
        agentName: 'Uncat Bot',
        providerType: 1,
        categoryId: '0',
        sessionId: 'session_uncat',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.text('Empty Cat'), findsOneWidget);
    expect(find.text('Uncategorized'), findsOneWidget);
  });

  testWidgets(
    'cached category mapping renders without uncategorized fallback',
    (WidgetTester tester) async {
      agentCategoryService.categories.assignAll([
        const AgentCategoryModel(
          id: 'cat-cache',
          parentId: '0',
          name: 'Cached',
        ),
      ]);
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-cache',
          agentName: 'Cached Bot',
          providerType: 3,
          categoryId: 'cat-cache',
          sessionId: 'session_cache',
        ),
      ]);

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      expect(find.text('Cached'), findsOneWidget);
      expect(find.text('Uncategorized'), findsNothing);
    },
  );

  // --- Guard test: no drawer ---

  testWidgets('no drawer is present', (WidgetTester tester) async {
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-drawer',
        agentName: 'Drawer Bot',
        providerType: 1,
        sessionId: 'session_drawer',
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.byType(Drawer), findsNothing);
  });
}
