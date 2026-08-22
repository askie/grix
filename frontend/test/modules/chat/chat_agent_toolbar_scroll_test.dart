import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/scroll/app_scroll_behavior.dart';
import 'package:grix/app/scroll/horizontal_drag_scroll_behavior.dart';
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
    return const SessionDetailResult(
      data: {'session_type': 1, 'member_count': 0, 'members': []},
    );
  }
}

class _FakeOssService extends OssService {}

class _FakeChatFontSizeService extends ChatFontSizeService {
  @override
  double get scale => 1;
}

AgentToolbarItemModel _buttonItem(int index) {
  return AgentToolbarItemModel(
    itemId: 'item_$index',
    groupId: 'group_$index',
    kind: 'button',
    actionId: 'action_$index',
    label: 'Toolbar Item $index With Long Label',
    icon: 'stop',
    variant: 'neutral',
    disabled: false,
    loading: false,
    selected: false,
    tooltip: '',
    badgeText: '',
    confirmTitle: '',
    confirmText: '',
    value: '',
    placeholder: '',
    options: const <AgentToolbarOptionModel>[],
    percent: 0,
    centerText: '',
    progressDesc: '',
    progressDetail: '',
    localAction: '',
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const sessionId = 'session_toolbar_scroll';

  setUp(() {
    // ignore: invalid_use_of_visible_for_testing_member
    HardwareKeyboard.instance.clearState();
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
    // ignore: invalid_use_of_visible_for_testing_member
    HardwareKeyboard.instance.clearState();
    debugDefaultTargetPlatformOverride = null;
    resetChatInitialMessageRenderWarmupSchedulerForTest();
    MessageBubble.resetFinalRenderCacheForTest();
    Get.reset();
  });

  Future<void> pumpToolbar(WidgetTester tester) async {
    final imService = Get.find<ImService>() as _FakeImService;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: sessionId,
        title: 'Toolbar Scroll',
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
      toolbarId: 'chat-toolbar:scroll:v1',
      revision: 1,
      visible: true,
      updatedAt: 1,
      items: List.generate(12, _buttonItem),
    );
    imService.setCurrentSessionForTest(sessionId);

    final controller = Get.put(ChatController());
    controller.sessionId = sessionId;
    controller.chatTitle = 'Toolbar Scroll';
    controller.chatType = 'private';

    await tester.binding.setSurfaceSize(const Size(360, 720));
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
  }

  Future<void> tearDownWidget(WidgetTester tester) async {
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(seconds: 5));
    await tester.pump(const Duration(seconds: 5));
    await tester.pump();
  }

  testWidgets('agent toolbar enables mouse drag scrolling on desktop', (
    WidgetTester tester,
  ) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
    try {
      await pumpToolbar(tester);

      final listFinder = find.descendant(
        of: find.byKey(const ValueKey('chat_agent_toolbar_container')),
        matching: find.byType(ListView),
      );
      expect(listFinder, findsOneWidget);

      final listContext = tester.element(listFinder);
      final behavior = ScrollConfiguration.of(listContext);
      expect(behavior, isA<HorizontalDragScrollBehavior>());
      expect(behavior.dragDevices, contains(PointerDeviceKind.mouse));
      expect(behavior.dragDevices, contains(PointerDeviceKind.trackpad));

      // 全局 AppScrollBehavior 在桌面禁鼠标拖动；工具栏局部行为必须覆盖它。
      expect(
        const AppScrollBehavior().dragDevices,
        isNot(contains(PointerDeviceKind.mouse)),
      );

      await tearDownWidget(tester);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });
}
