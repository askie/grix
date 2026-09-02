import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/modules/ai/controllers/agent_quick_onboard_controller.dart';
import 'package:grix/modules/ai/models/agent_install_task.dart';

class _FakeAgentService extends AgentService {
  AgentApiInstallGuideCatalog? guideCatalog;
  AgentModel? createResult;
  AgentModel? updateResult;
  AgentModel? getAgentResult;
  String operationError = '';

  Map<String, dynamic>? lastCreatePayload;
  String? lastUpdateAgentId;
  Map<String, dynamic>? lastUpdatePayload;
  int createCalls = 0;
  int updateCalls = 0;
  int getAgentCalls = 0;

  @override
  Future<void> loadAgents({String? categoryId}) async {}

  @override
  String get lastOperationError => operationError;

  @override
  Future<AgentApiInstallGuideCatalog?> getAgentApiInstallGuides() async {
    return guideCatalog;
  }

  @override
  Future<AgentModel?> createAgent(Map<String, dynamic> data) async {
    createCalls++;
    lastCreatePayload = data;
    return createResult;
  }

  @override
  Future<AgentModel?> updateAgent(
    String agentId,
    Map<String, dynamic> data,
  ) async {
    updateCalls++;
    lastUpdateAgentId = agentId;
    lastUpdatePayload = data;
    return updateResult;
  }

  @override
  Future<AgentModel?> getAgent(String agentId) async {
    getAgentCalls++;
    return getAgentResult;
  }
}

const _catalog = AgentApiInstallGuideCatalog(
  defaultType: 'claude',
  list: [
    AgentApiInstallGuide(
      type: 'claude',
      label: 'Claude',
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
    AgentApiInstallGuide(
      type: 'kimi',
      label: 'Kimi',
      copyTemplate: 'Install kimi CLI first, then use {{api_key}}.',
    ),
  ],
);

AgentModel _agent({
  String id = 'agent-9',
  String name = 'Claude',
  String clientType = 'claude',
  bool online = false,
  String apiKey = '',
  String apiEndpoint = 'wss://grix.example/v1/agent-api/ws?agent_id=agent-9',
}) {
  return AgentModel(
    id: id,
    agentName: name,
    providerType: 3,
    agentClientType: clientType,
    online: online,
    apiKey: apiKey,
    apiKeyHint: 'hint',
    apiEndpoint: apiEndpoint,
  );
}

AgentQuickOnboardController _controller(
  _FakeAgentService service, {
  Future<String?> Function({
    required String agentId,
    required String agentName,
    required String probeText,
  })?
  sendProbe,
  Future<String?> Function({
    required String peerId,
    required int peerType,
    required String fallbackTitle,
  })?
  openChat,
  void Function(String message, {bool isError})? showToast,
}) {
  final controller = AgentQuickOnboardController(
    agentService: service,
    autoPolling: false,
    writeClipboard: (_) async {},
    sendProbe:
        sendProbe ??
        ({
          required String agentId,
          required String agentName,
          required String probeText,
        }) async => 'session-1',
    openChat:
        openChat ??
        ({
          required String peerId,
          required int peerType,
          required String fallbackTitle,
        }) async => 'session-1',
    showToast: showToast ?? (message, {bool isError = false}) {},
  );
  return controller;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(Get.reset);

  group('resolveAgentInstallTask', () {
    test('resolves every placeholder', () {
      final task = resolveAgentInstallTask(
        template: '{{agent_name}}/{{agent_id}}/{{api_key}}/{{api_endpoint}}',
        agentName: 'Claude',
        agentId: 'a1',
        apiKey: 'k1',
        apiEndpoint: 'wss://x',
      );
      expect(task, 'Claude/a1/k1/wss://x');
    });

    test('returns empty rather than a half-filled task', () {
      final task = resolveAgentInstallTask(
        template: 'key={{api_key}}',
        agentName: 'Claude',
        agentId: 'a1',
        apiKey: '',
        apiEndpoint: 'wss://x',
      );
      expect(task, isEmpty);
    });
  });

  group('selectTypeAndCreate', () {
    test(
      'creates an API agent named after the type and enters install',
      () async {
        final service = _FakeAgentService()
          ..guideCatalog = _catalog
          ..createResult = _agent(apiKey: 'one-time-secret');
        final controller = _controller(service);
        await controller.loadInstallGuides();

        await controller.selectTypeAndCreate('claude');

        expect(service.createCalls, 1);
        expect(service.lastCreatePayload, {
          'agent_name': 'Claude',
          'introduction': '',
          'provider_type': 3,
          'category_id': '0',
          'agent_client_type': 'claude',
        });
        expect(controller.step.value, QuickOnboardStep.install);
        expect(controller.agentId.value, 'agent-9');
        expect(controller.apiKey.value, 'one-time-secret');
        expect(controller.installTask, contains('one-time-secret'));
        expect(controller.installTask, contains('as claude'));
      },
    );

    test('dedups the agent name against existing agents', () async {
      final service = _FakeAgentService()
        ..guideCatalog = _catalog
        ..createResult = _agent(name: 'Claude-2', apiKey: 's');
      service.agents.assignAll([_agent(id: 'other', name: 'claude')]);
      final controller = _controller(service);
      await controller.loadInstallGuides();

      await controller.selectTypeAndCreate('claude');

      expect(service.lastCreatePayload?['agent_name'], 'Claude-2');
    });

    test('failure keeps the question step and toasts', () async {
      final messages = <String>[];
      final service = _FakeAgentService()
        ..guideCatalog = _catalog
        ..operationError = 'boom';
      final controller = _controller(
        service,
        showToast: (message, {bool isError = false}) => messages.add(message),
      );
      await controller.loadInstallGuides();

      await controller.selectTypeAndCreate('claude');

      expect(controller.step.value, QuickOnboardStep.selectType);
      expect(controller.currentAgent, isNull);
      expect(messages, ['boom']);
    });

    test('a second tap while an agent exists is a no-op', () async {
      final service = _FakeAgentService()
        ..guideCatalog = _catalog
        ..createResult = _agent(apiKey: 's');
      final controller = _controller(service);
      await controller.loadInstallGuides();

      await controller.selectTypeAndCreate('claude');
      await controller.selectTypeAndCreate('codex');

      expect(service.createCalls, 1);
      expect(controller.selectedType.value, 'claude');
    });
  });

  group('switchType', () {
    test(
      'swaps the template instantly and never creates a second agent',
      () async {
        final service = _FakeAgentService()
          ..guideCatalog = _catalog
          ..createResult = _agent(apiKey: 'secret')
          ..updateResult = _agent(name: 'Codex', clientType: 'codex');
        final controller = _controller(service);
        await controller.loadInstallGuides();
        await controller.selectTypeAndCreate('claude');

        await controller.switchType('codex');

        expect(service.createCalls, 1);
        expect(service.updateCalls, 1);
        expect(service.lastUpdateAgentId, 'agent-9');
        expect(service.lastUpdatePayload, {
          'agent_client_type': 'codex',
          'agent_name': 'Codex',
        });
        expect(controller.selectedType.value, 'codex');
        // Credentials survive: the update response has no api_key, yet the
        // one-time secret must keep resolving into the swapped task.
        expect(controller.installTask, contains('secret'));
        expect(controller.installTask, contains('as codex'));
      },
    );

    test('does not rename once the owner picked their own name', () async {
      final service = _FakeAgentService()
        ..guideCatalog = _catalog
        ..createResult = _agent(apiKey: 's')
        ..updateResult = _agent(clientType: 'codex');
      final controller = _controller(service);
      await controller.loadInstallGuides();
      await controller.selectTypeAndCreate('claude');
      // Simulate a rename that happened elsewhere (e.g. agent edit page).
      service.getAgentResult = _agent(name: 'My Bot');
      await controller.pollNow();

      await controller.switchType('codex');

      expect(service.lastUpdatePayload, {'agent_client_type': 'codex'});
    });

    test('update failure still leaves the swapped template usable', () async {
      final service = _FakeAgentService()
        ..guideCatalog = _catalog
        ..createResult = _agent(apiKey: 'secret')
        ..updateResult = null;
      final controller = _controller(service);
      await controller.loadInstallGuides();
      await controller.selectTypeAndCreate('claude');

      await controller.switchType('codex');

      expect(controller.selectedType.value, 'codex');
      expect(controller.installTask, contains('as codex'));
    });
  });

  group('going online', () {
    test('pollNow flips to online and sends the probe exactly once', () async {
      var probeCalls = 0;
      String? probeAgentId;
      final service = _FakeAgentService()
        ..guideCatalog = _catalog
        ..createResult = _agent(apiKey: 's');
      final controller = _controller(
        service,
        sendProbe:
            ({
              required String agentId,
              required String agentName,
              required String probeText,
            }) async {
              probeCalls++;
              probeAgentId = agentId;
              return 'session-7';
            },
      );
      await controller.loadInstallGuides();
      await controller.selectTypeAndCreate('claude');
      expect(controller.step.value, QuickOnboardStep.install);

      service.getAgentResult = _agent(online: true);
      await controller.pollNow();

      expect(controller.step.value, QuickOnboardStep.online);
      expect(probeCalls, 1);
      expect(probeAgentId, 'agent-9');
      expect(controller.probeDelivered.value, isTrue);

      await controller.pollNow();
      expect(probeCalls, 1, reason: 'probe must never repeat');
    });

    test('probe failure still lands on the online step', () async {
      final service = _FakeAgentService()
        ..guideCatalog = _catalog
        ..createResult = _agent(apiKey: 's');
      final controller = _controller(
        service,
        sendProbe:
            ({
              required String agentId,
              required String agentName,
              required String probeText,
            }) async => throw Exception('no session'),
      );
      await controller.loadInstallGuides();
      await controller.selectTypeAndCreate('claude');

      service.getAgentResult = _agent(online: true);
      await controller.pollNow();

      expect(controller.step.value, QuickOnboardStep.online);
      expect(controller.probeDelivered.value, isFalse);
    });

    test('startChat opens the private chat with the agent', () async {
      String? openedPeerId;
      int? openedPeerType;
      final service = _FakeAgentService()
        ..guideCatalog = _catalog
        ..createResult = _agent(apiKey: 's');
      final controller = _controller(
        service,
        openChat:
            ({
              required String peerId,
              required int peerType,
              required String fallbackTitle,
            }) async {
              openedPeerId = peerId;
              openedPeerType = peerType;
              return 'session-1';
            },
      );
      await controller.loadInstallGuides();
      await controller.selectTypeAndCreate('claude');
      service.getAgentResult = _agent(online: true);
      await controller.pollNow();

      await controller.startChat();

      expect(openedPeerId, 'agent-9');
      expect(openedPeerType, 2);
    });
  });
}
