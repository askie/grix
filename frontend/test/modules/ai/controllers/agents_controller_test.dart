import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_category_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/ai/controllers/agents_controller.dart';
import 'package:grix/modules/ai/models/agent_editor_result.dart';

class _FakeAgentService extends AgentService {
  _FakeAgentService({List<String>? callOrder}) : _callOrder = callOrder;

  int loadAgentsCalls = 0;
  String? lastLoadAgentsCategoryId;
  final List<String>? _callOrder;

  @override
  Future<void> loadAgents({String? categoryId}) async {
    loadAgentsCalls++;
    lastLoadAgentsCategoryId = categoryId;
    _callOrder?.add('load_agents');
  }
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
  _FakeAgentCategoryService({List<String>? callOrder}) : _callOrder = callOrder;

  int loadCategoriesCalls = 0;
  int restoreCachedCategoriesCalls = 0;
  int syncCategoriesFromRemoteCalls = 0;
  final List<String>? _callOrder;

  @override
  Future<void> restoreCachedCategories() async {
    restoreCachedCategoriesCalls++;
    _callOrder?.add('restore_cached_categories');
  }

  @override
  Future<void> syncCategoriesFromRemote() async {
    syncCategoriesFromRemoteCalls++;
    _callOrder?.add('sync_categories_from_remote');
  }

  @override
  Future<void> loadCategories() async {
    loadCategoriesCalls++;
    _callOrder?.add('load_categories');
    await restoreCachedCategories();
    await syncCategoriesFromRemote();
  }
}

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
    Get.addTranslations(AppTranslations().keys);
    Get.locale = const Locale('en', 'US');
    Get.fallbackLocale = const Locale('en', 'US');
  });

  tearDown(() {
    Get.reset();
  });

  // --- Guard tests: controller interface contract ---

  test('loadAgents() does not pass categoryId', () async {
    final agentService = _FakeAgentService();
    final imService = _FakeImService();
    final agentCategoryService = _FakeAgentCategoryService();
    Get.put<AgentService>(agentService);
    Get.put<ImService>(imService);
    Get.put<AgentCategoryService>(agentCategoryService);

    final controller = Get.put(AgentsController());
    await Future<void>.delayed(Duration.zero);
    controller.loadAgents();

    expect(
      agentService.loadAgentsCalls,
      2,
    ); // 1 from onInit + 1 from loadAgents
    expect(agentService.lastLoadAgentsCategoryId, isNull);
  });

  test('refreshAgents() does not pass categoryId', () async {
    final agentService = _FakeAgentService();
    final imService = _FakeImService();
    final agentCategoryService = _FakeAgentCategoryService();
    Get.put<AgentService>(agentService);
    Get.put<ImService>(imService);
    Get.put<AgentCategoryService>(agentCategoryService);

    final controller = Get.put(AgentsController());
    await Future<void>.delayed(Duration.zero);
    await controller.refreshAgents();

    expect(
      agentService.loadAgentsCalls,
      2,
    ); // 1 from onInit + 1 from refreshAgents
    expect(agentService.lastLoadAgentsCategoryId, isNull);
  });

  group('providerDisplayLabel', () {
    late AgentsController controller;

    setUp(() {
      final agentService = _FakeAgentService();
      final imService = _FakeImService();
      final agentCategoryService = _FakeAgentCategoryService();
      Get.put<AgentService>(agentService);
      Get.put<ImService>(imService);
      Get.put<AgentCategoryService>(agentCategoryService);
      controller = Get.put(AgentsController());
    });

    test('returns modelProvider for providerType=1 with modelProvider', () {
      final agent = AgentModel(
        id: '1',
        agentName: 'Test',
        providerType: 1,
        modelProvider: 'gpt-4',
      );
      expect(controller.providerDisplayLabel(agent), 'gpt-4');
    });

    test('returns "Remote" for providerType=1 without modelProvider', () {
      final agent = AgentModel(id: '1', agentName: 'Test', providerType: 1);
      expect(controller.providerDisplayLabel(agent), 'Remote');
    });

    test('returns "Local" for providerType=2', () {
      final agent = AgentModel(id: '1', agentName: 'Test', providerType: 2);
      expect(controller.providerDisplayLabel(agent), 'Local');
    });

    test(
      'returns client type label for providerType=3 with known clientType',
      () {
        for (final entry in {
          'claude': 'Claude',
          'codex': 'Codex',
          'gemini': 'Gemini',
          'hermes': 'Hermes',
          'openclaw': 'OpenClaw',
          'qwen': 'Qwen',
        }.entries) {
          final agent = AgentModel(
            id: '1',
            agentName: 'Test',
            providerType: 3,
            agentClientType: entry.key,
          );
          expect(
            controller.providerDisplayLabel(agent),
            entry.value,
            reason: 'clientType=${entry.key}',
          );
        }
      },
    );

    test('returns "Agent" for providerType=3 without clientType', () {
      final agent = AgentModel(id: '1', agentName: 'Test', providerType: 3);
      expect(controller.providerDisplayLabel(agent), 'Agent');
    });

    test(
      'returns raw clientType for providerType=3 with unknown clientType',
      () {
        final agent = AgentModel(
          id: '1',
          agentName: 'Test',
          providerType: 3,
          agentClientType: 'CustomBot',
        );
        expect(controller.providerDisplayLabel(agent), 'CustomBot');
      },
    );
  });

  // --- Existing tests (kept) ---

  test('agents controller loads agents during init', () async {
    final agentService = _FakeAgentService();
    final imService = _FakeImService();
    final agentCategoryService = _FakeAgentCategoryService();
    Get.put<AgentService>(agentService);
    Get.put<ImService>(imService);
    Get.put<AgentCategoryService>(agentCategoryService);

    Get.put(AgentsController());
    await Future<void>.delayed(Duration.zero);

    expect(agentService.loadAgentsCalls, 1);
    expect(agentCategoryService.restoreCachedCategoriesCalls, 1);
    expect(agentCategoryService.syncCategoriesFromRemoteCalls, 1);
  });

  test('agents controller refreshes when agents tab becomes active', () async {
    final agentService = _FakeAgentService();
    final imService = _FakeImService();
    final agentCategoryService = _FakeAgentCategoryService();
    final homeTabIndex = 0.obs;
    Get.put<AgentService>(agentService);
    Get.put<ImService>(imService);
    Get.put<AgentCategoryService>(agentCategoryService);

    final controller = Get.put(AgentsController(homeTabIndex: homeTabIndex));
    await Future<void>.delayed(Duration.zero);

    expect(agentService.loadAgentsCalls, 0);
    expect(imService.refreshAgentOnlineStatesCalls, 0);

    homeTabIndex.value = HomeTab.agents.index;
    await Future<void>.delayed(Duration.zero);

    expect(agentService.loadAgentsCalls, 1);
    expect(imService.refreshAgentOnlineStatesCalls, 1);

    homeTabIndex.value = HomeTab.conversations.index;
    await Future<void>.delayed(Duration.zero);
    homeTabIndex.value = HomeTab.agents.index;
    await Future<void>.delayed(Duration.zero);

    expect(agentService.loadAgentsCalls, 2);
    expect(imService.refreshAgentOnlineStatesCalls, 2);
    expect(agentCategoryService.restoreCachedCategoriesCalls, 3);
    expect(agentCategoryService.syncCategoriesFromRemoteCalls, 2);

    controller.onClose();
  });

  test('init restores cached categories before first agent load', () async {
    final callOrder = <String>[];
    final agentService = _FakeAgentService(callOrder: callOrder);
    final imService = _FakeImService();
    final agentCategoryService = _FakeAgentCategoryService(
      callOrder: callOrder,
    );
    Get.put<AgentService>(agentService);
    Get.put<ImService>(imService);
    Get.put<AgentCategoryService>(agentCategoryService);

    Get.put(AgentsController());
    await Future<void>.delayed(Duration.zero);

    final restoreIndex = callOrder.indexOf('restore_cached_categories');
    final loadIndex = callOrder.indexOf('load_agents');
    expect(restoreIndex, isNonNegative);
    expect(loadIndex, isNonNegative);
    expect(restoreIndex, lessThan(loadIndex));
  });

  test('agents tab refresh restores cache before loading agents', () async {
    final callOrder = <String>[];
    final agentService = _FakeAgentService(callOrder: callOrder);
    final imService = _FakeImService();
    final agentCategoryService = _FakeAgentCategoryService(
      callOrder: callOrder,
    );
    final homeTabIndex = HomeTab.conversations.index.obs;
    Get.put<AgentService>(agentService);
    Get.put<ImService>(imService);
    Get.put<AgentCategoryService>(agentCategoryService);

    Get.put(AgentsController(homeTabIndex: homeTabIndex));
    await Future<void>.delayed(Duration.zero);
    callOrder.clear();

    homeTabIndex.value = HomeTab.agents.index;
    await Future<void>.delayed(Duration.zero);

    final restoreIndex = callOrder.indexOf('restore_cached_categories');
    final loadIndex = callOrder.indexOf('load_agents');
    expect(restoreIndex, isNonNegative);
    expect(loadIndex, isNonNegative);
    expect(restoreIndex, lessThan(loadIndex));
  });

  test(
    'open agent edit shows saved toast after returning from editor',
    () async {
      final toastMessages = <({String message, bool isError})>[];
      String? openedRoute;
      dynamic openedArguments;
      Map<String, String>? openedParameters;
      final agentService = _FakeAgentService();
      final imService = _FakeImService();
      final agentCategoryService = _FakeAgentCategoryService();
      Get.put<AgentService>(agentService);
      Get.put<ImService>(imService);
      Get.put<AgentCategoryService>(agentCategoryService);

      final controller = Get.put(
        AgentsController(
          openRoute:
              <T>(
                String route, {
                dynamic arguments,
                Map<String, String>? parameters,
              }) async {
                openedRoute = route;
                openedArguments = arguments;
                openedParameters = parameters;
                return AgentEditorResult.saved as T;
              },
          showToast: (message, {isError = true}) {
            toastMessages.add((message: message, isError: isError));
          },
        ),
      );
      final agent = AgentModel(
        id: 'agent-1',
        agentName: 'Agent 1',
        providerType: 3,
        agentClientType: 'gemini',
        apiEndpoint: 'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=agent-1',
        apiKeyHint: '5678',
        sessionId: 'session-1',
      );

      await controller.openAgentEdit(agent);

      expect(openedRoute, '/agent/edit');
      expect(openedArguments, isA<Map<String, dynamic>>());
      final arguments = openedArguments as Map<String, dynamic>;
      expect(arguments['agent_id'], 'agent-1');
      expect(arguments['agent'], same(agent));
      expect(openedParameters, isNull);
      expect(agentService.loadAgentsCalls, 2);
      expect(toastMessages, hasLength(1));
      expect(toastMessages.single.message, 'Saved');
      expect(toastMessages.single.isError, isFalse);
    },
  );

  test('isApiAgentOnline falls back to API online field', () {
    final agentService = _FakeAgentService();
    final imService = _FakeImService();
    final agentCategoryService = _FakeAgentCategoryService();
    Get.put<AgentService>(agentService);
    Get.put<ImService>(imService);
    Get.put<AgentCategoryService>(agentCategoryService);

    final controller = Get.put(AgentsController());

    expect(
      controller.isApiAgentOnline(
        AgentModel(
          id: 'agent-1',
          agentName: 'Atlas',
          providerType: 3,
          online: true,
        ),
      ),
      isTrue,
    );
  });

  test('isApiAgentOnline prefers realtime state when available', () {
    final agentService = _FakeAgentService();
    final imService = _FakeImService()..knownAgentIds.add('agent-1');
    final agentCategoryService = _FakeAgentCategoryService();
    Get.put<AgentService>(agentService);
    Get.put<ImService>(imService);
    Get.put<AgentCategoryService>(agentCategoryService);

    final controller = Get.put(AgentsController());

    expect(
      controller.isApiAgentOnline(
        AgentModel(
          id: 'agent-1',
          agentName: 'Atlas',
          providerType: 3,
          online: true,
        ),
      ),
      isFalse,
    );

    imService.onlineAgentIds.add('agent-1');

    expect(
      controller.isApiAgentOnline(
        AgentModel(
          id: 'agent-1',
          agentName: 'Atlas',
          providerType: 3,
          online: false,
        ),
      ),
      isTrue,
    );
  });
}
