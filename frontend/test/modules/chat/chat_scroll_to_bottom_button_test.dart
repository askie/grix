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
import 'package:grix/modules/chat/widgets/chat_updated_above_pill.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 「回到底部」悬浮按钮 feature 的测试：
/// 1. 离底超过约一屏出现、回到底部附近隐藏。
/// 2. 窗口不含最新消息（hasNewerMessages）时无论滚动位置都显示。
/// 3. 点击：窗口内含最新时平滑滚到底；被挤出窗口时直接重置为最新一页。
/// 4. 离开底部期间收到的新消息计数（99+ 封顶），回底清零。
/// 5. 回底后恢复自动跟随；胶囊跳转失败后跟随同样能恢复。
/// 6. 与「上方有 N 条已更新」胶囊同时出现且互不重叠。
class _FakeImService extends ImService {
  bool hasOlder = false;
  bool hasNewer = false;

  int forceReloadCalls = 0;
  String? forceReloadSessionId;
  bool? forceReloadTriggerPullSync;

  /// Served as the "latest page" by [forceReloadSessionWindow].
  List<MessageModel> latestPage = const [];

  @override
  bool get hasOlderMessages => hasOlder;

  @override
  bool get hasNewerMessages => hasNewer;

  @override
  Future<void> forceReloadSessionWindow(
    String sessionId, {
    bool triggerPullSync = true,
  }) async {
    forceReloadCalls++;
    forceReloadSessionId = sessionId;
    forceReloadTriggerPullSync = triggerPullSync;
    currentMessages.assignAll(latestPage);
    hasNewer = false;
  }

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

  List<MessageModel> buildMessages(
    String sessionId,
    int count, {
    int start = 0,
  }) {
    return List.generate(
      count,
      (i) => MessageModel(
        msgId: 'm${start + i}',
        sessionId: sessionId,
        senderId: 'peer',
        content: 'line ${start + i}',
        createdAt: start + i,
      ),
    );
  }

  /// 模拟一次真实用户拖拽：从当前位置甩到 [target] 并松手，让控制器走
  /// onUserScrollStart/Active/End 的完整路径（底部跟随只认用户交互）。
  Future<void> userScrollTo(
    WidgetTester tester,
    ChatController controller,
    double target,
  ) async {
    controller.onUserScrollStart(controller.scrollController.position);
    controller.scrollController.jumpTo(target);
    await tester.pump();
    controller.onUserScrollActive(controller.scrollController.position);
    controller.onUserScrollEnd(controller.scrollController.position);
    await tester.pump();
  }

  double maxExtent(ChatController controller) =>
      controller.scrollController.position.maxScrollExtent;

  final buttonFinder = find.byIcon(Icons.arrow_downward_rounded);

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

  group('scroll-to-bottom button', () {
    testWidgets('appears beyond one viewport up, hides again near the bottom',
        (tester) async {
      const sessionId = 'session_stb_visibility';
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: buildMessages(sessionId, 80),
      );

      // 初始钉在底部：不显示。
      expect(buttonFinder, findsNothing);
      expect(controller.scrollToBottomButtonVisible.value, isFalse);

      // 甩到最顶：离底远超一屏，出现。
      await userScrollTo(tester, controller, 0);
      expect(controller.scrollToBottomButtonVisible.value, isTrue);
      expect(buttonFinder, findsOneWidget);

      // 回到底部附近（距离 < 一屏）：隐藏。
      await userScrollTo(tester, controller, maxExtent(controller) - 100);
      expect(controller.scrollToBottomButtonVisible.value, isFalse);
      expect(buttonFinder, findsNothing);
    });

    testWidgets('stays visible at any scroll offset while the window lacks '
        'the latest messages', (tester) async {
      const sessionId = 'session_stb_has_newer';
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: buildMessages(sessionId, 80),
      );
      final imService = Get.find<ImService>() as _FakeImService;

      // 模拟"向上翻页把最新挤出窗口"：仍在窗口底部附近，但 hasNewer=true。
      imService.hasNewer = true;
      await userScrollTo(tester, controller, maxExtent(controller) - 100);
      expect(controller.scrollToBottomButtonVisible.value, isTrue);
      expect(buttonFinder, findsOneWidget);

      // 完全贴底也依然显示。
      await userScrollTo(tester, controller, maxExtent(controller));
      expect(controller.scrollToBottomButtonVisible.value, isTrue);
      expect(buttonFinder, findsOneWidget);
    });

    testWidgets('tap smooth-scrolls to the bottom when the latest messages '
        'are still in the window, and resumes bottom-follow', (tester) async {
      const sessionId = 'session_stb_tap_in_window';
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: buildMessages(sessionId, 80),
      );
      final imService = Get.find<ImService>() as _FakeImService;

      await userScrollTo(tester, controller, 0);
      expect(buttonFinder, findsOneWidget);

      await tester.tap(buttonFinder);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 60));
      await tester.pump(const Duration(milliseconds: 300));

      expect(
        controller.scrollController.position.extentAfter,
        lessThanOrEqualTo(1.0),
      );
      expect(controller.scrollToBottomButtonVisible.value, isFalse);

      // 跟随已恢复：新消息到达后自动贴底。
      imService.currentMessages.add(
        buildMessages(sessionId, 1, start: 80).first,
      );
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump();
      expect(
        controller.scrollController.position.extentAfter,
        lessThanOrEqualTo(1.0),
      );
    });

    testWidgets('tap resets the window straight to the latest page when the '
        'newest messages were trimmed out', (tester) async {
      const sessionId = 'session_stb_tap_reset';
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: buildMessages(sessionId, 80),
      );
      final imService = Get.find<ImService>() as _FakeImService;
      imService.latestPage = buildMessages(sessionId, 20, start: 180);
      imService.hasNewer = true;

      await userScrollTo(tester, controller, 0);
      expect(buttonFinder, findsOneWidget);

      await tester.tap(buttonFinder);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 60));
      await tester.pump(const Duration(milliseconds: 300));

      expect(imService.forceReloadCalls, 1);
      expect(imService.forceReloadSessionId, sessionId);
      expect(imService.forceReloadTriggerPullSync, isFalse);
      expect(imService.hasNewer, isFalse);
      expect(imService.currentMessages.first.msgId, 'm180');
      expect(
        controller.scrollController.position.extentAfter,
        lessThanOrEqualTo(1.0),
      );
      expect(controller.scrollToBottomButtonVisible.value, isFalse);
    });

    testWidgets('counts messages received while away from the bottom and '
        'clears the badge on return', (tester) async {
      const sessionId = 'session_stb_badge';
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: buildMessages(sessionId, 80),
      );
      final imService = Get.find<ImService>() as _FakeImService;

      await userScrollTo(tester, controller, 0);
      expect(find.text('1'), findsNothing);

      imService.currentMessages.add(
        buildMessages(sessionId, 1, start: 80).first,
      );
      await tester.pump();
      expect(controller.scrollToBottomNewMessageCount.value, 1);
      expect(find.text('1'), findsOneWidget);

      imService.currentMessages.addAll(buildMessages(sessionId, 2, start: 81));
      await tester.pump();
      expect(controller.scrollToBottomNewMessageCount.value, 3);
      expect(find.text('3'), findsOneWidget);

      // 手动滑回底部也清零。
      await userScrollTo(tester, controller, maxExtent(controller));
      expect(controller.scrollToBottomNewMessageCount.value, 0);
      expect(buttonFinder, findsNothing);
      await tester.pump(const Duration(milliseconds: 200));
    });

    testWidgets('badge caps at 99+', (tester) async {
      const sessionId = 'session_stb_badge_cap';
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: buildMessages(sessionId, 80),
      );

      await userScrollTo(tester, controller, 0);
      controller.scrollToBottomNewMessageCount.value = 150;
      await tester.pump();
      expect(find.text('99+'), findsOneWidget);
    });

    testWidgets(
      'force scroll-to-bottom during fling (finger already up) takes effect '
      'immediately',
      (tester) async {
        const sessionId = 'session_stb_fling_force';
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: sessionId,
          messages: buildMessages(sessionId, 80),
        );

        // Drag away from bottom, then release into fling: Start/Active fire
        // with dragDetails, but ScrollEnd has none so onUserScrollEnd never
        // runs. `_userScrollInteractionActive` stays true until idle reset —
        // exactly the stuck state that used to swallow the button press.
        controller.onUserScrollStart(controller.scrollController.position);
        controller.scrollController.jumpTo(0);
        await tester.pump();
        controller.onUserScrollActive(controller.scrollController.position);
        // Finger is off the screen during fling — no pointer-contact flag.
        expect(buttonFinder, findsOneWidget);
        expect(
          controller.scrollController.position.extentAfter,
          greaterThan(1.0),
        );

        await controller.onScrollToBottomButtonPressed();
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 60));
        await tester.pump(const Duration(milliseconds: 300));

        expect(
          controller.scrollController.position.extentAfter,
          lessThanOrEqualTo(1.0),
        );
        expect(controller.scrollToBottomButtonVisible.value, isFalse);
      },
    );

    testWidgets(
      'force scroll-to-bottom still refuses while a finger contacts the list',
      (tester) async {
        const sessionId = 'session_stb_pointer_blocks_force';
        final controller = await pumpChatViewWithMessages(
          tester,
          sessionId: sessionId,
          messages: buildMessages(sessionId, 80),
        );

        await userScrollTo(tester, controller, 0);
        final awayFromBottom =
            controller.scrollController.position.extentAfter;
        expect(awayFromBottom, greaterThan(1.0));

        // Real drag in progress: pointer is down on the list.
        controller.onMessageListPointerDown();
        controller.onUserScrollStart(controller.scrollController.position);
        await controller.onScrollToBottomButtonPressed();
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 300));

        expect(
          controller.scrollController.position.extentAfter,
          closeTo(awayFromBottom, 1.0),
        );

        controller.onMessageListPointerUpOrCancel();
        controller.onUserScrollEnd(controller.scrollController.position);
      },
    );

    testWidgets('shows alongside the updated-above button without overlapping '
        'it', (tester) async {
      const sessionId = 'session_stb_with_pill';
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: buildMessages(sessionId, 80),
      );
      final imService = Get.find<ImService>() as _FakeImService;

      await userScrollTo(tester, controller, 0);

      final edited = imService.currentMessages[60].copyWith(
        content: 'line 60 edited',
      );
      imService.currentMessages[0] = edited;
      imService.emitMessageEditedForTest(edited);
      await tester.pump(const Duration(milliseconds: 100));

      final pillFinder = find.byKey(ChatUpdatedAbovePill.buttonKey);
      expect(pillFinder, findsOneWidget);
      expect(buttonFinder, findsOneWidget);

      final pillRect = tester.getRect(pillFinder);
      final buttonRect = tester.getRect(buttonFinder);
      expect(pillRect.overlaps(buttonRect), isFalse);
      expect(pillRect.bottom, lessThanOrEqualTo(buttonRect.top));
      expect(pillRect.right, closeTo(buttonRect.right, 0.5));

      for (var i = 0; i < 6; i++) {
        await tester.pump(const Duration(seconds: 1));
      }
    });

    testWidgets('a failed updated-above pill jump restores bottom-follow', (
      tester,
    ) async {
      const sessionId = 'session_stb_pill_failure';
      final controller = await pumpChatViewWithMessages(
        tester,
        sessionId: sessionId,
        messages: buildMessages(sessionId, 80),
      );
      final imService = Get.find<ImService>() as _FakeImService;

      // 贴在底部时收到一条"窗口外更早消息"的编辑事件；hasOlder=false，
      // 点击胶囊注定跳不过去。
      final ghost = MessageModel(
        msgId: 'ghost-outside-window',
        sessionId: sessionId,
        senderId: 'peer',
        content: 'edited outside window',
        createdAt: 0,
      );
      imService.emitMessageEditedForTest(ghost);
      await tester.pump(const Duration(milliseconds: 100));
      expect(controller.pendingUpdatedMessageIds, ['ghost-outside-window']);

      await controller.jumpToEarliestUpdatedMessage();
      await tester.pump();
      expect(controller.pendingUpdatedMessageIds, isEmpty);

      // 跳转失败后跟随必须恢复：新消息到达时自动贴底。
      imService.currentMessages.add(
        buildMessages(sessionId, 1, start: 80).first,
      );
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump();
      expect(
        controller.scrollController.position.extentAfter,
        lessThanOrEqualTo(1.0),
      );
    });
  });
}
