import 'dart:async';
import 'dart:collection';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/egg_market_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/eggs/controllers/egg_market_controller.dart';

class _InstallRequestRecord {
  const _InstallRequestRecord({
    required this.eggID,
    required this.version,
    required this.idempotencyKey,
    required this.installMode,
    required this.targetAgentID,
    required this.executorAgentID,
  });

  final String eggID;
  final int version;
  final String idempotencyKey;
  final String installMode;
  final String? targetAgentID;
  final String? executorAgentID;
}

class _FakeEggMarketService extends EggMarketService {
  final Queue<EggInstallAcceptModel> installResponses =
      Queue<EggInstallAcceptModel>();
  final List<_InstallRequestRecord> installRequests = <_InstallRequestRecord>[];

  @override
  Future<EggSearchResult> searchEggs({
    required String keyword,
    int page = 1,
    int pageSize = 20,
    String locale = '',
  }) async {
    return EggSearchResult(
      localeUsed: locale,
      page: page,
      pageSize: pageSize,
      hasMore: false,
      list: const <EggMarketEggModel>[],
    );
  }

  @override
  Future<EggInstallAcceptModel> installEgg({
    required String eggID,
    required int version,
    required String idempotencyKey,
    required String installMode,
    String? targetAgentID,
    String? executorAgentID,
  }) async {
    installRequests.add(
      _InstallRequestRecord(
        eggID: eggID,
        version: version,
        idempotencyKey: idempotencyKey,
        installMode: installMode,
        targetAgentID: targetAgentID,
        executorAgentID: executorAgentID,
      ),
    );
    if (installResponses.isEmpty) {
      throw StateError('missing install response');
    }
    return installResponses.removeFirst();
  }
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

class _FakeImService extends ImService {
  final Map<String, String> boundTitles = <String, String>{};

  @override
  bool hasSessionDisplayTitleById(String sessionId) {
    return boundTitles.containsKey(sessionId.trim());
  }

  @override
  Future<void> bindSessionDisplayTitle(
    String sessionId,
    String title, {
    String type = 'private',
    String peerId = '',
    int peerType = 0,
  }) async {
    boundTitles[sessionId.trim()] = title.trim();
  }
}

class _TestEggMarketController extends EggMarketController {
  @override
  Future<void> refreshAll() async {}
}

EggMarketEggModel _buildEgg() {
  return EggMarketEggModel(
    id: 'lobster.executor',
    name: 'Executor Egg',
    description: 'Chooses a main Agent before opening chat.',
    color: '#8B7355',
    emoji: '🥚',
    vibe: 'helper',
    canCreateAgent: true,
    existingAgentClientTypes: const <String>[EggInstallTargetType.openclaw],
    status: 'active',
    version: 3,
    versionDesc: 'v3',
    installCount: 10,
  );
}

Widget _buildApp() {
  return GetMaterialApp(
    translations: AppTranslations(),
    locale: const Locale('en', 'US'),
    fallbackLocale: const Locale('en', 'US'),
    initialRoute: AppRoutes.home,
    getPages: [
      GetPage(
        name: AppRoutes.home,
        page: () => const Scaffold(body: SizedBox.shrink()),
      ),
      GetPage(
        name: AppRoutes.chat,
        page: () => const Scaffold(body: Text('chat')),
      ),
    ],
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeEggMarketService eggMarketService;
  late _FakeAgentService agentService;
  late _FakeImService imService;
  late EggMarketController controller;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    eggMarketService = _FakeEggMarketService();
    agentService = _FakeAgentService();
    imService = _FakeImService();
    Get.put<EggMarketService>(eggMarketService);
    Get.put<AgentService>(agentService);
    Get.put<ImService>(imService);
    controller = Get.put<EggMarketController>(_TestEggMarketController());
    agentService.agents.assignAll([
      AgentModel(
        id: '101',
        agentName: 'Main A',
        providerType: 3,
        agentClientType: EggInstallTargetType.openclaw,
        status: 1,
        isMain: true,
      ),
      AgentModel(
        id: '102',
        agentName: 'Main B',
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

  testWidgets('installEgg retries with first executor and opens chat', (
    WidgetTester tester,
  ) async {
    eggMarketService.installResponses.addAll([
      EggInstallAcceptModel(
        installID: '',
        status: 'choose_executor',
        sessionID: '',
        executorAgentID: '',
        candidates: [
          EggInstallCandidateModel(
            agentID: '101',
            agentName: 'Main A',
            agentClientType: 'openclaw',
          ),
          EggInstallCandidateModel(
            agentID: '102',
            agentName: 'Main B',
            agentClientType: 'claude',
          ),
        ],
      ),
      EggInstallAcceptModel(
        installID: 'install-1',
        status: 'running',
        sessionID: 'session-1',
        executorAgentID: '101',
        candidates: const <EggInstallCandidateModel>[],
      ),
    ]);

    await tester.pumpWidget(_buildApp());
    await tester.pumpAndSettle();

    unawaited(
      controller.installEgg(
        egg: _buildEgg(),
        installMode: EggInstallMode.createNew,
      ),
    );
    await tester.pumpAndSettle();

    expect(eggMarketService.installRequests, hasLength(2));
    expect(eggMarketService.installRequests.first.executorAgentID, isNull);
    expect(eggMarketService.installRequests.last.executorAgentID, '101');
    expect(
      eggMarketService.installRequests.last.idempotencyKey,
      eggMarketService.installRequests.first.idempotencyKey,
    );
    expect(
      eggMarketService.installRequests.last.installMode,
      EggInstallMode.createNew,
    );
    expect(Uri.parse(Get.currentRoute).path, AppRoutes.chat);
    expect(imService.boundTitles['session-1'], 'Main A');
  });

  testWidgets('installEgg stops when executor selection has no candidates', (
    WidgetTester tester,
  ) async {
    eggMarketService.installResponses.add(
      EggInstallAcceptModel(
        installID: '',
        status: 'choose_executor',
        sessionID: '',
        executorAgentID: '',
        candidates: [
          EggInstallCandidateModel(
            agentID: '',
            agentName: 'Main A',
            agentClientType: 'openclaw',
          ),
        ],
      ),
    );

    await tester.pumpWidget(_buildApp());
    await tester.pumpAndSettle();

    unawaited(
      controller.installEgg(
        egg: _buildEgg(),
        installMode: EggInstallMode.createNew,
      ),
    );
    await tester.pumpAndSettle();

    expect(eggMarketService.installRequests, hasLength(1));
    expect(Uri.parse(Get.currentRoute).path, AppRoutes.home);
    expect(imService.boundTitles, isEmpty);

    // 排空 installEgg 失败提示 toast 的 3s 定时器，避免 "Timer still pending"
    await tester.pump(const Duration(seconds: 3));
  });

  test('agentsForHatchType includes any typed main API agent as creators', () {
    agentService.agents.assignAll([
      AgentModel(
        id: 'cursor-main',
        agentName: 'Cursor Main',
        providerType: 3,
        agentClientType: 'cursor',
        status: 1,
        isMain: true,
      ),
      AgentModel(
        id: 'cursor-worker',
        agentName: 'Cursor Worker',
        providerType: 3,
        agentClientType: 'cursor',
        status: 1,
        isMain: false,
      ),
      AgentModel(
        id: 'claude-main',
        agentName: 'Claude Main',
        providerType: 3,
        agentClientType: EggInstallTargetType.claude,
        status: 1,
        isMain: true,
      ),
    ]);

    final agents = controller.agentsForHatchType(EggHatchType.agent);

    expect(agents.map((agent) => agent.id), contains('cursor-main'));
    expect(agents.map((agent) => agent.id), contains('claude-main'));
    expect(agents.map((agent) => agent.id), isNot(contains('cursor-worker')));
  });
}
