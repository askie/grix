import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/modules/ai/agent_create_wizard_view.dart';
import 'package:grix/modules/ai/controllers/agent_create_wizard_controller.dart';

class _FakeAgentService extends AgentService {
  AgentModel? createResult;
  Map<String, dynamic>? createPayload;
  List<VoiceModelOption> voiceOptions = const [
    VoiceModelOption(
      id: 'openai-realtime',
      label: 'OpenAI Realtime',
      provider: 'openai_realtime',
      model: 'gpt-realtime',
      endpoint: 'wss://api.openai.com/v1/realtime',
      voices: [VoicePresetOption(id: 'alloy', label: 'Alloy')],
    ),
  ];
  int voiceLoadCalls = 0;

  @override
  Future<AgentModel?> createAgent(Map<String, dynamic> data) async {
    createPayload = Map<String, dynamic>.from(data);
    return createResult;
  }

  @override
  Future<List<VoiceModelOption>?> getVoiceModels() async {
    voiceLoadCalls += 1;
    return voiceOptions;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() {
    Get.reset();
  });

  Future<void> pumpWizard(
    WidgetTester tester, {
    _FakeAgentService? service,
    AgentSetupOpener? openSetup,
    int? presetProviderType,
  }) async {
    final fakeService = service ?? _FakeAgentService();
    Get.put<AgentCreateWizardController>(
      AgentCreateWizardController(
        agentService: fakeService,
        openSetup: openSetup ?? (_) async {},
        presetProviderType: presetProviderType,
      ),
    );
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateWizardView(),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('starts with task choices instead of the full form', (
    tester,
  ) async {
    await pumpWizard(tester);

    expect(find.byKey(const Key('agent-create-type-3')), findsOneWidget);
    expect(find.byKey(const Key('agent-create-type-1')), findsOneWidget);
    expect(find.byKey(const Key('agent-create-type-2')), findsOneWidget);
    expect(find.byKey(const Key('agent-create-type-4')), findsOneWidget);
    expect(find.byKey(const Key('agent-create-name-field')), findsNothing);
    expect(find.text('Delete Agent'), findsNothing);
  });

  testWidgets('preset provider opens the matching minimal form', (
    tester,
  ) async {
    final service = _FakeAgentService();
    await pumpWizard(tester, service: service, presetProviderType: 4);

    expect(find.byKey(const Key('agent-create-name-field')), findsOneWidget);
    expect(
      find.byKey(const Key('agent-create-voice-model-field')),
      findsOneWidget,
    );
    expect(service.voiceLoadCalls, 1);
  });

  testWidgets('reads preset_provider_type from route arguments', (
    tester,
  ) async {
    final service = _FakeAgentService();
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const SizedBox.shrink(),
        getPages: [
          GetPage(
            name: '/create',
            page: () => const AgentCreateWizardView(),
            binding: BindingsBuilder(() {
              Get.put(
                AgentCreateWizardController(
                  agentService: service,
                  openSetup: (_) async {},
                ),
              );
            }),
          ),
        ],
      ),
    );

    Get.toNamed<void>('/create', arguments: {'preset_provider_type': 4});
    await tester.pumpAndSettle();

    expect(Get.find<AgentCreateWizardController>().providerType.value, 4);
    expect(
      find.byKey(const Key('agent-create-voice-model-field')),
      findsOneWidget,
    );
    expect(service.voiceLoadCalls, 1);
  });

  testWidgets(
    'API path submits only minimal creation fields then opens setup',
    (tester) async {
      final service = _FakeAgentService()
        ..createResult = AgentModel(
          id: 'agent-1',
          agentName: 'Atlas',
          providerType: 3,
          apiEndpoint: 'wss://example.test/v1/agent-api',
          apiKey: 'secret',
        );
      AgentModel? openedAgent;
      await pumpWizard(
        tester,
        service: service,
        openSetup: (agent) async => openedAgent = agent,
      );

      await tester.tap(find.byKey(const Key('agent-create-type-3')));
      await tester.pumpAndSettle();
      await tester.enterText(
        find.byKey(const Key('agent-create-name-field')),
        'Atlas',
      );
      await tester.tap(find.byKey(const Key('agent-create-submit')));
      await tester.pumpAndSettle();

      expect(service.createPayload, {
        'agent_name': 'Atlas',
        'introduction': '',
        'provider_type': 3,
        'category_id': '0',
      });
      expect(openedAgent?.id, 'agent-1');
    },
  );

  testWidgets('local path keeps endpoint and model fields focused', (
    tester,
  ) async {
    final service = _FakeAgentService()
      ..createResult = AgentModel(
        id: 'agent-local',
        agentName: 'Local',
        providerType: 2,
      );
    await pumpWizard(tester, service: service);

    await tester.tap(find.byKey(const Key('agent-create-type-2')));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('agent-create-local-endpoint-field')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('agent-create-local-model-field')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('agent-create-prompt-field')), findsNothing);
  });

  testWidgets('voice path lazy-loads model and sends required BYOK fields', (
    tester,
  ) async {
    final service = _FakeAgentService()
      ..createResult = AgentModel(
        id: 'agent-voice',
        agentName: 'Voice',
        providerType: 4,
      );
    await pumpWizard(tester, service: service);

    expect(service.voiceLoadCalls, 0);
    final voiceType = find.byKey(const Key('agent-create-type-4'));
    await tester.ensureVisible(voiceType);
    await tester.tap(voiceType);
    await tester.pumpAndSettle();
    expect(service.voiceLoadCalls, 1);

    await tester.enterText(
      find.byKey(const Key('agent-create-name-field')),
      'Voice',
    );
    await tester.enterText(
      find.byKey(const Key('agent-create-voice-key-field')),
      'voice-secret',
    );
    await tester.tap(find.byKey(const Key('agent-create-submit')));
    await tester.pumpAndSettle();

    expect(service.createPayload?['voice_provider'], 'openai_realtime');
    expect(service.createPayload?['voice_model'], 'gpt-realtime');
    expect(service.createPayload?['voice_id'], 'alloy');
    expect(service.createPayload?['voice_api_key'], 'voice-secret');
  });

  testWidgets('fits a 320px-wide mobile viewport without overflow', (
    tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(320, 720);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);

    await pumpWizard(tester);
    expect(tester.takeException(), isNull);
  });
}
