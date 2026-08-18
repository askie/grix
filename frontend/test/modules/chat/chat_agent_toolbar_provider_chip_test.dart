import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/scroll/app_scroll_behavior.dart';
import 'package:grix/app/settings/chat_font_size_service.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/agent_toolbar_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeImService extends ImService {
  @override
  void enterSession(
    String sessionId, {
    Duration initialLoadDelay = Duration.zero,
  }) {}

  @override
  void leaveSession([String? explicitSessionId]) {}

  @override
  void connect(String wsUrl) {}

  @override
  void updateSessionComposing(String sessionId, {required bool active}) {}

  @override
  Future<void> retryMessage(String? clientMsgId, {String? msgId}) async {}

  @override
  Future<void> sendMessage(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) async {}

  @override
  bool stopAgentOutput(String sessionId, {String? runId}) => true;
}

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

class _FakeSessionService extends SessionService {
  @override
  Future<Map<String, dynamic>?> fetchSessionDetail(String sessionId) async {
    return {'session_type': 1, 'member_count': 0, 'members': []};
  }

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return SessionDetailResult(
      data: {'session_type': 1, 'member_count': 0, 'members': []},
    );
  }
}

class _FakeOssService extends OssService {}

class _FakeChatFontSizeService extends ChatFontSizeService {
  @override
  double get scale => 1;
}

AgentToolbarItemModel _selectItem({
  required String actionId,
  required String icon,
  required String value,
  required String badgeText,
  required List<AgentToolbarOptionModel> options,
  String label = '',
  String variant = 'primary',
}) {
  return AgentToolbarItemModel(
    itemId: actionId,
    groupId: '${actionId}_group',
    kind: 'select',
    actionId: actionId,
    label: label,
    icon: icon,
    variant: variant,
    disabled: false,
    loading: false,
    selected: false,
    tooltip: '',
    badgeText: badgeText,
    confirmTitle: '',
    confirmText: '',
    value: value,
    placeholder: '请选择',
    options: options,
    percent: 0,
    centerText: '',
    progressDesc: '',
    progressDetail: '',
    localAction: '',
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const sessionId = 'session_toolbar_provider_chip';

  setUp(() {
    Get.testMode = true;
    Get.reset();
    resetChatInitialMessageRenderWarmupSchedulerForTest();
    MessageBubble.resetFinalRenderCacheForTest();
    SharedPreferences.setMockInitialValues({});
    Get.put<ImService>(_FakeImService());
    Get.put<AuthService>(_FakeAuthService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<SessionService>(_FakeSessionService());
    Get.put<OssService>(_FakeOssService());
    Get.put<ChatFontSizeService>(_FakeChatFontSizeService());
  });

  tearDown(() async {
    resetChatInitialMessageRenderWarmupSchedulerForTest();
    MessageBubble.resetFinalRenderCacheForTest();
    Get.reset();
  });

  testWidgets('deepseek profile and provider chips show names', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: sessionId,
        title: 'Provider Chip',
        type: 'private',
        peerId: '2001',
        peerType: 2,
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);
    imService.agentToolbars[sessionId] = AgentToolbarModel(
      sessionId: sessionId,
      agentId: '2001',
      toolbarId: 'agent-toolbar:deepseek:v1',
      revision: 1,
      visible: true,
      updatedAt: 1,
      items: [
        _selectItem(
          actionId: 'select_profile',
          icon: 'profile',
          value: 'web',
          badgeText: 'web（插件托管）',
          options: const [
            AgentToolbarOptionModel(
              optionId: 'web',
              label: 'web（插件托管）',
              disabled: false,
            ),
          ],
        ),
        _selectItem(
          actionId: 'select_provider',
          icon: 'server',
          value: 'deepseek-official',
          badgeText: 'DeepSeek',
          options: const [
            AgentToolbarOptionModel(
              optionId: 'deepseek-official',
              label: 'DeepSeek',
              disabled: false,
            ),
            AgentToolbarOptionModel(
              optionId: 'opencode-go',
              label: 'OpenCode Go',
              disabled: false,
            ),
          ],
        ),
        _selectItem(
          actionId: 'select_model',
          icon: 'cpu',
          value: 'deepseek-v4-pro',
          badgeText: 'DeepSeek-V4-Pro',
          options: const [
            AgentToolbarOptionModel(
              optionId: 'deepseek-v4-pro',
              label: 'DeepSeek-V4-Pro',
              disabled: false,
            ),
          ],
        ),
        _selectItem(
          actionId: 'select_thinking',
          icon: 'spark',
          value: 'enabled',
          badgeText: '开启',
          label: 'Thinking',
          options: const [
            AgentToolbarOptionModel(
              optionId: 'enabled',
              label: '开启',
              disabled: false,
            ),
          ],
        ),
      ],
    );
    imService.setCurrentSessionForTest(sessionId);

    final controller = Get.put(ChatController());
    controller.sessionId = sessionId;
    controller.chatTitle = 'Provider Chip';
    controller.chatType = 'private';

    // 工具栏横向懒构建，宽度要够放下三个带名字的 chip，否则末尾项不会被 build。
    await tester.binding.setSurfaceSize(const Size(800, 720));
    addTearDown(() async {
      await tester.binding.setSurfaceSize(null);
    });

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        scrollBehavior: const AppScrollBehavior(),
        home: ChatView(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    // 后端没给静态标题的项：当前状态就是 chip 主文本。
    expect(find.text('web（插件托管）'), findsOneWidget);
    expect(find.text('DeepSeek'), findsOneWidget);
    expect(find.text('DeepSeek-V4-Pro'), findsOneWidget);
    // 后端给了静态标题的项：标题当主文本，状态另起徽章，两者都在。
    expect(find.text('Thinking'), findsOneWidget);
    expect(find.text('开启'), findsOneWidget);
    // 未选中的供应商只在下拉里出现，chip 上不该出现。
    expect(find.text('OpenCode Go'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(seconds: 5));
    await tester.pump();
  });

  testWidgets('empty label uses state as primary text; warning shows bang', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: sessionId,
        title: 'Label As State',
        type: 'private',
        peerId: '2001',
        peerType: 2,
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);
    imService.agentToolbars[sessionId] = AgentToolbarModel(
      sessionId: sessionId,
      agentId: '2001',
      toolbarId: 'agent-toolbar:cursor:v1',
      revision: 1,
      visible: true,
      updatedAt: 1,
      items: [
        // label-as-state（qwen/pi/cursor 等）：Label 本身就是状态，badge 为空。
        _selectItem(
          actionId: 'select_model',
          icon: 'cpu',
          label: 'gpt-5.4',
          value: 'gpt-5.4',
          badgeText: '',
          options: const [
            AgentToolbarOptionModel(
              optionId: 'gpt-5.4',
              label: 'gpt-5.4',
              disabled: false,
            ),
          ],
        ),
        // 有静态标题 + warning：标题、状态徽章、叹号都在。
        _selectItem(
          actionId: 'select_thinking',
          icon: 'spark',
          label: 'Thinking',
          value: 'enabled',
          badgeText: '开启',
          variant: 'warning',
          options: const [
            AgentToolbarOptionModel(
              optionId: 'enabled',
              label: '开启',
              disabled: false,
            ),
          ],
        ),
      ],
    );
    imService.setCurrentSessionForTest(sessionId);

    final controller = Get.put(ChatController());
    controller.sessionId = sessionId;
    controller.chatTitle = 'Label As State';
    controller.chatType = 'private';

    await tester.binding.setSurfaceSize(const Size(800, 720));
    addTearDown(() async {
      await tester.binding.setSurfaceSize(null);
    });

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        scrollBehavior: const AppScrollBehavior(),
        home: ChatView(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(find.text('gpt-5.4'), findsOneWidget);
    expect(find.text('Thinking'), findsOneWidget);
    expect(find.text('开启'), findsOneWidget);
    expect(find.byIcon(Icons.priority_high_rounded), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(seconds: 5));
    await tester.pump();
  });
}
