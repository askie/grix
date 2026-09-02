import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/account_info/account_info_view.dart';
import 'package:grix/modules/account_info/controllers/account_info_controller.dart';
import 'package:grix/shared/widgets/session_avatar.dart';

class _FakeImService extends ImService {
  @override
  bool get isConnected => true;
}

class _FakeAgentService extends AgentService {}

class _FakeAuthService extends AuthService {
  _FakeAuthService({required String userId, String username = 'owner'})
    : _fakeUser = User(id: userId, username: username, nickname: username);

  final User _fakeUser;

  @override
  User? get user => _fakeUser;

  @override
  String? get userId => _fakeUser.id;
}

class _FakeFriendService extends FriendService {
  @override
  Future<String?> fetchUserProfile(String userId) async => null;
}

class _FakeSessionService extends SessionService {
  final Map<String, SessionDetailResult> detailsBySessionId =
      <String, SessionDetailResult>{};

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    final sid = sessionId.trim();
    return detailsBySessionId[sid] ??
        const SessionDetailResult(code: 50001, message: 'missing detail');
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('places account meta directly below nickname', (
    WidgetTester tester,
  ) async {
    Get.put<AccountInfoController>(
      AccountInfoController(
        initialArguments: {
          'peer_id': '2030808796745437184',
          'peer_type': '1',
          'nickname': 'askie',
          'username': 'askie',
        },
        imService: _FakeImService(),
        friendService: _FakeFriendService(),
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AccountInfoView(),
      ),
    );
    await tester.pump();

    final avatarFinder = find.byType(SessionAvatar);
    final nicknameFinder = find.text('askie');
    final accountFinder = find.text('Account @askie', findRichText: true);
    final userIdFinder = find.text(
      'User ID 2030808796745437184',
      findRichText: true,
    );

    expect(avatarFinder, findsOneWidget);
    expect(nicknameFinder, findsOneWidget);
    expect(accountFinder, findsOneWidget);
    expect(userIdFinder, findsOneWidget);

    final avatarRect = tester.getRect(avatarFinder);
    final nicknameOffset = tester.getTopLeft(nicknameFinder);
    final accountOffset = tester.getTopLeft(accountFinder);
    final userIdOffset = tester.getTopLeft(userIdFinder);

    expect(accountOffset.dy, greaterThan(nicknameOffset.dy));
    expect(userIdOffset.dy, greaterThan(accountOffset.dy));
    expect(accountOffset.dx, closeTo(nicknameOffset.dx, 0.1));
    expect(userIdOffset.dx, closeTo(nicknameOffset.dx, 0.1));
    expect(accountOffset.dx, greaterThan(avatarRect.right));
  });

  testWidgets('tap on user id row copies the number and shows toast promptly', (
    WidgetTester tester,
  ) async {
    String? clipboardText;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.setData') {
            final args = call.arguments as Map<dynamic, dynamic>;
            clipboardText = args['text'] as String?;
            return null;
          }
          return null;
        });
    addTearDown(() {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, null);
    });

    Get.put<AccountInfoController>(
      AccountInfoController(
        initialArguments: {
          'peer_id': '2030808796745437184',
          'peer_type': '1',
          'nickname': 'askie',
          'username': 'askie',
        },
        imService: _FakeImService(),
        friendService: _FakeFriendService(),
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AccountInfoView(),
      ),
    );
    await tester.pump();

    final userIdFinder = find.text(
      'User ID 2030808796745437184',
      findRichText: true,
    );
    expect(userIdFinder, findsOneWidget);

    await tester.tap(userIdFinder);
    await tester.idle();

    expect(clipboardText, '2030808796745437184');
    expect(tester.binding.hasScheduledFrame, isTrue);

    await tester.pump();
    await tester.pump();

    expect(find.text('Copied to clipboard'), findsOneWidget);

    // 等待 CustomToast 内部 3 秒延时定时器完成，避免拖出未结束的 timer。
    await tester.pump(const Duration(seconds: 4));
    await tester.pump();
  });

  testWidgets('shows forward action in more menu', (WidgetTester tester) async {
    Get.put<AccountInfoController>(
      AccountInfoController(
        initialArguments: {
          'peer_id': '2030808796745437184',
          'peer_type': '1',
          'nickname': 'askie',
          'username': 'askie',
        },
        imService: _FakeImService(),
        friendService: _FakeFriendService(),
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const AccountInfoView(),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.more_vert_rounded));
    await tester.pumpAndSettle();

    expect(find.text('Forward'), findsOneWidget);
  });

  testWidgets('shows introduction when profile contains it', (
    WidgetTester tester,
  ) async {
    Get.put<AccountInfoController>(
      AccountInfoController(
        initialArguments: {
          'peer_id': '2030808796745437184',
          'peer_type': '1',
          'nickname': 'askie',
          'username': 'askie',
          'introduction': '把复杂的事说简单一点',
        },
        imService: _FakeImService(),
        friendService: _FakeFriendService(),
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const AccountInfoView(),
      ),
    );
    await tester.pump();

    expect(find.text('把复杂的事说简单一点'), findsOneWidget);
  });

  testWidgets('owned agent shows start chat and history section', (
    WidgetTester tester,
  ) async {
    final imService = _FakeImService();
    final agentService = _FakeAgentService();

    agentService.agents.assignAll([
      AgentModel(id: 'agent-9', agentName: 'Planner', ownerID: 'owner-1'),
    ]);
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'agent-sid',
        title: 'Planner',
        type: 'private',
        peerId: 'agent-9',
        peerType: 2,
        updatedAt: 10,
        unreadCount: 0,
        lastMessage: 'hello',
        lastMessageTime: 10,
      ),
    ]);

    Get.put<AccountInfoController>(
      AccountInfoController(
        initialArguments: {'peer_id': 'agent-9', 'peer_type': '2'},
        imService: imService,
        agentService: agentService,
        authService: _FakeAuthService(userId: 'owner-1'),
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const AccountInfoView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('+会话'), findsOneWidget);
    expect(find.text('搜索对话'), findsOneWidget);
    expect(find.text('Planner'), findsAtLeastNWidgets(1));
    expect(find.text('hello'), findsOneWidget);
    expect(find.textContaining('账号 ', findRichText: true), findsNothing);
    expect(find.textContaining('编号 ', findRichText: true), findsOneWidget);
  });

  testWidgets('keeps +会话 ElevatedButton enabled while creating session', (
    WidgetTester tester,
  ) async {
    final imService = _FakeImService();
    final agentService = _FakeAgentService();
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'agent-sid',
        title: 'Planner',
        type: 'private',
        peerId: 'agent-9',
        peerType: 2,
        updatedAt: 10,
        unreadCount: 0,
        lastMessage: 'hello',
        lastMessageTime: 10,
      ),
    ]);
    agentService.agents.assignAll([
      AgentModel(id: 'agent-9', agentName: 'Planner', ownerID: 'owner-1'),
    ]);

    final controller = AccountInfoController(
      initialArguments: {'peer_id': 'agent-9', 'peer_type': '2'},
      imService: imService,
      agentService: agentService,
      authService: _FakeAuthService(userId: 'owner-1'),
    );
    Get.put<AccountInfoController>(controller);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const AccountInfoView(),
      ),
    );
    await tester.pumpAndSettle();

    final buttonFinder = find.widgetWithText(ElevatedButton, '+会话');
    expect(buttonFinder, findsOneWidget);
    expect(tester.widget<ElevatedButton>(buttonFinder).onPressed, isNotNull);

    // 建会话等待中若把 onPressed 置 null，Material 会掐断红色 splash。
    controller.isActionProcessing.value = true;
    await tester.pump();

    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    expect(find.text('+会话'), findsNothing);
    final processingButton = tester.widget<ElevatedButton>(
      find.byType(ElevatedButton),
    );
    expect(processingButton.onPressed, isNotNull);
    expect(processingButton.enabled, isTrue);
  });

  testWidgets(
    'refreshes profile meta after resolving missing private peer info',
    (WidgetTester tester) async {
      final imService = _FakeImService();
      final friendService = _FakeFriendService();
      final sessionService = _FakeSessionService();

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'seed-user-1',
          title: 'Alice',
          type: 'private',
          updatedAt: 10,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: 10,
        ),
      ]);
      sessionService.detailsBySessionId['seed-user-1'] =
          const SessionDetailResult(
            data: <String, dynamic>{
              'session_type': 1,
              'members': <Map<String, dynamic>>[
                <String, dynamic>{
                  'member_id': '1001',
                  'member_type': 1,
                  'nickname': 'Alice',
                  'username': 'alice_01',
                },
                <String, dynamic>{'member_id': '9009', 'member_type': 1},
              ],
            },
          );

      Get.put<AccountInfoController>(
        AccountInfoController(
          initialArguments: {
            'session_id': 'seed-user-1',
            'peer_type': '0',
            'title': 'Alice',
          },
          imService: imService,
          friendService: friendService,
          authService: _FakeAuthService(userId: '9009'),
          sessionService: sessionService,
        ),
      );

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: const AccountInfoView(),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(
        find.text('Account @alice_01', findRichText: true),
        findsOneWidget,
      );
      expect(find.text('User ID 1001', findRichText: true), findsOneWidget);
    },
  );

  testWidgets(
    'history list highlights the last tapped session and migrates on a new tap',
    (WidgetTester tester) async {
      final imService = _FakeImService();
      final friendService = _FakeFriendService();
      final now = DateTime.now().millisecondsSinceEpoch;

      friendService.friendList.assignAll([
        FriendItem(
          id: 'f-1',
          userId: '1001',
          username: 'alice_01',
          nickname: 'Alice',
          remarkName: '',
          avatarUrl: '',
        ),
      ]);

      // 两条不同 sessionId 的私聊会话历史，给予可识别的预览文本以便定位。
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session-a',
          title: '历史会话A',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'preview-a',
          lastMessageTime: now,
        ),
        SessionModel(
          sessionId: 'session-b',
          title: '历史会话B',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now - 1000,
          unreadCount: 0,
          lastMessage: 'preview-b',
          lastMessageTime: now - 1000,
        ),
      ]);

      final controller = AccountInfoController(
        initialArguments: {
          'peer_id': '1001',
          'peer_type': '1',
          'nickname': 'Alice',
          'username': 'alice_01',
        },
        imService: imService,
        friendService: friendService,
      );
      Get.put<AccountInfoController>(controller);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: const AccountInfoView(),
        ),
      );
      await tester.pumpAndSettle();

      // 通过预览文本定位到对应的 _SessionHistoryTile，向上回到带圆角装饰的高亮 Container。
      Color? backgroundColorOfTileWithPreview(String previewText) {
        final tileFinder = find
            .ancestor(
              of: find.text(previewText),
              matching: find.byWidgetPredicate((widget) {
                if (widget is! Container) {
                  return false;
                }
                final decoration = widget.decoration;
                if (decoration is! BoxDecoration) {
                  return false;
                }
                return decoration.borderRadius == BorderRadius.circular(14);
              }),
            )
            .first;
        final container = tester.widget<Container>(tileFinder);
        final decoration = container.decoration as BoxDecoration;
        return decoration.color;
      }

      // 初始：两项均无高亮（透明）
      expect(backgroundColorOfTileWithPreview('preview-a'), Colors.transparent);
      expect(backgroundColorOfTileWithPreview('preview-b'), Colors.transparent);

      // 点击会话 A：A 高亮、B 仍为透明
      controller.lastTappedSessionId.value = 'session-a';
      await tester.pump();
      expect(
        backgroundColorOfTileWithPreview('preview-a'),
        AppTheme.primaryColor.withValues(alpha: 0.06),
      );
      expect(backgroundColorOfTileWithPreview('preview-b'), Colors.transparent);

      // 点击会话 B：高亮迁移到 B，A 取消高亮
      controller.lastTappedSessionId.value = 'session-b';
      await tester.pump();
      expect(backgroundColorOfTileWithPreview('preview-a'), Colors.transparent);
      expect(
        backgroundColorOfTileWithPreview('preview-b'),
        AppTheme.primaryColor.withValues(alpha: 0.06),
      );
    },
  );
}
