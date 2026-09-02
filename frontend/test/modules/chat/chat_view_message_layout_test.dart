import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:grix/shared/widgets/session_avatar.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../shared/widgets/markdown_link_finder.dart';

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
  int sessionType = 1;
  List<Map<String, dynamic>> members = const [];

  Map<String, dynamic> get _detail => {
    'session_type': sessionType,
    'member_count': members.length,
    'members': members,
  };

  @override
  Future<Map<String, dynamic>?> fetchSessionDetail(String sessionId) async {
    return _detail;
  }

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return SessionDetailResult(data: _detail);
  }
}

class _FakeOssService extends OssService {}

/// webhook 推送的邮件目录链接形态（邮箱地址里的 `@` 是百分号编码）。
const _mailDirFileUri =
    'file:///workspace/mail/dmarc%40example.com/2026-09-02/0759-aed5ae0b92';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<void> pumpChatView(
    WidgetTester tester, {
    required List<MessageModel> messages,
    String chatType = 'private',
    int peerType = 1,
    String toolbarAgentId = '',
  }) async {
    final imService = Get.find<ImService>();
    // 私聊对端昵称来源于会话（peerDisplayName 走 _resolvePrivatePeerNameFromSession），
    // 故为私聊注入一条 private 会话以驱动气泡旁的发送者名。
    if (chatType == 'private') {
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_layout_test',
          type: 'private',
          peerId: peerType == 2 ? 'agent-1' : 'peer',
          peerType: peerType,
          peerNickname: 'Peer User',
          updatedAt: 0,
          lastMessageTime: 0,
        ),
      ]);
    }
    // 群工具栏选中的 agent 只能由「群里有这个 agent 成员 + 我有权使用它 + 唯一
    // 固定艾特」共同推出，这里按同样的链路把它布置好。
    if (toolbarAgentId.isNotEmpty) {
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_layout_test',
          title: 'Group',
          type: 'group',
          updatedAt: 0,
          lastMessageTime: 0,
        ),
      ]);
      Get.find<AgentService>().agents.assignAll([
        AgentModel.fromJson({
          'id': toolbarAgentId,
          'agent_name': 'Toolbar Agent',
          'owner_id': '1001',
          'status': 1,
          'provider_type': 3,
        }),
      ]);
      final sessionService = Get.find<SessionService>() as _FakeSessionService;
      sessionService.sessionType = 2;
      sessionService.members = [
        {'member_id': '1001', 'member_type': 1, 'role': 3},
        {
          'member_id': toolbarAgentId,
          'member_type': 2,
          'role': 1,
          'nickname': 'Toolbar Agent',
        },
      ];
    }
    imService.currentMessages.assignAll(messages);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_layout_test';
    controller.chatTitle = 'Peer User';
    controller.chatType = chatType;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: ChatView(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    if (toolbarAgentId.isNotEmpty) {
      await controller.refreshSessionDetail();
      controller.togglePinnedMention({
        'member_id': toolbarAgentId,
        'member_type': 2,
        'nickname': 'Toolbar Agent',
      });
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
      expect(
        controller.groupToolbarTargetAgentId,
        toolbarAgentId,
        reason: '用例前置条件：群工具栏需选中该 agent',
      );
    }
  }

  Finder bubbleContainer(String bubbleKey) {
    return find.descendant(
      of: find.byKey(ValueKey(bubbleKey)),
      matching: find.byWidgetPredicate((widget) {
        if (widget is! Container) {
          return false;
        }
        final decoration = widget.decoration;
        return decoration is BoxDecoration && decoration.border != null;
      }),
    );
  }

  Container bubbleWidget(WidgetTester tester, String bubbleKey) {
    return tester.widget<Container>(bubbleContainer(bubbleKey));
  }

  BorderRadius bubbleRadius(WidgetTester tester, String bubbleKey) {
    final bubbleBox = bubbleWidget(tester, bubbleKey);
    final decoration = bubbleBox.decoration! as BoxDecoration;
    return decoration.borderRadius! as BorderRadius;
  }

  Finder avatarFinder(String itemKey) {
    return find.descendant(
      of: find.byKey(ValueKey(itemKey)),
      matching: find.byType(SessionAvatar),
    );
  }

  Finder senderMetaPaddingFinder(String itemKey) {
    return find.descendant(
      of: find.byKey(ValueKey(itemKey)),
      matching: find.byWidgetPredicate((widget) {
        if (widget is! Padding) {
          return false;
        }
        final padding = widget.padding;
        return padding is EdgeInsets &&
            padding.top == 4 &&
            padding.bottom == 1 &&
            (padding.left == 4 || padding.right == 4);
      }),
    );
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
    MessageBubble.resetFinalRenderCacheForTest();
    SharedPreferences.setMockInitialValues({});
    Get.put<ImService>(_FakeImService());
    Get.put<AuthService>(_FakeAuthService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<SessionService>(_FakeSessionService());
    Get.put<OssService>(_FakeOssService());
  });

  tearDown(() {
    MessageBubble.resetFinalRenderCacheForTest();
    Get.reset();
  });

  testWidgets(
    'ChatView spaces avatars away from content and keeps sender names aligned',
    (WidgetTester tester) async {
      await pumpChatView(
        tester,
        messages: [
          MessageModel(
            msgId: 'peer-msg',
            sessionId: 'session_layout_test',
            senderId: 'peer',
            senderType: 1,
            content: 'Hello from peer',
            createdAt: 1710000000000,
          ),
          MessageModel(
            msgId: 'my-msg',
            sessionId: 'session_layout_test',
            senderId: '1001',
            senderType: 1,
            content: 'Hello from me',
            createdAt: 1710000060000,
          ),
        ],
      );

      final listView = tester.widget<ListView>(find.byType(ListView));
      final listPadding = listView.padding! as EdgeInsets;
      expect(listPadding.left, 4);
      expect(listPadding.right, 4);

      final peerName = find.descendant(
        of: find.byKey(const ValueKey('m:peer-msg')),
        matching: find.text('Peer User'),
      );
      final myName = find.descendant(
        of: find.byKey(const ValueKey('m:my-msg')),
        matching: find.text('Me'),
      );

      final peerNameLeft = tester.getTopLeft(peerName).dx;
      final peerBubbleLeft = tester
          .getTopLeft(bubbleContainer('m:peer-msg_bubble'))
          .dx;
      final peerBubbleMargin =
          bubbleWidget(tester, 'm:peer-msg_bubble').margin! as EdgeInsets;
      final peerBubbleVisibleLeft = peerBubbleLeft + peerBubbleMargin.left;
      final peerAvatarRight = tester.getTopRight(avatarFinder('m:peer-msg')).dx;
      expect((peerNameLeft - peerBubbleVisibleLeft).abs(), lessThan(0.1));
      expect(peerBubbleVisibleLeft - peerAvatarRight, closeTo(10, 0.1));

      final myNameRight = tester.getTopRight(myName).dx;
      final myBubbleRight = tester
          .getTopRight(bubbleContainer('m:my-msg_bubble'))
          .dx;
      final myBubbleMargin =
          bubbleWidget(tester, 'm:my-msg_bubble').margin! as EdgeInsets;
      final myBubbleVisibleRight = myBubbleRight - myBubbleMargin.right;
      final myAvatarLeft = tester.getTopLeft(avatarFinder('m:my-msg')).dx;
      expect((myNameRight - myBubbleVisibleRight).abs(), lessThan(0.1));
      expect(myAvatarLeft - myBubbleVisibleRight, closeTo(10, 0.1));

      expect(peerBubbleMargin.top, 1);
      expect(myBubbleMargin.top, 1);
      expect(senderMetaPaddingFinder('m:peer-msg'), findsOneWidget);
      expect(senderMetaPaddingFinder('m:my-msg'), findsOneWidget);
    },
  );

  testWidgets('ChatView uses a square corner next to the sender name', (
    WidgetTester tester,
  ) async {
    await pumpChatView(
      tester,
      messages: [
        MessageModel(
          msgId: 'peer-msg',
          sessionId: 'session_layout_test',
          senderId: 'peer',
          senderType: 1,
          content: 'Hello from peer',
          createdAt: 1710000000000,
        ),
        MessageModel(
          msgId: 'my-msg',
          sessionId: 'session_layout_test',
          senderId: '1001',
          senderType: 1,
          content: 'Hello from me',
          createdAt: 1710000060000,
        ),
      ],
    );

    final peerRadius = bubbleRadius(tester, 'm:peer-msg_bubble');
    expect(peerRadius.topLeft, Radius.zero);
    expect(peerRadius.topRight, const Radius.circular(12));
    expect(peerRadius.bottomLeft, const Radius.circular(12));
    expect(peerRadius.bottomRight, const Radius.circular(12));

    final myRadius = bubbleRadius(tester, 'm:my-msg_bubble');
    expect(myRadius.topLeft, const Radius.circular(12));
    expect(myRadius.topRight, Radius.zero);
    expect(myRadius.bottomLeft, const Radius.circular(12));
    expect(myRadius.bottomRight, const Radius.circular(12));
  });

  testWidgets('ChatView leaves agent paths unwired when no agent host exists', (
    WidgetTester tester,
  ) async {
    await pumpChatView(
      tester,
      messages: [
        MessageModel(
          msgId: 'agent-path-msg',
          sessionId: 'session_layout_test',
          senderId: 'agent-1',
          senderType: 2,
          content: '[Agent README](/workspace/README.md)',
          createdAt: 1710000000000,
        ),
        MessageModel(
          msgId: 'user-path-msg',
          sessionId: 'session_layout_test',
          senderId: 'peer',
          senderType: 1,
          content: '[Mail dir]($_mailDirFileUri)',
          createdAt: 1710000060000,
        ),
      ],
    );

    final agentBubble = tester.widget<MessageBubble>(
      find.byKey(const ValueKey('m:agent-path-msg_bubble')),
    );
    final userBubble = tester.widget<MessageBubble>(
      find.byKey(const ValueKey('m:user-path-msg_bubble')),
    );

    expect(agentBubble.onAgentFilePathTap, isNotNull);
    // 私聊对端不是 agent，没有可用的文件宿主：回调保持 null，链接降级成纯文本。
    expect(userBubble.onAgentFilePathTap, isNull);
    final linkTexts = tappableLinkTexts(tester);
    expect(linkTexts, contains('Agent README'));
    expect(linkTexts, isNot(contains('Mail dir')));
  });

  testWidgets(
    'ChatView wires agent paths for non-agent messages in an agent private chat',
    (WidgetTester tester) async {
      await pumpChatView(
        tester,
        peerType: 2,
        messages: [
          // webhook 推送的邮件通知以「我本人」身份落库（senderType == 1）。
          MessageModel(
            msgId: 'webhook-path-msg',
            sessionId: 'session_layout_test',
            senderId: '1001',
            senderType: 1,
            content: '[Mail dir]($_mailDirFileUri)',
            createdAt: 1710000000000,
          ),
        ],
      );

      final bubble = tester.widget<MessageBubble>(
        find.byKey(const ValueKey('m:webhook-path-msg_bubble')),
      );

      expect(bubble.onAgentFilePathTap, isNotNull);
      expect(tappableLinkTexts(tester), contains('Mail dir'));
    },
  );

  testWidgets('ChatView leaves agent paths unwired in group chats without a '
      'toolbar agent', (WidgetTester tester) async {
    await pumpChatView(
      tester,
      chatType: 'group',
      messages: [
        MessageModel(
          msgId: 'group-path-msg',
          sessionId: 'session_layout_test',
          senderId: '1001',
          senderType: 1,
          content: '[Mail dir]($_mailDirFileUri)',
          createdAt: 1710000000000,
        ),
      ],
    );

    final bubble = tester.widget<MessageBubble>(
      find.byKey(const ValueKey('m:group-path-msg_bubble')),
    );

    expect(bubble.onAgentFilePathTap, isNull);
    expect(tappableLinkTexts(tester), isNot(contains('Mail dir')));
  });

  testWidgets('ChatView keeps group file links unwired for other members', (
    WidgetTester tester,
  ) async {
    await pumpChatView(
      tester,
      chatType: 'group',
      toolbarAgentId: 'agent-1',
      messages: [
        MessageModel(
          msgId: 'group-agent-path-msg',
          sessionId: 'session_layout_test',
          senderId: 'agent-1',
          senderType: 2,
          content: '[Agent dir]($_mailDirFileUri)',
          createdAt: 1710000000000,
        ),
        MessageModel(
          msgId: 'group-mine-path-msg',
          sessionId: 'session_layout_test',
          senderId: '1001',
          senderType: 1,
          content: '[My dir]($_mailDirFileUri)',
          createdAt: 1710000060000,
        ),
        // 群里另一个真人成员：这条路径指向他那台机器，用我的工具栏 agent 去解析
        // 是语义错误，必须降级成纯文本。
        MessageModel(
          msgId: 'group-other-path-msg',
          sessionId: 'session_layout_test',
          senderId: '2002',
          senderType: 1,
          content: '[Other dir]($_mailDirFileUri)',
          createdAt: 1710000120000,
        ),
      ],
    );

    MessageBubble bubbleOf(String msgId) {
      return tester.widget<MessageBubble>(
        find.byKey(ValueKey('m:${msgId}_bubble')),
      );
    }

    expect(bubbleOf('group-agent-path-msg').onAgentFilePathTap, isNotNull);
    expect(bubbleOf('group-mine-path-msg').onAgentFilePathTap, isNotNull);
    expect(bubbleOf('group-other-path-msg').onAgentFilePathTap, isNull);

    final linkTexts = tappableLinkTexts(tester);
    expect(linkTexts, contains('Agent dir'));
    expect(linkTexts, contains('My dir'));
    expect(linkTexts, isNot(contains('Other dir')));
  });

  testWidgets(
    'ChatView keeps separate avatars for consecutive incoming owners',
    (WidgetTester tester) async {
      await pumpChatView(
        tester,
        messages: [
          MessageModel(
            msgId: 'agent-stream-msg',
            sessionId: 'session_layout_test',
            senderId: 'agent-1',
            senderType: 2,
            msgType: 4,
            content: 'streaming response',
            createdAt: 1710000000000,
          ),
          MessageModel(
            msgId: 'peer-after-agent-msg',
            sessionId: 'session_layout_test',
            senderId: 'peer',
            senderType: 1,
            content: 'message after agent',
            createdAt: 1710000060000,
          ),
        ],
      );

      expect(avatarFinder('m:agent-stream-msg'), findsOneWidget);
      expect(avatarFinder('m:peer-after-agent-msg'), findsOneWidget);
    },
  );

  testWidgets(
    'ChatView keeps timestamps visible on consecutive agent bubbles',
    (WidgetTester tester) async {
      await pumpChatView(
        tester,
        messages: [
          MessageModel(
            msgId: 'agent-msg-1',
            sessionId: 'session_layout_test',
            senderId: 'agent-1',
            senderType: 2,
            content: 'first agent bubble',
            createdAt: 1710000000000,
          ),
          MessageModel(
            msgId: 'agent-msg-2',
            sessionId: 'session_layout_test',
            senderId: 'agent-1',
            senderType: 2,
            content: 'second agent bubble',
            createdAt: 1710000060000,
          ),
        ],
      );

      expect(senderMetaPaddingFinder('m:agent-msg-1'), findsOneWidget);
      expect(senderMetaPaddingFinder('m:agent-msg-2'), findsOneWidget);
      expect(avatarFinder('m:agent-msg-1'), findsOneWidget);
      expect(avatarFinder('m:agent-msg-2'), findsNothing);

      // 连续 agent 气泡：首条显示昵称，后续只留时间。
      final firstAgentName = find.descendant(
        of: find.byKey(const ValueKey('m:agent-msg-1')),
        matching: find.text('Agent agent-1'),
      );
      final secondAgentName = find.descendant(
        of: find.byKey(const ValueKey('m:agent-msg-2')),
        matching: find.text('Agent agent-1'),
      );
      expect(firstAgentName, findsOneWidget);
      expect(secondAgentName, findsNothing);
    },
  );

  testWidgets(
    'ChatView ignores hidden directive messages when grouping avatars',
    (WidgetTester tester) async {
      await pumpChatView(
        tester,
        messages: [
          MessageModel(
            msgId: 'my-visible-msg',
            sessionId: 'session_layout_test',
            senderId: '1001',
            senderType: 1,
            content: 'my visible message',
            createdAt: 1710000000000,
          ),
          MessageModel(
            msgId: 'hidden-directive-msg',
            sessionId: 'session_layout_test',
            senderId: 'peer',
            senderType: 1,
            content: '[[exec-approval-resolution|noop]]',
            createdAt: 1710000030000,
          ),
          MessageModel(
            msgId: 'peer-visible-msg',
            sessionId: 'session_layout_test',
            senderId: 'peer',
            senderType: 1,
            content: 'peer visible message',
            createdAt: 1710000060000,
          ),
        ],
      );

      expect(find.text('[[exec-approval-resolution|noop]]'), findsNothing);
      expect(avatarFinder('m:peer-visible-msg'), findsOneWidget);
    },
  );

  testWidgets(
    'ChatView keeps quoted preview when avatar is hidden for consecutive sender',
    (WidgetTester tester) async {
      const quotedText = 'quoted source for grouped sender';
      await pumpChatView(
        tester,
        messages: [
          MessageModel(
            msgId: 'source-msg',
            sessionId: 'session_layout_test',
            senderId: '1001',
            senderType: 1,
            content: quotedText,
            createdAt: 1710000000000,
          ),
          MessageModel(
            msgId: 'reply-msg-1',
            sessionId: 'session_layout_test',
            senderId: 'peer',
            senderType: 1,
            content: 'first reply',
            quotedMessageId: 'source-msg',
            createdAt: 1710000060000,
          ),
          MessageModel(
            msgId: 'reply-msg-2',
            sessionId: 'session_layout_test',
            senderId: 'peer',
            senderType: 1,
            content: 'second reply',
            quotedMessageId: 'source-msg',
            createdAt: 1710000120000,
          ),
        ],
      );

      expect(avatarFinder('m:reply-msg-1'), findsOneWidget);
      expect(avatarFinder('m:reply-msg-2'), findsNothing);

      // 头像隐藏（连续同发送者）不应影响引用预览：两条回复都引用同一源
      // 消息，引用预览都要正常显示（修复私聊引用自己消息预览消失的问题）。
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('m:reply-msg-1_bubble')),
          matching: find.text(quotedText),
        ),
        findsOneWidget,
      );
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('m:reply-msg-2_bubble')),
          matching: find.text(quotedText),
        ),
        findsOneWidget,
      );
    },
  );

  testWidgets('ChatView shows lock for received restricted messages', (
    WidgetTester tester,
  ) async {
    final sessionService = Get.find<SessionService>() as _FakeSessionService;
    sessionService.sessionType = 2;
    await pumpChatView(
      tester,
      messages: [
        MessageModel(
          msgId: 'peer-hidden-msg',
          sessionId: 'session_layout_test',
          senderId: 'peer',
          senderType: 1,
          content: 'restricted reply',
          visibleTo: const ['1001'],
          createdAt: 1710000180000,
        ),
      ],
      chatType: 'group',
    );

    final messageItem = find.byKey(const ValueKey('m:peer-hidden-msg'));
    final lockIcon = find.descendant(
      of: messageItem,
      matching: find.byIcon(Icons.lock_outline),
    );

    expect(lockIcon, findsOneWidget);

    await tester.tap(lockIcon);
    await tester.pumpAndSettle();

    expect(find.text('Visible only to: peer、1001'), findsOneWidget);
    sessionService.sessionType = 1;
  });
}
