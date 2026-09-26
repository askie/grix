import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:grix/modules/chat/message_cards/models/chat_tool_execution_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/modules/chat/message_cards/widgets/chat_tool_execution_group_card_view.dart';
import 'package:grix/modules/chat/models/chat_message_identity.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

/// 离线语义：本地库已全量时，首屏填窗不依赖任何远端历史回包。
class _FakeSessionService extends SessionService {
  int historyCalls = 0;

  @override
  bool get isInitialized => true;

  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    historyCalls++;
    return const SessionMessageHistoryResult(
      code: 0,
      messages: [],
      hasMore: false,
    );
  }

  @override
  Future<Map<String, dynamic>?> fetchSessionDetail(String sessionId) async {
    return const {'session_type': 1, 'member_count': 2, 'members': []};
  }

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return const SessionDetailResult(
      data: {'session_type': 1, 'member_count': 2, 'members': []},
    );
  }
}

class _FakeOssService extends OssService {}

const _sid = 'first-screen-tool-group-session';
const _senderId = 'agent-1';

Map<String, dynamic> _textRow(int seq) {
  return {
    'msg_id': 'fs-text-$seq',
    'session_id': _sid,
    'sender_id': _senderId,
    'sender_type': 2,
    'msg_type': 1,
    'content': 'first_screen_text_$seq',
    'created_at': 1735689600000 + seq,
    'status': 'sent',
    'state_version': '1',
  };
}

Map<String, dynamic> _toolRow(int seq) {
  final envelope = ChatMessageCardCodec.encode(
    ChatToolExecutionCardData(summaryText: 'Bash: fs_step_$seq'),
  );
  return {
    'msg_id': 'fs-tool-$seq',
    'session_id': _sid,
    'sender_id': _senderId,
    'sender_type': 2,
    'msg_type': 1,
    'content': envelope.content,
    'extra': envelope.extra,
    'created_at': 1735689600000 + seq,
    'status': 'sent',
    'state_version': '1',
  };
}

/// Internal directives: ChatView hides these as zero-height rows via
/// `isInternalDirectiveMessage` (approval slash commands and open-session
/// directives), so they must not count as visible bubbles.
Map<String, dynamic> _directiveRow(int seq) {
  final content = seq.isOdd
      ? '/approve req-$seq'
      : 'grix://open/session?cwd=/tmp/ws_$seq';
  return {
    'msg_id': 'fs-directive-$seq',
    'session_id': _sid,
    'sender_id': '1001',
    'sender_type': 1,
    'msg_type': 1,
    'content': content,
    'created_at': 1735689600000 + seq,
    'status': 'sent',
    'state_version': '1',
  };
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late ImService imService;
  late String userId;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    MessageBubble.resetFinalRenderCacheForTest();
    SharedPreferences.setMockInitialValues({});
    userId = 'fs-tool-${DateTime.now().microsecondsSinceEpoch}';
    Get.put<AuthService>(_FakeAuthService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<SessionService>(_FakeSessionService());
    Get.put<OssService>(_FakeOssService());
    await LocalDb.setActiveUser(userId);
    imService = ImService();
    Get.put<ImService>(imService);
  });

  tearDown(() async {
    imService.onClose();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  /// 交替推进真实异步（sqlite/计时器）与帧（布局/填窗循环的 postFrame 等待）。
  Future<void> pumpUntil(
    WidgetTester tester,
    bool Function() cond, {
    int maxRounds = 400,
    String? description,
  }) async {
    for (var round = 0; round < maxRounds && !cond(); round++) {
      await Future<void>.delayed(const Duration(milliseconds: 10));
      await tester.pump();
    }
    if (!cond()) {
      fail(
        'Timed out waiting for: ${description ?? 'condition'} '
        '(window=${imService.currentMessages.length}, '
        'autoFillPages=${Get.find<ChatController>().initialAutoFillPagesForTest})',
      );
    }
  }

  /// Returns true when at least one `fs-text-*` bubble has a mounted render
  /// box intersecting the message list viewport (i.e. really on screen).
  bool textBubbleVisibleOnScreen(ChatController controller) {
    final listBox =
        find
                .byType(ListView)
                .evaluate()
                .firstOrNull
                ?.findRenderObject()
            as RenderBox?;
    if (listBox == null) return false;
    final listTop = listBox.localToGlobal(Offset.zero).dy;
    final listBottom = listTop + listBox.size.height;
    for (final msg in imService.currentMessages) {
      if (!msg.msgId.startsWith('fs-text-')) continue;
      final key = controller.peekMessageViewportItemGlobalKey(
        ChatMessageIdentity.selectionKey(msg),
      );
      final renderBox = key?.currentContext?.findRenderObject() as RenderBox?;
      if (renderBox == null || !renderBox.attached) continue;
      final top = renderBox.localToGlobal(Offset.zero).dy;
      final bottom = top + renderBox.size.height;
      if (renderBox.size.height > 1 && bottom > listTop && top < listBottom) {
        return true;
      }
    }
    return false;
  }

  testWidgets(
    '超长连续工具卡组场景：进页无手势即看到正文，最新消息贴底，展开计数准确，上翻仍可分页',
    (WidgetTester tester) async {
      // 60 body texts followed by 300 consecutive same-sender tool cards.
      // 300 > 30 initial + 4 x 40 auto-fill budget = 190, so the pre-fix
      // auto-fill stops inside the tool run with an empty first screen.
      await tester.runAsync(() async {
        await LocalDb.batchInsertMessages([
          for (var seq = 1; seq <= 60; seq++) _textRow(seq),
          for (var seq = 61; seq <= 360; seq++) _toolRow(seq),
        ]);

        imService.sessions.assignAll([
          SessionModel(
            sessionId: _sid,
            type: 'private',
            peerId: _senderId,
            peerType: 2,
            peerNickname: 'Tool Agent',
            updatedAt: 0,
            lastMessageTime: 0,
          ),
        ]);

        final controller = Get.put(ChatController());
        controller.sessionId = _sid;
        controller.chatTitle = 'Tool Agent';
        controller.chatType = 'private';

        await tester.pumpWidget(
          GetMaterialApp(
            translations: AppTranslations(),
            locale: const Locale('en', 'US'),
            home: ChatView(),
          ),
        );
        await tester.pump();

        // 首屏填窗完成：视口变为可滚动（正文已进入窗口）。
        await pumpUntil(
          tester,
          () =>
              controller.scrollController.hasClients &&
              controller.scrollController.position.maxScrollExtent > 1.0,
          description: 'first screen filled without any user gesture',
        );
        // 初始贴底锚定可能还在追最新帧，再等它稳定。
        // 初始贴底用 1e8 哨兵偏移，布局校正前 maxExtent - pixels 恒为负，
        // 必须等 pixels 真正回落到 maxExtent 附近才算贴底。
        await pumpUntil(
          tester,
          () =>
              controller.scrollController.position.maxScrollExtent > 1.0 &&
              controller.scrollController.position.pixels <=
                  controller.scrollController.position.maxScrollExtent + 1.0,
          description: 'newest messages pinned to bottom',
        );

        final messages = imService.currentMessages;
        expect(messages.map((m) => m.msgId).toSet().length, messages.length);
        expect(
          messages.last.msgId,
          'fs-tool-360',
          reason: '最新消息不得被裁出窗口',
        );
        expect(
          messages.any((m) => m.msgId == 'fs-text-60'),
          isTrue,
          reason: '工具卡组前方的正文必须从 LocalDb 自动补入窗口',
        );
        for (var i = 1; i < messages.length; i++) {
          expect(
            messages[i].createdAt >= messages[i - 1].createdAt,
            isTrue,
            reason: '窗口顺序必须保持升序',
          );
        }

        // 进页无手势即看到正文：至少一条正文气泡的渲染框在视口内。
        // 贴底 jumpTo 先更新 pixels 再重排视口，补两帧等绘制稳定后再读几何。
        await tester.pump();
        await tester.pump();
        expect(
          textBubbleVisibleOnScreen(controller),
          isTrue,
          reason: '首屏必须直接渲染出工具卡组之前的正文气泡',
        );

        // 折叠工具卡组聚合为单个气泡且就在首屏视口内，计数准确。
        final groupFinder = find.byType(
          ChatToolExecutionGroupCardView,
          skipOffstage: false,
        );
        expect(groupFinder, findsOneWidget);
        final groupBox =
            groupFinder.evaluate().single.findRenderObject() as RenderBox;
        final listBox =
            find.byType(ListView).evaluate().first.findRenderObject()
                as RenderBox;
        final listTop = listBox.localToGlobal(Offset.zero).dy;
        final listBottom = listTop + listBox.size.height;
        final groupTop = groupBox.localToGlobal(Offset.zero).dy;
        expect(
          groupTop < listBottom && groupTop + groupBox.size.height > listTop,
          isTrue,
          reason: '聚合工具卡气泡必须在首屏视口内',
        );
        expect(find.text('300', skipOffstage: false), findsOneWidget);

        // 展开工具卡组：明细内容与计数准确。
        await tester.tap(
          find.byKey(
            const Key('chat_message_card_tool_execution_group_toggle'),
            skipOffstage: false,
          ),
        );
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 50));
        await tester.pump();
        expect(
          find.text('Bash: fs_step_61', skipOffstage: false),
          findsOneWidget,
        );
        expect(
          find.text('Bash: fs_step_360', skipOffstage: false),
          findsOneWidget,
        );
        expect(find.text('300', skipOffstage: false), findsOneWidget);

        // 用户上翻手势仍可分页加载更老的正文（展开态下逐段 fling 到顶部触发区）。
        final beforePage = imService.currentMessages.length;
        expect(
          imService.currentMessages.any((m) => m.msgId == 'fs-text-1'),
          isFalse,
          reason: '用例前置：最老的正文尚未进入窗口',
        );
        // 每翻一页会做偏移保持（视口跳回中部），需再次上翻到顶部触发下一页，
        // 直到最老的正文进入窗口。
        for (
          var round = 0;
          round < 10 &&
              !imService.currentMessages.any((m) => m.msgId == 'fs-text-1');
          round++
        ) {
          // 即使已在顶部触发区也要再拨一次：顶部加载只在滚动事件中判定，
          // 偏移保持后的停留位置不会自发触发。
          var guard = 0;
          do {
            await tester.fling(
              find.byType(ListView),
              const Offset(0, 800),
              4000,
              warnIfMissed: false,
            );
            await tester.pump();
            await tester.pump(const Duration(milliseconds: 100));
          } while (controller.scrollController.position.pixels > 150 &&
              ++guard < 30);
          final beforeRound = imService.currentMessages.length;
          await pumpUntil(
            tester,
            () =>
                imService.currentMessages.length != beforeRound ||
                imService.currentMessages.any((m) => m.msgId == 'fs-text-1'),
            maxRounds: 100,
            description: 'older page arrives after scrolling to top',
          );
        }
        await pumpUntil(
          tester,
          () => imService.currentMessages.length > beforePage,
          description: 'user scroll-up pages older history',
        );
        expect(
          imService.currentMessages.any((m) => m.msgId == 'fs-text-1'),
          isTrue,
          reason: 'oldest local text eventually paged in',
        );
        final paged = imService.currentMessages;
        expect(paged.map((m) => m.msgId).toSet().length, paged.length);
        expect(paged.first.msgId, 'fs-text-1');
        expect(paged.every((m) => m.sessionId == _sid), isTrue);

        controller.onClose();
      });
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    '首屏尾部全为零高度内部指令时仍继续填到正文（计量口径与 ChatView 可见项一致）',
    (WidgetTester tester) async {
      // 60 body texts followed by 300 internal directives (approval slash
      // commands + open-session directives) that ChatView hides as zero-height
      // rows. A raw/visible-unit miscount stops the fill at the cap with a
      // blank viewport; the fix must keep paging until body text renders.
      await tester.runAsync(() async {
        await LocalDb.batchInsertMessages([
          for (var seq = 1; seq <= 60; seq++) _textRow(seq),
          for (var seq = 61; seq <= 360; seq++) _directiveRow(seq),
        ]);

        imService.sessions.assignAll([
          SessionModel(
            sessionId: _sid,
            type: 'private',
            peerId: _senderId,
            peerType: 2,
            peerNickname: 'Tool Agent',
            updatedAt: 0,
            lastMessageTime: 0,
          ),
        ]);

        final controller = Get.put(ChatController());
        controller.sessionId = _sid;
        controller.chatTitle = 'Tool Agent';
        controller.chatType = 'private';

        await tester.pumpWidget(
          GetMaterialApp(
            translations: AppTranslations(),
            locale: const Locale('en', 'US'),
            home: ChatView(),
          ),
        );
        await tester.pump();

        await pumpUntil(
          tester,
          () =>
              controller.scrollController.hasClients &&
              controller.scrollController.position.maxScrollExtent > 1.0,
          description: 'first screen filled past the directive tail',
        );
        // 初始贴底用 1e8 哨兵偏移，布局校正前 maxExtent - pixels 恒为负，
        // 必须等 pixels 真正回落到 maxExtent 附近才算贴底。
        await pumpUntil(
          tester,
          () =>
              controller.scrollController.position.maxScrollExtent > 1.0 &&
              controller.scrollController.position.pixels <=
                  controller.scrollController.position.maxScrollExtent + 1.0,
          description: 'newest messages pinned to bottom',
        );
        await tester.pump();
        await tester.pump();

        final messages = imService.currentMessages;
        expect(messages.map((m) => m.msgId).toSet().length, messages.length);
        expect(messages.last.msgId, 'fs-directive-360');
        expect(imService.hasNewerMessages, isFalse);
        expect(
          textBubbleVisibleOnScreen(controller),
          isTrue,
          reason: '指令行零高度，首屏必须直接渲染出更早的正文气泡',
        );
        // 口径一致：服务窗口计量的可见气泡数 == ChatView 实际可见子项数
        // == 窗口内的正文行数（指令全部零高度，不占可见名额）。填屏在视口
        // 填满后即停，因此不要求 60 条正文全部入窗。
        final textsInWindow = messages
            .where((m) => m.msgId.startsWith('fs-text-'))
            .length;
        final visibleChildren =
            controller.messageListSnapshot.visibleMessageIndexes.length;
        expect(textsInWindow, greaterThanOrEqualTo(10));
        expect(visibleChildren, textsInWindow);
        expect(imService.currentWindowVisibleBubbleCount, visibleChildren);

        controller.onClose();
      });
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );
}
