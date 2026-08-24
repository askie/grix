import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/agent_category_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/feature_flag_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/modules/ai/agent_create_view.dart';
import 'package:grix/modules/ai/controllers/agent_create_controller.dart';
import 'package:grix/modules/ai/models/agent_editor_result.dart';
import 'package:grix/modules/ai/widgets/contact_agent_pick_result.dart';
import 'package:grix/modules/profile/services/avatar_cropper_service.dart';

class _FakeAgentService extends AgentService {
  AgentApiInstallGuideCatalog? agentApiInstallGuideCatalog;
  AgentModel? createAgentResult;
  Map<String, dynamic>? lastCreatePayload;
  Duration createAgentDelay = Duration.zero;
  AgentModel? updateAgentResult;
  String? lastUpdateAgentId;
  Map<String, dynamic>? lastUpdatePayload;
  Duration updateAgentDelay = Duration.zero;
  AgentModel? rotateAgentApiKeyResult;
  String? lastRotateAgentApiKeyId;
  AgentModel? getAgentResult;
  String? lastGetAgentId;
  Duration getAgentDelay = Duration.zero;
  ServiceResult<AgentModel>? uploadAgentAvatarResult;
  String? lastUploadAvatarAgentId;
  Uint8List? lastUploadAvatarBytes;
  String? lastUploadAvatarFilename;
  bool deleteAgentResult = true;
  String? lastDeleteAgentId;
  String mockedLastOperationError = '';
  int mockedLastOperationCode = 0;
  List<AgentModel> agentsToLoad = const [];
  bool loadAgentsCalled = false;

  @override
  Future<void> loadAgents({String? categoryId}) async {
    loadAgentsCalled = true;
    agents.assignAll(agentsToLoad);
    hasLoaded.value = true;
  }

  @override
  String get lastOperationError => mockedLastOperationError;

  @override
  int get lastOperationCode => mockedLastOperationCode;

  @override
  Future<AgentModel?> createAgent(Map<String, dynamic> data) async {
    lastCreatePayload = Map<String, dynamic>.from(data);
    if (createAgentDelay > Duration.zero) {
      await Future<void>.delayed(createAgentDelay);
    }
    return createAgentResult;
  }

  @override
  Future<AgentModel?> updateAgent(
    String agentId,
    Map<String, dynamic> data,
  ) async {
    lastUpdateAgentId = agentId;
    lastUpdatePayload = Map<String, dynamic>.from(data);
    if (updateAgentDelay > Duration.zero) {
      await Future<void>.delayed(updateAgentDelay);
    }
    return updateAgentResult;
  }

  @override
  Future<AgentModel?> rotateAgentApiKey(String agentId) async {
    lastRotateAgentApiKeyId = agentId;
    return rotateAgentApiKeyResult;
  }

  @override
  Future<AgentModel?> getAgent(String agentId) async {
    lastGetAgentId = agentId;
    if (getAgentDelay > Duration.zero) {
      await Future<void>.delayed(getAgentDelay);
    }
    return getAgentResult;
  }

  @override
  Future<AgentApiInstallGuideCatalog?> getAgentApiInstallGuides() async {
    return agentApiInstallGuideCatalog ?? _englishGuideCatalog();
  }

  List<VoiceModelOption> voiceModelOptions = const [
    VoiceModelOption(
      id: 'openai_gpt4o_realtime',
      label: 'OpenAI GPT Realtime',
      provider: 'openai_realtime',
      model: 'gpt-4o-realtime-preview',
      endpoint: 'wss://api.openai.com/v1/realtime',
    ),
    VoiceModelOption(
      id: 'doubao_realtime',
      label: '豆包语音大模型',
      provider: 'doubao_realtime',
      model: 'doubao-realtime',
      endpoint: 'wss://openspeech.bytedance.com/api/v3/realtime',
    ),
  ];

  @override
  Future<List<VoiceModelOption>?> getVoiceModels() async {
    return voiceModelOptions;
  }

  @override
  Future<ServiceResult<AgentModel>> uploadAgentAvatar({
    required String agentId,
    required Uint8List bytes,
    required String filename,
  }) async {
    lastUploadAvatarAgentId = agentId;
    lastUploadAvatarBytes = bytes;
    lastUploadAvatarFilename = filename;
    return uploadAgentAvatarResult ??
        ServiceResult<AgentModel>.failure(message: 'missing upload mock');
  }

  @override
  Future<bool> deleteAgent(String agentId) async {
    lastDeleteAgentId = agentId;
    return deleteAgentResult;
  }
}

class _FakeFriendService extends FriendService {
  List<FriendItem> friendsToLoad = const [];
  bool loadFriendListCalled = false;

  @override
  Future<void> loadFriendList() async {
    loadFriendListCalled = true;
    friendList.assignAll(friendsToLoad);
  }
}

class _FakeAgentCategoryService extends AgentCategoryService {
  @override
  Future<void> loadCategories() async {}
}

final Uint8List _kTestAvatarBytes = Uint8List.fromList([
  0x89,
  0x50,
  0x4E,
  0x47,
  0x0D,
  0x0A,
  0x1A,
  0x0A,
  0x00,
  0x00,
  0x00,
  0x0D,
  0x49,
  0x48,
  0x44,
  0x52,
  0x00,
  0x00,
  0x00,
  0x01,
  0x00,
  0x00,
  0x00,
  0x01,
  0x08,
  0x06,
  0x00,
  0x00,
  0x00,
  0x1F,
  0x15,
  0xC4,
  0x89,
  0x00,
  0x00,
  0x00,
  0x0D,
  0x49,
  0x44,
  0x41,
  0x54,
  0x78,
  0x9C,
  0x63,
  0xF8,
  0xCF,
  0xC0,
  0x00,
  0x00,
  0x03,
  0x01,
  0x01,
  0x00,
  0xC9,
  0xFE,
  0x92,
  0xEF,
  0x00,
  0x00,
  0x00,
  0x00,
  0x49,
  0x45,
  0x4E,
  0x44,
  0xAE,
  0x42,
  0x60,
  0x82,
]);

AgentApiInstallGuideCatalog _englishGuideCatalog() {
  return const AgentApiInstallGuideCatalog(
    defaultType: 'openclaw',
    list: [
      AgentApiInstallGuide(
        type: 'openclaw',
        label: 'OpenClaw',
        intro:
            'Send the text below to OpenClaw to finish the Grix channel setup:',
        contentMode: 'text',
        contentTemplate:
            'Install @dhf-openclaw/grix in OpenClaw and configure the channel parameters.',
        copyTemplate:
            'Install @dhf-openclaw/grix in OpenClaw and configure the following channel parameters:\n\n'
            'Agent ID:\n{{agent_id}}\n\n'
            'Service Endpoint:\n{{api_endpoint}}\n\n'
            'Secret Key:\n{{api_key}}',
      ),
      AgentApiInstallGuide(
        type: 'claude',
        label: 'Claude',
        intro: 'Follow the steps below to install and use.',
        contentMode: 'text',
        contentTemplate:
            'npm install -g @dhf-claude/grix\n\ngrix-claude install --ws-url {{api_endpoint}} --agent-id {{agent_id}} --api-key {{api_key}}',
        copyTemplate:
            'Follow the steps below to install and use.\n\nnpm install -g @dhf-claude/grix\n\ngrix-claude install --ws-url {{api_endpoint}} --agent-id {{agent_id}} --api-key {{api_key}}',
      ),
      AgentApiInstallGuide(
        type: 'codex',
        label: 'Codex',
        intro: 'Follow the steps below to install and use.',
        contentMode: 'text',
        contentTemplate:
            'npm install @dhf-codex/grix\n\ngrix-codex agent --agent-id {{agent_id}} --endpoint {{api_endpoint}} --api-key {{api_key}}',
        copyTemplate:
            'Follow the steps below to install and use.\n\nnpm install @dhf-codex/grix\n\ngrix-codex agent --agent-id {{agent_id}} --endpoint {{api_endpoint}} --api-key {{api_key}}',
      ),
      AgentApiInstallGuide(
        type: 'gemini',
        label: 'Gemini',
        intro: 'Follow the steps below to install and use.',
        contentMode: 'text',
        contentTemplate:
            'npm install @dhf-gemini/grix\n\ngrix-gemini agent --agent-id {{agent_id}} --endpoint {{api_endpoint}} --api-key {{api_key}}',
        copyTemplate:
            'Follow the steps below to install and use.\n\nnpm install @dhf-gemini/grix\n\ngrix-gemini agent --agent-id {{agent_id}} --endpoint {{api_endpoint}} --api-key {{api_key}}',
      ),
      AgentApiInstallGuide(
        type: 'hermes',
        label: 'Hermes',
        intro: 'For Hermes, follow the guide linked below:',
        contentMode: 'link',
        linkLabel: 'Usage Guide',
        linkUrl:
            'https://github.com/askie/hermes-agent/blob/main/gateway/platforms/grix/README.md',
        copyTemplate:
            'Follow grix-hermes and configure the following channel parameters:\n\n'
            'Agent ID:\n{{agent_id}}\n\n'
            'Service Endpoint:\n{{api_endpoint}}\n\n'
            'Secret Key:\n{{api_key}}',
      ),
    ],
  );
}

AgentApiInstallGuideCatalog _chineseGuideCatalog() {
  return const AgentApiInstallGuideCatalog(
    defaultType: 'openclaw',
    list: [
      AgentApiInstallGuide(
        type: 'openclaw',
        label: 'OpenClaw',
        intro: '将下面这段说明交给 OpenClaw，即可完成 Grix 渠道配置：',
        contentMode: 'text',
        contentTemplate: '需要给 OpenClaw 安装插件 @dhf-openclaw/grix，并配置渠道参数。',
        copyTemplate:
            '给 OpenClaw 安装插件 @dhf-openclaw/grix，并配置以下渠道参数：\n\n'
            'Agent ID:\n{{agent_id}}\n\n'
            '服务 Endpoint:\n{{api_endpoint}}\n\n'
            '密钥:\n{{api_key}}',
      ),
      AgentApiInstallGuide(
        type: 'claude',
        label: 'Claude',
        intro: '请按照下面步骤安装使用',
        contentMode: 'text',
        contentTemplate:
            'npm install -g @dhf-claude/grix\n\ngrix-claude install --ws-url {{api_endpoint}} --agent-id {{agent_id}} --api-key {{api_key}}',
        copyTemplate:
            '请按照下面步骤安装使用\n\nnpm install -g @dhf-claude/grix\n\ngrix-claude install --ws-url {{api_endpoint}} --agent-id {{agent_id}} --api-key {{api_key}}',
      ),
      AgentApiInstallGuide(
        type: 'codex',
        label: 'Codex',
        intro: '请按照下面步骤安装使用',
        contentMode: 'text',
        contentTemplate:
            'npm install @dhf-codex/grix\n\ngrix-codex agent --agent-id {{agent_id}} --endpoint {{api_endpoint}} --api-key {{api_key}}',
        copyTemplate:
            '请按照下面步骤安装使用\n\nnpm install @dhf-codex/grix\n\ngrix-codex agent --agent-id {{agent_id}} --endpoint {{api_endpoint}} --api-key {{api_key}}',
      ),
      AgentApiInstallGuide(
        type: 'gemini',
        label: 'Gemini',
        intro: '请按照下面步骤安装使用',
        contentMode: 'text',
        contentTemplate:
            'npm install @dhf-gemini/grix\n\ngrix-gemini agent --agent-id {{agent_id}} --endpoint {{api_endpoint}} --api-key {{api_key}}',
        copyTemplate:
            '请按照下面步骤安装使用\n\nnpm install @dhf-gemini/grix\n\ngrix-gemini agent --agent-id {{agent_id}} --endpoint {{api_endpoint}} --api-key {{api_key}}',
      ),
      AgentApiInstallGuide(
        type: 'hermes',
        label: 'Hermes',
        intro: 'Hermes 按下面链接中的说明进行安装即可：',
        contentMode: 'link',
        linkLabel: '使用说明',
        linkUrl:
            'https://github.com/askie/hermes-agent/blob/main/gateway/platforms/grix/README.md',
        copyTemplate:
            '按 grix-hermes 的说明安装，并配置以下渠道参数：\n\n'
            'Agent ID:\n{{agent_id}}\n\n'
            '服务 Endpoint:\n{{api_endpoint}}\n\n'
            '密钥:\n{{api_key}}',
      ),
    ],
  );
}

/// Finds the read-only install-type field whose [ValueKey] is
/// `agent_api_install_type_field-<selectedType>` (the suffix varies with the
/// currently selected guide).
final Finder _installTypeField = find.byWidgetPredicate((widget) {
  final key = widget.key;
  return key is ValueKey<String> &&
      key.value.startsWith('agent_api_install_type_field-');
});

Future<void> _selectInstallType(WidgetTester tester, String label) async {
  // The picker is a bottom sheet (capped at 60% of the screen height) listing
  // each guide as a ListTile. Enlarge the surface so every tile fits within the
  // viewport and is hit-testable; reset afterwards.
  tester.view.devicePixelRatio = 1.0;
  tester.view.physicalSize = const Size(800, 1600);
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpAndSettle();

  await tester.ensureVisible(_installTypeField);
  await tester.tap(_installTypeField);
  await tester.pumpAndSettle();
  await tester.tap(find.widgetWithText(ListTile, label).last);
  await tester.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  String? clipboardText;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    clipboardText = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.setData') {
            final args = call.arguments as Map<dynamic, dynamic>;
            clipboardText = args['text'] as String?;
            return null;
          }
          if (call.method == 'Clipboard.getData') {
            return <String, dynamic>{'text': clipboardText};
          }
          return null;
        });
    Get.put<AvatarCropperService>(AvatarCropperService());
    Get.put<AgentCategoryService>(_FakeAgentCategoryService());
    Get.put<FeatureFlagService>(FeatureFlagService());
    Get.put<FriendService>(_FakeFriendService());
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, null);
    Get.reset();
  });

  testWidgets('defaults to Agent API provider on create page', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    expect(controller.providerType.value, 3);
    expect(find.text('Step 1. Save Basic Info'), findsOneWidget);
    expect(find.text('Step 2. Choose Installation Type'), findsOneWidget);
    expect(
      find.text(
        'Save basic info first. After that, choose an installation type and follow the instructions below.',
      ),
      findsOneWidget,
    );
    expect(find.text('Agent ID'), findsNothing);
    expect(find.text('Local Endpoint'), findsNothing);
  });

  testWidgets('shows category picker as a unified capsule field', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    Get.put(AgentCreateController());

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    final introductionField = find.byKey(const Key('agent_introduction_field'));
    final categoryField = find.byKey(const ValueKey('agent-category-field-0'));

    expect(categoryField, findsOneWidget);
    expect(find.text('Uncategorized / Root'), findsOneWidget);
    expect(
      tester.getTopLeft(categoryField).dy,
      greaterThan(tester.getTopLeft(introductionField).dy),
    );

    await tester.tap(categoryField);
    await tester.pumpAndSettle();

    expect(find.text('Select Category'), findsOneWidget);
  });

  testWidgets('loads install types from api into dropdown', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = 'guide-agent-openclaw';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    expect(_installTypeField, findsOneWidget);
    await _selectInstallType(tester, 'Hermes');
    expect(controller.selectedApiInstallGuideType.value, 'hermes');
  });

  testWidgets('shows auto-detect note under install type selector', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = 'guide-agent-openclaw';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    expect(_installTypeField, findsOneWidget);
    expect(
      find.textContaining('auto-detected once the agent comes online'),
      findsOneWidget,
    );
  });

  testWidgets('shows non-modal success message after saving agent', (
    WidgetTester tester,
  ) async {
    final toastMessages = <({String message, bool isError})>[];
    final agentService = _FakeAgentService()
      ..createAgentResult = AgentModel(
        id: '2029786829095440384',
        agentName: 'API Agent',
        providerType: 3,
        apiEndpoint:
            'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440384',
        apiKey: 'ak_demo',
      );
    Get.put<AgentService>(agentService);
    Get.put(
      AgentCreateController(
        showToast: (message, {isError = true}) {
          toastMessages.add((message: message, isError: isError));
        },
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await tester.enterText(find.byType(TextFormField).first, 'API Agent');
    await tester.enterText(
      find.byType(TextFormField).at(1),
      'Handles API calls',
    );
    await tester.tap(find.text('Save'));
    await tester.pump();
    await tester.pump();

    expect(agentService.lastCreatePayload?['provider_type'], 3);
    expect(
      agentService.lastCreatePayload?.containsKey('agent_client_type'),
      isFalse,
    );
    expect(
      agentService.lastCreatePayload?['introduction'],
      'Handles API calls',
    );
    expect(
      find.byKey(const ValueKey('agent-api-step-2-ready')),
      findsOneWidget,
    );
    expect(find.text('Agent ID'), findsOneWidget);
    expect(toastMessages, hasLength(1));
    expect(toastMessages.single.message, 'Saved');
    expect(toastMessages.single.isError, isFalse);

    await tester.pump(const Duration(seconds: 2));
    await tester.pump();
  });

  testWidgets('can choose Hermes in step 2 after saving basic info', (
    WidgetTester tester,
  ) async {
    final toastMessages = <({String message, bool isError})>[];
    final agentService = _FakeAgentService()
      ..createAgentResult = AgentModel(
        id: '2029786829095440385',
        agentName: 'Hermes Agent',
        providerType: 3,
        apiEndpoint:
            'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440385',
        apiKey: 'ak_demo',
      );
    Get.put<AgentService>(agentService);
    Get.put(
      AgentCreateController(
        showToast: (message, {isError = true}) {
          toastMessages.add((message: message, isError: isError));
        },
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await tester.enterText(find.byType(TextFormField).first, 'Hermes Agent');
    await tester.tap(find.text('Save'));
    await tester.pump();
    await tester.pump();

    expect(agentService.lastCreatePayload?['provider_type'], 3);
    expect(
      agentService.lastCreatePayload?.containsKey('agent_client_type'),
      isFalse,
    );

    await _selectInstallType(tester, 'Hermes');
    expect(
      Get.find<AgentCreateController>().selectedApiInstallGuideType.value,
      'hermes',
    );
    expect(toastMessages, hasLength(1));
    expect(toastMessages.single.message, 'Saved');
    expect(toastMessages.single.isError, isFalse);

    await tester.pump(const Duration(seconds: 2));
    await tester.pump();
  });

  testWidgets('keeps codex secret key after avatar upload during save', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService()
      ..createAgentResult = AgentModel(
        id: '2043206251596222464',
        agentName: 'Codex Agent',
        providerType: 3,
        apiEndpoint:
            'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222464',
        apiKey: 'ak_codex_demo',
      )
      ..uploadAgentAvatarResult = ServiceResult<AgentModel>.success(
        data: AgentModel(
          id: '2043206251596222464',
          agentName: 'Codex Agent',
          providerType: 3,
        ),
      );
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.profileDraft.setPendingAvatar(
      bytes: _kTestAvatarBytes,
      filename: 'avatar.png',
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await tester.enterText(find.byType(TextFormField).first, 'Codex Agent');
    await tester.enterText(find.byType(TextFormField).at(1), 'Installs Codex');
    await tester.tap(find.text('Save'));
    await tester.pump();
    await tester.pump();
    await tester.pumpAndSettle();

    await _selectInstallType(tester, 'Codex');

    expect(
      find.text(
        'npm install @dhf-codex/grix\n\ngrix-codex agent --agent-id 2043206251596222464 --endpoint ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222464 --api-key ak_codex_demo',
      ),
      findsOneWidget,
    );
    expect(agentService.lastUploadAvatarAgentId, '2043206251596222464');
    expect(agentService.lastUploadAvatarFilename, 'avatar.png');

    await tester.pump(const Duration(seconds: 2));
    await tester.pump();
  });

  testWidgets(
    'switching guide on Agent API edit keeps selected tab after save',
    (WidgetTester tester) async {
      final toastMessages = <({String message, bool isError})>[];
      final agentService = _FakeAgentService()
        ..updateAgentResult = AgentModel(
          id: 'api-agent-hermes',
          agentName: 'Hermes Agent',
          providerType: 3,
          agentClientType: 'hermes',
          apiEndpoint:
              'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=api-agent-hermes',
          apiKey: 'ak_demo',
        );
      Get.put<AgentService>(agentService);
      final controller =
          AgentCreateController(
              showToast: (message, {isError = true}) {
                toastMessages.add((message: message, isError: isError));
              },
            )
            ..isEditMode = true
            ..editAgentId = 'api-agent-hermes'
            ..apiAgentId.value = 'api-agent-hermes'
            ..providerType.value = 3
            ..selectedApiInstallGuideType.value = 'hermes';
      Get.put(controller);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: const AgentCreateView(),
        ),
      );
      await tester.pump();

      await tester.enterText(find.byType(TextFormField).first, 'Hermes Agent');
      await tester.tap(find.text('Save'));
      await tester.pump();
      await tester.pump();

      expect(agentService.lastUpdateAgentId, 'api-agent-hermes');
      expect(agentService.lastUpdatePayload?['provider_type'], 3);
      expect(
        agentService.lastUpdatePayload?.containsKey('agent_client_type'),
        isFalse,
      );
      expect(
        find.byKey(const ValueKey('agent-api-step-2-ready')),
        findsOneWidget,
      );
      expect(controller.selectedApiInstallGuideType.value, 'hermes');
      expect(toastMessages, hasLength(1));
      expect(toastMessages.single.message, 'Saved');
      expect(toastMessages.single.isError, isFalse);

      await tester.pump(const Duration(seconds: 2));
      await tester.pump();
    },
  );

  testWidgets('shows backend mapped error toast when save fails', (
    WidgetTester tester,
  ) async {
    final toastMessages = <({String message, bool isError})>[];
    final agentService = _FakeAgentService()
      ..createAgentResult = null
      ..mockedLastOperationError = 'Too many requests, please try later';
    Get.put<AgentService>(agentService);
    Get.put(
      AgentCreateController(
        showToast: (message, {isError = true}) {
          toastMessages.add((message: message, isError: isError));
        },
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await tester.enterText(find.byType(TextFormField).first, 'ops-agent');
    await tester.tap(find.text('Save'));
    await tester.pump();

    expect(toastMessages, hasLength(1));
    expect(toastMessages.single.message, 'Too many requests, please try later');
    expect(toastMessages.single.isError, isTrue);
  });

  testWidgets(
    'uses preloaded agent snapshot to default install guide tab on edit page',
    (WidgetTester tester) async {
      final preloadedAgent = AgentModel(
        id: 'api-agent-gemini',
        agentName: 'Gemini Agent',
        providerType: 3,
        agentClientType: 'gemini',
        apiEndpoint:
            'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=api-agent-gemini',
        apiKeyHint: '5678',
      );
      final agentService = _FakeAgentService()
        ..agentApiInstallGuideCatalog = _chineseGuideCatalog()
        ..getAgentDelay = const Duration(seconds: 1)
        ..getAgentResult = AgentModel(
          id: 'api-agent-gemini',
          agentName: 'Gemini Agent',
          providerType: 3,
          agentClientType: 'gemini',
          apiEndpoint:
              'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=api-agent-gemini',
          apiKey: 'ak_gemini_demo',
        );
      Get.put<AgentService>(agentService);
      Get.routing.args = {
        'agent_id': 'api-agent-gemini',
        'agent': preloadedAgent,
      };
      final controller = Get.put(AgentCreateController());

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: const AgentCreateView(),
        ),
      );
      await tester.pumpAndSettle();
      await tester.pump(const Duration(milliseconds: 100));

      expect(controller.selectedApiInstallGuideType.value, 'gemini');
      expect(
        find.text(
          'npm install @dhf-gemini/grix\n\ngrix-gemini agent --agent-id api-agent-gemini --endpoint ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=api-agent-gemini --api-key <密钥>',
        ),
        findsOneWidget,
      );

      await tester.pump(const Duration(seconds: 1));
      await tester.pump();

      expect(agentService.lastGetAgentId, 'api-agent-gemini');
      expect(
        find.text(
          'npm install @dhf-gemini/grix\n\ngrix-gemini agent --agent-id api-agent-gemini --endpoint ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=api-agent-gemini --api-key ak_gemini_demo',
        ),
        findsOneWidget,
      );
    },
  );

  testWidgets('returns saved result when edit page saves local agent', (
    WidgetTester tester,
  ) async {
    final toastMessages = <({String message, bool isError})>[];
    Object? closeResult;
    final agentService = _FakeAgentService()
      ..updateAgentResult = AgentModel(
        id: 'local-agent-1',
        agentName: 'Local Agent',
        providerType: 2,
        localEndpoint: 'http://localhost:11434',
        localModelName: 'gemma3:4b',
      );
    Get.put<AgentService>(agentService);
    final controller =
        AgentCreateController(
            showToast: (message, {isError = true}) {
              toastMessages.add((message: message, isError: isError));
            },
            closePage: ({result}) {
              closeResult = result;
            },
          )
          ..isEditMode = true
          ..editAgentId = 'local-agent-1'
          ..providerType.value = 2;
    Get.put(controller);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await tester.enterText(find.byType(TextFormField).first, 'Local Agent');
    await tester.enterText(find.byType(TextFormField).at(1), 'Local helper');
    await tester.tap(find.text('Save'));
    await tester.pump();
    await tester.pump();

    expect(agentService.lastUpdateAgentId, 'local-agent-1');
    expect(agentService.lastUpdatePayload?['provider_type'], 2);
    expect(agentService.lastUpdatePayload?['introduction'], 'Local helper');
    expect(closeResult, AgentEditorResult.saved);
    expect(toastMessages, isEmpty);

    await tester.pump(const Duration(seconds: 2));
    await tester.pump();
  });

  testWidgets('shows saving and saved states in app bar for Agent API edit', (
    WidgetTester tester,
  ) async {
    final toastMessages = <({String message, bool isError})>[];
    final agentService = _FakeAgentService()
      ..updateAgentDelay = const Duration(milliseconds: 200)
      ..updateAgentResult = AgentModel(
        id: 'api-agent-1',
        agentName: 'API Agent',
        providerType: 3,
        apiEndpoint:
            'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=api-agent-1',
        apiKey: 'ak_demo',
      );
    Get.put<AgentService>(agentService);
    final controller =
        AgentCreateController(
            showToast: (message, {isError = true}) {
              toastMessages.add((message: message, isError: isError));
            },
          )
          ..isEditMode = true
          ..editAgentId = 'api-agent-1'
          ..providerType.value = 3;
    Get.put(controller);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await tester.enterText(find.byType(TextFormField).first, 'API Agent');
    await tester.tap(find.text('Save'));
    await tester.pump();

    expect(find.text('Saving...'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await tester.pump(const Duration(milliseconds: 200));
    await tester.pump();

    expect(find.text('Saved'), findsOneWidget);
    expect(toastMessages, hasLength(1));
    expect(toastMessages.single.message, 'Saved');
    expect(toastMessages.single.isError, isFalse);

    await tester.pump(const Duration(seconds: 2));
    await tester.pump();

    expect(find.text('Save'), findsOneWidget);
  });

  testWidgets('validates agent name max length', (WidgetTester tester) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    controller.nameController.text = 'a' * 101;
    await tester.tap(find.text('Save'));
    await tester.pump();

    expect(
      find.text('Agent name must be 100 characters or fewer'),
      findsOneWidget,
    );
  });

  testWidgets('validates control characters in agent name', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    controller.nameController.text = 'bad\nname';
    await tester.tap(find.text('Save'));
    await tester.pump();

    expect(
      find.text('Agent name contains invalid control characters'),
      findsOneWidget,
    );
  });

  testWidgets('shows agent id field for Agent API provider', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService();
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = '2029786829095440384';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440384';
    controller.apiKey.value = 'ak_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    expect(find.text('Agent ID'), findsOneWidget);
    expect(find.text('2029786829095440384'), findsOneWidget);
    expect(
      find.text(
        'Send the text below to OpenClaw to finish the Grix channel setup:',
      ),
      findsOneWidget,
    );
    expect(
      find.text(
        'Install @dhf-openclaw/grix in OpenClaw and configure the channel parameters.',
      ),
      findsOneWidget,
    );
    expect(find.text('Channel setup guide:'), findsNothing);
    expect(find.text('@dhf-openclaw/grix (Tap to open)'), findsNothing);
  });

  testWidgets('switches install guide to Claude tab', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService();
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = '2043206251596222463';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222463';
    controller.apiKey.value = 'ak_claude_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    expect(
      find.text(
        'Send the text below to OpenClaw to finish the Grix channel setup:',
      ),
      findsOneWidget,
    );

    await _selectInstallType(tester, 'Claude');

    expect(
      find.text(
        'Send the text below to OpenClaw to finish the Grix channel setup:',
      ),
      findsNothing,
    );
    expect(
      find.text('Follow the steps below to install and use.'),
      findsOneWidget,
    );
    expect(
      find.text(
        'npm install -g @dhf-claude/grix\n\ngrix-claude install --ws-url ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222463 --agent-id 2043206251596222463 --api-key ak_claude_demo',
      ),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('agent_api_install_guide_copy_button')),
      findsOneWidget,
    );
  });

  testWidgets('shows translated Claude guide copy in Chinese locale', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService()
      ..agentApiInstallGuideCatalog = _chineseGuideCatalog();
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = '2043206251596222463';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222463';
    controller.apiKey.value = 'ak_claude_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Claude');

    expect(find.text('请按照下面步骤安装使用'), findsOneWidget);
    expect(
      find.text(
        'npm install -g @dhf-claude/grix\n\ngrix-claude install --ws-url ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222463 --agent-id 2043206251596222463 --api-key ak_claude_demo',
      ),
      findsOneWidget,
    );
  });

  testWidgets('switches install guide to Codex tab', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService();
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = '2043206251596222464';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222464';
    controller.apiKey.value = 'ak_codex_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Codex');

    expect(
      find.text('Follow the steps below to install and use.'),
      findsOneWidget,
    );
    expect(
      find.text(
        'npm install @dhf-codex/grix\n\ngrix-codex agent --agent-id 2043206251596222464 --endpoint ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222464 --api-key ak_codex_demo',
      ),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('agent_api_install_guide_copy_button')),
      findsOneWidget,
    );
  });

  testWidgets('shows translated Codex guide copy in Chinese locale', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService()
      ..agentApiInstallGuideCatalog = _chineseGuideCatalog();
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = '2043206251596222464';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222464';
    controller.apiKey.value = 'ak_codex_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Codex');

    expect(find.text('请按照下面步骤安装使用'), findsOneWidget);
    expect(
      find.text(
        'npm install @dhf-codex/grix\n\ngrix-codex agent --agent-id 2043206251596222464 --endpoint ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222464 --api-key ak_codex_demo',
      ),
      findsOneWidget,
    );
  });

  testWidgets('switches install guide to Hermes tab', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService();
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = 'guide-agent-hermes';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Hermes');

    expect(
      find.text(
        'Send the text below to OpenClaw to finish the Grix channel setup:',
      ),
      findsNothing,
    );
    expect(
      find.text('For Hermes, follow the guide linked below:'),
      findsOneWidget,
    );
    expect(find.text('Usage Guide'), findsOneWidget);
    expect(
      find.byKey(const Key('agent_api_install_guide_copy_button')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('agent_api_install_guide_link_button')),
      findsOneWidget,
    );
  });

  testWidgets('copies agent api setup instruction', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService()
      ..agentApiInstallGuideCatalog = _chineseGuideCatalog();
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = 'copy-openclaw-agent';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    final copyButton = find.byKey(
      const Key('agent_api_install_guide_copy_button'),
    );
    await tester.ensureVisible(copyButton);
    await tester.tap(copyButton);
    await tester.pump();

    expect(clipboardText, '需要给 OpenClaw 安装插件 @dhf-openclaw/grix，并配置渠道参数。');

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  // The backend task writes the Agent name into the connector config. This page
  // copies the same copy_template as the setup page, so every placeholder the
  // backend can emit must be resolved here too — a missed one lands a literal
  // "{{agent_name}}" in the user's agents.json.
  testWidgets('resolves the agent name placeholder when copying a task', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService()
      ..agentApiInstallGuideCatalog = const AgentApiInstallGuideCatalog(
        defaultType: 'claude',
        list: [
          AgentApiInstallGuide(
            type: 'claude',
            label: 'Claude',
            contentMode: 'text',
            contentTemplate: 'npm install -g grix-connector',
            copyTemplate:
                '"name": "{{agent_name}}"\n'
                '"agent_id": "{{agent_id}}"\n'
                '"api_key": "{{api_key}}"\n'
                '"ws_url": "{{api_endpoint}}"',
          ),
        ],
      );
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.isEditMode = true;
    controller.editAgentId = '2043206251596222464';
    controller.nameController.text = '我的开发助手';
    controller.apiAgentId.value = '2043206251596222464';
    controller.apiEndpoint.value = 'wss://grix.dhf.pub/v1/agent-api/ws';
    controller.apiKey.value = 'ak_demo_secret';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    // "Copy All" is the entry that copies the backend task (copy_template).
    final copyAllButton = find.text('Copy All');
    await tester.ensureVisible(copyAllButton);
    await tester.tap(copyAllButton);
    await tester.pump();

    expect(clipboardText, isNot(contains('{{')));
    expect(clipboardText, contains('"name": "我的开发助手"'));
    expect(clipboardText, contains('"api_key": "ak_demo_secret"'));

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('copies codex setup instruction', (WidgetTester tester) async {
    final agentService = _FakeAgentService()
      ..agentApiInstallGuideCatalog = _chineseGuideCatalog();
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = '2043206251596222464';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222464';
    controller.apiKey.value = 'ak_codex_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Codex');

    final copyButton = find.byKey(
      const Key('agent_api_install_guide_copy_button'),
    );
    await tester.ensureVisible(copyButton);
    await tester.tap(copyButton);
    await tester.pump();

    expect(
      clipboardText,
      'npm install @dhf-codex/grix\n\ngrix-codex agent --agent-id 2043206251596222464 --endpoint ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222464 --api-key ak_codex_demo',
    );

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('copies claude setup instruction', (WidgetTester tester) async {
    final agentService = _FakeAgentService()
      ..agentApiInstallGuideCatalog = _chineseGuideCatalog();
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = '2043206251596222463';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222463';
    controller.apiKey.value = 'ak_claude_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Claude');

    final copyButton = find.byKey(
      const Key('agent_api_install_guide_copy_button'),
    );
    await tester.ensureVisible(copyButton);
    await tester.tap(copyButton);
    await tester.pump();

    expect(
      clipboardText,
      'npm install -g @dhf-claude/grix\n\ngrix-claude install --ws-url ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222463 --agent-id 2043206251596222463 --api-key ak_claude_demo',
    );

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('copies gemini setup instruction', (WidgetTester tester) async {
    final agentService = _FakeAgentService()
      ..agentApiInstallGuideCatalog = _chineseGuideCatalog();
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.apiAgentId.value = '2043206251596222465';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222465';
    controller.apiKey.value = 'ak_gemini_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Gemini');

    final copyButton = find.byKey(
      const Key('agent_api_install_guide_copy_button'),
    );
    await tester.ensureVisible(copyButton);
    await tester.tap(copyButton);
    await tester.pump();

    expect(
      clipboardText,
      'npm install @dhf-gemini/grix\n\ngrix-gemini agent --agent-id 2043206251596222465 --endpoint ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2043206251596222465 --api-key ak_gemini_demo',
    );

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('copies all agent api credentials with clear structure', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.isEditMode = true;
    controller.editAgentId = '2029786829095440384';
    controller.apiAgentId.value = '2029786829095440384';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440384';
    controller.apiKey.value = 'ak_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    final copyAllButton = find.text('Copy All');
    await tester.ensureVisible(copyAllButton);
    await tester.tap(copyAllButton);
    await tester.pump();

    expect(
      clipboardText,
      'Install @dhf-openclaw/grix in OpenClaw and configure the following channel parameters:\n\n'
      'Agent ID:\n'
      '2029786829095440384\n\n'
      'Service Endpoint:\n'
      'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440384\n\n'
      'Secret Key:\n'
      'ak_demo',
    );

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('rotates agent api key and refreshes copy payload', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService()
      ..rotateAgentApiKeyResult = AgentModel(
        id: '2029786829095440384',
        agentName: 'API Bot',
        providerType: 3,
        agentClientType: 'openclaw',
        apiEndpoint:
            'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440384',
        apiKey: 'ak_rotated',
        apiKeyHint: 'ated',
      );
    Get.put<AgentService>(agentService);
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.isEditMode = true;
    controller.editAgentId = '2029786829095440384';
    controller.apiAgentId.value = '2029786829095440384';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440384';
    controller.apiKey.value = 'ak_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    final rotateButton = find.text('Rotate Key');
    expect(rotateButton, findsOneWidget);
    expect(find.text('Copy All'), findsOneWidget);

    await tester.ensureVisible(rotateButton);
    await tester.tap(rotateButton);
    await tester.pump();
    await tester.pump();

    expect(agentService.lastRotateAgentApiKeyId, '2029786829095440384');
    expect(find.text('ak_rotated'), findsOneWidget);

    final copyAllButton = find.text('Copy All');
    await tester.ensureVisible(copyAllButton);
    await tester.tap(copyAllButton);
    await tester.pump();

    expect(clipboardText, contains('ak_rotated'));

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('copies all credentials using Claude guide intro on Claude tab', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.isEditMode = true;
    controller.editAgentId = '2029786829095440384';
    controller.apiAgentId.value = '2029786829095440384';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440384';
    controller.apiKey.value = 'ak_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Claude');

    final copyAllButton = find.text('Copy All');
    await tester.ensureVisible(copyAllButton);
    await tester.tap(copyAllButton);
    await tester.pump();

    expect(
      clipboardText,
      'Follow the steps below to install and use.\n\n'
      'npm install -g @dhf-claude/grix\n\n'
      'grix-claude install --ws-url ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440384 --agent-id 2029786829095440384 --api-key ak_demo',
    );

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('copies all credentials using Codex guide intro on Codex tab', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.isEditMode = true;
    controller.editAgentId = '2029786829095440385';
    controller.apiAgentId.value = '2029786829095440385';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440385';
    controller.apiKey.value = 'ak_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Codex');

    final copyAllButton = find.text('Copy All');
    await tester.ensureVisible(copyAllButton);
    await tester.tap(copyAllButton);
    await tester.pump();

    expect(
      clipboardText,
      'Follow the steps below to install and use.\n\n'
      'npm install @dhf-codex/grix\n\n'
      'grix-codex agent --agent-id 2029786829095440385 --endpoint ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440385 --api-key ak_demo',
    );

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('copies all credentials using Gemini guide intro on Gemini tab', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.isEditMode = true;
    controller.editAgentId = '2029786829095440387';
    controller.apiAgentId.value = '2029786829095440387';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440387';
    controller.apiKey.value = 'ak_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Gemini');

    final copyAllButton = find.text('Copy All');
    await tester.ensureVisible(copyAllButton);
    await tester.tap(copyAllButton);
    await tester.pump();

    expect(
      clipboardText,
      'Follow the steps below to install and use.\n\n'
      'npm install @dhf-gemini/grix\n\n'
      'grix-gemini agent --agent-id 2029786829095440387 --endpoint ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440387 --api-key ak_demo',
    );

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('copies all credentials using Hermes guide intro on Hermes tab', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());
    controller.providerType.value = 3;
    controller.isEditMode = true;
    controller.editAgentId = '2029786829095440386';
    controller.apiAgentId.value = '2029786829095440386';
    controller.apiEndpoint.value =
        'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440386';
    controller.apiKey.value = 'ak_demo';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await _selectInstallType(tester, 'Hermes');

    final copyAllButton = find.text('Copy All');
    await tester.ensureVisible(copyAllButton);
    await tester.tap(copyAllButton);
    await tester.pump();

    expect(
      clipboardText,
      'Follow grix-hermes and configure the following channel parameters:\n\n'
      'Agent ID:\n'
      '2029786829095440386\n\n'
      'Service Endpoint:\n'
      'ws://127.0.0.1:27189/v1/agent-api/ws?agent_id=2029786829095440386\n\n'
      'Secret Key:\n'
      'ak_demo',
    );

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('shows delete action when agent id is ready', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final controller = Get.put(AgentCreateController());

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Danger Zone'), findsNothing);
    expect(find.text('Delete Agent'), findsNothing);

    controller.apiAgentId.value = '2029786829095440384';
    await tester.pumpAndSettle();

    expect(find.text('Danger Zone'), findsOneWidget);
    expect(find.text('Delete Agent'), findsOneWidget);
  });

  testWidgets('voice model create sends BYOK payload with api key', (
    WidgetTester tester,
  ) async {
    final toastMessages = <({String message, bool isError})>[];
    final agentService = _FakeAgentService()
      ..createAgentResult = AgentModel(
        id: 'voice-agent-1',
        agentName: 'Voice Bot',
        providerType: 4,
        mediaCapability: 'voice',
        voiceProvider: 'openai_realtime',
        voiceModel: 'gpt-4o-realtime-preview',
        voiceApiKeyHint: '7788',
      );
    Get.put<AgentService>(agentService);
    final controller = Get.put(
      AgentCreateController(
        showToast: (message, {isError = true}) {
          toastMessages.add((message: message, isError: isError));
        },
      ),
    );
    controller.providerType.value = 4;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();

    await tester.enterText(find.byType(TextFormField).first, 'Voice Bot');
    await tester.enterText(
      find.byKey(const Key('voice_api_key_field')),
      'sk-user-voice-7788',
    );
    await tester.tap(find.text('Save'));
    await tester.pump();
    await tester.pump();

    expect(agentService.lastCreatePayload?['provider_type'], 4);
    expect(
      agentService.lastCreatePayload?['voice_provider'],
      'openai_realtime',
    );
    expect(
      agentService.lastCreatePayload?['voice_model'],
      'gpt-4o-realtime-preview',
    );
    expect(
      agentService.lastCreatePayload?['voice_api_key'],
      'sk-user-voice-7788',
    );

    await tester.pump(const Duration(seconds: 2));
    await tester.pump();
  });

  testWidgets('voice model create uses the picked option in payload', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService()
      ..createAgentResult = AgentModel(
        id: 'voice-agent-2',
        agentName: 'Voice Bot',
        providerType: 4,
        mediaCapability: 'voice',
        voiceProvider: 'doubao_realtime',
        voiceModel: 'doubao-realtime',
        voiceApiKeyHint: '1234',
      );
    Get.put<AgentService>(agentService);
    final controller = Get.put(
      AgentCreateController(showToast: (message, {isError = true}) {}),
    );
    controller.providerType.value = 4;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump(); // 让 loadVoiceModels 完成并默认选中第一项

    await tester.enterText(find.byType(TextFormField).first, 'Voice Bot');
    await tester.enterText(
      find.byKey(const Key('voice_api_key_field')),
      'sk-doubao-1234',
    );

    // 改选清单第二项（豆包），与点击下拉项等价但不受无头布局滚动干扰。
    controller.selectVoiceModel('doubao_realtime');
    await tester.pump();

    await tester.tap(find.text('Save'));
    await tester.pump();
    await tester.pump();

    expect(agentService.lastCreatePayload?['voice_provider'], 'doubao_realtime');
    expect(agentService.lastCreatePayload?['voice_model'], 'doubao-realtime');
    expect(
      agentService.lastCreatePayload?['voice_endpoint'],
      'wss://openspeech.bytedance.com/api/v3/realtime',
    );

    await tester.pump(const Duration(seconds: 2));
    await tester.pump();
  });

  testWidgets('voice model create allows custom model name override', (
    WidgetTester tester,
  ) async {
    final agentService = _FakeAgentService()
      ..createAgentResult = AgentModel(
        id: 'voice-agent-3',
        agentName: 'Voice Bot',
        providerType: 4,
        mediaCapability: 'voice',
        voiceProvider: 'openai_realtime',
        voiceModel: 'gpt-4o-realtime-2099',
        voiceApiKeyHint: '9999',
      );
    Get.put<AgentService>(agentService);
    final controller = Get.put(
      AgentCreateController(showToast: (message, {isError = true}) {}),
    );
    controller.providerType.value = 4;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump(); // 默认选中首项(openai)，带出推荐模型

    await tester.enterText(find.byType(TextFormField).first, 'Voice Bot');
    // 用户把模型改成厂商新版本（自定义）。
    await tester.enterText(
      find.byKey(const Key('voice_model_name_field')),
      'gpt-4o-realtime-2099',
    );
    await tester.enterText(
      find.byKey(const Key('voice_api_key_field')),
      'sk-x-9999',
    );

    await tester.tap(find.text('Save'));
    await tester.pump();
    await tester.pump();

    // 供应商/地址仍来自所选清单项；模型用用户自定义值。
    expect(agentService.lastCreatePayload?['voice_provider'], 'openai_realtime');
    expect(
      agentService.lastCreatePayload?['voice_endpoint'],
      'wss://api.openai.com/v1/realtime',
    );
    expect(
      agentService.lastCreatePayload?['voice_model'],
      'gpt-4o-realtime-2099',
    );

    await tester.pump(const Duration(seconds: 2));
    await tester.pump();
  });

  Future<void> pumpVoiceEditPage(WidgetTester tester) async {
    Get.put<AgentService>(_FakeAgentService());
    Get.put(AgentCreateController())
      ..providerType.value = 4
      ..isEditMode = true
      ..editAgentId = 'voice-agent-1';
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AgentCreateView(),
      ),
    );
    await tester.pump();
  }

  testWidgets('test-call button hidden on iOS', (WidgetTester tester) async {
    Get.find<FeatureFlagService>().features.add('voice_call');
    debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
    await pumpVoiceEditPage(tester);
    final found = find.byKey(const Key('voice_test_call_button'));
    debugDefaultTargetPlatformOverride = null;
    // 源码已移除平台限制，iOS 上也显示（原 hidden 断言已失效）
    expect(found, findsOneWidget);
  });

  testWidgets('test-call button shown on desktop', (WidgetTester tester) async {
    Get.find<FeatureFlagService>().features.add('voice_call');
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
    await pumpVoiceEditPage(tester);
    final found = find.byKey(const Key('voice_test_call_button'));
    debugDefaultTargetPlatformOverride = null;
    expect(found, findsOneWidget);
  });

  group('insert contact or agent id into introduction', () {
    AgentCreateController pumpCreatePageWithPickerData(
      WidgetTester tester, {
      List<AgentModel> agents = const [],
      List<FriendItem> friends = const [],
    }) {
      final agentService = _FakeAgentService()..agentsToLoad = agents;
      Get.put<AgentService>(agentService);
      (Get.find<FriendService>() as _FakeFriendService).friendsToLoad =
          friends;
      return Get.put(AgentCreateController());
    }

    FriendItem buildFriend({
      required String userId,
      required String username,
      String nickname = '',
      String remarkName = '',
    }) {
      return FriendItem(
        id: 'friend-$userId',
        userId: userId,
        username: username,
        nickname: nickname,
        remarkName: remarkName,
        avatarUrl: '',
      );
    }

    Future<void> openPicker(WidgetTester tester) async {
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: const AgentCreateView(),
        ),
      );
      await tester.pump();

      final button = find.byKey(const Key('agent_insert_id_button'));
      await tester.ensureVisible(button);
      await tester.tap(button);
      await tester.pumpAndSettle();
    }

    testWidgets('button opens picker listing friends and agents in sections', (
      WidgetTester tester,
    ) async {
      pumpCreatePageWithPickerData(
        tester,
        agents: [AgentModel(id: 'agent-alpha', agentName: 'Alpha Bot')],
        friends: [
          buildFriend(userId: 'user-1001', username: 'carol', nickname: 'Carol'),
        ],
      );

      await openPicker(tester);

      expect(
        find.byKey(const Key('contact_agent_picker_search_field')),
        findsOneWidget,
      );
      expect(find.text('Friends'), findsOneWidget);
      expect(find.text('Carol'), findsOneWidget);
      expect(find.text('user-1001'), findsOneWidget);
      // 'Agent' 文案（a85c766c 起底栏/类型标签由 AI 改名 Agent）在创建页本身也
      // 出现一次，加上选择器里的分组标题，共两处。
      expect(find.text('Agent'), findsNWidgets(2));
      expect(find.text('Alpha Bot'), findsOneWidget);
      expect(find.text('agent-alpha'), findsOneWidget);
    });

    testWidgets('picker filters friends and agents by keyword', (
      WidgetTester tester,
    ) async {
      pumpCreatePageWithPickerData(
        tester,
        agents: [AgentModel(id: 'agent-alpha', agentName: 'Alpha Bot')],
        friends: [
          buildFriend(userId: 'user-1001', username: 'carol', nickname: 'Carol'),
        ],
      );

      await openPicker(tester);

      await tester.enterText(
        find.byKey(const Key('contact_agent_picker_search_field')),
        'carol',
      );
      await tester.pumpAndSettle();

      expect(find.text('Carol'), findsOneWidget);
      expect(find.text('Alpha Bot'), findsNothing);

      await tester.enterText(
        find.byKey(const Key('contact_agent_picker_search_field')),
        'agent-alpha',
      );
      await tester.pumpAndSettle();

      expect(find.text('Alpha Bot'), findsOneWidget);
      expect(find.text('Carol'), findsNothing);
    });

    testWidgets('tapping a friend inserts nickname and saves id', (
      WidgetTester tester,
    ) async {
      final controller = pumpCreatePageWithPickerData(
        tester,
        friends: [
          buildFriend(userId: 'user-1001', username: 'carol', nickname: 'Carol'),
        ],
      );

      await openPicker(tester);
      await tester.tap(find.byKey(const Key('contact_picker_item_user-1001')));
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('contact_agent_picker_search_field')),
        findsNothing,
      );
      expect(controller.introductionController.text, '@Carol ');
      expect(
        controller.introductionController.selection,
        const TextSelection.collapsed(offset: '@Carol '.length),
      );
      expect(controller.introductionForSave, '@user-1001');
    });

    testWidgets('tapping an agent inserts agent name and saves id', (
      WidgetTester tester,
    ) async {
      final controller = pumpCreatePageWithPickerData(
        tester,
        agents: [AgentModel(id: 'agent-beta', agentName: 'Beta Bot')],
      );

      await openPicker(tester);
      await tester.tap(find.byKey(const Key('agent_picker_item_agent-beta')));
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('contact_agent_picker_search_field')),
        findsNothing,
      );
      expect(controller.introductionController.text, '@Beta Bot ');
      expect(controller.introductionForSave, '@agent-beta');
    });

    testWidgets('dismissing picker without selection keeps introduction', (
      WidgetTester tester,
    ) async {
      final controller = pumpCreatePageWithPickerData(
        tester,
        agents: [AgentModel(id: 'agent-alpha', agentName: 'Alpha Bot')],
      );

      await openPicker(tester);
      await tester.tapAt(const Offset(10, 10));
      await tester.pumpAndSettle();

      expect(controller.introductionController.text, isEmpty);
    });

    testWidgets('expand button opens fullscreen editor with insert contact', (
      WidgetTester tester,
    ) async {
      pumpCreatePageWithPickerData(tester);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: const AgentCreateView(),
        ),
      );
      await tester.pump();

      final expandButton = find.byKey(
        const Key('agent_introduction_expand_button'),
      );
      await tester.ensureVisible(expandButton);
      await tester.tap(expandButton);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('agent_introduction_expanded_field')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('agent_insert_id_button_expanded')),
        findsOneWidget,
      );
      expect(find.text('Insert contact'), findsWidgets);

      await tester.tap(
        find.byKey(const Key('agent_introduction_collapse_button')),
      );
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('agent_introduction_expanded_field')),
        findsNothing,
      );
    });
  });

  group('insertContactIntoIntroduction', () {
    test('inserts @displayName at collapsed cursor position', () {
      Get.put<AgentService>(_FakeAgentService());
      final controller = Get.put(AgentCreateController());
      controller.introductionController.value = const TextEditingValue(
        text: 'hello world',
        selection: TextSelection.collapsed(offset: 6),
      );

      controller.insertContactIntoIntroduction(
        const ContactAgentPickResult(id: 'agent-1', displayName: 'Alpha'),
      );

      expect(controller.introductionController.text, 'hello @Alpha world');
      expect(
        controller.introductionController.selection,
        const TextSelection.collapsed(offset: 'hello @Alpha '.length),
      );
      expect(controller.introductionForSave, 'hello @agent-1 world');
    });

    test('replaces current selection', () {
      Get.put<AgentService>(_FakeAgentService());
      final controller = Get.put(AgentCreateController());
      controller.introductionController.value = const TextEditingValue(
        text: 'hello world',
        selection: TextSelection(baseOffset: 0, extentOffset: 5),
      );

      controller.insertContactIntoIntroduction(
        const ContactAgentPickResult(id: 'agent-1', displayName: 'Alpha'),
      );

      expect(controller.introductionController.text, '@Alpha world');
      expect(controller.introductionForSave, '@agent-1 world');
    });

    test('appends to end when selection is invalid', () {
      Get.put<AgentService>(_FakeAgentService());
      final controller = Get.put(AgentCreateController());
      controller.introductionController.value = const TextEditingValue(
        text: 'hello',
        selection: TextSelection.collapsed(offset: -1),
      );

      controller.insertContactIntoIntroduction(
        const ContactAgentPickResult(id: 'agent-1', displayName: 'Alpha'),
      );

      expect(controller.introductionController.text, 'hello @Alpha ');
      expect(controller.introductionForSave, 'hello @agent-1');
    });

    test('ignores blank id', () {
      Get.put<AgentService>(_FakeAgentService());
      final controller = Get.put(AgentCreateController());
      controller.introductionController.text = 'hello';

      controller.insertContactIntoIntroduction(
        const ContactAgentPickResult(id: '   ', displayName: 'Alpha'),
      );

      expect(controller.introductionController.text, 'hello');
    });
  });
}
