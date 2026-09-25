import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:grix/modules/chat/widgets/chat_scroll_to_bottom_button.dart';
import 'package:grix/modules/chat/widgets/chat_updated_above_pill.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 「原地编辑过的消息」可见性 feature 的测试：
/// 1. 编辑事件命中不在可视区内的消息 -> 底部胶囊出现，点击后跳转并消失。
/// 2. 编辑事件命中可视区内的消息 -> 不出胶囊，原位刷新即可。
/// 3. 胶囊与自主滚动到场的消息共存/互不干扰。
/// 4. 会话内置顶消息：置顶/取消置顶、跳转、编辑同步刷新摘要。
class _FakeImService extends ImService {
  bool hasOlder = false;
  bool hasNewer = false;

  /// Canned pages, one served per load call.
  final List<List<MessageModel>> olderPages = [];
  final List<List<MessageModel>> newerPages = [];
  int loadOlderCalls = 0;
  int loadNewerCalls = 0;

  /// When > 0, paging trims the opposite window end to this many messages —
  /// mirroring the resident-message cap trims — and the trimmed segment
  /// becomes available to the opposite paging direction.
  int windowCap = 0;

  @override
  bool get hasOlderMessages => hasOlder;

  @override
  bool get hasNewerMessages => hasNewer;

  @override
  Future<void> loadOlderForCurrentSession() async {
    loadOlderCalls++;
    if (!hasOlder) return;
    if (olderPages.isNotEmpty) {
      currentMessages.insertAll(0, olderPages.removeAt(0));
    }
    hasOlder = olderPages.isNotEmpty;
    if (windowCap > 0 && currentMessages.length > windowCap) {
      newerPages.insert(0, currentMessages.sublist(windowCap));
      currentMessages.removeRange(windowCap, currentMessages.length);
      hasNewer = true;
    }
  }

  @override
  Future<void> loadNewerForCurrentSession() async {
    loadNewerCalls++;
    if (!hasNewer) return;
    if (newerPages.isNotEmpty) {
      currentMessages.addAll(newerPages.removeAt(0));
    }
    hasNewer = newerPages.isNotEmpty;
    if (windowCap > 0 && currentMessages.length > windowCap) {
      final overflow = currentMessages.length - windowCap;
      olderPages.insert(0, currentMessages.sublist(0, overflow));
      currentMessages.removeRange(0, overflow);
      hasOlder = true;
    }
  }

  @override
  void enterSession(String sessionId) {}

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

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<ChatController> pumpChatViewWithMessages(
    WidgetTester tester, {
    required String sessionId,
    required List<MessageModel> messages,
  }) async {
    final imService = Get.find<ImService>();
    imService.currentMessages.assignAll(messages);

    final controller = Get.put(ChatController());
    controller.sessionId = sessionId;
    controller.chatTitle = sessionId;
    controller.chatType = 'private';
    addTearDown(() {
      if (!controller.isClosed) {
        controller.onClose();
      }
    });

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatView(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));
    return controller;
  }

  List<MessageModel> buildMessages(String sessionId, int count) {
    return List.generate(
      count,
      (i) => MessageModel(
        msgId: 'm$i',
        sessionId: sessionId,
        senderId: 'peer',
        content: 'line $i',
        createdAt: i,
      ),
    );
  }

  /// 把胶囊/置顶点击触发的 jump 循环推进到收敛：用零时长的帧推进滚动
  /// 步进循环里的每次 `endOfFrame` 等待，再用一帧覆盖
  /// `Scrollable.ensureVisible` 的 200ms 动画，让高亮计时器刚好启动——
  /// 调用方可以在此之后立即断言高亮状态，再用 [pumpDrainTimers] 冲掉计时器。
  Future<void> pumpJumpSteps(WidgetTester tester, {int steps = 60}) async {
    for (var i = 0; i < steps; i++) {
      await tester.pump();
    }
    await tester.pump(const Duration(milliseconds: 300));
  }

  /// 冲掉高亮自动清除、上方更新按钮 5s 自动关闭等一次性计时器，避免测试结束时被判定为"泄漏"。
  Future<void> pumpDrainTimers(WidgetTester tester) async {
    for (var i = 0; i < 6; i++) {
      await tester.pump(const Duration(seconds: 1));
    }
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

  tearDown(() async {
    Get.reset();
  });

  group('updated-above notice pill', () {
    testWidgets(
      'edit above the viewport shows the pill; tap jumps and dismisses it',
      (tester) async {
        const sessionId = 'session_edit_notice_offscreen';
        final messages = buildMessages(sessionId, 80);
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: sessionId,
          messages: messages,
        );
        final imService = Get.find<ImService>() as _FakeImService;

        controller.scrollController.jumpTo(
          controller.scrollController.position.maxScrollExtent,
        );
        await tester.pump();

        // message 0 太靠上，此时既不可见也未挂载。
        expect(find.text('line 0'), findsNothing);

        final edited = imService.currentMessages[0].copyWith(
          content: 'line 0 edited',
        );
        imService.currentMessages[0] = edited;
        imService.emitMessageEditedForTest(edited);
        await tester.pump(const Duration(milliseconds: 100));

        expect(controller.pendingUpdatedMessageIds, ['m0']);
        expect(find.byKey(ChatUpdatedAbovePill.buttonKey), findsOneWidget);

        await tester.tap(find.byKey(ChatUpdatedAbovePill.buttonKey));
        await pumpJumpSteps(tester);

        expect(controller.pendingUpdatedMessageIds, isEmpty);
        expect(find.text('line 0 edited'), findsOneWidget);
        expect(controller.highlightedMessageItemKey.value, 'm:m0');
        await pumpDrainTimers(tester);
      },
    );

    testWidgets(
      'circular button sits at bottom-right, stacked above scroll-to-bottom',
      (tester) async {
        await tester.binding.setSurfaceSize(const Size(320, 720));
        addTearDown(() => tester.binding.setSurfaceSize(null));

        const sessionId = 'session_edit_notice_en_overflow';
        final messages = buildMessages(sessionId, 80);
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: sessionId,
          messages: messages,
        );
        final imService = Get.find<ImService>() as _FakeImService;

        // Scroll to the top so scroll-to-bottom appears and a mid-list
        // edit lands outside the viewport.
        controller.onUserScrollStart(controller.scrollController.position);
        controller.scrollController.jumpTo(0);
        await tester.pump();
        controller.onUserScrollActive(controller.scrollController.position);
        controller.onUserScrollEnd(controller.scrollController.position);
        await tester.pump();

        final edited = imService.currentMessages[60].copyWith(
          content: 'line 60 edited',
        );
        imService.currentMessages[60] = edited;
        imService.emitMessageEditedForTest(edited);
        await tester.pump(const Duration(milliseconds: 100));

        expect(controller.pendingUpdatedMessageIds, isNotEmpty);
        expect(find.byKey(ChatUpdatedAbovePill.buttonKey), findsOneWidget);
        expect(
          find.descendant(
            of: find.byKey(ChatUpdatedAbovePill.buttonKey),
            matching: find.byIcon(Icons.arrow_upward_rounded),
          ),
          findsOneWidget,
        );
        expect(find.byIcon(Icons.arrow_downward_rounded), findsOneWidget);

        final pillRect =
            tester.getRect(find.byKey(ChatUpdatedAbovePill.buttonKey));
        final downRect = tester.getRect(
          find.descendant(
            of: find.byType(ChatScrollToBottomButton),
            matching: find.byType(Material),
          ),
        );
        expect(pillRect.size, const Size(44, 44));
        expect(pillRect.right, closeTo(320 - 12, 0.5));
        expect(downRect.right, closeTo(320 - 12, 0.5));
        expect(pillRect.bottom + 8, closeTo(downRect.top, 1.0));
        expect(pillRect.overlaps(downRect), isFalse);
        await pumpDrainTimers(tester);
      },
    );

    testWidgets(
      'circular button auto-dismisses after five seconds without user action',
      (tester) async {
        const sessionId = 'session_edit_notice_auto_dismiss';
        final messages = buildMessages(sessionId, 80);
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: sessionId,
          messages: messages,
        );
        final imService = Get.find<ImService>() as _FakeImService;

        controller.scrollController.jumpTo(
          controller.scrollController.position.maxScrollExtent,
        );
        await tester.pump();

        final edited = imService.currentMessages[0].copyWith(
          content: 'line 0 edited',
        );
        imService.currentMessages[0] = edited;
        imService.emitMessageEditedForTest(edited);
        await tester.pump(const Duration(milliseconds: 100));

        final button = find.byKey(ChatUpdatedAbovePill.buttonKey);
        expect(controller.pendingUpdatedMessageIds, ['m0']);
        expect(button, findsOneWidget);

        await tester.pump(const Duration(seconds: 4));
        expect(button, findsOneWidget);

        await tester.pump(const Duration(seconds: 2));
        expect(controller.pendingUpdatedMessageIds, isEmpty);
        expect(button, findsNothing);
      },
    );

    testWidgets('edit inside the current viewport never shows the pill', (
      tester,
    ) async {
      const sessionId = 'session_edit_notice_onscreen';
      final messages = buildMessages(sessionId, 10);
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: messages,
      );
      final imService = Get.find<ImService>() as _FakeImService;

      // 只有 10 条消息，全部在缓存范围内可见。
      expect(find.text('line 9'), findsOneWidget);

      final edited = imService.currentMessages[9].copyWith(
        content: 'line 9 edited',
      );
      imService.currentMessages[9] = edited;
      imService.emitMessageEditedForTest(edited);
      await tester.pump(const Duration(milliseconds: 100));

      expect(controller.pendingUpdatedMessageIds, isEmpty);
      expect(find.text('line 9 edited'), findsOneWidget);
      expect(
        find.byWidgetPredicate(
          (w) => w is Text && (w.data ?? '').contains('已更新'),
        ),
        findsNothing,
      );
    });

    testWidgets(
      'scrolling the edited message into view drops it without a tap',
      (tester) async {
        const sessionId = 'session_edit_notice_manual_scroll';
        final messages = buildMessages(sessionId, 80);
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: sessionId,
          messages: messages,
        );
        final imService = Get.find<ImService>() as _FakeImService;

        controller.scrollController.jumpTo(
          controller.scrollController.position.maxScrollExtent,
        );
        await tester.pump();

        final edited = imService.currentMessages[0].copyWith(
          content: 'line 0 edited',
        );
        imService.currentMessages[0] = edited;
        imService.emitMessageEditedForTest(edited);
        await tester.pump(const Duration(milliseconds: 100));
        expect(controller.pendingUpdatedMessageIds, ['m0']);

        // 用户自己滚回顶部找到了它。
        controller.scrollController.jumpTo(0);
        await tester.pump();
        await tester.pump();

        expect(controller.pendingUpdatedMessageIds, isEmpty);
      },
    );

    testWidgets('two off-screen edits queue in conversation order', (
      tester,
    ) async {
      const sessionId = 'session_edit_notice_multi';
      final messages = buildMessages(sessionId, 80);
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: messages,
      );
      final imService = Get.find<ImService>() as _FakeImService;

      controller.scrollController.jumpTo(
        controller.scrollController.position.maxScrollExtent,
      );
      await tester.pump();

      // 先编辑靠后的（m5），再编辑更靠前的（m2）——期望胶囊队列仍按会话顺序排列，
      // 与编辑事件到达的先后无关。
      final edited5 = imService.currentMessages[5].copyWith(
        content: 'edited 5',
      );
      imService.currentMessages[5] = edited5;
      imService.emitMessageEditedForTest(edited5);
      await tester.pump(const Duration(milliseconds: 100));

      final edited2 = imService.currentMessages[2].copyWith(
        content: 'edited 2',
      );
      imService.currentMessages[2] = edited2;
      imService.emitMessageEditedForTest(edited2);
      await tester.pump(const Duration(milliseconds: 100));

      expect(controller.pendingUpdatedMessageIds, ['m2', 'm5']);

      // 跳到最早的一条（m2）后应从队列里摘除，m5 因为路径不同仍待处理。
      unawaited(controller.jumpToEarliestUpdatedMessage());
      await pumpJumpSteps(tester);
      expect(controller.pendingUpdatedMessageIds.contains('m2'), isFalse);
      await pumpDrainTimers(tester);
    });

    testWidgets(
      'edit outside the loaded window shows the pill; tap pages history, '
      'jumps and highlights',
      (tester) async {
        const sessionId = 'session_edit_notice_outside_window';
        // 窗口只持有最近的尾巴（m40..m79）；被编辑的 m5 远在窗口之上。
        final windowMessages = buildMessages(sessionId, 80).sublist(40);
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: sessionId,
          messages: windowMessages,
        );
        final imService = Get.find<ImService>() as _FakeImService;
        // 更老的一页里 m5 已是编辑后的内容（本地库已被 push_edit 更新）。
        imService.olderPages.add(
          buildMessages(sessionId, 40)
              .map(
                (m) =>
                    m.msgId == 'm5' ? m.copyWith(content: 'line 5 edited') : m,
              )
              .toList(),
        );
        imService.hasOlder = true;

        controller.scrollController.jumpTo(
          controller.scrollController.position.maxScrollExtent,
        );
        await tester.pump();

        imService.emitMessageEditedForTest(
          MessageModel(
            msgId: 'm5',
            sessionId: sessionId,
            senderId: 'peer',
            content: 'line 5 edited',
            createdAt: 5,
          ),
        );
        await tester.pump(const Duration(milliseconds: 100));

        expect(controller.pendingUpdatedMessageIds, ['m5']);
        expect(find.byKey(ChatUpdatedAbovePill.buttonKey), findsOneWidget);

        await tester.tap(find.byKey(ChatUpdatedAbovePill.buttonKey));
        await pumpJumpSteps(tester);

        expect(imService.loadOlderCalls, greaterThan(0));
        expect(controller.pendingUpdatedMessageIds, isEmpty);
        expect(find.text('line 5 edited'), findsOneWidget);
        expect(controller.highlightedMessageItemKey.value, 'm:m5');
        await pumpDrainTimers(tester);
      },
    );

    testWidgets(
      'out-of-window pending edit survives scroll settling',
      (tester) async {
        const sessionId = 'session_edit_notice_scroll_settle';
        final windowMessages = buildMessages(sessionId, 80).sublist(40);
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: sessionId,
          messages: windowMessages,
        );
        final imService = Get.find<ImService>() as _FakeImService;

        controller.scrollController.jumpTo(
          controller.scrollController.position.maxScrollExtent,
        );
        await tester.pump();

        imService.emitMessageEditedForTest(
          MessageModel(
            msgId: 'm5',
            sessionId: sessionId,
            senderId: 'peer',
            content: 'line 5 edited',
            createdAt: 5,
          ),
        );
        await tester.pump(const Duration(milliseconds: 100));
        expect(controller.pendingUpdatedMessageIds, ['m5']);

        // 普通滚动停顿不能清掉窗口外的待跳转项。
        controller.scrollController.jumpTo(
          controller.scrollController.position.maxScrollExtent - 200,
        );
        await tester.pump();
        await tester.pump();

        expect(controller.pendingUpdatedMessageIds, ['m5']);
        await pumpDrainTimers(tester);
      },
    );

    testWidgets(
      'full window: tap pages older in one direction without oscillating',
      (tester) async {
        const sessionId = 'session_edit_notice_full_window';
        // 窗口顶满 200 条常驻上限（m60..m259）；每次 loadOlder 都会裁掉
        // 底部并置 hasNewer——交替翻页会在这里往返振荡、净进度为零。
        final all = List.generate(
          260,
          (i) => MessageModel(
            msgId: 'm$i',
            sessionId: sessionId,
            senderId: 'peer',
            content: 'line $i',
            createdAt: i,
          ),
        );
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: sessionId,
          messages: all.sublist(60),
        );
        final imService = Get.find<ImService>() as _FakeImService;
        imService.windowCap = 200;
        imService.olderPages.addAll([
          all.sublist(20, 60),
          all
              .sublist(0, 20)
              .map(
                (m) =>
                    m.msgId == 'm5' ? m.copyWith(content: 'line 5 edited') : m,
              )
              .toList(),
        ]);
        imService.hasOlder = true;

        controller.scrollController.jumpTo(
          controller.scrollController.position.maxScrollExtent,
        );
        await tester.pump();

        imService.emitMessageEditedForTest(
          MessageModel(
            msgId: 'm5',
            sessionId: sessionId,
            senderId: 'peer',
            content: 'line 5 edited',
            createdAt: 5,
          ),
        );
        await tester.pump(const Duration(milliseconds: 100));
        expect(controller.pendingUpdatedMessageIds, ['m5']);

        await tester.tap(find.byKey(ChatUpdatedAbovePill.buttonKey));
        await pumpJumpSteps(tester);

        // 两页 older 直达目标；全程不得向 newer 方向回拉（自动分页在跳转
        // 期间被挂起，fake 的 loadNewer 一旦被调用就会还原底部并裁掉目标）。
        expect(imService.loadOlderCalls, 2);
        expect(imService.loadNewerCalls, 0);
        expect(controller.pendingUpdatedMessageIds, isEmpty);
        expect(find.text('line 5 edited'), findsOneWidget);
        expect(controller.highlightedMessageItemKey.value, 'm:m5');
        await pumpDrainTimers(tester);
      },
    );
  });

  group('pinned message', () {
    testWidgets('pinning shows the bar; tap jumps to and highlights it', (
      tester,
    ) async {
      const sessionId = 'session_pin_basic';
      final messages = buildMessages(sessionId, 80);
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: messages,
      );
      await tester.pump();

      expect(controller.isMessagePinned('m3'), isFalse);
      await controller.togglePinMessage(
        controller.imService.currentMessages[3],
      );
      await tester.pump();

      expect(controller.isMessagePinned('m3'), isTrue);
      expect(controller.pinnedMessage.value?.summary, 'line 3');
      expect(find.text('line 3'), findsWidgets);

      controller.scrollController.jumpTo(
        controller.scrollController.position.maxScrollExtent,
      );
      await tester.pump();

      unawaited(controller.jumpToPinnedMessage());
      await pumpJumpSteps(tester);

      expect(controller.highlightedMessageItemKey.value, 'm:m3');
      await pumpDrainTimers(tester);
    });

    testWidgets('pinning again replaces the previous pin (one per session)', (
      tester,
    ) async {
      const sessionId = 'session_pin_replace';
      final messages = buildMessages(sessionId, 10);
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: messages,
      );

      await controller.togglePinMessage(
        controller.imService.currentMessages[1],
      );
      await tester.pump();
      expect(controller.isMessagePinned('m1'), isTrue);

      await controller.togglePinMessage(
        controller.imService.currentMessages[4],
      );
      await tester.pump();

      expect(controller.isMessagePinned('m1'), isFalse);
      expect(controller.isMessagePinned('m4'), isTrue);
    });

    testWidgets('toggling pin on the pinned message unpins it', (tester) async {
      const sessionId = 'session_pin_toggle_off';
      final messages = buildMessages(sessionId, 10);
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: messages,
      );

      await controller.togglePinMessage(
        controller.imService.currentMessages[2],
      );
      await tester.pump();
      expect(controller.pinnedMessage.value, isNotNull);

      await controller.togglePinMessage(
        controller.imService.currentMessages[2],
      );
      await tester.pump();

      expect(controller.pinnedMessage.value, isNull);
      expect(controller.isMessagePinned('m2'), isFalse);
    });

    testWidgets('editing the pinned message refreshes the bar summary', (
      tester,
    ) async {
      const sessionId = 'session_pin_edit_refresh';
      final messages = buildMessages(sessionId, 10);
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: messages,
      );
      final imService = Get.find<ImService>() as _FakeImService;

      await controller.togglePinMessage(imService.currentMessages[5]);
      await tester.pump();
      expect(controller.pinnedMessage.value?.summary, 'line 5');

      final edited = imService.currentMessages[5].copyWith(
        content: 'line 5 after edit',
      );
      imService.currentMessages[5] = edited;
      imService.emitMessageEditedForTest(edited);
      await tester.pump(const Duration(milliseconds: 100));

      expect(controller.pinnedMessage.value?.summary, 'line 5 after edit');
      expect(find.text('line 5 after edit'), findsWidgets);
    });

    testWidgets(
      'pinned message edited outside the window refreshes the bar summary',
      (tester) async {
        const sessionId = 'session_pin_edit_outside_window';
        // 窗口只持有最近的尾巴；置顶的 m5 不在窗口内。
        final windowMessages = buildMessages(sessionId, 80).sublist(40);
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: sessionId,
          messages: windowMessages,
        );
        final imService = Get.find<ImService>() as _FakeImService;

        await controller.togglePinMessage(
          MessageModel(
            msgId: 'm5',
            sessionId: sessionId,
            senderId: 'peer',
            content: 'line 5',
            createdAt: 5,
          ),
        );
        await tester.pump();
        expect(controller.pinnedMessage.value?.summary, 'line 5');

        imService.emitMessageEditedForTest(
          MessageModel(
            msgId: 'm5',
            sessionId: sessionId,
            senderId: 'peer',
            content: 'line 5 after edit',
            createdAt: 5,
          ),
        );
        await tester.pump(const Duration(milliseconds: 100));

        expect(controller.pinnedMessage.value?.summary, 'line 5 after edit');
        expect(find.text('line 5 after edit'), findsWidgets);
        await pumpDrainTimers(tester);
      },
    );

    testWidgets('pin persists across a fresh controller for the same session', (
      tester,
    ) async {
      const sessionId = 'session_pin_persist';
      final messages = buildMessages(sessionId, 10);
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: messages,
      );

      await controller.togglePinMessage(
        controller.imService.currentMessages[6],
      );
      await tester.pump();
      expect(controller.isMessagePinned('m6'), isTrue);

      await Get.delete<ChatController>(force: true);
      await tester.pump();

      final reopened = Get.put(ChatController());
      reopened.sessionId = sessionId;
      reopened.chatTitle = sessionId;
      reopened.chatType = 'private';
      addTearDown(() {
        if (!reopened.isClosed) {
          reopened.onClose();
        }
      });
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      expect(reopened.isMessagePinned('m6'), isTrue);
      expect(reopened.pinnedMessage.value?.summary, 'line 6');
    });
  });
}
