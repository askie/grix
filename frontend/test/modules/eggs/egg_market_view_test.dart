import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/egg_market_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/eggs/controllers/egg_market_controller.dart';
import 'package:grix/modules/eggs/egg_market_view.dart';

class _FakeEggMarketService extends EggMarketService {}

class _PagingFakeEggMarketService extends EggMarketService {
  _PagingFakeEggMarketService(this.responses);

  final Map<String, EggSearchResult> responses;
  final List<_SearchRequest> requests = <_SearchRequest>[];

  @override
  Future<EggSearchResult> searchEggs({
    required String keyword,
    int page = 1,
    int pageSize = 20,
    String locale = '',
  }) async {
    requests.add(_SearchRequest(keyword: keyword, page: page));
    return responses['${keyword.trim()}::$page'] ??
        EggSearchResult(
          localeUsed: locale,
          page: page,
          pageSize: pageSize,
          hasMore: false,
          list: const <EggMarketEggModel>[],
        );
  }
}

class _SearchRequest {
  const _SearchRequest({required this.keyword, required this.page});

  final String keyword;
  final int page;
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

class _FakeImService extends ImService {
  @override
  bool get isConnected => true;
}

class _RecordingEggMarketController extends EggMarketController {
  _RecordingEggMarketController();

  final List<_InstallRequest> installRequests = <_InstallRequest>[];

  @override
  Future<void> refreshAll() async {}

  @override
  Future<void> installEgg({
    required EggMarketEggModel egg,
    required String installMode,
    String? targetAgentID,
    String? executorAgentID,
    bool isSkillInstall = false,
  }) async {
    installRequests.add(
      _InstallRequest(
        egg: egg,
        installMode: installMode,
        targetAgentID: targetAgentID,
        executorAgentID: executorAgentID,
        isSkillInstall: isSkillInstall,
      ),
    );
  }
}

class _InstallRequest {
  const _InstallRequest({
    required this.egg,
    required this.installMode,
    required this.targetAgentID,
    required this.executorAgentID,
    required this.isSkillInstall,
  });

  final EggMarketEggModel egg;
  final String installMode;
  final String? targetAgentID;
  final String? executorAgentID;
  final bool isSkillInstall;
}

EggMarketEggModel _buildEgg({
  required String id,
  String? name,
  String? description,
  String? versionDesc,
  bool canCreateAgent = true,
  List<String>? existingAgentClientTypes,
}) {
  return EggMarketEggModel(
    id: id,
    name: name ?? 'Egg $id',
    description: description ?? 'Helps verify route versions.',
    color: '#8B7355',
    emoji: '🥚',
    vibe: 'helper',
    canCreateAgent: canCreateAgent,
    existingAgentClientTypes:
        existingAgentClientTypes ?? <String>[EggInstallTargetType.openclaw],
    status: 'active',
    version: 3,
    versionDesc: versionDesc ?? 'v3',
    installCount: 10,
  );
}

List<EggMarketEggModel> _buildEggBatch({
  required int start,
  required int count,
}) {
  return List<EggMarketEggModel>.generate(count, (index) {
    final value = start + index;
    return _buildEgg(id: 'egg-$value', name: 'Egg $value');
  });
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAgentService agentService;
  late _RecordingEggMarketController controller;
  late EggMarketEggModel egg;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    agentService = _FakeAgentService();
    Get.put<EggMarketService>(_FakeEggMarketService());
    Get.put<AgentService>(agentService);
    Get.put<ImService>(_FakeImService());
    controller =
        Get.put<EggMarketController>(_RecordingEggMarketController())
            as _RecordingEggMarketController;
    egg = _buildEgg(id: 'egg-1', name: 'Route Helper');
    controller.hotEggs.assignAll(<EggMarketEggModel>[egg]);
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-openclaw-1',
        agentName: 'OpenClaw Helper',
        providerType: 3,
        agentClientType: EggInstallTargetType.openclaw,
        status: 1,
        isMain: true,
      ),
    ]);
  });

  tearDown(() {
    Get.reset();
  });

  Widget buildApp({Locale locale = const Locale('en', 'US')}) {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: locale,
      fallbackLocale: const Locale('en', 'US'),
      theme: AppTheme.lightTheme,
      home: EggMarketView(),
    );
  }

  Future<void> openInstallDialog(WidgetTester tester) async {
    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();
    await tester.tap(find.text(egg.name));
    await tester.pumpAndSettle();
    final detailDialog = find.byType(AlertDialog).last;
    await tester.tap(
      find.descendant(
        of: detailDialog,
        matching: find.widgetWithText(ElevatedButton, 'Hatch'),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('keeps search bar pinned and taps title to scroll back to top', (
    WidgetTester tester,
  ) async {
    controller.hotEggs.assignAll(_buildEggBatch(start: 1, count: 24));

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    final searchField = find.byType(TextField);
    final listView = find.byType(ListView);
    final title = find.text('Eggs Market');
    final initialSearchTop = tester.getTopLeft(searchField).dy;

    await tester.drag(listView, const Offset(0, -1200));
    await tester.pumpAndSettle();

    expect(controller.scrollController.offset, greaterThan(0));
    expect(tester.getTopLeft(searchField).dy, initialSearchTop);

    await tester.tap(title);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pumpAndSettle();

    expect(controller.scrollController.offset, 0);
    expect(tester.getTopLeft(searchField).dy, initialSearchTop);
  });

  testWidgets('loads next page automatically when list nears the bottom', (
    WidgetTester tester,
  ) async {
    final pagingService = _PagingFakeEggMarketService({
      '::1': EggSearchResult(
        localeUsed: 'en-US',
        page: 1,
        pageSize: 20,
        hasMore: true,
        list: _buildEggBatch(start: 1, count: 20),
      ),
      '::2': EggSearchResult(
        localeUsed: 'en-US',
        page: 2,
        pageSize: 20,
        hasMore: false,
        list: _buildEggBatch(start: 21, count: 8),
      ),
    });

    Get.reset();
    agentService = _FakeAgentService();
    Get.put<EggMarketService>(pagingService);
    Get.put<AgentService>(agentService);
    Get.put<ImService>(_FakeImService());
    final pagingController = Get.put<EggMarketController>(
      EggMarketController(),
    );
    await pagingController.refreshAll();

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(
      pagingService.requests.where(
        (request) => request.keyword.isEmpty && request.page == 1,
      ),
      hasLength(1),
    );

    pagingController.scrollController.jumpTo(
      pagingController.scrollController.position.maxScrollExtent,
    );
    await tester.pump();
    await tester.pumpAndSettle();

    expect(
      pagingService.requests.where(
        (request) => request.keyword.isEmpty && request.page == 2,
      ),
      hasLength(1),
    );
    expect(pagingController.hotEggs, hasLength(28));
    expect(pagingController.hotEggs.last.name, 'Egg 28');
  });

  testWidgets('search button switches list to keyword results', (
    WidgetTester tester,
  ) async {
    final pagingService = _PagingFakeEggMarketService({
      '::1': EggSearchResult(
        localeUsed: 'en-US',
        page: 1,
        pageSize: 20,
        hasMore: false,
        list: <EggMarketEggModel>[_buildEgg(id: 'hot-1', name: 'Hot Egg')],
      ),
      'shrimp::1': EggSearchResult(
        localeUsed: 'en-US',
        page: 1,
        pageSize: 30,
        hasMore: false,
        list: <EggMarketEggModel>[
          _buildEgg(id: 'search-1', name: 'Shrimp Search Result'),
        ],
      ),
    });

    Get.reset();
    agentService = _FakeAgentService();
    Get.put<EggMarketService>(pagingService);
    Get.put<AgentService>(agentService);
    Get.put<ImService>(_FakeImService());
    final searchController = Get.put<EggMarketController>(
      EggMarketController(),
    );

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.text('Hot Egg'), findsOneWidget);

    await tester.tap(find.byType(TextField));
    await tester.pump();
    await tester.enterText(find.byType(TextField), 'shrimp');
    await tester.pump();
    expect(searchController.searchFocusNode.hasFocus, isTrue);
    expect(
      find.byKey(const ValueKey('egg_market_clear_search_button')),
      findsOneWidget,
    );
    await tester.tap(find.widgetWithText(ElevatedButton, 'Search'));
    await tester.pump();
    await tester.pumpAndSettle();

    expect(
      pagingService.requests.where(
        (request) => request.keyword == 'shrimp' && request.page == 1,
      ),
      hasLength(1),
    );
    expect(searchController.searchFocusNode.hasFocus, isFalse);
    expect(find.text('Shrimp Search Result'), findsOneWidget);
    expect(find.text('Hot Egg'), findsNothing);
  });

  testWidgets('keyboard search action switches list to keyword results', (
    WidgetTester tester,
  ) async {
    final pagingService = _PagingFakeEggMarketService({
      '::1': EggSearchResult(
        localeUsed: 'en-US',
        page: 1,
        pageSize: 20,
        hasMore: false,
        list: <EggMarketEggModel>[
          _buildEgg(id: 'hot-2', name: 'Default Hot Egg'),
        ],
      ),
      'lobster::1': EggSearchResult(
        localeUsed: 'en-US',
        page: 1,
        pageSize: 30,
        hasMore: false,
        list: <EggMarketEggModel>[
          _buildEgg(id: 'search-2', name: 'Lobster Search Result'),
        ],
      ),
    });

    Get.reset();
    agentService = _FakeAgentService();
    Get.put<EggMarketService>(pagingService);
    Get.put<AgentService>(agentService);
    Get.put<ImService>(_FakeImService());
    final searchController = Get.put<EggMarketController>(
      EggMarketController(),
    );

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    await tester.tap(find.byType(TextField));
    await tester.enterText(find.byType(TextField), 'lobster');
    await tester.testTextInput.receiveAction(TextInputAction.search);
    await tester.pump();
    await tester.pumpAndSettle();

    expect(
      pagingService.requests.where(
        (request) => request.keyword == 'lobster' && request.page == 1,
      ),
      hasLength(1),
    );
    expect(searchController.searchFocusNode.hasFocus, isFalse);
    expect(find.text('Lobster Search Result'), findsOneWidget);
    expect(find.text('Default Hot Egg'), findsNothing);
  });

  testWidgets('clear button resets search input and restores hot eggs', (
    WidgetTester tester,
  ) async {
    final pagingService = _PagingFakeEggMarketService({
      '::1': EggSearchResult(
        localeUsed: 'en-US',
        page: 1,
        pageSize: 20,
        hasMore: false,
        list: <EggMarketEggModel>[_buildEgg(id: 'hot-1', name: 'Hot Egg')],
      ),
      'shrimp::1': EggSearchResult(
        localeUsed: 'en-US',
        page: 1,
        pageSize: 30,
        hasMore: false,
        list: <EggMarketEggModel>[
          _buildEgg(id: 'search-1', name: 'Shrimp Search Result'),
        ],
      ),
    });

    Get.reset();
    agentService = _FakeAgentService();
    Get.put<EggMarketService>(pagingService);
    Get.put<AgentService>(agentService);
    Get.put<ImService>(_FakeImService());
    final searchController = Get.put<EggMarketController>(
      EggMarketController(),
    );

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), 'shrimp');
    await tester.tap(find.widgetWithText(ElevatedButton, 'Search'));
    await tester.pumpAndSettle();

    expect(find.text('Shrimp Search Result'), findsOneWidget);

    await tester.tap(
      find.byKey(const ValueKey('egg_market_clear_search_button')),
    );
    await tester.pumpAndSettle();

    expect(searchController.keywordController.text, isEmpty);
    expect(find.text('Hot Egg'), findsOneWidget);
    expect(find.text('Shrimp Search Result'), findsNothing);
  });

  testWidgets(
    'shows agent picker defaulting to agent-hatch when multiple candidates match',
    (WidgetTester tester) async {
      egg = _buildEgg(
        id: 'egg-1',
        name: 'Route Helper',
        existingAgentClientTypes: <String>[
          EggInstallTargetType.openclaw,
          EggInstallTargetType.claude,
        ],
      );
      controller.hotEggs.assignAll(<EggMarketEggModel>[egg]);
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-openclaw-1',
          agentName: 'OpenClaw Helper',
          providerType: 3,
          agentClientType: EggInstallTargetType.openclaw,
          status: 1,
          isMain: true,
        ),
        AgentModel(
          id: 'agent-claude-1',
          agentName: 'Claude Helper',
          providerType: 3,
          agentClientType: EggInstallTargetType.claude,
          status: 1,
          isMain: false,
        ),
      ]);

      await openInstallDialog(tester);

      expect(find.text('Hatch Egg'), findsOneWidget);
      expect(find.text('Select Agent'), findsOneWidget);
      expect(
        find.text('Hatch a new agent via OpenClaw: OpenClaw Helper'),
        findsOneWidget,
      );
      expect(
        find.widgetWithText(ElevatedButton, 'Start Hatching'),
        findsOneWidget,
      );
      expect(controller.installRequests, isEmpty);
    },
  );

  testWidgets('shows cursor main agents as agent-hatch executors', (
    WidgetTester tester,
  ) async {
    egg = _buildEgg(
      id: 'egg-1',
      name: 'Route Helper',
      existingAgentClientTypes: const <String>[],
    );
    controller.hotEggs.assignAll(<EggMarketEggModel>[egg]);
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-cursor-1',
        agentName: 'Cursor Helper',
        providerType: 3,
        agentClientType: 'cursor',
        status: 1,
        isMain: true,
      ),
    ]);

    await openInstallDialog(tester);

    expect(controller.installRequests, hasLength(1));
    final request = controller.installRequests.single;
    expect(request.installMode, EggInstallMode.createNew);
    expect(request.executorAgentID, 'agent-cursor-1');
    expect(request.targetAgentID, isNull);
    expect(request.isSkillInstall, isFalse);
  });

  testWidgets(
    'uses deepseek main agents as hatch executors for dual-package eggs',
    (WidgetTester tester) async {
      egg = _buildEgg(
        id: 'egg-1',
        name: 'Route Helper',
        existingAgentClientTypes: <String>[
          EggInstallTargetType.openclaw,
          EggInstallTargetType.claude,
        ],
      );
      controller.hotEggs.assignAll(<EggMarketEggModel>[egg]);
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-deepseek-1',
          agentName: 'DeepSeek Helper',
          providerType: 3,
          agentClientType: EggInstallTargetType.deepseek,
          status: 1,
          isMain: true,
        ),
      ]);

      await openInstallDialog(tester);

      expect(controller.installRequests, hasLength(1));
      final request = controller.installRequests.single;
      expect(request.installMode, EggInstallMode.createNew);
      expect(request.executorAgentID, 'agent-deepseek-1');
      expect(request.targetAgentID, isNull);
      expect(request.isSkillInstall, isFalse);
    },
  );

  testWidgets(
    'selecting an existing skill agent in the picker installs directly to it',
    (WidgetTester tester) async {
      egg = _buildEgg(
        id: 'egg-1',
        name: 'Route Helper',
        existingAgentClientTypes: <String>[
          EggInstallTargetType.openclaw,
          EggInstallTargetType.claude,
        ],
      );
      controller.hotEggs.assignAll(<EggMarketEggModel>[egg]);
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-openclaw-1',
          agentName: 'OpenClaw Helper',
          providerType: 3,
          agentClientType: EggInstallTargetType.openclaw,
          status: 1,
          isMain: true,
        ),
        AgentModel(
          id: 'agent-claude-1',
          agentName: 'Claude Helper',
          providerType: 3,
          agentClientType: EggInstallTargetType.claude,
          status: 1,
          isMain: false,
        ),
      ]);

      await openInstallDialog(tester);

      await tester.tap(
        find.byKey(const ValueKey('egg_market_agent_picker_field')),
      );
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const ValueKey('egg_market_agent_option_agent-claude-1')),
      );
      await tester.pumpAndSettle();

      expect(
        find.text('Install the skill to Claude: Claude Helper'),
        findsOneWidget,
      );
      expect(controller.installRequests, isEmpty);

      await tester.tap(find.widgetWithText(ElevatedButton, 'Start Installing'));
      await tester.pumpAndSettle();

      expect(controller.installRequests, hasLength(1));
      final request = controller.installRequests.single;
      expect(request.installMode, EggInstallMode.existingAgent);
      expect(request.targetAgentID, 'agent-claude-1');
      expect(request.isSkillInstall, isTrue);
    },
  );

  testWidgets('agent picker sheet auto-scrolls to reveal the selected agent', (
    WidgetTester tester,
  ) async {
    egg = _buildEgg(
      id: 'egg-1',
      name: 'Route Helper',
      existingAgentClientTypes: <String>[
        EggInstallTargetType.openclaw,
        EggInstallTargetType.claude,
      ],
    );
    controller.hotEggs.assignAll(<EggMarketEggModel>[egg]);
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-openclaw-1',
        agentName: 'OpenClaw Helper',
        providerType: 3,
        agentClientType: EggInstallTargetType.openclaw,
        status: 1,
        isMain: true,
      ),
      for (var i = 1; i <= 30; i++)
        AgentModel(
          id: 'agent-claude-$i',
          agentName: 'Claude Helper ${i.toString().padLeft(2, '0')}',
          providerType: 3,
          agentClientType: EggInstallTargetType.claude,
          status: 1,
          isMain: false,
        ),
    ]);

    await openInstallDialog(tester);

    await tester.tap(
      find.byKey(const ValueKey('egg_market_agent_picker_field')),
    );
    await tester.pumpAndSettle();

    // 选中的 OpenClaw agent 按标签排序在 30 个 Claude agent 之后，
    // 不自动滚动的话超出视口和缓存范围，根本不会被构建出来。
    final selectedOption = find.byKey(
      const ValueKey('egg_market_agent_option_agent-openclaw-1'),
    );
    expect(selectedOption, findsOneWidget);
    expect(tester.widget<ListTile>(selectedOption).selected, isTrue);
  });

  testWidgets(
    'tapping an egg with a single compatible agent installs directly without a picker',
    (WidgetTester tester) async {
      egg = _buildEgg(
        id: 'egg-1',
        name: 'Route Helper',
        description:
            'A longer description that should be readable in the details dialog.',
        versionDesc: 'Shows the full release notes in the details dialog.',
      );
      controller.hotEggs.assignAll(<EggMarketEggModel>[egg]);

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      await tester.tap(find.text('Route Helper'));
      await tester.pumpAndSettle();

      expect(find.text('Egg Details'), findsOneWidget);
      expect(
        find.text('Shows the full release notes in the details dialog.'),
        findsNothing,
      );

      final detailDialog = find.byType(AlertDialog).last;
      await tester.tap(
        find.descendant(
          of: detailDialog,
          matching: find.widgetWithText(ElevatedButton, 'Hatch'),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsNothing);
      expect(controller.installRequests, hasLength(1));
      final request = controller.installRequests.single;
      expect(request.installMode, EggInstallMode.createNew);
      expect(request.executorAgentID, 'agent-openclaw-1');
      expect(request.isSkillInstall, isFalse);
    },
  );
}
