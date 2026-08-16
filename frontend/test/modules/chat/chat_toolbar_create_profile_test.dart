import 'dart:async';
import 'dart:convert';

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
import 'package:web_socket_channel/web_socket_channel.dart';

// 注意：sendAgentToolbarAction 是 ImService 的 extension 方法，子类 override 永远
// 不会被调到（静态分派）。要断言发送内容，只能用真 extension + 假 WebSocket 通道
// 拦包验证。
class _ToolbarFakeImService extends ImService {
  @override
  void enterSession(
    String sessionId, {
    Duration initialLoadDelay = Duration.zero,
  }) {}

  @override
  void leaveSession([String? explicitSessionId]) {}

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

  @override
  String? get token => 'test_access_token';

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) => true;

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async => TokenRefreshStatus.ready;
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

class _RecordingSink implements WebSocketSink {
  final List<Map<String, dynamic>> packets = [];

  @override
  void add(dynamic data) {
    packets.add(jsonDecode(data as String) as Map<String, dynamic>);
  }

  @override
  Future<void> close([int? closeCode, String? closeReason]) async {}

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeWebSocketChannel implements WebSocketChannel {
  _FakeWebSocketChannel({
    required this.ready,
    required Stream<dynamic> stream,
    required WebSocketSink sink,
  }) : _stream = stream,
       _sink = sink;

  @override
  final Future<void> ready;

  final Stream<dynamic> _stream;
  final WebSocketSink _sink;

  @override
  Stream<dynamic> get stream => _stream;

  @override
  WebSocketSink get sink => _sink;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

AgentToolbarItemModel _profileItem() {
  return const AgentToolbarItemModel(
    itemId: kChatToolbarDshProfileItemId,
    groupId: 'profile_control',
    kind: 'select',
    actionId: 'select_profile',
    label: '',
    icon: 'profile',
    variant: 'secondary',
    disabled: false,
    loading: false,
    selected: false,
    tooltip: '',
    badgeText: 'web（插件托管）',
    confirmTitle: '',
    confirmText: '',
    value: 'web',
    placeholder: '选择 Profile',
    options: <AgentToolbarOptionModel>[
      AgentToolbarOptionModel(
        optionId: 'web',
        label: 'web（插件托管）',
        disabled: false,
      ),
      AgentToolbarOptionModel(
        optionId: 'team',
        label: 'team',
        disabled: false,
      ),
      AgentToolbarOptionModel(
        optionId: kChatToolbarCreateProfileOptionId,
        label: '＋ 新建 Profile…',
        disabled: false,
      ),
    ],
    percent: 0,
    centerText: '',
    progressDesc: '',
    progressDetail: '',
    localAction: '',
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const sessionId = 'session_toolbar_create_profile';

  late _RecordingSink sink;
  late StreamController<dynamic> downstream;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    resetChatInitialMessageRenderWarmupSchedulerForTest();
    MessageBubble.resetFinalRenderCacheForTest();
    SharedPreferences.setMockInitialValues({});
    sink = _RecordingSink();
    downstream = StreamController<dynamic>();
    ImService.channelConnectorForTest = (uri) => _FakeWebSocketChannel(
      ready: Future<void>.value(),
      stream: downstream.stream,
      sink: sink,
    );
    Get.put<ImService>(_ToolbarFakeImService());
    Get.put<AuthService>(_FakeAuthService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<SessionService>(_FakeSessionService());
    Get.put<OssService>(_FakeOssService());
    Get.put<ChatFontSizeService>(_FakeChatFontSizeService());
  });

  tearDown(() async {
    ImService.channelConnectorForTest = null;
    resetChatInitialMessageRenderWarmupSchedulerForTest();
    MessageBubble.resetFinalRenderCacheForTest();
    await downstream.close();
    Get.reset();
  });

  Future<void> pumpChatWithProfileToolbar(WidgetTester tester) async {
    final imService = Get.find<ImService>();
    imService.sessions.assignAll([
      SessionModel(
        sessionId: sessionId,
        title: 'Create Profile',
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
      items: [_profileItem()],
    );
    imService.setCurrentSessionForTest(sessionId);

    final controller = Get.put(ChatController());
    controller.sessionId = sessionId;
    controller.chatTitle = 'Create Profile';
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

    imService.connect('ws://127.0.0.1:1/ws');
    await tester.pump();
    expect(sink.packets.any((p) => p['cmd'] == 'auth'), isTrue,
        reason: '连接后应发出 auth 包');
    // auth_ack 的处理链（_downstreamQueue 串行 future + 鉴权后的引导）在
    // FakeAsync 里不会随 pump 推进，必须用 runAsync 走真实事件循环等待。
    await tester.runAsync(() async {
      downstream.add(
        jsonEncode(<String, dynamic>{
          'cmd': 'auth_ack',
          'payload': <String, dynamic>{'code': 0, 'user_id': '1001'},
        }),
      );
      final deadline = DateTime.now().add(const Duration(seconds: 5));
      while (!imService.isAuthenticated && DateTime.now().isBefore(deadline)) {
        await Future<void>.delayed(const Duration(milliseconds: 25));
      }
    });
    expect(imService.isAuthenticated, isTrue,
        reason: 'auth_ack code=0 后应进入已鉴权态');
  }

  // action 发出后 chip 进入 loading（永续 spinner），pumpAndSettle 永不返回，
  // 全程用有界 pump。
  Future<void> settle(WidgetTester tester) async {
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump();
  }

  Future<void> openProfileMenu(WidgetTester tester) async {
    await tester.tap(
      find.byKey(
        const ValueKey('chat_agent_toolbar_item_$kChatToolbarDshProfileItemId'),
      ),
    );
    await settle(tester);
  }

  // 聊天输入框也是 TextField，定位对话框输入框必须收窄到 AlertDialog 里。
  Finder dialogTextField() => find.descendant(
        of: find.byType(AlertDialog),
        matching: find.byType(TextField),
      );

  Map<String, dynamic>? latestToolbarActionPayload() {
    for (final packet in sink.packets.reversed) {
      if (packet['cmd'] == 'agent_toolbar_action') {
        return packet['payload'] as Map<String, dynamic>;
      }
    }
    return null;
  }

  testWidgets('普通选项直发 select_profile，不弹输入框', (WidgetTester tester) async {
    await pumpChatWithProfileToolbar(tester);

    await openProfileMenu(tester);
    await tester.tap(find.widgetWithText(PopupMenuItem<String>, 'team'));
    await settle(tester);

    final payload = latestToolbarActionPayload();
    expect(payload, isNotNull);
    expect(payload!['action_id'], 'select_profile');
    expect(payload['option_id'], 'team');
    // 没有弹出新建对话框。
    expect(dialogTextField(), findsNothing);

    // 真连接带重连/心跳/banner 等定时器；onClose 的清理清单最全（含 disconnect），
    // 收尾先调它再冲刷，避免留下 pending Timer。
    Get.find<ImService>().onClose();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(seconds: 40));
    await tester.pump();
  });

  testWidgets('__create__ 伪选项弹输入框，确认后以 create_profile 发回输入的名字', (
    WidgetTester tester,
  ) async {
    await pumpChatWithProfileToolbar(tester);

    await openProfileMenu(tester);
    await tester.tap(find.widgetWithText(PopupMenuItem<String>, '＋ 新建 Profile…'));
    await settle(tester);

    // 对话框已弹出；空名字确认只显示错误，不发送。
    expect(dialogTextField(), findsOneWidget);
    await tester.tap(find.text('common_confirm'.tr));
    await settle(tester);
    expect(find.text('chat_toolbar_profile_create_invalid'.tr), findsOneWidget);
    expect(latestToolbarActionPayload(), isNull);

    // 输入名字后确认：以 create_profile + option_id=名字 发出。
    await tester.enterText(dialogTextField(), 'team-alpha');
    await tester.tap(find.text('common_confirm'.tr));
    await settle(tester);

    final payload = latestToolbarActionPayload();
    expect(payload, isNotNull);
    expect(payload!['item_id'], kChatToolbarDshProfileItemId);
    expect(payload['action_id'], 'create_profile');
    expect(payload['option_id'], 'team-alpha');

    // 真连接带重连/心跳/banner 等定时器；onClose 的清理清单最全（含 disconnect），
    // 收尾先调它再冲刷，避免留下 pending Timer。
    Get.find<ImService>().onClose();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(seconds: 40));
    await tester.pump();
  });

  testWidgets('__create__ 对话框取消不发送任何 action', (WidgetTester tester) async {
    await pumpChatWithProfileToolbar(tester);

    await openProfileMenu(tester);
    await tester.tap(find.widgetWithText(PopupMenuItem<String>, '＋ 新建 Profile…'));
    await settle(tester);
    expect(dialogTextField(), findsOneWidget);

    await tester.tap(find.text('common_cancel'.tr));
    await settle(tester);

    expect(latestToolbarActionPayload(), isNull);

    // 真连接带重连/心跳/banner 等定时器；onClose 的清理清单最全（含 disconnect），
    // 收尾先调它再冲刷，避免留下 pending Timer。
    Get.find<ImService>().onClose();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(seconds: 40));
    await tester.pump();
  });
}
