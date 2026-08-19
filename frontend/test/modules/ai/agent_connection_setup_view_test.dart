import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/modules/ai/agent_connection_setup_view.dart';
import 'package:grix/modules/ai/controllers/agent_connection_setup_controller.dart';

class _FakeAgentService extends AgentService {
  AgentModel? getAgentResult;
  AgentApiInstallGuideCatalog? guideCatalog;
  String? lastGetAgentId;
  int getAgentCalls = 0;

  @override
  Future<AgentModel?> getAgent(String agentId) async {
    lastGetAgentId = agentId;
    getAgentCalls++;
    return getAgentResult;
  }

  @override
  Future<AgentApiInstallGuideCatalog?> getAgentApiInstallGuides() async {
    return guideCatalog;
  }
}

/// Mirrors the backend catalog: one task per client type, each carrying the
/// four placeholders the client resolves before copying.
const _guideCatalog = AgentApiInstallGuideCatalog(
  defaultType: 'claude',
  list: [
    AgentApiInstallGuide(
      type: 'claude',
      label: 'Claude',
      intro: 'Connect Claude through grix-connector',
      contentTemplate: 'npm install -g grix-connector',
      // Long enough to overflow the preview's 260px frame, like the real task —
      // otherwise there is nothing to scroll and the scroll-reset test is vacuous.
      copyTemplate:
          'Connect {{agent_name}} via grix-connector.\n'
          'Run the steps in order and report back when done.\n'
          '\n'
          '1) npm install -g grix-connector\n'
          '\n'
          '2) Merge this entry into ~/.grix/config/agents.json:\n'
          '{\n'
          '  "name": "{{agent_name}}",\n'
          '"agent_id": "{{agent_id}}"\n'
          '"api_key": "{{api_key}}"\n'
          '"ws_url": "{{api_endpoint}}"\n'
          '"client_type": "claude"\n'
          '}\n'
          '\n'
          '3) grix-connector status, then start or reload.\n'
          '\n'
          '4) Verify with the local admin API.\n'
          '\n'
          'Never overwrite the whole file.',
    ),
    AgentApiInstallGuide(
      type: 'codex',
      label: 'Codex',
      contentTemplate: 'npm install -g grix-connector',
      copyTemplate:
          'Connect {{agent_name}} via grix-connector.\n'
          '"agent_id": "{{agent_id}}"\n'
          '"api_key": "{{api_key}}"\n'
          '"ws_url": "{{api_endpoint}}"\n'
          '"client_type": "codex"',
    ),
    AgentApiInstallGuide(
      type: 'openclaw',
      label: 'OpenClaw',
      contentTemplate: 'openclaw plugins install grix-connector',
      copyTemplate:
          'Connect {{agent_name}} to OpenClaw.\n'
          'openclaw config set channels.grix.accounts.<account-id> '
          '{"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"} '
          '--strict-json',
    ),
    AgentApiInstallGuide(
      type: 'hermes',
      label: 'Hermes',
      contentTemplate:
          'hermes plugins install askie/grix-hermes-python --enable',
      // Also long enough to scroll: if the task you switch TO were short, the
      // scroll position would clamp back to 0 on its own and the reset test
      // would pass even without the fix.
      copyTemplate:
          'Connect {{agent_name}} to Hermes.\n'
          'Run the steps in order and report back when done.\n'
          '\n'
          '1) hermes plugins install askie/grix-hermes-python --enable\n'
          '\n'
          '2) Write the credentials into the profile .env:\n'
          'GRIX_ENDPOINT={{api_endpoint}}\n'
          'GRIX_AGENT_ID={{agent_id}}\n'
          'GRIX_API_KEY={{api_key}}\n'
          '\n'
          '3) hermes gateway restart\n'
          '\n'
          '4) Check the gateway log for the connection line.\n'
          '\n'
          'One agent per profile.',
    ),
  ],
);

AgentModel _agent({
  String id = 'agent-1',
  int providerType = 3,
  bool online = false,
  String apiEndpoint = 'wss://grix.example/v1/agent-api',
  String apiKey = 'one-time-secret',
  String apiKeyHint = 'cret',
  String agentClientType = '',
}) {
  return AgentModel(
    id: id,
    agentName: 'Research Agent',
    providerType: providerType,
    online: online,
    apiEndpoint: apiEndpoint,
    apiKey: apiKey,
    apiKeyHint: apiKeyHint,
    agentClientType: agentClientType,
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(Get.reset);

  Future<void> pumpSetup(WidgetTester tester, {Locale? locale}) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: locale ?? const Locale('en', 'US'),
        home: const AgentConnectionSetupView(),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('copies the backend task with every placeholder resolved', (
    WidgetTester tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(800, 1800);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);

    final service = _FakeAgentService()..guideCatalog = _guideCatalog;
    String copiedText = '';
    final controller = Get.put(
      AgentConnectionSetupController(
        agentService: service,
        initialArguments: <String, dynamic>{'agent': _agent()},
        writeClipboard: (text) async => copiedText = text,
        showToast: (_, {isError = false}) {},
      ),
    );

    await pumpSetup(tester);

    expect(find.byKey(const Key('agent-setup-success-card')), findsOneWidget);
    expect(
      find.byKey(const Key('agent-setup-credentials-card')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('agent-setup-task-preview')), findsOneWidget);

    await tester.tap(find.byKey(const Key('agent-setup-copy-task')));
    await tester.pump();

    expect(copiedText, controller.installTask);
    // A task shipped with a live "{{...}}" would be pasted into an agent as-is.
    expect(copiedText, isNot(contains('{{')));
    expect(copiedText, contains('Research Agent'));
    expect(copiedText, contains('"agent_id": "agent-1"'));
    expect(copiedText, contains('"api_key": "one-time-secret"'));
    expect(copiedText, contains('"ws_url": "wss://grix.example/v1/agent-api"'));
    expect(copiedText, contains('"client_type": "claude"'));
  });

  test(
    'keeps every client type the backend serves, including openclaw and hermes',
    () async {
      final service = _FakeAgentService()..guideCatalog = _guideCatalog;
      final controller = AgentConnectionSetupController(
        agentService: service,
        initialArguments: <String, dynamic>{
          'agent': _agent(agentClientType: 'codex'),
        },
        showToast: (_, {isError = false}) {},
      );

      await controller.initialize();

      expect(controller.installGuides.map((guide) => guide.type), <String>[
        'claude',
        'codex',
        'openclaw',
        'hermes',
      ]);
      // The Agent's own client type wins over the catalog default.
      expect(controller.selectedClientType, 'codex');
      expect(controller.installTask, contains('"client_type": "codex"'));
    },
  );

  test("switching type swaps in that install path's own task", () async {
    final service = _FakeAgentService()..guideCatalog = _guideCatalog;
    final controller = AgentConnectionSetupController(
      agentService: service,
      initialArguments: <String, dynamic>{'agent': _agent()},
      showToast: (_, {isError = false}) {},
    );

    await controller.initialize();
    expect(controller.selectedClientType, 'claude');

    controller.selectInstallGuide('openclaw');
    expect(controller.selectedClientType, 'openclaw');
    final openclawTask = controller.installTask;
    expect(openclawTask, contains('openclaw config set'));
    expect(openclawTask, contains('"wsUrl":"wss://grix.example/v1/agent-api"'));

    controller.selectInstallGuide('hermes');
    final hermesTask = controller.installTask;
    expect(hermesTask, contains('GRIX_API_KEY=one-time-secret'));
    expect(hermesTask, isNot(contains('openclaw')));

    controller.selectInstallGuide('unknown-type');
    expect(controller.selectedClientType, 'hermes');
  });

  testWidgets('picking a type in the sheet rewrites the task on screen', (
    WidgetTester tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(800, 1800);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);

    final service = _FakeAgentService()..guideCatalog = _guideCatalog;
    String copiedText = '';
    final controller = Get.put(
      AgentConnectionSetupController(
        agentService: service,
        initialArguments: <String, dynamic>{'agent': _agent()},
        writeClipboard: (text) async => copiedText = text,
        showToast: (_, {isError = false}) {},
      ),
    );

    await pumpSetup(tester);
    expect(controller.selectedClientType, 'claude');

    await tester.tap(find.byKey(const Key('agent-setup-guide-selector')));
    await tester.pumpAndSettle();

    // Every type the backend serves must be reachable in the sheet, including
    // the ones a dropdown overlay would have clipped off the bottom.
    expect(find.byKey(const Key('agent-setup-guide-sheet')), findsOneWidget);
    expect(find.byKey(const Key('agent-setup-guide-option-hermes')), findsOneWidget);

    await tester.tap(find.byKey(const Key('agent-setup-guide-option-openclaw')));
    await tester.pumpAndSettle();

    // The regression this pins: the selection changed but the rendered task —
    // the thing the owner actually copies — kept showing the previous type.
    final preview = tester.widget<SelectableText>(
      find.descendant(
        of: find.byKey(const Key('agent-setup-task-preview')),
        matching: find.byType(SelectableText),
      ),
    );
    expect(preview.data, contains('openclaw config set'));
    expect(preview.data, isNot(contains('"client_type": "claude"')));
    expect(
      find.byKey(const Key('agent-setup-guide-selector-label')),
      findsOneWidget,
    );
    expect(find.text('OpenClaw'), findsOneWidget);

    await tester.tap(find.byKey(const Key('agent-setup-copy-task')));
    await tester.pump();
    expect(copiedText, contains('openclaw config set'));
    expect(copiedText, isNot(contains('{{')));
  });

  testWidgets('switching type scrolls the task back to the top', (
    WidgetTester tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(800, 1800);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);

    final service = _FakeAgentService()..guideCatalog = _guideCatalog;
    Get.put(
      AgentConnectionSetupController(
        agentService: service,
        initialArguments: <String, dynamic>{'agent': _agent()},
        showToast: (_, {isError = false}) {},
      ),
    );

    await pumpSetup(tester);

    final preview = find.byKey(const Key('agent-setup-task-preview'));
    final taskText = find.descendant(
      of: preview,
      matching: find.byType(SelectableText),
    );
    final restingTop = tester.getTopLeft(taskText).dy;

    await tester.drag(preview, const Offset(0, -60));
    await tester.pumpAndSettle();
    expect(tester.getTopLeft(taskText).dy, lessThan(restingTop));

    await tester.tap(find.byKey(const Key('agent-setup-guide-selector')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('agent-setup-guide-option-hermes')));
    await tester.pumpAndSettle();

    // Two tasks look alike mid-body: landing at the old offset makes a swapped
    // task read as "nothing changed", which is how the original bug looked.
    expect(tester.getTopLeft(taskText).dy, restingTop);
  });

  testWidgets('the status card itself re-checks the connection', (
    WidgetTester tester,
  ) async {
    // Once the Agent is online the bottom button becomes "start chat", and
    // pull-to-refresh is off for mouse pointers — without this the owner has no
    // way to re-check a stale "online" on Web or desktop.
    final service = _FakeAgentService()
      ..guideCatalog = _guideCatalog
      ..getAgentResult = _agent(online: true);
    Get.put(
      AgentConnectionSetupController(
        agentService: service,
        initialArguments: <String, dynamic>{'agent': _agent(online: true)},
        showToast: (_, {isError = false}) {},
      ),
    );

    await pumpSetup(tester);

    final refresh = find.byKey(const Key('agent-setup-status-refresh'));
    await tester.ensureVisible(refresh);
    await tester.tap(refresh);
    await tester.pumpAndSettle();

    expect(service.getAgentCalls, greaterThan(0));
    expect(service.lastGetAgentId, 'agent-1');
  });

  testWidgets('offers no task once the one-time secret is gone', (
    WidgetTester tester,
  ) async {
    final service = _FakeAgentService()..guideCatalog = _guideCatalog;
    final controller = Get.put(
      AgentConnectionSetupController(
        agentService: service,
        initialArguments: <String, dynamic>{'agent': _agent(apiKey: '')},
        showToast: (_, {isError = false}) {},
      ),
    );

    await pumpSetup(tester);

    // Without the secret the task would carry a masked key and silently fail on
    // the target machine — better to offer nothing and point at key rotation.
    expect(controller.hasOneTimeSecret, isFalse);
    expect(controller.installTask, isEmpty);
    expect(find.byKey(const Key('agent-setup-copy-task')), findsNothing);
    expect(find.byKey(const Key('agent-setup-task-preview')), findsNothing);
    expect(
      find.byKey(const Key('agent-setup-install-guide-card')),
      findsOneWidget,
    );
  });

  testWidgets('refresh updates online state without erasing one-time API key', (
    WidgetTester tester,
  ) async {
    final service = _FakeAgentService()
      ..guideCatalog = _guideCatalog
      ..getAgentResult = _agent(online: true, apiKey: '');
    final controller = Get.put(
      AgentConnectionSetupController(
        agentService: service,
        initialArguments: <String, dynamic>{'agent': _agent()},
        showToast: (_, {isError = false}) {},
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentConnectionSetupView(),
      ),
    );
    await tester.pumpAndSettle();

    // The pinned bottom action is the only refresh button now — the status card
    // used to carry a second one that called exactly the same thing.
    expect(find.byKey(const Key('agent-setup-refresh-status')), findsNothing);
    final refresh = find.byKey(const Key('agent-setup-primary-action'));
    await tester.ensureVisible(refresh);
    await tester.tap(refresh);
    await tester.pumpAndSettle();

    expect(service.lastGetAgentId, 'agent-1');
    expect(controller.isOnline, isTrue);
    expect(controller.apiKey.value, 'one-time-secret');
    expect(find.text('one-time-secret'), findsOneWidget);
  });

  testWidgets('loads an Agent when route arguments only contain agent_id', (
    WidgetTester tester,
  ) async {
    final service = _FakeAgentService()
      ..guideCatalog = _guideCatalog
      ..getAgentResult = _agent(id: 'loaded-agent', providerType: 1);
    final controller = Get.put(
      AgentConnectionSetupController(
        agentService: service,
        initialArguments: const <String, dynamic>{'agent_id': 'loaded-agent'},
        showToast: (_, {isError = false}) {},
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentConnectionSetupView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(service.lastGetAgentId, 'loaded-agent');
    expect(controller.currentAgent?.id, 'loaded-agent');
    expect(find.byKey(const Key('agent-setup-ready-card')), findsOneWidget);
  });

  testWidgets('non-API success offers chat, configuration and done actions', (
    WidgetTester tester,
  ) async {
    String? openedPeerId;
    int? openedPeerType;
    String? openedRoute;
    Object? openedArguments;
    Object? closeResult;
    final controller = Get.put(
      AgentConnectionSetupController(
        agentService: _FakeAgentService(),
        initialArguments: <String, dynamic>{
          'agent': _agent(providerType: 1, apiEndpoint: '', apiKey: ''),
        },
        openChat:
            ({
              required peerId,
              required peerType,
              required fallbackTitle,
            }) async {
              openedPeerId = peerId;
              openedPeerType = peerType;
              return 'session-1';
            },
        openRoute: (route, {arguments}) async {
          openedRoute = route;
          openedArguments = arguments;
        },
        closePage: ({result}) => closeResult = result,
        showToast: (_, {isError = false}) {},
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentConnectionSetupView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('agent-setup-credentials-card')), findsNothing);
    expect(find.byKey(const Key('agent-setup-ready-card')), findsOneWidget);

    await tester.tap(find.byKey(const Key('agent-setup-primary-action')));
    await tester.pump();
    expect(openedPeerId, 'agent-1');
    expect(openedPeerType, 2);

    await tester.tap(find.byKey(const Key('agent-setup-continue-config')));
    await tester.pump();
    expect(openedRoute, AppRoutes.agentEdit);
    expect((openedArguments as Map<String, dynamic>)['agent_id'], 'agent-1');

    await tester.tap(find.byKey(const Key('agent-setup-done')));
    expect(closeResult, same(controller.currentAgent));
  });

  testWidgets('content is centered and capped at 720 logical pixels', (
    WidgetTester tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(1200, 1000);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);

    final service = _FakeAgentService()..guideCatalog = _guideCatalog;
    Get.put(
      AgentConnectionSetupController(
        agentService: service,
        initialArguments: <String, dynamic>{'agent': _agent()},
        showToast: (_, {isError = false}) {},
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentConnectionSetupView(),
      ),
    );
    await tester.pumpAndSettle();

    final successCard = find.byKey(const Key('agent-setup-success-card'));
    expect(tester.getSize(successCard).width, lessThanOrEqualTo(720));
    expect(tester.getCenter(successCard).dx, closeTo(600, 1));
  });

  testWidgets('connector setup remains overflow-free at 320px width', (
    WidgetTester tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(320, 700);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);

    final service = _FakeAgentService()..guideCatalog = _guideCatalog;
    Get.put(
      AgentConnectionSetupController(
        agentService: service,
        initialArguments: <String, dynamic>{'agent': _agent()},
        showToast: (_, {isError = false}) {},
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentConnectionSetupView(),
      ),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);

    final scroll = find.byKey(const Key('agent-setup-scroll'));
    await tester.fling(scroll, const Offset(0, -1800), 1800);
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    await tester.fling(scroll, const Offset(0, -1800), 1800);
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });
}
