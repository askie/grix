import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeImService extends ImService {
  final List<String> sentContents = [];

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
  Future<void> sendMessage(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) async {
    sentContents.add(content);
  }
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
  SessionDetailResult detailResult = const SessionDetailResult(
    data: {'session_type': 1, 'member_count': 0, 'members': []},
  );

  @override
  Future<Map<String, dynamic>?> fetchSessionDetail(String sessionId) async {
    return detailResult.data;
  }

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(
    String sessionId,
  ) async {
    return detailResult;
  }
}

class _FakeOssService extends OssService {}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const expandButtonKey = Key('chat_input_expand_button');
  const expandedFieldKey = Key('chat_expanded_input_field');
  const collapseButtonKey = Key('chat_input_collapse_button');
  const expandedSendKey = Key('chat_expanded_input_send_button');

  late ChatController controller;

  Future<void> pumpChatView(WidgetTester tester) async {
    final imService = Get.find<ImService>();
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_expand_test',
        type: 'private',
        peerId: 'peer',
        peerType: 1,
        peerNickname: 'Peer User',
        updatedAt: 0,
        lastMessageTime: 0,
      ),
    ]);
    imService.currentMessages.clear();

    controller = Get.put(ChatController());
    controller.sessionId = 'session_expand_test';
    controller.chatTitle = 'Peer User';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));
  }

  Future<void> flushInputTimers(WidgetTester tester) async {
    // composing 去抖 500ms + 草稿持久化 180ms
    await tester.pump(const Duration(seconds: 1));
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    Get.put<ImService>(_FakeImService());
    Get.put<AuthService>(_FakeAuthService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<SessionService>(_FakeSessionService());
    Get.put<OssService>(_FakeOssService());
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('短文本不显示展开按钮，多行/长文本显示', (WidgetTester tester) async {
    await pumpChatView(tester);

    expect(find.byKey(expandButtonKey), findsNothing);

    controller.inputController.text = 'hi';
    await tester.pump();
    expect(find.byKey(expandButtonKey), findsNothing);

    controller.inputController.text = 'line1\nline2\nline3';
    await tester.pump();
    expect(find.byKey(expandButtonKey), findsOneWidget);

    controller.inputController.text = 'a' * 91;
    await tester.pump();
    expect(find.byKey(expandButtonKey), findsOneWidget);

    controller.inputController.text = '';
    await tester.pump();
    expect(find.byKey(expandButtonKey), findsNothing);

    await flushInputTimers(tester);
  });

  testWidgets('展开进入全屏编辑器，文字双向同步，收起后保留', (WidgetTester tester) async {
    await pumpChatView(tester);

    controller.inputController.text = 'line1\nline2\nline3';
    await tester.pump();
    await tester.tap(find.byKey(expandButtonKey));
    await tester.pumpAndSettle();

    // 全屏编辑器已打开且带入现有文字
    final expandedField = find.byKey(expandedFieldKey);
    expect(expandedField, findsOneWidget);
    expect(
      tester.widget<TextField>(expandedField).controller!.text,
      'line1\nline2\nline3',
    );

    // 在全屏编辑器中修改文字
    await tester.enterText(expandedField, 'edited in fullscreen');
    await tester.pump();

    // 收起后回到聊天页，小输入框保留编辑结果
    await tester.tap(find.byKey(collapseButtonKey));
    await tester.pumpAndSettle();
    expect(find.byKey(expandedFieldKey), findsNothing);
    expect(controller.inputController.text, 'edited in fullscreen');
    expect(controller.isExpandedInputEditorOpen, isFalse);

    await flushInputTimers(tester);
  });

  testWidgets('全屏编辑器内直接发送：消息发出、编辑器关闭、输入框清空', (WidgetTester tester) async {
    await pumpChatView(tester);

    controller.inputController.text = 'line1\nline2\nline3';
    await tester.pump();
    await tester.tap(find.byKey(expandButtonKey));
    await tester.pumpAndSettle();

    await tester.enterText(
      find.byKey(expandedFieldKey),
      'send from fullscreen',
    );
    await tester.pump();
    await tester.tap(find.byKey(expandedSendKey));
    await tester.pumpAndSettle();

    final imService = Get.find<ImService>() as _FakeImService;
    expect(imService.sentContents, ['send from fullscreen']);
    expect(find.byKey(expandedFieldKey), findsNothing);
    expect(controller.inputController.text, isEmpty);

    await flushInputTimers(tester);
  });

  testWidgets('宽窗口(≥700)展开呈居中面板，点遮罩收起', (WidgetTester tester) async {
    tester.view.physicalSize = const Size(1200, 800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    });

    await pumpChatView(tester);

    controller.inputController.text = 'line1\nline2\nline3';
    await tester.pump();
    await tester.tap(find.byKey(expandButtonKey));
    await tester.pumpAndSettle();

    expect(find.byKey(expandedFieldKey), findsOneWidget);

    // 宽屏下面板宽度应受 maxWidth 720 约束，即比窗口窄。
    final fieldRect = tester.getRect(find.byKey(expandedFieldKey));
    expect(fieldRect.width, lessThan(1200));

    // 遮罩位于面板外，点面板上方靠角落的空白区域应命中遮罩并触发收起。
    await tester.tapAt(const Offset(20, 20));
    await tester.pumpAndSettle();

    expect(find.byKey(expandedFieldKey), findsNothing);
    expect(controller.isExpandedInputEditorOpen, isFalse);
    // 收起后小输入框保留文字
    expect(controller.inputController.text, 'line1\nline2\nline3');

    await flushInputTimers(tester);
  });

  testWidgets('群聊全屏 @提及：候选列表弹出且选中后焦点留在全屏', (WidgetTester tester) async {
    // 大屏模拟桌面/网页宽窗口
    tester.view.physicalSize = const Size(1200, 800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    });

    // 群聊 session
    final sessionService = Get.find<SessionService>() as _FakeSessionService;
    sessionService.detailResult = const SessionDetailResult(
      data: {
        'session_type': 2,
        'member_count': 3,
        'members': [
          {'member_id': '1001', 'member_type': 1, 'role': 3, 'nickname': 'Me'},
          {
            'member_id': '2001',
            'member_type': 1,
            'role': 1,
            'nickname': 'Alpha',
          },
          {'member_id': '9001', 'member_type': 2, 'role': 1},
        ],
      },
    );
    final agentService = Get.find<AgentService>();
    agentService.agents.assignAll([
      AgentModel(id: '9001', agentName: 'Bot One'),
    ]);

    final imService = Get.find<ImService>();
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_group_expand',
        type: 'group',
        peerId: 'group_peer',
        peerType: 3,
        peerNickname: 'Team',
        updatedAt: 0,
        lastMessageTime: 0,
      ),
    ]);
    imService.currentMessages.clear();

    controller = Get.put(ChatController());
    controller.sessionId = 'session_group_expand';
    controller.chatTitle = 'Team';
    controller.chatType = 'group';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    // 塞点内容让展开按钮出现
    controller.inputController.text = 'line1\nline2\nline3';
    await tester.pump();
    await tester.tap(find.byKey(expandButtonKey));
    await tester.pumpAndSettle();

    // 在全屏输入 @
    await tester.enterText(find.byKey(expandedFieldKey), '@');
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    // 候选列表出现且包含群成员（全屏面板和底部小输入框都会渲染一份，
    // 底部那份被全屏遮罩覆盖看不到，功能上以上层为准）
    expect(
      find.byKey(const Key('chat_mention_list_container')),
      findsWidgets,
    );
    expect(find.text('Alpha'), findsWidgets);

    // 点选全屏面板里的 Alpha（hitTestable 只保留可见/可命中的，避开 Offstage 的旧页面）
    await tester.tap(find.text('Alpha').hitTestable());
    await tester.pumpAndSettle();

    expect(find.byKey(expandedFieldKey), findsOneWidget);
    expect(controller.expandedInputFocusNodeOverride, isNotNull);
    // 焦点确实归到 override 而不是底部小输入框（核心修复点）
    expect(controller.focusNode.hasFocus, isFalse);
    expect(controller.expandedInputFocusNodeOverride!.hasFocus, isTrue);
    expect(controller.inputController.text.contains('@Alpha'), isTrue);

    await flushInputTimers(tester);
  });
}
