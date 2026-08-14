import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/modules/ai/agent_quick_onboard_view.dart';
import 'package:grix/modules/ai/controllers/agent_quick_onboard_controller.dart';

class _FakeAgentService extends AgentService {
  AgentApiInstallGuideCatalog? guideCatalog;
  AgentModel? createResult;
  AgentModel? updateResult;
  AgentModel? getAgentResult;

  @override
  Future<void> loadAgents({String? categoryId}) async {}

  @override
  Future<AgentApiInstallGuideCatalog?> getAgentApiInstallGuides() async {
    return guideCatalog;
  }

  @override
  Future<AgentModel?> createAgent(Map<String, dynamic> data) async {
    return createResult;
  }

  @override
  Future<AgentModel?> updateAgent(
    String agentId,
    Map<String, dynamic> data,
  ) async {
    return updateResult;
  }

  @override
  Future<AgentModel?> getAgent(String agentId) async {
    return getAgentResult;
  }
}

const _catalog = AgentApiInstallGuideCatalog(
  defaultType: 'claude',
  list: [
    AgentApiInstallGuide(
      type: 'claude',
      label: 'Claude',
      intro: 'Connect Claude through grix-connector',
      copyTemplate:
          'Connect {{agent_name}} ({{agent_id}}) via {{api_endpoint}} '
          'with key {{api_key}} as claude.',
    ),
    AgentApiInstallGuide(
      type: 'codex',
      label: 'Codex',
      copyTemplate:
          'Connect {{agent_name}} ({{agent_id}}) via {{api_endpoint}} '
          'with key {{api_key}} as codex.',
    ),
  ],
);

AgentModel _agent({
  String name = 'Claude',
  String clientType = 'claude',
  bool online = false,
  String apiKey = 'one-time-secret',
}) {
  return AgentModel(
    id: 'agent-1',
    agentName: name,
    providerType: 3,
    agentClientType: clientType,
    online: online,
    apiKey: apiKey,
    apiKeyHint: 'cret',
    apiEndpoint: 'wss://grix.example/v1/agent-api/ws?agent_id=agent-1',
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(Get.reset);

  AgentQuickOnboardController putController(
    _FakeAgentService service, {
    void Function(String)? onCopy,
  }) {
    return Get.put(
      AgentQuickOnboardController(
        agentService: service,
        autoPolling: false,
        writeClipboard: (text) async => onCopy?.call(text),
        sendProbe:
            ({
              required String agentId,
              required String agentName,
              required String probeText,
            }) async => 'session-1',
        openChat:
            ({
              required String peerId,
              required int peerType,
              required String fallbackTitle,
            }) async => 'session-1',
        showToast: (_, {isError = false}) {},
      ),
    );
  }

  // The install step shows an indeterminate spinner, so pumpAndSettle would
  // never settle there — use bounded pumps after any action that reaches it.
  Future<void> settle(WidgetTester tester) async {
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    await tester.pump(const Duration(milliseconds: 400));
  }

  Future<void> pumpView(WidgetTester tester) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(800, 1800);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentQuickOnboardView(),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('asks the one question and lists every catalog type', (
    WidgetTester tester,
  ) async {
    final service = _FakeAgentService()..guideCatalog = _catalog;
    putController(service);

    await pumpView(tester);

    expect(find.byKey(const Key('quick-onboard-option-claude')), findsOneWidget);
    expect(find.byKey(const Key('quick-onboard-option-codex')), findsOneWidget);
  });

  testWidgets('tapping a type creates the agent and shows a copyable task', (
    WidgetTester tester,
  ) async {
    final service = _FakeAgentService()
      ..guideCatalog = _catalog
      ..createResult = _agent();
    String copied = '';
    putController(service, onCopy: (text) => copied = text);

    await pumpView(tester);
    await tester.tap(find.byKey(const Key('quick-onboard-option-claude')));
    await settle(tester);

    expect(find.byKey(const Key('quick-onboard-task-preview')), findsOneWidget);
    await tester.tap(find.byKey(const Key('quick-onboard-copy-task')));
    await tester.pump();

    expect(copied, isNot(contains('{{')));
    expect(copied, contains('one-time-secret'));
    expect(copied, contains('agent-1'));
    expect(copied, contains('as claude'));
  });

  testWidgets('switching type swaps the task without a new agent', (
    WidgetTester tester,
  ) async {
    final service = _FakeAgentService()
      ..guideCatalog = _catalog
      ..createResult = _agent()
      ..updateResult = _agent(name: 'Codex', clientType: 'codex', apiKey: '');
    putController(service);

    await pumpView(tester);
    await tester.tap(find.byKey(const Key('quick-onboard-option-claude')));
    await settle(tester);

    await tester.tap(find.byKey(const Key('quick-onboard-switch-type')));
    await settle(tester);
    await tester.tap(find.byKey(const Key('quick-onboard-type-option-codex')));
    await settle(tester);

    final preview = tester.widget<SelectableText>(
      find.descendant(
        of: find.byKey(const Key('quick-onboard-task-preview')),
        matching: find.byType(SelectableText),
      ),
    );
    expect(preview.data, contains('as codex'));
    expect(preview.data, contains('one-time-secret'));
  });

  testWidgets('online step celebrates and offers to chat', (
    WidgetTester tester,
  ) async {
    final service = _FakeAgentService()
      ..guideCatalog = _catalog
      ..createResult = _agent();
    final controller = putController(service);

    await pumpView(tester);
    await tester.tap(find.byKey(const Key('quick-onboard-option-claude')));
    await settle(tester);

    service.getAgentResult = _agent(online: true, apiKey: '');
    await controller.pollNow();
    await settle(tester);

    expect(find.byKey(const Key('quick-onboard-online-card')), findsOneWidget);
    expect(find.byKey(const Key('quick-onboard-start-chat')), findsOneWidget);
  });
}
