import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/conversation_summary_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/conversations_view.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:grix/shared/utils/chat_draft_index.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 搜索态下把搜索框钉在顶部这条改动只关心滚动/布局，不涉及网络，所以这里的
/// 假 [ImService] 比 `conversations_view_quick_actions_test.dart` 更精简。
class _FakeImService extends ImService {
  @override
  bool get isConnected => true;

  @override
  Future<void> refreshSessionsNow() async {}

  @override
  Future<void> refreshSessionsWindowNow() async {}

  @override
  Future<void> refreshSessionsIfStale({
    Duration maxAge = const Duration(seconds: 45),
  }) async {}

  @override
  Future<bool> loadMoreSessionWindowIfNeeded({bool force = false}) async {
    return false;
  }

  @override
  Future<void> reconcilePinsFromConversationSummaries(
    List<ConversationSummaryModel> items, {
    required bool hasMore,
  }) async {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late _FakeImService imService;
  late ConversationsController controller;

  const sessionCount = 40;

  List<SessionModel> buildManySessions() {
    final now = DateTime.now().millisecondsSinceEpoch;
    return List<SessionModel>.generate(sessionCount, (i) {
      return SessionModel(
        sessionId: 'session-$i',
        title: 'Group $i',
        type: 'group',
        updatedAt: now - i * 1000,
        lastMessage: 'message $i',
        lastMessageTime: now - i * 1000,
      );
    });
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
    ChatDraftIndex.resetForTest();
    SharedPreferences.setMockInitialValues({});
    imService = _FakeImService();
    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
    controller = Get.put(ConversationsController());
  });

  tearDown(() {
    ChatDraftIndex.resetForTest();
    Get.reset();
  });

  Widget buildApp() {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      fallbackLocale: const Locale('en', 'US'),
      getPages: [
        GetPage(
          name: AppRoutes.favorites,
          page: () => const Scaffold(body: Text('favorites-page')),
          transition: Transition.noTransition,
        ),
      ],
      home: const ConversationsView(),
    );
  }

  ScrollController scrollControllerOf(WidgetTester tester) {
    return tester.widget<CustomScrollView>(find.byType(CustomScrollView)).controller!;
  }

  testWidgets(
    '搜索态下滚动很多结果之后，搜索框仍然钉在顶部（不滚出屏幕）',
    (WidgetTester tester) async {
      final sessions = buildManySessions();
      controller.searchSessionRecordsOverrideForTest = (_) async =>
          sessions.map((s) => s.toJson()).toList();
      controller.searchMessagesOverrideForTest = (_) async => const [];

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      controller.updateSearchQuery('Group');
      await tester.pump(const Duration(milliseconds: 320));
      await tester.pumpAndSettle();

      expect(find.byType(TextField), findsOneWidget);
      final beforeY = tester.getTopLeft(find.byType(TextField)).dy;

      await tester.fling(
        find.byType(CustomScrollView),
        const Offset(0, -3000),
        3000,
      );
      await tester.pumpAndSettle();

      expect(
        find.byType(TextField),
        findsOneWidget,
        reason: '搜索态下大幅向下滚动后，搜索框应该还钉在顶部',
      );
      final afterY = tester.getTopLeft(find.byType(TextField)).dy;
      expect(
        afterY,
        closeTo(beforeY, 0.5),
        reason: '钉住的搜索框在滚动前后位置应该不变',
      );
    },
  );

  testWidgets(
    '非搜索态行为不变：向下滚动很多会话之后，搜索框正常滚出屏幕',
    (WidgetTester tester) async {
      imService.sessions.assignAll(buildManySessions());

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      expect(find.byType(TextField), findsOneWidget);

      await tester.fling(
        find.byType(CustomScrollView),
        const Offset(0, -3000),
        3000,
      );
      await tester.pumpAndSettle();

      expect(
        find.byType(TextField),
        findsNothing,
        reason: '非搜索态下搜索框应该照旧随列表滚出屏幕，不能变成钉住',
      );
    },
  );

  testWidgets(
    '从翻到中段的列表进入搜索时，滚动位置立即回到顶部',
    (WidgetTester tester) async {
      final sessions = buildManySessions();
      imService.sessions.assignAll(sessions);
      // 搜索时给真实数量的结果，好验证"搜索态下也能滚、叉掉后回顶部"，
      // 而不是滚不动的空结果页。
      controller.searchSessionRecordsOverrideForTest = (_) async =>
          sessions.map((s) => s.toJson()).toList();
      controller.searchMessagesOverrideForTest = (_) async => const [];

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      // 先滚到中段，模拟用户已经翻了一段会话列表。
      await tester.fling(
        find.byType(CustomScrollView),
        const Offset(0, -1200),
        3000,
      );
      await tester.pumpAndSettle();
      expect(scrollControllerOf(tester).offset, greaterThan(0));

      // 进入搜索：不需要等去抖结果回来，滚动位置就应该立即归零。
      controller.updateSearchQuery('Group');
      await tester.pump();

      expect(
        scrollControllerOf(tester).offset,
        0,
        reason: '进入搜索时滚动位置应该立即回到顶部',
      );

      // 等搜索结果落地后再滚一次，证明搜索态下的列表本身也能正常滚动。
      await tester.pump(const Duration(milliseconds: 320));
      await tester.pumpAndSettle();
      await tester.fling(
        find.byType(CustomScrollView),
        const Offset(0, -1200),
        3000,
      );
      await tester.pumpAndSettle();
      expect(scrollControllerOf(tester).offset, greaterThan(0));

      // 叉掉退出搜索：不等去抖，滚动位置也应该立即回到顶部。
      controller.applyExternalSearchQuery('');
      await tester.pump();

      expect(
        scrollControllerOf(tester).offset,
        0,
        reason: '叉掉退出搜索时滚动位置也应该立即回到顶部',
      );

      // 清空关键词自己也会派发一次 200ms 去抖（幂等兜底分支），测试结束前
      // 让它落地，避免遗留未触发的 Timer 导致 flutter_test 的收尾断言报错。
      await tester.pump(const Duration(milliseconds: 320));
      await tester.pumpAndSettle();
    },
  );
}
