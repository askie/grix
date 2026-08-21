import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/settings/chat_font_size_service.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/data/models/agent_toolbar_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_question_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_agent_card_action_encoder.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/modules/chat/services/chat_bottom_obstruction_observer.dart';
import 'package:grix/modules/chat/services/chat_keyboard_platform_behavior.dart';
import 'package:grix/modules/chat/widgets/chat_retry_action_button.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:grix/shared/widgets/session_avatar.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeImService extends ImService {
  int retryCalls = 0;
  int sendCalls = 0;
  int agentOutputStopCalls = 0;
  int setSessionMutedCalls = 0;
  bool hasOlder = true;
  String? retriedClientMsgId;
  String? retriedMsgId;
  String? sentContent;
  String? sentSessionId;
  String? stoppedOutputSessionId;
  String? stoppedOutputRunId;
  String? lastMutedSessionId;
  bool? lastMutedValue;
  bool setSessionMutedResult = true;

  @override
  bool get hasOlderMessages => hasOlder;

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
  Future<void> retryMessage(String? clientMsgId, {String? msgId}) async {
    retryCalls++;
    retriedClientMsgId = clientMsgId;
    retriedMsgId = msgId;
  }

  @override
  Future<void> sendMessage(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) async {
    sendCalls++;
    sentContent = content;
    sentSessionId = sessionId;
  }

  @override
  Future<bool> setSessionMuted(
    String sessionId, {
    required bool isMuted,
  }) async {
    setSessionMutedCalls++;
    lastMutedSessionId = sessionId;
    lastMutedValue = isMuted;
    if (!setSessionMutedResult) {
      return false;
    }
    final idx = sessions.indexWhere((s) => s.sessionId == sessionId);
    if (idx >= 0) {
      sessions[idx] = sessions[idx].copyWith(isMuted: isMuted);
    }
    return true;
  }

  @override
  bool stopAgentOutput(String sessionId, {String? runId}) {
    agentOutputStopCalls++;
    stoppedOutputSessionId = sessionId;
    stoppedOutputRunId = runId;
    return true;
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
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return detailResult;
  }
}

class _FakeOssService extends OssService {}

class _FakeChatFontSizeService extends ChatFontSizeService {
  _FakeChatFontSizeService(double initialScale) : _scale = initialScale.obs;

  final RxDouble _scale;

  @override
  RxDouble get scaleRx => _scale;

  @override
  double get scale => _scale.value;

  void setScale(double nextScale) {
    _scale.value = nextScale;
  }
}

class _FakeChatBottomObstructionObserver
    implements ChatBottomObstructionObserver {
  _FakeChatBottomObstructionObserver({double initialBottomObstruction = 0})
    : _currentBottomObstruction = initialBottomObstruction;

  final StreamController<double> _changedController =
      StreamController<double>.broadcast();
  double _currentBottomObstruction;

  void emit(double nextBottomObstruction) {
    _currentBottomObstruction = nextBottomObstruction;
    _changedController.add(nextBottomObstruction);
  }

  @override
  double get currentBottomObstruction => _currentBottomObstruction;

  @override
  Stream<double> get onChanged => _changedController.stream;

  @override
  void dispose() {
    _changedController.close();
  }
}

class _TestChatController extends ChatController {
  int pickImageCalls = 0;
  int pickImageFromCameraCalls = 0;
  int pickVideoCalls = 0;
  int pickVideoFromCameraCalls = 0;
  int pickFileCalls = 0;
  int headerAvatarTapCalls = 0;
  int messageAvatarTapCalls = 0;
  int messageMentionCalls = 0;
  String? mentionedSenderId;
  int? mentionedSenderType;
  bool? mentionedSenderIsMine;
  String? mentionedSenderName;

  @override
  Future<void> pickAndSendImage() async {
    pickImageCalls++;
  }

  @override
  Future<void> pickAndSendImageFromCamera() async {
    pickImageFromCameraCalls++;
  }

  @override
  Future<void> pickAndSendVideo() async {
    pickVideoCalls++;
  }

  @override
  Future<void> pickAndSendVideoFromCamera() async {
    pickVideoFromCameraCalls++;
  }

  @override
  Future<void> pickAndSendFile() async {
    pickFileCalls++;
  }

  @override
  void onHeaderAvatarTap() {
    headerAvatarTapCalls++;
  }

  @override
  void onMessageAvatarTap({
    required String senderId,
    required int senderType,
    required bool isMine,
    required String senderName,
    required String senderAvatarUrl,
  }) {
    messageAvatarTapCalls++;
  }

  @override
  void mentionSenderFromMessage({
    required String senderId,
    required int senderType,
    required bool isMine,
    required String senderName,
  }) {
    messageMentionCalls++;
    mentionedSenderId = senderId;
    mentionedSenderType = senderType;
    mentionedSenderIsMine = isMine;
    mentionedSenderName = senderName;
  }
}

BoxDecoration _boxDecorationOf(WidgetTester tester, Finder finder) {
  final widget = tester.widget(finder);
  if (widget is Container) {
    return widget.decoration! as BoxDecoration;
  }
  if (widget is AnimatedContainer) {
    return widget.decoration! as BoxDecoration;
  }
  throw StateError('Unsupported widget for BoxDecoration lookup: $widget');
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late ErrorWidgetBuilder defaultErrorWidgetBuilder;

  void resetSimulatedKeyboardState() {
    // Flutter 3.41 asserts harder on stale simulated key state.
    // ignore: invalid_use_of_visible_for_testing_member
    HardwareKeyboard.instance.clearState();
  }

  Future<void> sendKeyPress(WidgetTester tester, LogicalKeyboardKey key) async {
    await tester.sendKeyEvent(key);
  }

  Future<void> sendModifiedKeyPress(
    WidgetTester tester, {
    required LogicalKeyboardKey triggerKey,
    required List<LogicalKeyboardKey> modifiers,
  }) async {
    try {
      for (final modifier in modifiers) {
        await tester.sendKeyDownEvent(modifier);
      }
      await sendKeyPress(tester, triggerKey);
    } finally {
      for (final modifier in modifiers.reversed) {
        if (HardwareKeyboard.instance.isLogicalKeyPressed(modifier)) {
          await tester.sendKeyUpEvent(modifier);
        }
      }
    }
  }

  Future<ChatController> pumpChatViewWithMessages(
    WidgetTester tester, {
    required String sessionId,
    required List<MessageModel> messages,
    required Locale locale,
  }) async {
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll(messages);

    final controller = Get.put(ChatController());
    controller.sessionId = sessionId;
    controller.chatTitle = sessionId;
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: locale,
        home: ChatView(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));
    return controller;
  }

  Future<void> expectMouseWheelScrollsMessageText(
    WidgetTester tester, {
    required TargetPlatform platform,
    required String messagePrefix,
  }) async {
    debugDefaultTargetPlatformOverride = platform;
    try {
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: 'session_test_scroll_${platform.name}',
        locale: const Locale('zh', 'CN'),
        messages: List.generate(
          200,
          (index) => MessageModel(
            msgId: 'msg_${platform.name}_$index',
            sessionId: 'session_test_scroll_${platform.name}',
            senderId: index.isEven ? 'peer' : '1001',
            content: '${messagePrefix}_$index',
            createdAt: index,
          ),
        ),
      );
      (Get.find<ImService>() as _FakeImService).hasOlder = false;
      controller.scrollController.jumpTo(0);
      await tester.pump();

      final messageFinder = find.text('${messagePrefix}_0');
      expect(messageFinder, findsOneWidget);

      final pointer = TestPointer(1, PointerDeviceKind.mouse);
      final position = tester.getCenter(messageFinder);
      await tester.sendEventToBinding(pointer.hover(position));
      await tester.sendEventToBinding(pointer.scroll(const Offset(0.0, 180.0)));
      await tester.pump();
      await tester.pump();

      expect(controller.scrollController.offset, closeTo(180.0, 0.5));
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  }

  setUp(() {
    resetSimulatedKeyboardState();
    Get.testMode = true;
    Get.reset();
    resetChatInitialMessageRenderWarmupSchedulerForTest();
    resetChatViewDebugBuildCounterForTest();
    MessageBubble.resetFinalRenderCacheForTest();
    defaultErrorWidgetBuilder = ErrorWidget.builder;
    SharedPreferences.setMockInitialValues({});
    Get.put<ImService>(_FakeImService());
    Get.put<AuthService>(_FakeAuthService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<SessionService>(_FakeSessionService());
    Get.put<OssService>(_FakeOssService());
  });

  tearDown(() async {
    resetSimulatedKeyboardState();
    ErrorWidget.builder = defaultErrorWidgetBuilder;
    resetChatInitialMessageRenderWarmupSchedulerForTest();
    resetChatViewDebugBuildCounterForTest();
    MessageBubble.resetFinalRenderCacheForTest();
    Get.reset();
  });

  testWidgets('chat menu notification disable triggers session mute update', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    const sessionId = 'session_notify_toggle';
    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: sessionId,
        title: 'Notify Session',
        type: 'private',
        updatedAt: now,
        unreadCount: 0,
        isMuted: false,
        lastMessage: 'hello',
        lastMessageTime: now,
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = sessionId;
    controller.chatTitle = 'Notify Session';
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

    await tester.tap(find.byIcon(Icons.more_vert_rounded));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Notifications'));
    await tester.pumpAndSettle();
    expect(find.text('Mute notifications'), findsOneWidget);

    await tester.tap(find.text('Mute notifications'));
    await tester.pumpAndSettle();

    expect(imService.setSessionMutedCalls, 1);
    expect(imService.lastMutedSessionId, sessionId);
    expect(imService.lastMutedValue, isTrue);

    // Allow toast auto-dismiss timer to complete in fake async zone.
    await tester.pump(const Duration(seconds: 3));
    await tester.pump();
  });

  testWidgets('ChatView handles large message list with lazy building', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll(
      List.generate(
        10000,
        (index) => MessageModel(
          msgId: 'msg_$index',
          sessionId: 'session_test_1',
          senderId: index.isEven ? 'me' : 'peer',
          content: 'message_$index',
          createdAt: index,
        ),
      ),
    );

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
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

    final builtCount = find.byType(MessageBubble).evaluate().length;
    expect(builtCount, greaterThan(0));
    expect(builtCount, lessThan(300));

    final position = controller.scrollController.position;
    expect(position.maxScrollExtent, greaterThan(0));

    controller.scrollController.jumpTo(position.maxScrollExtent);
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('m:msg_9999')), findsOneWidget);

    ErrorWidget.builder = defaultErrorWidgetBuilder;
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
  });

  testWidgets('ChatView forward selection toggle keeps root scope stable', (
    WidgetTester tester,
  ) async {
    const sessionId = 'session_forward_scope_stable';
    final messages = <MessageModel>[
      MessageModel(
        msgId: 'msg_forward_1',
        sessionId: sessionId,
        senderId: 'peer',
        content: 'forward one',
        createdAt: 1,
      ),
      MessageModel(
        msgId: 'msg_forward_2',
        sessionId: sessionId,
        senderId: 'peer',
        content: 'forward two',
        createdAt: 2,
      ),
    ];
    final controller = await pumpChatViewWithMessages(
      tester,
      sessionId: sessionId,
      messages: messages,
      locale: const Locale('en', 'US'),
    );

    final rootBeforeForward = chatViewDebugBuildCountForTest('root_obx');
    final listBeforeForward = chatViewDebugBuildCountForTest(
      'message_list_obx',
    );
    expect(rootBeforeForward, greaterThan(0));
    expect(listBeforeForward, greaterThan(0));

    controller.beginForwardSelection(messages.first);
    await tester.pump();

    final rootAfterEnterForward = chatViewDebugBuildCountForTest('root_obx');
    final listAfterEnterForward = chatViewDebugBuildCountForTest(
      'message_list_obx',
    );
    final titleAfterEnterForward = chatViewDebugBuildCountForTest(
      'forward_appbar_title_obx',
    );
    expect(rootAfterEnterForward, greaterThan(rootBeforeForward));
    expect(listAfterEnterForward, listBeforeForward);
    expect(titleAfterEnterForward, greaterThan(0));

    controller.toggleForwardMessageSelection(messages[1]);
    await tester.pump();

    expect(controller.selectedForwardMessageCount, 2);
    expect(chatViewDebugBuildCountForTest('root_obx'), rootAfterEnterForward);
    expect(
      chatViewDebugBuildCountForTest('message_list_obx'),
      listAfterEnterForward,
    );
    expect(
      chatViewDebugBuildCountForTest('forward_appbar_title_obx'),
      greaterThan(titleAfterEnterForward),
    );
  });

  testWidgets(
    'ChatView streaming append/finalize keeps root scope stable and rebuilds snapshot once per mutation',
    (WidgetTester tester) async {
      const sessionId = 'session_stream_scope_stable';
      final initialMessages = <MessageModel>[
        MessageModel(
          msgId: 'msg_stream_base',
          sessionId: sessionId,
          senderId: 'peer',
          content: 'base',
          createdAt: 1,
        ),
      ];
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: initialMessages,
        locale: const Locale('en', 'US'),
      );
      final imService = Get.find<ImService>();

      final rootBeforeAppend = chatViewDebugBuildCountForTest('root_obx');
      final listBeforeAppend = chatViewDebugBuildCountForTest(
        'message_list_obx',
      );
      final snapshotBuildBeforeAppend =
          controller.debugMessageListSnapshotBuildCount;

      final streamingMessage = MessageModel(
        msgId: 'msg_streaming_1',
        sessionId: sessionId,
        senderId: 'peer',
        content: 'partial',
        status: 'sending',
        createdAt: 2,
      );
      imService.currentMessages.add(streamingMessage);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));

      expect(
        controller.debugMessageListSnapshotBuildCount,
        snapshotBuildBeforeAppend + 1,
      );
      final rootAfterAppend = chatViewDebugBuildCountForTest('root_obx');
      final listAfterAppend = chatViewDebugBuildCountForTest(
        'message_list_obx',
      );
      expect(rootAfterAppend, rootBeforeAppend);
      expect(listAfterAppend, greaterThan(listBeforeAppend));

      imService.currentMessages[1] = streamingMessage.copyWith(
        content: 'finalized',
        status: 'success',
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));

      expect(
        controller.debugMessageListSnapshotBuildCount,
        snapshotBuildBeforeAppend + 2,
      );
      expect(chatViewDebugBuildCountForTest('root_obx'), rootAfterAppend);
      expect(
        chatViewDebugBuildCountForTest('message_list_obx'),
        greaterThan(listAfterAppend),
      );
      expect(controller.messageListSnapshot.messages.last.content, 'finalized');
    },
  );

  testWidgets('ChatView font scale change does not rebuild root scope', (
    WidgetTester tester,
  ) async {
    const sessionId = 'session_font_scope_stable';
    final fontSizeService = _FakeChatFontSizeService(1.0);
    Get.put<ChatFontSizeService>(fontSizeService);
    await pumpChatViewWithMessages(
      tester,
      sessionId: sessionId,
      messages: <MessageModel>[
        MessageModel(
          msgId: 'msg_font_scope_1',
          sessionId: sessionId,
          senderId: 'peer',
          content: 'font scope message',
          createdAt: 1,
        ),
      ],
      locale: const Locale('en', 'US'),
    );

    final rootBefore = chatViewDebugBuildCountForTest('root_obx');
    final appBarBefore = chatViewDebugBuildCountForTest('app_bar_scope_obx');
    expect(rootBefore, greaterThan(0));
    expect(appBarBefore, greaterThan(0));

    fontSizeService.setScale(1.2);
    await tester.pump();

    expect(chatViewDebugBuildCountForTest('root_obx'), rootBefore);
    expect(
      chatViewDebugBuildCountForTest('app_bar_scope_obx'),
      greaterThan(appBarBefore),
    );
  });

  testWidgets('ChatView mention list shrinks to matched content', (
    WidgetTester tester,
  ) async {
    tester.view.physicalSize = const Size(800, 600);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    });

    final sessionService = Get.find<SessionService>() as _FakeSessionService;
    final agentService = Get.find<AgentService>();
    sessionService.detailResult = const SessionDetailResult(
      data: {
        'session_type': 2,
        'member_count': 4,
        'members': [
          {'member_id': '1001', 'member_type': 1, 'role': 3, 'nickname': 'Me'},
          {
            'member_id': '2001',
            'member_type': 1,
            'role': 1,
            'nickname': 'Member One',
          },
          {
            'member_id': '2002',
            'member_type': 1,
            'role': 1,
            'nickname': 'Member Two',
          },
          {'member_id': '9001', 'member_type': 2, 'role': 1},
        ],
      },
    );
    agentService.agents.assignAll([
      AgentModel(id: '9001', agentName: 'Agent One'),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_mention_small';
    controller.chatTitle = 'Team Room';
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

    await tester.tap(find.byType(TextField));
    await tester.enterText(find.byType(TextField), '@');
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    final mentionContainer = find.byKey(
      const Key('chat_mention_list_container'),
    );
    final mentionList = find.byKey(const Key('chat_mention_list_scrollable'));
    expect(mentionContainer, findsOneWidget);
    expect(mentionList, findsOneWidget);
    expect(find.text('Member One'), findsOneWidget);
    expect(find.text('Agent One'), findsOneWidget);
    expect(tester.getSize(mentionContainer).height, lessThan(220));
    expect(tester.widget<ListView>(mentionList).shrinkWrap, isFalse);

    await tester.enterText(find.byType(TextField), '');
    await tester.pump();
  });

  testWidgets(
    'ChatView mention list caps height at thirty percent of screen and stays scrollable',
    (WidgetTester tester) async {
      tester.view.physicalSize = const Size(800, 600);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() {
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });

      final sessionService = Get.find<SessionService>() as _FakeSessionService;
      final agentService = Get.find<AgentService>();
      sessionService.detailResult = SessionDetailResult(
        data: {
          'session_type': 2,
          'member_count': 21,
          'members': [
            {
              'member_id': '1001',
              'member_type': 1,
              'role': 3,
              'nickname': 'Me',
            },
            for (var i = 0; i < 15; i++)
              {
                'member_id': '${2001 + i}',
                'member_type': 1,
                'role': 1,
                'nickname': 'Member ${i + 1}',
              },
            for (var i = 0; i < 5; i++)
              {'member_id': '900${i + 1}', 'member_type': 2, 'role': 1},
          ],
        },
      );
      agentService.agents.assignAll([
        for (var i = 0; i < 5; i++)
          AgentModel(id: '900${i + 1}', agentName: 'Agent ${i + 1}'),
      ]);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_large';
      controller.chatTitle = 'Team Room';
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

      await tester.tap(find.byType(TextField));
      await tester.enterText(find.byType(TextField), '@');
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      final mentionContainer = find.byKey(
        const Key('chat_mention_list_container'),
      );
      final mentionList = find.byKey(const Key('chat_mention_list_scrollable'));
      final mentionScrollable = find.descendant(
        of: mentionList,
        matching: find.byType(Scrollable),
      );
      expect(mentionContainer, findsOneWidget);
      expect(mentionList, findsOneWidget);
      expect(mentionScrollable, findsOneWidget);
      expect(tester.getSize(mentionContainer).height, lessThanOrEqualTo(180));
      expect(tester.widget<ListView>(mentionList).shrinkWrap, isFalse);
      expect(tester.widget<ListView>(mentionList).itemExtent, isNotNull);
      expect(find.text('Agent 5'), findsNothing);

      await tester.scrollUntilVisible(
        find.text('Agent 5'),
        200,
        scrollable: mentionScrollable,
      );
      await tester.pump();

      expect(find.text('Agent 5'), findsOneWidget);

      await tester.tap(find.text('Agent 5'));
      await tester.pump();

      expect(controller.inputController.text, '@Agent 5 ');

      await tester.enterText(find.byType(TextField), '');
      await tester.pump();
    },
  );

  testWidgets(
    'ChatView mention navigation keeps mention container scope stable',
    (WidgetTester tester) async {
      tester.view.physicalSize = const Size(800, 600);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() {
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });

      final sessionService = Get.find<SessionService>() as _FakeSessionService;
      final agentService = Get.find<AgentService>();
      sessionService.detailResult = const SessionDetailResult(
        data: {
          'session_type': 2,
          'member_count': 4,
          'members': [
            {
              'member_id': '1001',
              'member_type': 1,
              'role': 3,
              'nickname': 'Me',
            },
            {
              'member_id': '2001',
              'member_type': 1,
              'role': 1,
              'nickname': 'Member One',
            },
            {
              'member_id': '2002',
              'member_type': 1,
              'role': 1,
              'nickname': 'Member Two',
            },
            {'member_id': '9001', 'member_type': 2, 'role': 1},
          ],
        },
      );
      agentService.agents.assignAll([
        AgentModel(id: '9001', agentName: 'Agent One'),
      ]);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_scope_stable';
      controller.chatTitle = 'Team Room';
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

      await tester.tap(find.byType(TextField));
      await tester.enterText(find.byType(TextField), '@');
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      expect(
        find.byKey(const Key('chat_mention_list_container')),
        findsOneWidget,
      );
      final outerBefore = chatViewDebugBuildCountForTest(
        'mention_list_outer_obx',
      );
      final rowBefore = chatViewDebugBuildCountForTest('mention_list_row_obx');
      expect(outerBefore, greaterThan(0));
      expect(rowBefore, greaterThan(0));

      controller.mentionMoveDown();
      await tester.pump();

      expect(controller.mentionSelectedIndex.value, greaterThan(0));
      expect(
        chatViewDebugBuildCountForTest('mention_list_outer_obx'),
        outerBefore,
      );
      expect(
        chatViewDebugBuildCountForTest('mention_list_row_obx'),
        greaterThan(rowBefore),
      );

      await tester.enterText(find.byType(TextField), '');
      await tester.pump(const Duration(milliseconds: 120));
    },
  );

  testWidgets('ChatView hides mention list when query has no matches', (
    WidgetTester tester,
  ) async {
    final sessionService = Get.find<SessionService>() as _FakeSessionService;
    sessionService.detailResult = const SessionDetailResult(
      data: {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '1001', 'member_type': 1, 'role': 3, 'nickname': 'Me'},
          {
            'member_id': '2001',
            'member_type': 1,
            'role': 1,
            'nickname': 'Member One',
          },
        ],
      },
    );

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_mention_none';
    controller.chatTitle = 'Team Room';
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

    await tester.tap(find.byType(TextField));
    await tester.enterText(find.byType(TextField), '@zzz');
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(find.byKey(const Key('chat_mention_list_container')), findsNothing);

    await tester.enterText(find.byType(TextField), '');
    await tester.pump();
  });

  testWidgets('ChatView precaches the latest ten message render states', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    final messages = List.generate(
      12,
      (index) => MessageModel(
        msgId: 'msg_cache_$index',
        sessionId: 'session_cache_test',
        senderId: index.isEven ? 'me' : 'peer',
        content: '# cache heading $index',
        createdAt: index,
      ),
    );
    imService.currentMessages.assignAll(messages);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_cache_test';
    controller.chatTitle = 'session_cache_test';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pump(const Duration(milliseconds: 120));

    for (var i = 2; i < 12; i++) {
      expect(
        MessageBubble.hasCachedFinalRenderState('# cache heading $i'),
        isTrue,
      );
    }
  });

  testWidgets('ChatView delegate robot button toggles delegate panel', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    final agentService = Get.find<AgentService>();

    imService.delegateStates['session_test_2'] = {
      'agent_id': 'agent-1',
      'active': true,
    };
    agentService.agents.assignAll([
      AgentModel(id: 'agent-1', agentName: 'Support Bot'),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_2';
    controller.chatTitle = 'session_test_2';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    // 托管中：AppBar 显示机器人按钮，面板默认收起。
    expect(find.byIcon(Icons.smart_toy_rounded), findsOneWidget);
    expect(find.text('Rounds'), findsNothing);

    // 点击机器人按钮 → 展开托管控制面板（轮数控制 + 停止）。
    await tester.tap(find.byIcon(Icons.smart_toy_rounded));
    await tester.pumpAndSettle();

    expect(find.text('Rounds'), findsOneWidget);
    expect(find.text('Stop'), findsOneWidget);

    // 再次点击 → 面板收起。
    await tester.tap(find.byIcon(Icons.smart_toy_rounded));
    await tester.pumpAndSettle();

    expect(find.text('Rounds'), findsNothing);
  });

  testWidgets('ChatView shows status for active agent output', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'stream-msg-view-1',
        sessionId: 'session_test_output_stop',
        senderId: 'agent-1',
        senderType: 2,
        msgType: 4,
        content: 'streaming reply',
        createdAt: 1,
      ),
    ]);
    imService.agentOutputStates['session_test_output_stop'] = {
      'run_id': 'run-view-1',
      'session_id': 'session_test_output_stop',
      'stream_msg_id': 'stream-msg-view-1',
      'state': 'streaming',
      'can_stop': true,
      'updated_at': 100,
    };

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_output_stop';
    controller.chatTitle = 'Support Bot';
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

    expect(find.text('Support Bot'), findsAtLeastNWidgets(1));
  });

  testWidgets('ChatView routes header avatar tap through controller', (
    WidgetTester tester,
  ) async {
    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    controller.sessionId = 'session_test_avatar_tap';
    controller.chatTitle = 'session_test_avatar_tap';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byType(SessionAvatar).first);
    await tester.pump();

    expect(controller.headerAvatarTapCalls, 1);
  });

  testWidgets('ChatView only shows agent toolbar for the active session', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    const sessionId = 'session_toolbar_active_only';
    imService.sessions.assignAll([
      SessionModel(
        sessionId: sessionId,
        title: 'Toolbar Session',
        type: 'private',
        peerType: 2,
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);
    imService.agentToolbars[sessionId] = const AgentToolbarModel(
      sessionId: sessionId,
      agentId: '2001',
      toolbarId: 'toolbar-active-only',
      revision: 1,
      visible: true,
      updatedAt: 1,
      items: <AgentToolbarItemModel>[
        AgentToolbarItemModel(
          itemId: 'stop_output',
          groupId: 'run_control',
          kind: 'button',
          actionId: 'stop_output',
          label: 'Toolbar Stop',
          icon: 'stop',
          variant: 'danger',
          disabled: false,
          loading: false,
          selected: false,
          tooltip: '',
          badgeText: '',
          confirmTitle: '',
          confirmText: '',
          value: '',
          placeholder: '',
          options: <AgentToolbarOptionModel>[],
          percent: 0,
          centerText: '',
          progressDesc: '',
          progressDetail: '',
          localAction: '',
          commands: <CommandItemModel>[],
        ),
      ],
    );
    imService.setCurrentSessionForTest('another_session');

    await pumpChatViewWithMessages(
      tester,
      sessionId: sessionId,
      locale: const Locale('zh', 'CN'),
      messages: const <MessageModel>[],
    );

    expect(find.text('Toolbar Stop'), findsNothing);

    imService.setCurrentSessionForTest(sessionId);
    await tester.pump();

    expect(find.text('Toolbar Stop'), findsOneWidget);
  });

  testWidgets('ChatView agent toolbar reflects pending action loading state', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    const sessionId = 'session_toolbar_pending_action';
    imService.sessions.assignAll([
      SessionModel(
        sessionId: sessionId,
        title: 'Toolbar Session',
        type: 'private',
        peerType: 2,
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);
    const toolbar = AgentToolbarModel(
      sessionId: sessionId,
      agentId: '2001',
      toolbarId: 'toolbar-pending-action',
      revision: 1,
      visible: true,
      updatedAt: 1,
      items: <AgentToolbarItemModel>[
        AgentToolbarItemModel(
          itemId: 'run_command',
          groupId: 'run_control',
          kind: 'button',
          actionId: 'run_command',
          label: 'Run Command',
          icon: 'run',
          variant: 'primary',
          disabled: false,
          loading: false,
          selected: false,
          tooltip: '',
          badgeText: '',
          confirmTitle: '',
          confirmText: '',
          value: '',
          placeholder: '',
          options: <AgentToolbarOptionModel>[],
          percent: 0,
          centerText: '',
          progressDesc: '',
          progressDetail: '',
          localAction: '',
          commands: <CommandItemModel>[],
        ),
      ],
    );
    imService.agentToolbars[sessionId] = toolbar;
    imService.setCurrentSessionForTest(sessionId);

    await pumpChatViewWithMessages(
      tester,
      sessionId: sessionId,
      locale: const Locale('zh', 'CN'),
      messages: const <MessageModel>[],
    );

    expect(find.text('Run Command'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);

    // sendAgentToolbarAction 依赖已鉴权 WebSocket；本 harness 的 FakeImService
    // 未挂 channel，发包失败会立刻清掉 pending loading（fd186b96）。
    // 这里直接注入 item.loading，验证工具栏 UI 对 loading 态的呈现；
    // send→pending loading 的状态机由 im_service_agent_toolbar_test 覆盖。
    imService.agentToolbars[sessionId] = AgentToolbarModel(
      sessionId: sessionId,
      agentId: toolbar.agentId,
      toolbarId: toolbar.toolbarId,
      revision: toolbar.revision,
      visible: toolbar.visible,
      updatedAt: toolbar.updatedAt,
      items: <AgentToolbarItemModel>[
        toolbar.items.first.copyWith(loading: true, disabled: true),
      ],
    );
    await tester.pump();

    expect(find.byType(CircularProgressIndicator), findsOneWidget);
  });

  testWidgets('ChatView agent toolbar uses flat styling without borders', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    const sessionId = 'session_toolbar_flat_style';
    imService.sessions.assignAll([
      SessionModel(
        sessionId: sessionId,
        title: 'Toolbar Session',
        type: 'private',
        peerType: 2,
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);
    imService.agentToolbars[sessionId] = const AgentToolbarModel(
      sessionId: sessionId,
      agentId: '2001',
      toolbarId: 'toolbar-flat-style',
      revision: 1,
      visible: true,
      updatedAt: 1,
      items: <AgentToolbarItemModel>[
        AgentToolbarItemModel(
          itemId: 'stop_output',
          groupId: 'run_control',
          kind: 'button',
          actionId: 'stop_output',
          label: 'Toolbar Stop',
          icon: 'stop',
          variant: 'danger',
          disabled: false,
          loading: false,
          selected: false,
          tooltip: '',
          badgeText: '',
          confirmTitle: '',
          confirmText: '',
          value: '',
          placeholder: '',
          options: <AgentToolbarOptionModel>[],
          percent: 0,
          centerText: '',
          progressDesc: '',
          progressDetail: '',
          localAction: '',
          commands: <CommandItemModel>[],
        ),
      ],
    );
    imService.setCurrentSessionForTest(sessionId);

    await pumpChatViewWithMessages(
      tester,
      sessionId: sessionId,
      locale: const Locale('zh', 'CN'),
      messages: const <MessageModel>[],
    );

    final toolbarDecoration = _boxDecorationOf(
      tester,
      find.byKey(const ValueKey('chat_agent_toolbar_item_stop_output')),
    );
    expect(
      tester
          .widget<SizedBox>(
            find.byKey(const ValueKey('chat_agent_toolbar_container')),
          )
          .width,
      double.infinity,
    );
    final toolbarList = tester.widget<ListView>(
      find.descendant(
        of: find.byKey(const ValueKey('chat_agent_toolbar_container')),
        matching: find.byType(ListView),
      ),
    );
    expect(toolbarList.scrollDirection, Axis.horizontal);
    expect(toolbarDecoration.border, isNull);
  });

  testWidgets('ChatView private renamed session shows title only in header', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_private_renamed_header_1',
        title: '研发同步',
        type: 'private',
        peerId: '2002',
        peerType: 1,
        peerNickname: 'Liu',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_private_renamed_header_1';
    controller.chatTitle = '研发同步';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('研发同步'), findsOneWidget);
    expect(find.text('Liu'), findsNothing);
    // 现行设计：私聊头部恒显示头像（见提交 364097ea），含被重命名的私聊会话。
    expect(find.byType(SessionAvatar), findsOneWidget);
  });

  testWidgets('ChatView taps title area to scroll to loaded top', (
    WidgetTester tester,
  ) async {
    final controller = await pumpChatViewWithMessages(
      tester,
      sessionId: 'session_test_title_scroll_top',
      locale: const Locale('zh', 'CN'),
      messages: List.generate(
        240,
        (index) => MessageModel(
          msgId: 'msg_title_scroll_$index',
          sessionId: 'session_test_title_scroll_top',
          senderId: index.isEven ? 'peer' : '1001',
          content: 'title_scroll_$index',
          createdAt: index,
        ),
      ),
    );

    controller.scrollController.jumpTo(600);
    await tester.pump();
    expect(controller.scrollController.offset, greaterThan(0));

    final titleTapFinder = find.descendant(
      of: find.byType(AppBar),
      matching: find.byWidgetPredicate((widget) {
        if (widget is! GestureDetector) {
          return false;
        }
        if (widget.onTap == null) {
          return false;
        }
        return widget.child is Row;
      }),
    );
    expect(titleTapFinder, findsOneWidget);
    final titleTapGesture = tester.widget<GestureDetector>(titleTapFinder);
    titleTapGesture.onTap!.call();
    await tester.pumpAndSettle();

    expect(controller.scrollController.offset, lessThanOrEqualTo(1));
  });

  testWidgets('ChatView shows top reached hint when no older history', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    imService.hasOlder = false;

    final controller = await pumpChatViewWithMessages(
      tester,
      sessionId: 'session_test_top_reached_hint',
      locale: const Locale('zh', 'CN'),
      messages: List.generate(
        60,
        (index) => MessageModel(
          msgId: 'msg_top_hint_$index',
          sessionId: 'session_test_top_reached_hint',
          senderId: index.isEven ? 'peer' : '1001',
          content: 'top_hint_$index',
          createdAt: index,
        ),
      ),
    );

    controller.scrollController.jumpTo(0);
    await tester.pumpAndSettle();

    expect(find.text('已到已加载消息顶部'), findsOneWidget);
  });

  testWidgets('ChatView shows private message avatars on both sides', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_private_peer_avatar',
        sessionId: 'session_private_avatar_both_1',
        senderId: 'peer',
        content: 'hello_peer',
        createdAt: 1,
      ),
      MessageModel(
        msgId: 'msg_private_me_avatar',
        sessionId: 'session_private_avatar_both_1',
        senderId: '1001',
        content: 'hello_mine',
        createdAt: 2,
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_private_avatar_both_1';
    controller.chatTitle = 'session_private_avatar_both_1';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(SessionAvatar), findsNWidgets(3));
    final avatars = find
        .byType(SessionAvatar)
        .evaluate()
        .map(
          (element) => tester.getRect(
            find.byElementPredicate((candidate) => candidate == element),
          ),
        )
        .where((rect) => rect.center.dy > 80)
        .toList(growable: false);
    expect(avatars.length, 2);

    final leftAvatar = avatars[0].left <= avatars[1].left
        ? avatars[0]
        : avatars[1];
    final rightAvatar = avatars[0].left > avatars[1].left
        ? avatars[0]
        : avatars[1];

    final peerBubbleRect = tester.getRect(find.text('hello_peer'));
    final myBubbleRect = tester.getRect(find.text('hello_mine'));

    expect(leftAvatar.left, lessThan(peerBubbleRect.left));
    expect(rightAvatar.right, greaterThan(myBubbleRect.right));
  });

  testWidgets('ChatView routes message avatar tap through controller', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_private_peer_avatar_tap',
        sessionId: 'session_private_avatar_tap_1',
        senderId: 'peer',
        content: 'avatar_tap_message',
        createdAt: 1,
      ),
    ]);

    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    controller.sessionId = 'session_private_avatar_tap_1';
    controller.chatTitle = 'session_private_avatar_tap_1';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final messageAvatars = find
        .byType(SessionAvatar)
        .evaluate()
        .map(
          (element) => tester.getRect(
            find.byElementPredicate((candidate) => candidate == element),
          ),
        )
        .where((rect) => rect.center.dy > 80)
        .toList(growable: false);
    expect(messageAvatars, isNotEmpty);

    final targetAvatarRect = messageAvatars.last;
    await tester.tapAt(targetAvatarRect.center);
    await tester.pump();

    expect(controller.messageAvatarTapCalls, 1);
  });

  testWidgets('ChatView routes agent message avatar tap through controller', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_private_agent_avatar_tap',
        sessionId: 'session_private_agent_avatar_tap_1',
        senderId: 'agent-1',
        senderType: 2,
        content: 'agent_avatar_tap_message',
        createdAt: 1,
      ),
    ]);

    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    controller.sessionId = 'session_private_agent_avatar_tap_1';
    controller.chatTitle = 'session_private_agent_avatar_tap_1';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final messageAvatars = find
        .byType(SessionAvatar)
        .evaluate()
        .map(
          (element) => tester.getRect(
            find.byElementPredicate((candidate) => candidate == element),
          ),
        )
        .where((rect) => rect.center.dy > 80)
        .toList(growable: false);
    expect(messageAvatars, isNotEmpty);

    final targetAvatarRect = messageAvatars.last;
    await tester.tapAt(targetAvatarRect.center);
    await tester.pump();

    expect(controller.messageAvatarTapCalls, 1);
  });

  testWidgets('ChatView does not route self message avatar tap', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_private_peer_avatar_for_self_tap',
        sessionId: 'session_private_self_avatar_tap_1',
        senderId: 'peer',
        content: 'peer_avatar_reference',
        createdAt: 1,
      ),
      MessageModel(
        msgId: 'msg_private_self_avatar_tap',
        sessionId: 'session_private_self_avatar_tap_1',
        senderId: '1001',
        content: 'self_avatar_tap_message',
        createdAt: 2,
      ),
    ]);

    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    controller.sessionId = 'session_private_self_avatar_tap_1';
    controller.chatTitle = 'session_private_self_avatar_tap_1';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final messageAvatars = find
        .byType(SessionAvatar)
        .evaluate()
        .map(
          (element) => tester.getRect(
            find.byElementPredicate((candidate) => candidate == element),
          ),
        )
        .where((rect) => rect.center.dy > 80)
        .toList(growable: false);
    expect(messageAvatars.length, 2);

    final selfAvatarRect = messageAvatars[0].right >= messageAvatars[1].right
        ? messageAvatars[0]
        : messageAvatars[1];

    await tester.tapAt(selfAvatarRect.center);
    await tester.pump();

    expect(controller.messageAvatarTapCalls, 0);
  });

  testWidgets('ChatView routes group sender name tap through controller', (
    WidgetTester tester,
  ) async {
    const senderId = 'group_sender_name_target';
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_group_sender_name_tap',
        sessionId: 'session_group_sender_name_tap',
        senderId: senderId,
        content: 'group_sender_name_message',
        createdAt: 1,
      ),
    ]);

    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    controller.sessionId = 'session_group_sender_name_tap';
    controller.chatTitle = 'group sender name tap';
    controller.chatType = 'group';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final messageStack = find.ancestor(
      of: find.text('group_sender_name_message'),
      matching: find.byType(Stack),
    );
    expect(messageStack, findsWidgets);

    final senderMetaTapTarget = find.descendant(
      of: messageStack.first,
      matching: find.byWidgetPredicate(
        (widget) => widget is GestureDetector && widget.child is Row,
      ),
    );
    expect(senderMetaTapTarget, findsOneWidget);

    await tester.tap(senderMetaTapTarget.first);
    await tester.pump();

    expect(controller.messageAvatarTapCalls, 1);
  });

  testWidgets('ChatView routes group sender name long press to mention', (
    WidgetTester tester,
  ) async {
    const senderId = 'group_sender_name_long_press_target';
    final imService = Get.find<ImService>();
    final sessionService = Get.find<SessionService>() as _FakeSessionService;
    sessionService.detailResult = const SessionDetailResult(
      data: {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '1001', 'member_type': 1, 'role': 1},
          {'member_id': senderId, 'member_type': 1, 'role': 1},
        ],
      },
    );
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_group_sender_name_long_press',
        sessionId: 'session_group_sender_name_long_press',
        senderId: senderId,
        content: 'group_sender_name_long_press_message',
        createdAt: 1,
      ),
    ]);

    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    controller.sessionId = 'session_group_sender_name_long_press';
    controller.chatTitle = 'group sender name long press';
    controller.chatType = 'group';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final messageStack = find.ancestor(
      of: find.text('group_sender_name_long_press_message'),
      matching: find.byType(Stack),
    );
    expect(messageStack, findsWidgets);

    final senderMetaTapTarget = find.descendant(
      of: messageStack.first,
      matching: find.byWidgetPredicate(
        (widget) => widget is GestureDetector && widget.child is Row,
      ),
    );
    expect(senderMetaTapTarget, findsOneWidget);

    await tester.longPress(senderMetaTapTarget.first);
    await tester.pump();

    expect(controller.messageMentionCalls, 1);
    expect(controller.messageAvatarTapCalls, 0);
    expect(controller.mentionedSenderId, senderId);
    expect(controller.mentionedSenderType, 1);
    expect(controller.mentionedSenderIsMine, isFalse);
  });

  testWidgets(
    'ChatView routes top-edge group message avatar tap through controller',
    (WidgetTester tester) async {
      final imService = Get.find<ImService>();
      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'msg_group_avatar_top_tap',
          sessionId: 'session_group_avatar_top_tap',
          senderId: 'group_avatar_top_sender',
          content: 'group_avatar_top_message',
          createdAt: 1,
        ),
      ]);

      final controller =
          Get.put<ChatController>(_TestChatController()) as _TestChatController;
      controller.sessionId = 'session_group_avatar_top_tap';
      controller.chatTitle = 'group avatar top tap';
      controller.chatType = 'group';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final messageAvatars = find
          .byType(SessionAvatar)
          .evaluate()
          .map(
            (element) => tester.getRect(
              find.byElementPredicate((candidate) => candidate == element),
            ),
          )
          .where((rect) => rect.center.dy > 80)
          .toList(growable: false);
      expect(messageAvatars, isNotEmpty);

      final targetAvatarRect = messageAvatars.last;
      final topEdgeTapPoint = Offset(
        targetAvatarRect.center.dx,
        targetAvatarRect.top + 1,
      );
      await tester.tapAt(topEdgeTapPoint);
      await tester.pump();

      expect(controller.messageAvatarTapCalls, 1);
    },
  );

  testWidgets('ChatView routes group message avatar long press to mention', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    final sessionService = Get.find<SessionService>() as _FakeSessionService;
    sessionService.detailResult = const SessionDetailResult(
      data: {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '1001', 'member_type': 1, 'role': 1},
          {
            'member_id': 'group_avatar_long_press_sender',
            'member_type': 1,
            'role': 1,
          },
        ],
      },
    );
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_group_avatar_long_press',
        sessionId: 'session_group_avatar_long_press',
        senderId: 'group_avatar_long_press_sender',
        content: 'group_avatar_long_press_message',
        createdAt: 1,
      ),
    ]);

    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    controller.sessionId = 'session_group_avatar_long_press';
    controller.chatTitle = 'group avatar long press';
    controller.chatType = 'group';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final messageAvatars = find
        .byType(SessionAvatar)
        .evaluate()
        .map(
          (element) => tester.getRect(
            find.byElementPredicate((candidate) => candidate == element),
          ),
        )
        .where((rect) => rect.center.dy > 80)
        .toList(growable: false);
    expect(messageAvatars, isNotEmpty);

    await tester.longPressAt(messageAvatars.last.center);
    await tester.pump();

    expect(controller.messageMentionCalls, 1);
    expect(controller.messageAvatarTapCalls, 0);
    expect(controller.mentionedSenderId, 'group_avatar_long_press_sender');
    expect(controller.mentionedSenderType, 1);
    expect(controller.mentionedSenderIsMine, isFalse);
  });

  testWidgets('ChatView manages keyboard inset without scaffold resize', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_resize_lock';
    controller.chatTitle = 'session_test_resize_lock';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final scaffold = tester.widget<Scaffold>(find.byType(Scaffold));
    expect(scaffold.resizeToAvoidBottomInset, isFalse);
  });

  testWidgets(
    'ChatView keeps bottom gesture spacing for Android gesture navigation',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;
      tester.view.viewPadding = FakeViewPadding.zero;
      tester.view.systemGestureInsets = const FakeViewPadding(bottom: 24);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_gesture_bottom_inset';
      controller.chatTitle = 'session_test_gesture_bottom_inset';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final inputArea = tester.widget<Container>(
        find.byKey(const Key('chat_input_area_container')),
      );
      final padding = inputArea.padding as EdgeInsets;
      expect(padding.bottom, 24);
    },
  );

  testWidgets(
    'ChatView keeps bottom safe area spacing for iPhone home indicator',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;
      tester.view.viewPadding = const FakeViewPadding(bottom: 34);
      tester.view.systemGestureInsets = FakeViewPadding.zero;

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_safe_bottom_inset';
      controller.chatTitle = 'session_test_safe_bottom_inset';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final inputArea = tester.widget<Container>(
        find.byKey(const Key('chat_input_area_container')),
      );
      final padding = inputArea.padding as EdgeInsets;
      expect(padding.bottom, 34);
    },
  );

  testWidgets(
    'ChatView applies platform viewport obstruction when input is focused',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final bottomObstructionObserver = _FakeChatBottomObstructionObserver();
      final controller = Get.put(
        ChatController(bottomObstructionObserver: bottomObstructionObserver),
      );
      controller.sessionId = 'session_test_platform_viewport_obstruction_focus';
      controller.chatTitle = 'session_test_platform_viewport_obstruction_focus';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final inputFinder = find.byType(TextField).first;
      final beforeObstructionRect = tester.getRect(inputFinder);

      await tester.tap(inputFinder);
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      bottomObstructionObserver.emit(260);
      await tester.pump();

      final afterObstructionRect = tester.getRect(inputFinder);
      expect(
        afterObstructionRect.top,
        lessThan(beforeObstructionRect.top - 200),
      );
      await tester.pump(const Duration(milliseconds: 120));
    },
  );

  testWidgets(
    'ChatView keeps the bottom composer docked when a card input opens the keyboard',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_card_input_keyboard';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      final openSessionCard = ChatMessageCardCodec.buildAgentOpenSessionCard(
        summaryText: 'Open workspace',
        detailText: 'Provide cwd',
      );
      Get.find<ImService>().currentMessages.assignAll([
        MessageModel(
          msgId: 'msg_card_input_keyboard',
          sessionId: 'session_test_card_input_keyboard',
          senderId: 'peer',
          content: openSessionCard.content,
          createdAt: 1,
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final inputAreaFinder = find.byKey(
        const Key('chat_input_area_container'),
      );
      final cardInputFinder = find.byKey(
        const Key('chat_message_card_agent_open_session_input'),
      );

      expect(cardInputFinder, findsOneWidget);

      final beforeKeyboardRect = tester.getRect(inputAreaFinder);

      await tester.tap(cardInputFinder);
      await tester.pump();
      expect(controller.focusNode.hasFocus, isFalse);

      tester.view.viewInsets = const FakeViewPadding(bottom: 300);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));

      final afterKeyboardRect = tester.getRect(inputAreaFinder);
      expect(
        (afterKeyboardRect.top - beforeKeyboardRect.top).abs(),
        lessThan(1.0),
      );
      expect(controller.shouldFollowKeyboardForInputDock, isFalse);

      tester.view.viewInsets = FakeViewPadding.zero;
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 320));
    },
  );

  testWidgets('ChatView keeps the latest card input above the keyboard', (
    WidgetTester tester,
  ) async {
    addTearDown(tester.view.reset);
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1.0;

    final openSessionCard = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'Open workspace',
      detailText: 'Provide cwd',
    );
    final messages = <MessageModel>[
      for (var index = 0; index < 24; index++)
        MessageModel(
          msgId: 'msg_card_keyboard_cover_$index',
          sessionId: 's_card_keyboard_cover',
          senderId: index.isEven ? 'peer' : '1001',
          content: 'message $index',
          createdAt: index,
        ),
      MessageModel(
        msgId: 'msg_card_keyboard_cover_final',
        sessionId: 's_card_keyboard_cover',
        senderId: 'peer',
        content: openSessionCard.content,
        createdAt: 99,
      ),
    ];

    final controller = Get.put(ChatController());
    controller.sessionId = 's_card_keyboard_cover';
    controller.chatTitle = 'Chat';
    controller.chatType = 'private';
    Get.find<ImService>().currentMessages.assignAll(messages);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    final cardInputFinder = find.byKey(
      const Key('chat_message_card_agent_open_session_input'),
    );
    expect(cardInputFinder, findsOneWidget);

    await tester.tap(cardInputFinder);
    await tester.pump();

    tester.view.viewInsets = const FakeViewPadding(bottom: 300);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));
    await tester.pump();

    final inputRect = tester.getRect(cardInputFinder);
    const keyboardTop = 500.0;
    expect(inputRect.bottom, lessThanOrEqualTo(keyboardTop + 1));

    tester.view.viewInsets = FakeViewPadding.zero;
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 120));
  });

  testWidgets(
    'ChatView keeps a focused card input visible after the keyboard hides',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final openSessionCard = ChatMessageCardCodec.buildAgentOpenSessionCard(
        summaryText: 'Open workspace',
        detailText: 'Provide cwd',
      );
      final messages = <MessageModel>[
        for (var index = 0; index < 18; index++)
          MessageModel(
            msgId: 'msg_card_keyboard_recovery_before_$index',
            sessionId: 's_card_keyboard_recovery',
            senderId: index.isEven ? 'peer' : '1001',
            content: 'before card $index',
            createdAt: index,
          ),
        MessageModel(
          msgId: 'msg_card_keyboard_recovery_card',
          sessionId: 's_card_keyboard_recovery',
          senderId: 'peer',
          content: openSessionCard.content,
          createdAt: 18,
        ),
        for (var index = 19; index < 40; index++)
          MessageModel(
            msgId: 'msg_card_keyboard_recovery_after_$index',
            sessionId: 's_card_keyboard_recovery',
            senderId: index.isEven ? 'peer' : '1001',
            content: 'after card $index',
            createdAt: index,
          ),
      ];

      final controller = Get.put(ChatController());
      controller.sessionId = 's_card_keyboard_recovery';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      Get.find<ImService>().currentMessages.assignAll(messages);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final cardInputFinder = find.byKey(
        const Key('chat_message_card_agent_open_session_input'),
      );
      final targetOffset =
          controller.scrollController.position.maxScrollExtent / 2;
      controller.scrollController.jumpTo(targetOffset);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      expect(cardInputFinder, findsOneWidget);

      await tester.tap(cardInputFinder);
      await tester.pump();

      tester.view.viewInsets = const FakeViewPadding(bottom: 300);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));
      await tester.pump();

      var cardInputRect = tester.getRect(cardInputFinder);
      const keyboardTop = 500.0;
      expect(cardInputRect.bottom, lessThanOrEqualTo(keyboardTop + 1));

      FocusManager.instance.primaryFocus?.unfocus();
      await tester.pump();

      tester.view.viewInsets = FakeViewPadding.zero;
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 320));
      await tester.pump();

      cardInputRect = tester.getRect(cardInputFinder);
      expect(cardInputRect.top, greaterThanOrEqualTo(0));
      expect(cardInputRect.bottom, lessThanOrEqualTo(800));
      expect(
        controller.scrollController.position.maxScrollExtent -
            controller.scrollController.position.pixels,
        greaterThan(1),
      );
    },
  );

  testWidgets('ChatView keeps a lower question input above the keyboard', (
    WidgetTester tester,
  ) async {
    addTearDown(tester.view.reset);
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1.0;

    const questionCard = ChatAgentQuestionCardData(
      requestId: 'req-question-lower-input',
      questions: <ChatAgentQuestionPrompt>[
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Pick environment.',
        ),
        ChatAgentQuestionPrompt(
          index: 2,
          header: 'Region',
          prompt: 'Pick region.',
        ),
        ChatAgentQuestionPrompt(
          index: 3,
          header: 'Owner',
          prompt: 'Describe the owner.',
        ),
      ],
    );
    final questionEnvelope = ChatMessageCardCodec.encode(questionCard);
    final messages = <MessageModel>[
      for (var index = 0; index < 24; index++)
        MessageModel(
          msgId: 'msg_question_focus_$index',
          sessionId: 's_question_focus',
          senderId: index.isEven ? 'peer' : '1001',
          content: 'message $index',
          createdAt: index,
        ),
      MessageModel(
        msgId: 'msg_question_focus_card',
        sessionId: 's_question_focus',
        senderId: 'peer',
        content: questionEnvelope.content,
        createdAt: 99,
      ),
    ];

    final controller = Get.put(ChatController());
    controller.sessionId = 's_question_focus';
    controller.chatTitle = 'Chat';
    controller.chatType = 'private';
    Get.find<ImService>().currentMessages.assignAll(messages);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    final lowerInputFinder = find.byKey(
      const Key('chat_message_card_agent_question_input_3'),
    );
    expect(lowerInputFinder, findsOneWidget);

    await tester.tap(lowerInputFinder);
    await tester.pump();

    tester.view.viewInsets = const FakeViewPadding(bottom: 300);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));
    await tester.pump();

    final inputRect = tester.getRect(lowerInputFinder);
    const keyboardTop = 500.0;
    expect(inputRect.bottom, lessThanOrEqualTo(keyboardTop + 1));

    tester.view.viewInsets = FakeViewPadding.zero;
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 120));
  });

  testWidgets(
    'ChatView switches between card inputs without forcing the list to bottom',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final openSessionCard = ChatMessageCardCodec.buildAgentOpenSessionCard(
        summaryText: 'Open workspace',
        detailText: 'Provide cwd',
      );
      const questionCard = ChatAgentQuestionCardData(
        requestId: 'req-question-switch',
        questions: <ChatAgentQuestionPrompt>[
          ChatAgentQuestionPrompt(
            index: 1,
            header: 'Owner',
            prompt: 'Describe the owner.',
          ),
        ],
      );
      final questionEnvelope = ChatMessageCardCodec.encode(questionCard);
      final messages = <MessageModel>[
        for (var index = 0; index < 8; index++)
          MessageModel(
            msgId: 'msg_card_switch_before_$index',
            sessionId: 's_card_switch',
            senderId: index.isEven ? 'peer' : '1001',
            content: 'before $index',
            createdAt: index,
          ),
        MessageModel(
          msgId: 'msg_card_switch_open',
          sessionId: 's_card_switch',
          senderId: 'peer',
          content: openSessionCard.content,
          createdAt: 8,
        ),
        MessageModel(
          msgId: 'msg_card_switch_question',
          sessionId: 's_card_switch',
          senderId: 'peer',
          content: questionEnvelope.content,
          createdAt: 9,
        ),
        for (var index = 10; index < 24; index++)
          MessageModel(
            msgId: 'msg_card_switch_after_$index',
            sessionId: 's_card_switch',
            senderId: index.isEven ? 'peer' : '1001',
            content: 'after $index',
            createdAt: index,
          ),
      ];

      final controller = Get.put(ChatController());
      controller.sessionId = 's_card_switch';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      Get.find<ImService>().currentMessages.assignAll(messages);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      controller.scrollController.jumpTo(
        controller.scrollController.position.maxScrollExtent / 3,
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      final openInputFinder = find.byKey(
        const Key('chat_message_card_agent_open_session_input'),
      );
      final questionInputFinder = find.byKey(
        const Key('chat_message_card_agent_question_input_1'),
      );

      expect(openInputFinder, findsOneWidget);
      expect(questionInputFinder, findsOneWidget);

      await tester.tap(openInputFinder);
      await tester.pump();

      tester.view.viewInsets = const FakeViewPadding(bottom: 300);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));
      await tester.pump();

      await tester.tap(questionInputFinder);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));

      final openInput = tester.widget<TextField>(openInputFinder);
      final questionInput = tester.widget<TextField>(questionInputFinder);
      expect(openInput.focusNode?.hasFocus, isFalse);
      expect(questionInput.focusNode?.hasFocus, isTrue);

      final questionInputRect = tester.getRect(questionInputFinder);
      const keyboardTop = 500.0;
      expect(questionInputRect.bottom, lessThanOrEqualTo(keyboardTop + 1));

      FocusManager.instance.primaryFocus?.unfocus();
      await tester.pump();

      tester.view.viewInsets = FakeViewPadding.zero;
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 320));
      await tester.pump();

      expect(
        controller.scrollController.position.maxScrollExtent -
            controller.scrollController.position.pixels,
        greaterThan(1),
      );
    },
  );

  testWidgets(
    'ChatView keeps card input focus while long pressing inside the field',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_card_input_long_press';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      final openSessionCard = ChatMessageCardCodec.buildAgentOpenSessionCard(
        summaryText: 'Open workspace',
        detailText: 'Provide cwd',
      );
      Get.find<ImService>().currentMessages.assignAll([
        MessageModel(
          msgId: 'msg_card_long_press',
          sessionId: 'session_test_card_input_long_press',
          senderId: 'peer',
          content: openSessionCard.content,
          createdAt: 1,
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final cardInputFinder = find.byKey(
        const Key('chat_message_card_agent_open_session_input'),
      );
      expect(cardInputFinder, findsOneWidget);

      await tester.tap(cardInputFinder);
      await tester.pump();

      var cardInput = tester.widget<TextField>(cardInputFinder);
      expect(cardInput.focusNode?.hasFocus, isTrue);

      await tester.longPress(cardInputFinder);
      await tester.pump();

      cardInput = tester.widget<TextField>(cardInputFinder);
      expect(cardInput.focusNode?.hasFocus, isTrue);

      await tester.pump(const Duration(milliseconds: 300));
    },
  );

  testWidgets('ChatView dismisses card input when tapping message area', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_card_input_outside_tap';
    controller.chatTitle = 'Chat';
    controller.chatType = 'private';
    final openSessionCard = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'Open workspace',
      detailText: 'Provide cwd',
    );
    Get.find<ImService>().currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_card_outside_tap_card',
        sessionId: 'session_test_card_input_outside_tap',
        senderId: 'peer',
        content: openSessionCard.content,
        createdAt: 1,
      ),
      MessageModel(
        msgId: 'msg_card_outside_tap_text',
        sessionId: 'session_test_card_input_outside_tap',
        senderId: 'peer',
        content: 'outside_message',
        createdAt: 2,
      ),
    ]);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final cardInputFinder = find.byKey(
      const Key('chat_message_card_agent_open_session_input'),
    );
    expect(cardInputFinder, findsOneWidget);

    await tester.tap(cardInputFinder);
    await tester.pump();

    var cardInput = tester.widget<TextField>(cardInputFinder);
    expect(cardInput.focusNode?.hasFocus, isTrue);

    await tester.tap(find.text('outside_message'));
    await tester.pump();

    cardInput = tester.widget<TextField>(cardInputFinder);
    expect(cardInput.focusNode?.hasFocus, isFalse);

    await tester.pump(const Duration(milliseconds: 300));
  });

  testWidgets(
    'ChatView transfers focus from bottom composer to card input and closes attachment menu',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_composer_to_card_input';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      final openSessionCard = ChatMessageCardCodec.buildAgentOpenSessionCard(
        summaryText: 'Open workspace',
        detailText: 'Provide cwd',
      );
      Get.find<ImService>().currentMessages.assignAll([
        MessageModel(
          msgId: 'msg_composer_to_card_input',
          sessionId: 'session_test_composer_to_card_input',
          senderId: 'peer',
          content: openSessionCard.content,
          createdAt: 1,
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final composerInputFinder = find.byWidgetPredicate(
        (widget) =>
            widget is TextField && widget.focusNode == controller.focusNode,
      );
      final cardInputFinder = find.byKey(
        const Key('chat_message_card_agent_open_session_input'),
      );

      expect(composerInputFinder, findsOneWidget);
      expect(cardInputFinder, findsOneWidget);

      await tester.tap(composerInputFinder);
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      await tester.tap(
        find.byKey(const Key('chat_attachment_menu_toggle_button')),
      );
      await tester.pumpAndSettle();
      expect(controller.isAttachmentMenuOpen.value, isTrue);
      expect(controller.focusNode.hasFocus, isFalse);

      // While the menu is open an opaque dismiss scrim covers the message list,
      // so the first tap over the card region is consumed by the scrim: it
      // closes the menu (and keeps focus cleared) rather than focusing the card.
      await tester.tap(cardInputFinder, warnIfMissed: false);
      await tester.pumpAndSettle();
      expect(controller.isAttachmentMenuOpen.value, isFalse);
      expect(controller.focusNode.hasFocus, isFalse);
      var cardInput = tester.widget<TextField>(cardInputFinder);
      expect(cardInput.focusNode?.hasFocus, isFalse);

      // With the menu (and its scrim) dismissed, tapping the card input now
      // transfers focus to it and the bottom composer stays unfocused.
      await tester.tap(cardInputFinder);
      await tester.pump();

      cardInput = tester.widget<TextField>(cardInputFinder);
      expect(controller.focusNode.hasFocus, isFalse);
      expect(controller.isAttachmentMenuOpen.value, isFalse);
      expect(cardInput.focusNode?.hasFocus, isTrue);

      await tester.pump(const Duration(milliseconds: 300));
    },
  );

  testWidgets(
    'ChatView hides standalone card action directives from the transcript',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_hidden_card_action';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      final openSessionCard = ChatMessageCardCodec.buildAgentOpenSessionCard(
        summaryText: 'Open workspace',
        detailText: 'Provide cwd',
      );
      Get.find<ImService>().currentMessages.assignAll([
        MessageModel(
          msgId: 'msg_hidden_card_1',
          sessionId: 'session_test_hidden_card_action',
          senderId: 'peer',
          createdAt: 1,
          content: openSessionCard.content,
        ),
        MessageModel(
          msgId: 'msg_hidden_card_2',
          sessionId: 'session_test_hidden_card_action',
          senderId: '1001',
          createdAt: 2,
          content: 'grix://open/session?cwd=%2Fworkspace%2Fdemo',
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('m:msg_hidden_card_2')), findsNothing);
      expect(find.textContaining('grix://open/session'), findsNothing);
      expect(
        find.byKey(const Key('chat_message_card_agent_open_session')),
        findsOneWidget,
      );
    },
  );

  testWidgets(
    'ChatView renders open session result back on the original card',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_open_session_projection';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      final openSessionCard = ChatMessageCardCodec.buildAgentOpenSessionCard(
        summaryText: 'Open workspace',
        detailText: 'Provide cwd',
      );
      final statusCard = ChatMessageCardCodec.encode(
        const ChatAgentStatusCardData(
          category: 'session',
          status: 'success',
          summary: 'Codex session opened for /workspace/demo.',
          detailText: 'Workspace: /workspace/demo\nWorker: starting',
          referenceId: 'session-1',
        ),
      );
      Get.find<ImService>().currentMessages.assignAll([
        MessageModel(
          msgId: 'msg_open_result_1',
          sessionId: 'session_test_open_session_projection',
          senderId: 'peer',
          createdAt: 1,
          content: openSessionCard.content,
        ),
        MessageModel(
          msgId: 'msg_open_result_2',
          sessionId: 'session_test_open_session_projection',
          senderId: '1001',
          createdAt: 2,
          content: 'grix://open/session?cwd=%2Fworkspace%2Fdemo',
        ),
        MessageModel(
          msgId: 'msg_open_result_3',
          sessionId: 'session_test_open_session_projection',
          senderId: 'peer',
          createdAt: 3,
          content: statusCard.content,
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('chat_message_card_agent_open_session')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('chat_message_card_agent_open_session_result')),
        findsOneWidget,
      );
      expect(find.text('已提交工作目录：/workspace/demo'), findsOneWidget);
      expect(
        find.text('Codex session opened for /workspace/demo.'),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('chat_message_card_agent_status')),
        findsNothing,
      );
      expect(find.byKey(const ValueKey('m:msg_open_result_3')), findsNothing);
    },
  );

  testWidgets(
    'ChatView keeps only the latest open session retry result on the original card',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_open_session_retry_projection';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      final openSessionCard = ChatMessageCardCodec.buildAgentOpenSessionCard(
        summaryText: 'Open workspace',
        detailText: 'Provide cwd',
      );
      final genericErrorCard = ChatMessageCardCodec.encode(
        const ChatAgentStatusCardData(
          category: 'session',
          status: 'error',
          summary: 'Codex session could not be opened.',
          detailText: 'Local service request failed with status 500',
          referenceId: 'session-1',
        ),
      );
      final detailErrorCard = ChatMessageCardCodec.encode(
        const ChatAgentStatusCardData(
          category: 'session',
          status: 'error',
          summary: 'Codex session could not be opened.',
          detailText: 'Directory does not exist: /eee',
          referenceId: 'session-1',
        ),
      );
      final successCard = ChatMessageCardCodec.encode(
        const ChatAgentStatusCardData(
          category: 'session',
          status: 'success',
          summary: 'Codex session opened for /workspace/demo.',
          detailText: 'Workspace: /workspace/demo\nWorker: starting',
          referenceId: 'session-1',
        ),
      );
      Get.find<ImService>().currentMessages.assignAll([
        MessageModel(
          msgId: 'msg_open_retry_1',
          sessionId: 'session_test_open_session_retry_projection',
          senderId: 'peer',
          createdAt: 1,
          content: openSessionCard.content,
        ),
        MessageModel(
          msgId: 'msg_open_retry_2',
          sessionId: 'session_test_open_session_retry_projection',
          senderId: '1001',
          createdAt: 2,
          content: 'grix://open/session?cwd=%2Feee',
        ),
        MessageModel(
          msgId: 'msg_open_retry_3',
          sessionId: 'session_test_open_session_retry_projection',
          senderId: 'peer',
          createdAt: 3,
          content: genericErrorCard.content,
        ),
        MessageModel(
          msgId: 'msg_open_retry_4',
          sessionId: 'session_test_open_session_retry_projection',
          senderId: 'peer',
          createdAt: 4,
          content: detailErrorCard.content,
        ),
        MessageModel(
          msgId: 'msg_open_retry_5',
          sessionId: 'session_test_open_session_retry_projection',
          senderId: '1001',
          createdAt: 5,
          content: 'grix://open/session?cwd=%2Fworkspace%2Fdemo',
        ),
        MessageModel(
          msgId: 'msg_open_retry_6',
          sessionId: 'session_test_open_session_retry_projection',
          senderId: 'peer',
          createdAt: 6,
          content: successCard.content,
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('chat_message_card_agent_open_session')),
        findsOneWidget,
      );
      expect(find.text('已提交工作目录：/workspace/demo'), findsOneWidget);
      expect(
        find.text('Codex session opened for /workspace/demo.'),
        findsOneWidget,
      );
      expect(
        find.text('Local service request failed with status 500'),
        findsNothing,
      );
      expect(find.text('Directory does not exist: /eee'), findsNothing);
      expect(
        find.byKey(const Key('chat_message_card_agent_status')),
        findsNothing,
      );
      expect(find.byKey(const ValueKey('m:msg_open_retry_3')), findsNothing);
      expect(find.byKey(const ValueKey('m:msg_open_retry_4')), findsNothing);
      expect(find.byKey(const ValueKey('m:msg_open_retry_6')), findsNothing);
    },
  );

  testWidgets(
    'ChatView keeps question success and failure inside the original card',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_question_projection';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      const questionCard = ChatAgentQuestionCardData(
        requestId: 'req-question-1',
        questions: [
          ChatAgentQuestionPrompt(
            index: 1,
            header: 'Environment',
            prompt: 'Choose environment.',
            options: ['prod', 'staging'],
          ),
        ],
      );
      final questionEnvelope = ChatMessageCardCodec.encode(questionCard);
      final errorStatus = ChatMessageCardCodec.encode(
        const ChatAgentStatusCardData(
          category: 'question',
          status: 'error',
          summary: 'Question request req-question-1 could not be recorded.',
          detailText: 'The reply format is invalid.',
          referenceId: 'req-question-1',
        ),
      );
      final successStatus = ChatMessageCardCodec.encode(
        const ChatAgentStatusCardData(
          category: 'question',
          status: 'success',
          summary: 'Question request req-question-1 answers recorded.',
          referenceId: 'req-question-1',
        ),
      );
      Get.find<ImService>().currentMessages.assignAll([
        MessageModel(
          msgId: 'msg_question_1',
          sessionId: 'session_test_question_projection',
          senderId: 'peer',
          createdAt: 1,
          content: questionEnvelope.content,
        ),
        MessageModel(
          msgId: 'msg_question_2',
          sessionId: 'session_test_question_projection',
          senderId: '1001',
          createdAt: 2,
          content: ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(
            questionCard,
            'staging',
          ),
        ),
        MessageModel(
          msgId: 'msg_question_3',
          sessionId: 'session_test_question_projection',
          senderId: 'peer',
          createdAt: 3,
          content: errorStatus.content,
        ),
        MessageModel(
          msgId: 'msg_question_4',
          sessionId: 'session_test_question_projection',
          senderId: '1001',
          createdAt: 4,
          content: ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(
            questionCard,
            'prod',
          ),
        ),
        MessageModel(
          msgId: 'msg_question_5',
          sessionId: 'session_test_question_projection',
          senderId: 'peer',
          createdAt: 5,
          content: successStatus.content,
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('chat_message_card_agent_question')),
        findsOneWidget,
      );
      expect(find.text('已回答：prod'), findsOneWidget);
      expect(
        find.text('Question request req-question-1 answers recorded.'),
        findsOneWidget,
      );
      expect(find.text('The reply format is invalid.'), findsNothing);
      expect(
        find.byKey(const Key('chat_message_card_agent_status')),
        findsNothing,
      );
      expect(find.byKey(const ValueKey('m:msg_question_3')), findsNothing);
      expect(find.byKey(const ValueKey('m:msg_question_5')), findsNothing);
    },
  );

  testWidgets(
    'ChatView keeps the latest message above the input when focus applies an existing platform obstruction',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final messages = List.generate(
        40,
        (index) => MessageModel(
          msgId: 'msg_existing_obstruction_$index',
          sessionId: 's_existing_obstruction',
          senderId: index.isEven ? 'peer' : '1001',
          content: index == 39
              ? 'latest message for existing obstruction'
              : 'message $index',
          createdAt: index,
        ),
      );

      final bottomObstructionObserver = _FakeChatBottomObstructionObserver(
        initialBottomObstruction: 260,
      );
      final controller = Get.put(
        ChatController(bottomObstructionObserver: bottomObstructionObserver),
      );
      controller.sessionId = 's_existing_obstruction';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      Get.find<ImService>().currentMessages.assignAll(messages);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      final inputFinder = find.byType(TextField).first;
      final latestMessageFinder = find.byKey(
        const ValueKey('m:msg_existing_obstruction_39'),
      );
      final inputAreaFinder = find.byKey(
        const Key('chat_input_area_container'),
      );
      expect(latestMessageFinder, findsOneWidget);

      await tester.tap(inputFinder);
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      await tester.pump(const Duration(milliseconds: 320));
      await tester.pump();

      final latestMessageRect = tester.getRect(latestMessageFinder);
      final inputAreaRect = tester.getRect(inputAreaFinder);
      expect(
        latestMessageRect.bottom,
        lessThanOrEqualTo(inputAreaRect.top + 1),
      );
      expect(
        (controller.scrollController.position.maxScrollExtent -
                controller.scrollController.position.pixels)
            .abs(),
        lessThanOrEqualTo(1),
      );
    },
  );

  testWidgets(
    'ChatView ignores platform viewport obstruction when input is not focused',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final bottomObstructionObserver = _FakeChatBottomObstructionObserver();
      final controller = Get.put(
        ChatController(bottomObstructionObserver: bottomObstructionObserver),
      );
      controller.sessionId = 'session_test_platform_viewport_obstruction_blur';
      controller.chatTitle = 'session_test_platform_viewport_obstruction_blur';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final inputFinder = find.byType(TextField).first;
      final beforeObstructionRect = tester.getRect(inputFinder);

      bottomObstructionObserver.emit(260);
      await tester.pump();

      final afterObstructionRect = tester.getRect(inputFinder);
      expect(
        (afterObstructionRect.top - beforeObstructionRect.top).abs(),
        lessThan(1.0),
      );
      expect(controller.focusNode.hasFocus, isFalse);
      await tester.pump(const Duration(milliseconds: 120));
    },
  );

  testWidgets(
    'ChatView keeps the latest message above the bottom dock when keyboard and reply preview are shown',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final messages = List.generate(
        40,
        (index) => MessageModel(
          msgId: 'msg_keyboard_reply_$index',
          sessionId: 's_reply_vis',
          senderId: index.isEven ? 'peer' : '1001',
          content: index == 39 ? 'latest bottom message' : 'message $index',
          createdAt: index,
        ),
      );

      final controller = Get.put(ChatController());
      controller.sessionId = 's_reply_vis';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      Get.find<ImService>().currentMessages.assignAll(messages);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      final inputFinder = find.byType(TextField).first;
      final latestMessageFinder = find.byKey(
        const ValueKey('m:msg_keyboard_reply_39'),
      );
      expect(latestMessageFinder, findsOneWidget);

      await tester.tap(inputFinder);
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      tester.view.viewInsets = const FakeViewPadding(bottom: 300);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));
      await tester.pump();

      controller.setReplyingToMessage(messages.first);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));
      await tester.pump();

      final latestMessageRect = tester.getRect(latestMessageFinder);
      final replyPreviewRect = tester.getRect(
        find.byKey(const Key('chat_reply_preview_container')),
      );

      expect(
        latestMessageRect.bottom,
        lessThanOrEqualTo(replyPreviewRect.top + 1),
      );
      expect(
        (controller.scrollController.position.maxScrollExtent -
                controller.scrollController.position.pixels)
            .abs(),
        lessThanOrEqualTo(1),
      );

      controller.inputController.clear();
      await tester.pump();
      Get.find<ImService>().updateSessionComposing(
        controller.sessionId,
        active: false,
      );
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets(
    'ChatView keeps the latest message above the input area through staged keyboard growth',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final messages = List.generate(
        40,
        (index) => MessageModel(
          msgId: 'msg_keyboard_staged_$index',
          sessionId: 's_keyboard_staged',
          senderId: index.isEven ? 'peer' : '1001',
          content: index == 39
              ? 'latest staged keyboard message'
              : 'message $index',
          createdAt: index,
        ),
      );

      final controller = Get.put(ChatController());
      controller.sessionId = 's_keyboard_staged';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      Get.find<ImService>().currentMessages.assignAll(messages);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      final inputFinder = find.byType(TextField).first;
      final latestMessageFinder = find.byKey(
        const ValueKey('m:msg_keyboard_staged_39'),
      );
      final inputAreaFinder = find.byKey(
        const Key('chat_input_area_container'),
      );
      expect(latestMessageFinder, findsOneWidget);

      await tester.tap(inputFinder);
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      for (final bottomInset in const <double>[100, 180, 260, 300]) {
        tester.view.viewInsets = FakeViewPadding(bottom: bottomInset);
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 80));
      }
      await tester.pump(const Duration(milliseconds: 220));
      await tester.pump();

      final latestMessageRect = tester.getRect(latestMessageFinder);
      final inputAreaRect = tester.getRect(inputAreaFinder);
      expect(
        latestMessageRect.bottom,
        lessThanOrEqualTo(inputAreaRect.top + 1),
      );
      expect(
        (controller.scrollController.position.maxScrollExtent -
                controller.scrollController.position.pixels)
            .abs(),
        lessThanOrEqualTo(1),
      );

      tester.view.viewInsets = FakeViewPadding.zero;
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));
    },
  );

  testWidgets(
    'ChatView keeps the latest message above the input area after keyboard hides',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final messages = List.generate(
        40,
        (index) => MessageModel(
          msgId: 'msg_keyboard_hide_$index',
          sessionId: 's_keyboard_hide',
          senderId: index.isEven ? 'peer' : '1001',
          content: index == 39
              ? 'latest keyboard hide message'
              : 'message $index',
          createdAt: index,
        ),
      );

      final controller = Get.put(ChatController());
      controller.sessionId = 's_keyboard_hide';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      Get.find<ImService>().currentMessages.assignAll(messages);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      final inputFinder = find.byType(TextField).first;
      final latestMessageFinder = find.byKey(
        const ValueKey('m:msg_keyboard_hide_39'),
      );
      final inputAreaFinder = find.byKey(
        const Key('chat_input_area_container'),
      );
      expect(latestMessageFinder, findsOneWidget);

      await tester.tap(inputFinder);
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      tester.view.viewInsets = const FakeViewPadding(bottom: 300);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));
      await tester.pump();

      var latestMessageRect = tester.getRect(latestMessageFinder);
      var inputAreaRect = tester.getRect(inputAreaFinder);
      expect(
        latestMessageRect.bottom,
        lessThanOrEqualTo(inputAreaRect.top + 1),
      );
      expect(
        (controller.scrollController.position.maxScrollExtent -
                controller.scrollController.position.pixels)
            .abs(),
        lessThanOrEqualTo(1),
      );

      tester.view.viewInsets = FakeViewPadding.zero;
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));
      await tester.pump();

      latestMessageRect = tester.getRect(latestMessageFinder);
      inputAreaRect = tester.getRect(inputAreaFinder);
      expect(
        latestMessageRect.bottom,
        lessThanOrEqualTo(inputAreaRect.top + 1),
      );
      expect(
        (controller.scrollController.position.maxScrollExtent -
                controller.scrollController.position.pixels)
            .abs(),
        lessThanOrEqualTo(1),
      );
    },
  );

  testWidgets(
    'ChatView keeps the latest message above the input area across repeated send and keyboard toggle cycles',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final imService = Get.find<ImService>() as _FakeImService;
      final messages = List.generate(
        40,
        (index) => MessageModel(
          msgId: 'msg_keyboard_repeat_send_$index',
          sessionId: 's_keyboard_repeat_send',
          senderId: index.isEven ? 'peer' : '1001',
          content: index == 39
              ? 'latest repeated keyboard message'
              : 'message $index',
          createdAt: index,
        ),
      );

      final controller = Get.put(ChatController());
      controller.sessionId = 's_keyboard_repeat_send';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      imService.currentMessages.assignAll(messages);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      final inputFinder = find.byType(TextField).first;
      final sendButtonFinder = find.byIcon(Icons.arrow_upward_rounded);
      final latestMessageFinder = find.byKey(
        const ValueKey('m:msg_keyboard_repeat_send_39'),
      );
      final inputAreaFinder = find.byKey(
        const Key('chat_input_area_container'),
      );
      expect(latestMessageFinder, findsOneWidget);

      for (var cycle = 0; cycle < 4; cycle++) {
        await tester.tap(inputFinder);
        await tester.pump();
        expect(controller.focusNode.hasFocus, isTrue);

        tester.view.viewInsets = const FakeViewPadding(bottom: 300);
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 120));
        await tester.pump();

        controller.inputController.text = 'cycle message $cycle';
        await tester.pump();

        await tester.tap(sendButtonFinder);
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 140));
        await tester.pump();

        var latestMessageRect = tester.getRect(latestMessageFinder);
        var inputAreaRect = tester.getRect(inputAreaFinder);
        expect(
          latestMessageRect.bottom,
          lessThanOrEqualTo(inputAreaRect.top + 1),
        );
        expect(
          (controller.scrollController.position.maxScrollExtent -
                  controller.scrollController.position.pixels)
              .abs(),
          lessThanOrEqualTo(1),
        );

        tester.view.viewInsets = FakeViewPadding.zero;
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 220));
        await tester.pump();

        latestMessageRect = tester.getRect(latestMessageFinder);
        inputAreaRect = tester.getRect(inputAreaFinder);
        expect(
          latestMessageRect.bottom,
          lessThanOrEqualTo(inputAreaRect.top + 1),
        );
        expect(
          (controller.scrollController.position.maxScrollExtent -
                  controller.scrollController.position.pixels)
              .abs(),
          lessThanOrEqualTo(1),
        );

        await tester.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 1300)),
        );
        await tester.pump();
      }

      expect(imService.sendCalls, 4);
    },
  );

  testWidgets(
    'ChatView keeps the latest message above the input area when multiline input expands',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final messages = List.generate(
        40,
        (index) => MessageModel(
          msgId: 'msg_keyboard_multiline_$index',
          sessionId: 's_multiline',
          senderId: index.isEven ? 'peer' : '1001',
          content: index == 39 ? 'latest multiline message' : 'message $index',
          createdAt: index,
        ),
      );

      final controller = Get.put(ChatController());
      controller.sessionId = 's_multiline';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      Get.find<ImService>().currentMessages.assignAll(messages);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      final inputFinder = find.byType(TextField).first;
      final latestMessageFinder = find.byKey(
        const ValueKey('m:msg_keyboard_multiline_39'),
      );
      final inputAreaFinder = find.byKey(
        const Key('chat_input_area_container'),
      );
      expect(latestMessageFinder, findsOneWidget);

      await tester.tap(inputFinder);
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      tester.view.viewInsets = const FakeViewPadding(bottom: 300);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));
      await tester.pump();

      await tester.enterText(inputFinder, 'line 1\nline 2\nline 3\nline 4');
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));
      await tester.pump();

      final latestMessageRect = tester.getRect(latestMessageFinder);
      final inputAreaRect = tester.getRect(inputAreaFinder);

      expect(
        latestMessageRect.bottom,
        lessThanOrEqualTo(inputAreaRect.top + 1),
      );
      expect(
        (controller.scrollController.position.maxScrollExtent -
                controller.scrollController.position.pixels)
            .abs(),
        lessThanOrEqualTo(1),
      );

      await tester.enterText(inputFinder, '');
      await tester.pump();
      Get.find<ImService>().updateSessionComposing(
        controller.sessionId,
        active: false,
      );
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('ChatView shows retry for agent delivery failure', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_failed_delivery',
        sessionId: 'session_test_3',
        senderId: '1001',
        content: 'hello',
        createdAt: 1,
        agentDeliveryStatus: 'failed',
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_3';
    controller.chatTitle = 'session_test_3';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('处理失败'), findsOneWidget);
    expect(find.textContaining('OpenClaw'), findsNothing);
    final retryButtonFinder = find.byType(ChatRetryActionButton);
    expect(retryButtonFinder, findsOneWidget);
    expect(
      find.descendant(
        of: retryButtonFinder,
        matching: find.byType(AnimatedScale),
      ),
      findsOneWidget,
    );
    expect(
      find.descendant(
        of: retryButtonFinder,
        matching: find.byType(AnimatedContainer),
      ),
      findsOneWidget,
    );
    expect(find.text('重试'), findsOneWidget);

    await tester.tap(retryButtonFinder);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));

    expect(imService.retryCalls, 1);
    expect(imService.retriedClientMsgId, isNull);
    expect(imService.retriedMsgId, 'msg_failed_delivery');
  });

  testWidgets('ChatView keeps retry wording for local send failure', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'temp_msg_failed_send',
        sessionId: 'session_test_local_failed',
        senderId: '1001',
        content: 'hello',
        createdAt: 1,
        status: 'failed',
        clientMsgId: 'cid_local_failed',
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_local_failed';
    controller.chatTitle = 'session_test_local_failed';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('chat_send_failed'.tr), findsOneWidget);
    expect(find.text('重试'), findsOneWidget);

    await tester.tap(find.text('重试'));
    await tester.pump();

    expect(imService.retryCalls, 1);
    expect(imService.retriedClientMsgId, 'cid_local_failed');
    expect(imService.retriedMsgId, 'temp_msg_failed_send');
  });

  testWidgets('ChatView hides keyboard when tapping message area', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_1',
        sessionId: 'session_test_4',
        senderId: 'peer',
        content: 'message_1',
        createdAt: 1,
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_4';
    controller.chatTitle = 'session_test_4';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byType(TextField).first);
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);

    await tester.tap(find.text('message_1'));
    await tester.pump();
    expect(controller.focusNode.hasFocus, isFalse);
  });

  testWidgets('ChatView keeps keyboard focus when tapping send button', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_2',
        sessionId: 'session_test_5',
        senderId: 'peer',
        content: 'message_2',
        createdAt: 1,
      ),
    ]);

    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    controller.sessionId = 'session_test_5';
    controller.chatTitle = 'session_test_5';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byType(TextField).first);
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);

    controller.inputController.text = 'hello';
    await tester.pump();

    await tester.tap(find.byIcon(Icons.arrow_upward_rounded));
    await tester.pump(const Duration(milliseconds: 120));

    expect(imService.sendCalls, 1);
    expect(imService.sentContent, 'hello');
    expect(imService.sentSessionId, 'session_test_5');
    expect(controller.focusNode.hasFocus, isTrue);
  });

  testWidgets(
    'ChatView aligns plus and send with single-line input center',
    (WidgetTester tester) async {
      final controller =
          Get.put<ChatController>(_TestChatController()) as _TestChatController;
      controller.sessionId = 'session_composer_align';
      controller.chatTitle = 'session_composer_align';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final plus = tester.getRect(
        find.byKey(const Key('chat_attachment_menu_toggle_button')),
      );
      final send = tester.getRect(find.byKey(const Key('chat_send_button')));
      final input = tester.getRect(find.byType(TextField).first);

      expect(plus.height, 40);
      expect(send.height, 40);
      // The composer field must never be shorter than the side controls
      // (regression: desktop builds showed the input ~10px below the send
      // button); it may be slightly taller due to font metrics.
      expect(input.height, greaterThanOrEqualTo(send.height));
      expect(plus.center.dy, closeTo(send.center.dy, 0.5));
      expect(plus.center.dy, closeTo(input.center.dy, 1.0));
      expect(send.center.dy, closeTo(input.center.dy, 1.0));
    },
  );

  testWidgets(
    'ChatView shows voice preview with the same color as typed text',
    (WidgetTester tester) async {
      final controller =
          Get.put<ChatController>(_TestChatController()) as _TestChatController;
      controller.sessionId = 'session_voice_preview_color';
      controller.chatTitle = 'session_voice_preview_color';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      controller.isVoiceCommandListening.value = true;
      controller.voiceCommandTranscriptPreview.value = '你好世界';
      await tester.pump();

      final field = tester.widget<TextField>(find.byType(TextField).first);
      expect(field.decoration?.hintText, '你好世界');
      expect(field.decoration?.hintStyle?.color, isNotNull);
      expect(field.decoration!.hintStyle!.color!.a, closeTo(1.0, 0.01));
    },
  );

  testWidgets(
    'ChatView restores composer focus after send when platform policy requires it',
    (WidgetTester tester) async {
      final imService = Get.find<ImService>() as _FakeImService;
      final controller = Get.put(
        ChatController(
          keyboardPlatformBehavior: ChatKeyboardPlatformBehavior.resolve(
            isWeb: true,
            targetPlatform: TargetPlatform.macOS,
          ),
        ),
      );
      controller.sessionId = 'session_test_policy_restore_focus';
      controller.chatTitle = 'session_test_policy_restore_focus';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      controller.inputController.text = 'hello';
      controller.focusNode.requestFocus();
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      controller.focusNode.unfocus();
      await tester.pump();
      expect(controller.focusNode.hasFocus, isFalse);

      controller.sendMessage();
      await tester.pump();

      expect(imService.sendCalls, 1);
      expect(controller.focusNode.hasFocus, isTrue);
      await tester.pump(const Duration(milliseconds: 120));
    },
  );

  testWidgets(
    'ChatView keeps composer unfocused after send when platform policy disables restore',
    (WidgetTester tester) async {
      final imService = Get.find<ImService>() as _FakeImService;
      final controller = Get.put(
        ChatController(
          keyboardPlatformBehavior: ChatKeyboardPlatformBehavior.resolve(
            isWeb: true,
            targetPlatform: TargetPlatform.iOS,
          ),
        ),
      );
      controller.sessionId = 'session_test_policy_no_restore_focus';
      controller.chatTitle = 'session_test_policy_no_restore_focus';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      controller.inputController.text = 'hello';
      controller.focusNode.requestFocus();
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      controller.focusNode.unfocus();
      await tester.pump();
      expect(controller.focusNode.hasFocus, isFalse);

      controller.sendMessage();
      await tester.pump();

      expect(imService.sendCalls, 1);
      expect(controller.focusNode.hasFocus, isFalse);
      await tester.pump(const Duration(milliseconds: 120));
    },
  );

  testWidgets('ChatView hides keyboard when opening attachment menu', (
    WidgetTester tester,
  ) async {
    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    controller.sessionId = 'session_test_6';
    controller.chatTitle = 'session_test_6';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byType(TextField).first);
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);

    await tester.tap(
      find.byKey(const Key('chat_attachment_menu_toggle_button')),
    );
    await tester.pump();

    expect(controller.focusNode.hasFocus, isFalse);
    expect(controller.isAttachmentMenuOpen.value, isTrue);
    expect(find.byKey(const Key('chat_attachment_menu_panel')), findsOneWidget);
    expect(find.text('上传图片'), findsOneWidget);
    expect(find.text('上传视频'), findsOneWidget);
    expect(find.text('上传文件'), findsOneWidget);
  });

  testWidgets('ChatView routes attachment menu actions', (
    WidgetTester tester,
  ) async {
    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_attachment_1',
        sessionId: 'session_test_attachments',
        senderId: 'peer',
        content: 'seed message',
        createdAt: 1,
      ),
    ]);
    controller.sessionId = 'session_test_attachments';
    controller.chatTitle = 'session_test_attachments';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(const Key('chat_attachment_menu_toggle_button')),
    );
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(const Key('chat_attachment_menu_image_button')),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('chat_attachment_source_gallery')));
    await tester.pumpAndSettle();
    expect(controller.pickImageCalls, 1);

    await tester.tap(
      find.byKey(const Key('chat_attachment_menu_toggle_button')),
    );
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(const Key('chat_attachment_menu_video_button')),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('chat_attachment_source_gallery')));
    await tester.pumpAndSettle();
    expect(controller.pickVideoCalls, 1);

    await tester.tap(
      find.byKey(const Key('chat_attachment_menu_toggle_button')),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('chat_attachment_menu_file_button')));
    await tester.pumpAndSettle();
    expect(controller.pickFileCalls, 1);
  });

  testWidgets('ChatView shows hide-send action in group attachment menu', (
    WidgetTester tester,
  ) async {
    tester.view.physicalSize = const Size(1200, 2200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    });

    final controller =
        Get.put<ChatController>(_TestChatController()) as _TestChatController;
    final sessionService = Get.find<SessionService>() as _FakeSessionService;
    final imService = Get.find<ImService>();
    sessionService.detailResult = const SessionDetailResult(
      data: {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '1001', 'member_type': 1, 'role': 3, 'nickname': 'Me'},
          {
            'member_id': '2001',
            'member_type': 1,
            'role': 1,
            'nickname': 'Member One',
          },
        ],
      },
    );
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_hide_send_seed',
        sessionId: 'session_test_hide_send_menu',
        senderId: '2001',
        content: 'seed message',
        createdAt: 1,
      ),
    ]);
    controller.sessionId = 'session_test_hide_send_menu';
    controller.chatTitle = 'session_test_hide_send_menu';
    controller.chatType = 'group';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(const Key('chat_attachment_menu_toggle_button')),
    );
    await tester.pumpAndSettle();

    final hideSendFinder = find.byKey(
      const Key('chat_attachment_menu_hide_send_button'),
    );
    final imageFinder = find.byKey(
      const Key('chat_attachment_menu_image_button'),
    );
    expect(hideSendFinder, findsOneWidget);
    expect(find.text('隐藏发送'), findsOneWidget);
    expect(
      tester.getTopLeft(hideSendFinder).dx < tester.getTopLeft(imageFinder).dx,
      isTrue,
    );

    await tester.tap(hideSendFinder);
    await tester.pumpAndSettle();
    expect(controller.isAttachmentMenuOpen.value, isFalse);
    expect(controller.showVisibleToPicker.value, isTrue);
    expect(
      find.byKey(const Key('visible_to_picker_container')),
      findsOneWidget,
    );
  });

  testWidgets('ChatView visible-to selection keeps picker container stable', (
    WidgetTester tester,
  ) async {
    final sessionService = Get.find<SessionService>() as _FakeSessionService;
    final imService = Get.find<ImService>();
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
            'nickname': 'Member One',
          },
          {
            'member_id': '2002',
            'member_type': 1,
            'role': 1,
            'nickname': 'Member Two',
          },
        ],
      },
    );
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_visible_to_scope_seed',
        sessionId: 'session_visible_to_scope',
        senderId: '2001',
        content: 'seed message',
        createdAt: 1,
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_visible_to_scope';
    controller.chatTitle = 'session_visible_to_scope';
    controller.chatType = 'group';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    controller.showVisibleToPicker.value = true;
    await tester.pump();

    expect(
      find.byKey(const Key('visible_to_picker_container')),
      findsOneWidget,
    );
    final outerBefore = chatViewDebugBuildCountForTest(
      'visible_to_picker_outer_obx',
    );
    final rowBefore = chatViewDebugBuildCountForTest(
      'visible_to_picker_row_obx',
    );
    expect(outerBefore, greaterThan(0));
    expect(rowBefore, greaterThan(0));

    controller.toggleVisibleToMember('2002');
    await tester.pump();

    expect(controller.isMemberSelectedForVisibleTo('2002'), isTrue);
    expect(
      chatViewDebugBuildCountForTest('visible_to_picker_outer_obx'),
      outerBefore,
    );
    expect(
      chatViewDebugBuildCountForTest('visible_to_picker_row_obx'),
      greaterThan(rowBefore),
    );
  });

  testWidgets(
    'ChatView message input disables autofill and keeps submit focus stable',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_input_traits';
      controller.chatTitle = 'session_test_input_traits';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final textField = tester.widget<TextField>(find.byType(TextField).first);
      expect(textField.keyboardType, TextInputType.multiline);
      expect(textField.autofillHints, isEmpty);
      expect(textField.onEditingComplete, isNotNull);
      expect(textField.onSubmitted, isNotNull);
      expect(textField.textInputAction, TextInputAction.newline);
    },
  );

  testWidgets('ChatView inserts line break on Enter key', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_enter_newline';
    controller.chatTitle = 'session_test_enter_newline';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final inputFinder = find.byType(TextField).first;
    await tester.tap(inputFinder);
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);

    controller.inputController.value = const TextEditingValue(
      text: 'hello enter',
      selection: TextSelection.collapsed(offset: 11),
    );
    await tester.pump();

    await sendKeyPress(tester, LogicalKeyboardKey.enter);

    await tester.pump();

    expect(imService.sendCalls, 0);
    expect(controller.inputController.text, 'hello enter\n');
    expect(
      controller.inputController.selection,
      const TextSelection.collapsed(offset: 12),
    );

    controller.inputController.clear();
    await tester.pump();
    Get.find<ImService>().updateSessionComposing(
      controller.sessionId,
      active: false,
    );
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('ChatView converts input action submit into line break', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_input_action_newline';
    controller.chatTitle = 'session_test_input_action_newline';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final inputFinder = find.byType(TextField).first;
    await tester.tap(inputFinder);
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);

    controller.inputController.value = const TextEditingValue(
      text: 'hello action',
      selection: TextSelection.collapsed(offset: 12),
    );
    await tester.pump();

    await tester.testTextInput.receiveAction(TextInputAction.send);
    await tester.pump();

    expect(imService.sendCalls, 0);
    expect(controller.inputController.text, 'hello action\n');
    expect(
      controller.inputController.selection,
      const TextSelection.collapsed(offset: 13),
    );

    controller.inputController.clear();
    await tester.pump();
    Get.find<ImService>().updateSessionComposing(
      controller.sessionId,
      active: false,
    );
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('ChatView does not insert line break on Enter while composing', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_enter_composing';
    controller.chatTitle = 'session_test_enter_composing';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final inputFinder = find.byType(TextField).first;
    await tester.tap(inputFinder);
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);

    controller.inputController.value = const TextEditingValue(
      text: 'hello',
      selection: TextSelection.collapsed(offset: 5),
      composing: TextRange(start: 0, end: 5),
    );
    await tester.pump();

    await sendKeyPress(tester, LogicalKeyboardKey.enter);
    await tester.pump(const Duration(milliseconds: 120));

    expect(imService.sendCalls, 0);
    expect(controller.inputController.text, 'hello');

    controller.inputController.clear();
    await tester.pump();
  });

  testWidgets(
    'ChatView inserts line break on the next Enter after composition commit',
    (WidgetTester tester) async {
      final imService = Get.find<ImService>() as _FakeImService;
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_enter_after_composing';
      controller.chatTitle = 'session_test_enter_after_composing';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final inputFinder = find.byType(TextField).first;
      await tester.tap(inputFinder);
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      controller.inputController.value = const TextEditingValue(
        text: 'hello',
        selection: TextSelection.collapsed(offset: 5),
        composing: TextRange(start: 0, end: 5),
      );
      await tester.pump();

      await sendKeyPress(tester, LogicalKeyboardKey.enter);
      await tester.pump();

      expect(imService.sendCalls, 0);

      controller.inputController.value = const TextEditingValue(
        text: 'hello',
        selection: TextSelection.collapsed(offset: 5),
      );
      await tester.pump();

      await sendKeyPress(tester, LogicalKeyboardKey.enter);
      await tester.pump(const Duration(milliseconds: 120));

      expect(imService.sendCalls, 0);
      expect(controller.inputController.text, 'hello\n');
      expect(
        controller.inputController.selection,
        const TextSelection.collapsed(offset: 6),
      );

      controller.inputController.clear();
      await tester.pump();
      Get.find<ImService>().updateSessionComposing(
        controller.sessionId,
        active: false,
      );
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('ChatView inserts line break on Shift+Enter', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_shift_enter';
    controller.chatTitle = 'session_test_shift_enter';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final inputFinder = find.byType(TextField).first;
    await tester.tap(inputFinder);
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);

    controller.inputController.value = const TextEditingValue(
      text: 'line',
      selection: TextSelection.collapsed(offset: 4),
    );
    await tester.pump();

    await sendModifiedKeyPress(
      tester,
      triggerKey: LogicalKeyboardKey.enter,
      modifiers: const <LogicalKeyboardKey>[LogicalKeyboardKey.shiftLeft],
    );
    await tester.pump();

    expect(controller.inputController.text, 'line\n');
    expect(
      controller.inputController.selection,
      const TextSelection.collapsed(offset: 5),
    );
    expect(imService.sendCalls, 0);

    controller.inputController.clear();
    await tester.pump();
  });

  testWidgets('ChatView sends message on Ctrl+Enter', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_ctrl_enter';
    controller.chatTitle = 'session_test_ctrl_enter';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final inputFinder = find.byType(TextField).first;
    await tester.tap(inputFinder);
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);

    controller.inputController.value = const TextEditingValue(
      text: 'line',
      selection: TextSelection.collapsed(offset: 4),
    );
    await tester.pump();

    await sendModifiedKeyPress(
      tester,
      triggerKey: LogicalKeyboardKey.enter,
      modifiers: const <LogicalKeyboardKey>[LogicalKeyboardKey.controlLeft],
    );
    await tester.pump();

    await tester.pump(const Duration(milliseconds: 120));

    expect(imService.sendCalls, 1);
    expect(imService.sentContent, 'line');
    expect(imService.sentSessionId, 'session_test_ctrl_enter');
    expect(controller.inputController.text, isEmpty);
    await tester.pump(const Duration(milliseconds: 300));
  });

  testWidgets('ChatView applies chat font scale to system and input text', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_system_font_scale',
        sessionId: 'session_font_scale',
        senderId: 'system',
        content: 'system_notice_font_scale',
        createdAt: 1,
        msgType: 3,
      ),
    ]);

    final fontSizeService = _FakeChatFontSizeService(1.12);
    Get.put<ChatFontSizeService>(fontSizeService);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_font_scale';
    controller.chatTitle = 'session_font_scale';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final systemTextBefore = tester.widget<Text>(
      find.text('system_notice_font_scale'),
    );
    expect(systemTextBefore.style?.fontSize, closeTo(13.44, 0.001));

    final inputBefore = tester.widget<EditableText>(find.byType(EditableText));
    expect(inputBefore.style.fontSize, closeTo(15.68, 0.001));

    fontSizeService.setScale(0.9);
    await tester.pump();

    final systemTextAfter = tester.widget<Text>(
      find.text('system_notice_font_scale'),
    );
    expect(systemTextAfter.style?.fontSize, closeTo(10.8, 0.001));

    final inputAfter = tester.widget<EditableText>(find.byType(EditableText));
    expect(inputAfter.style.fontSize, closeTo(12.6, 0.001));
  });

  testWidgets('ChatView reacts to real ChatFontSizeService level changes', (
    WidgetTester tester,
  ) async {
    SharedPreferences.setMockInitialValues({
      ChatFontSizeService.prefsKey: 'small',
    });

    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg_real_font_scale',
        sessionId: 'session_real_font_scale',
        senderId: 'peer',
        content: 'peer_font_scale_message',
        createdAt: 1,
      ),
    ]);

    final fontSizeService = await ChatFontSizeService().init();
    Get.put<ChatFontSizeService>(fontSizeService);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_real_font_scale';
    controller.chatTitle = 'session_real_font_scale';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pumpAndSettle();

    final bubbleFinder = find.descendant(
      of: find.byType(MessageBubble),
      matching: find.text('peer_font_scale_message'),
    );
    final bubbleBefore = tester.widget<Text>(bubbleFinder);
    expect(fontSizeService.levelRx.value, ChatFontSizeLevel.small);
    expect(bubbleBefore.style?.fontSize, closeTo(12.6, 0.001));

    final inputFinder = find.descendant(
      of: find.byType(TextField),
      matching: find.byType(EditableText),
    );
    final inputBefore = tester.widget<EditableText>(inputFinder);
    expect(inputBefore.style.fontSize, closeTo(12.6, 0.001));

    await fontSizeService.setLevel(ChatFontSizeLevel.large);
    await tester.pump();

    final bubbleAfter = tester.widget<Text>(bubbleFinder);
    expect(fontSizeService.levelRx.value, ChatFontSizeLevel.large);
    expect(bubbleAfter.style?.fontSize, closeTo(15.68, 0.001));

    final inputAfter = tester.widget<EditableText>(inputFinder);
    expect(inputAfter.style.fontSize, closeTo(15.68, 0.001));
  });

  testWidgets(
    'ChatView shrinks message and input text when level changes to small',
    (WidgetTester tester) async {
      SharedPreferences.setMockInitialValues({});

      final imService = Get.find<ImService>();
      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'msg_font_scale_small',
          sessionId: 'session_font_scale_small',
          senderId: 'peer',
          content: 'peer_font_scale_small_message',
          createdAt: 1,
        ),
      ]);

      final fontSizeService = await ChatFontSizeService().init();
      Get.put<ChatFontSizeService>(fontSizeService);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_font_scale_small';
      controller.chatTitle = 'session_font_scale_small';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      final bubbleFinder = find.descendant(
        of: find.byType(MessageBubble),
        matching: find.text('peer_font_scale_small_message'),
      );
      final bubbleBefore = tester.widget<Text>(bubbleFinder);
      expect(fontSizeService.levelRx.value, ChatFontSizeLevel.medium);
      expect(bubbleBefore.style?.fontSize, closeTo(14.0, 0.001));

      final inputFinder = find.descendant(
        of: find.byType(TextField),
        matching: find.byType(EditableText),
      );
      final inputBefore = tester.widget<EditableText>(inputFinder);
      expect(inputBefore.style.fontSize, closeTo(14.0, 0.001));

      await fontSizeService.setLevel(ChatFontSizeLevel.small);
      await tester.pump();

      final bubbleAfter = tester.widget<Text>(bubbleFinder);
      expect(fontSizeService.levelRx.value, ChatFontSizeLevel.small);
      expect(bubbleAfter.style?.fontSize, closeTo(12.6, 0.001));

      final inputAfter = tester.widget<EditableText>(inputFinder);
      expect(inputAfter.style.fontSize, closeTo(12.6, 0.001));
    },
  );

  testWidgets('ChatView proxies mouse wheel scrolling on iOS message text', (
    WidgetTester tester,
  ) async {
    await expectMouseWheelScrollsMessageText(
      tester,
      platform: TargetPlatform.iOS,
      messagePrefix: 'message_scroll_ios',
    );
  });

  testWidgets('ChatView keeps mouse wheel scrolling on macOS message text', (
    WidgetTester tester,
  ) async {
    await expectMouseWheelScrollsMessageText(
      tester,
      platform: TargetPlatform.macOS,
      messagePrefix: 'message_scroll_macos',
    );
  });

  testWidgets(
    'ChatView keeps consecutive macOS mouse wheel scrolling to bottom',
    (WidgetTester tester) async {
      debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
      try {
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: 'session_test_scroll_macos_consecutive',
          locale: const Locale('zh', 'CN'),
          messages: List.generate(
            200,
            (index) => MessageModel(
              msgId: 'msg_macos_consecutive_$index',
              sessionId: 'session_test_scroll_macos_consecutive',
              senderId: index.isEven ? 'peer' : '1001',
              content: '**message_scroll_macos_consecutive_$index**',
              createdAt: index,
            ),
          ),
        );
        controller.scrollController.jumpTo(0);
        await tester.pump();

        final firstMessageFinder = find.byKey(
          const ValueKey('m:msg_macos_consecutive_0'),
        );
        expect(firstMessageFinder, findsOneWidget);

        final pointer = TestPointer(1, PointerDeviceKind.mouse);
        final position = tester.getCenter(firstMessageFinder);
        await tester.sendEventToBinding(pointer.hover(position));

        var previousOffset = controller.scrollController.offset;
        for (var i = 0; i < 24; i++) {
          await tester.sendEventToBinding(
            pointer.scroll(const Offset(0.0, 900.0)),
          );
          await tester.pump();
          expect(
            controller.scrollController.offset,
            greaterThanOrEqualTo(previousOffset),
          );
          previousOffset = controller.scrollController.offset;
          if (controller.scrollController.position.maxScrollExtent -
                  controller.scrollController.offset <=
              1) {
            break;
          }
        }

        final positionAfterScroll = controller.scrollController.position;
        expect(
          positionAfterScroll.maxScrollExtent - positionAfterScroll.pixels,
          lessThanOrEqualTo(1),
        );
      } finally {
        debugDefaultTargetPlatformOverride = null;
      }
    },
  );

  testWidgets(
    'ChatView keeps auto-follow after mouse wheel scroll leaves bottom',
    (WidgetTester tester) async {
      final imService = Get.find<ImService>();
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: 'session_test_pointer_scroll_pause',
        locale: const Locale('zh', 'CN'),
        messages: List.generate(
          80,
          (index) => MessageModel(
            msgId: 'msg_pointer_pause_$index',
            sessionId: 'session_test_pointer_scroll_pause',
            senderId: index.isEven ? 'peer' : '1001',
            content: 'pointer_pause_$index',
            createdAt: index,
          ),
        ),
      );

      controller.scrollToBottom();
      await tester.pump();
      await tester.pump();

      final beforeScrollOffset = controller.scrollController.offset;
      final latestMessageFinder = find.byKey(
        const ValueKey('m:msg_pointer_pause_79'),
      );
      final pointer = TestPointer(1, PointerDeviceKind.mouse);
      final position = tester.getCenter(latestMessageFinder);
      await tester.sendEventToBinding(pointer.hover(position));
      await tester.sendEventToBinding(
        pointer.scroll(const Offset(0.0, -240.0)),
      );
      await tester.pump();

      final scrolledOffset = controller.scrollController.offset;
      expect(scrolledOffset, lessThan(beforeScrollOffset - 10));

      imService.currentMessages.add(
        MessageModel(
          msgId: 'msg_pointer_pause_new',
          sessionId: 'session_test_pointer_scroll_pause',
          senderId: 'peer',
          content: 'pointer_pause_new',
          createdAt: 1000,
        ),
      );
      await tester.pump(const Duration(milliseconds: 120));
      await tester.pump();

      // 用户向上滚动了 240px（超过 _bottomSnapThreshold=120），
      // _autoFollowBottom 已被设为 false，新消息不应触发 scrollToBottom。
      expect(controller.scrollController.offset, scrolledOffset);

      await tester.pump(const Duration(milliseconds: 120));
    },
  );

  testWidgets(
    'ChatView keeps the latest message above keyboard after wheel interaction settles',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final messages = List.generate(
        40,
        (index) => MessageModel(
          msgId: 'msg_keyboard_after_wheel_$index',
          sessionId: 's_keyboard_after_wheel',
          senderId: index.isEven ? 'peer' : '1001',
          content: index == 39
              ? 'latest message after wheel'
              : 'message $index',
          createdAt: index,
        ),
      );

      final controller = Get.put(ChatController());
      controller.sessionId = 's_keyboard_after_wheel';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      Get.find<ImService>().currentMessages.assignAll(messages);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      final inputFinder = find.byType(TextField).first;
      final latestMessageFinder = find.byKey(
        const ValueKey('m:msg_keyboard_after_wheel_39'),
      );
      final inputAreaFinder = find.byKey(
        const Key('chat_input_area_container'),
      );
      expect(latestMessageFinder, findsOneWidget);

      controller.scrollToBottom(force: true);
      await tester.pump();
      await tester.pump();

      // 模拟真实场景：wheel 交互在键盘弹出之前完成
      controller.onWheelScrollActive(controller.scrollController.position);
      await tester.pump();
      controller.onWheelScrollEnd(controller.scrollController.position);
      await tester.pump();

      await tester.tap(inputFinder);
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      tester.view.viewInsets = const FakeViewPadding(bottom: 300);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 160));
      await tester.pump(const Duration(milliseconds: 260));
      await tester.pump();

      final latestMessageRect = tester.getRect(latestMessageFinder);
      final inputAreaRect = tester.getRect(inputAreaFinder);
      expect(
        latestMessageRect.bottom,
        lessThanOrEqualTo(inputAreaRect.top + 1),
      );
      expect(
        (controller.scrollController.position.maxScrollExtent -
                controller.scrollController.position.pixels)
            .abs(),
        lessThanOrEqualTo(1),
      );

      tester.view.viewInsets = FakeViewPadding.zero;
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));
    },
  );

  testWidgets(
    'ChatView caps group member sheet height and shows invite action for eligible normal member',
    (WidgetTester tester) async {
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() {
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });

      final sessionService = Get.find<SessionService>() as _FakeSessionService;
      sessionService.detailResult = SessionDetailResult(
        data: {
          'session_type': 2,
          'allow_member_invite': true,
          'member_invite_threshold': 50,
          'member_count': 40,
          'members': [
            {'member_id': '1001', 'member_type': 1, 'role': 1},
            ...List.generate(
              39,
              (index) => {
                'member_id': '${2000 + index}',
                'member_type': 1,
                'role': 1,
              },
            ),
          ],
        },
      );

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_member_sheet';
      controller.chatTitle = 'group';
      controller.chatType = 'group';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: ChatView(),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.more_vert_rounded));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithIcon(ListTile, Icons.group_rounded));
      await tester.pumpAndSettle();

      expect(find.text('添加成员'), findsOneWidget);

      final bottomSheetRect = tester.getRect(find.byType(BottomSheet));
      expect(bottomSheetRect.height, lessThan(600));
    },
  );
}
