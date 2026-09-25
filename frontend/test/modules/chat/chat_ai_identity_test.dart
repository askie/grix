import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/themes/app_theme.dart';
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
import 'package:shared_preferences/shared_preferences.dart';

class _FakeImService extends ImService {
  @override
  bool get hasOlderMessages => false;

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
    return {
      'session_type': 1,
      'member_count': 2,
      'members': const [],
    };
  }

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return SessionDetailResult(
      data: {
        'session_type': 1,
        'member_count': 2,
        'members': const [],
      },
    );
  }
}

class _FakeOssService extends OssService {}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('chatMessageShowsAiBadge', () {
    test('agent senderType marks AI', () {
      expect(
        chatMessageShowsAiBadge(senderType: 2, extra: const {}),
        isTrue,
      );
    });

    test('human sender without delegate_origin is not AI', () {
      expect(
        chatMessageShowsAiBadge(senderType: 1, extra: const {}),
        isFalse,
      );
    });

    test('delegate_origin on human sender marks AI', () {
      expect(
        chatMessageShowsAiBadge(
          senderType: 1,
          extra: const {'delegate_origin': true},
        ),
        isTrue,
      );
    });
  });

  group('shouldShowChatAiDisclaimer', () {
    test('shows once at earliest history for agent private chat', () {
      expect(
        shouldShowChatAiDisclaimer(
          hasOlderHistory: false,
          isLoadingOlderHistory: false,
          isAgentPrivateChat: true,
          isGroupChat: false,
        ),
        isTrue,
      );
    });

    test('shows for all group chats when history exhausted', () {
      expect(
        shouldShowChatAiDisclaimer(
          hasOlderHistory: false,
          isLoadingOlderHistory: false,
          isAgentPrivateChat: false,
          isGroupChat: true,
        ),
        isTrue,
      );
    });

    test('hides while older history remains or is loading', () {
      expect(
        shouldShowChatAiDisclaimer(
          hasOlderHistory: true,
          isLoadingOlderHistory: false,
          isAgentPrivateChat: true,
          isGroupChat: false,
        ),
        isFalse,
      );
      expect(
        shouldShowChatAiDisclaimer(
          hasOlderHistory: false,
          isLoadingOlderHistory: true,
          isAgentPrivateChat: true,
          isGroupChat: false,
        ),
        isFalse,
      );
    });

    test('hides for human private chat', () {
      expect(
        shouldShowChatAiDisclaimer(
          hasOlderHistory: false,
          isLoadingOlderHistory: false,
          isAgentPrivateChat: false,
          isGroupChat: false,
        ),
        isFalse,
      );
    });
  });

  group('AI badge widget', () {
    testWidgets('shows AI badge for agent and delegate_origin, not plain human', (
      tester,
    ) async {
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          theme: AppTheme.lightTheme,
          home: Scaffold(
            body: Column(
              children: [
                buildChatMessageBubbleWithAvatar(
                  bubble: const Text('agent'),
                  senderMeta: null,
                  isMine: false,
                  senderType: 2,
                  senderId: 'agent-1',
                  senderName: 'Agent',
                  senderAvatarUrl: '',
                  senderVisualSeed: 'agent-1',
                  showAvatar: true,
                  isAi: chatMessageShowsAiBadge(senderType: 2, extra: const {}),
                ),
                buildChatMessageBubbleWithAvatar(
                  bubble: const Text('delegate'),
                  senderMeta: null,
                  isMine: true,
                  senderType: 1,
                  senderId: '1001',
                  senderName: 'Owner',
                  senderAvatarUrl: '',
                  senderVisualSeed: '1001',
                  showAvatar: true,
                  isAi: chatMessageShowsAiBadge(
                    senderType: 1,
                    extra: const {'delegate_origin': true},
                  ),
                ),
                buildChatMessageBubbleWithAvatar(
                  bubble: const Text('human'),
                  senderMeta: null,
                  isMine: true,
                  senderType: 1,
                  senderId: '1001',
                  senderName: 'Owner',
                  senderAvatarUrl: '',
                  senderVisualSeed: '1001',
                  showAvatar: true,
                  isAi: chatMessageShowsAiBadge(senderType: 1, extra: const {}),
                ),
              ],
            ),
          ),
        ),
      );
      await tester.pump();

      expect(find.byKey(const Key('chat_ai_badge')), findsNWidgets(2));
      expect(find.text('AI'), findsNWidgets(2));
    });

    testWidgets('dark theme AI badge uses theme colors not hard-coded light', (
      tester,
    ) async {
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          theme: AppTheme.darkTheme,
          home: Scaffold(
            body: Center(
              child: buildChatMessageBubbleWithAvatar(
                bubble: const Text('hello from AI'),
                senderMeta: null,
                isMine: false,
                senderType: 2,
                senderId: 'agent-1',
                senderName: 'Agent',
                senderAvatarUrl: '',
                senderVisualSeed: 'agent-1',
                showAvatar: true,
                isAi: true,
              ),
            ),
          ),
        ),
      );
      await tester.pump();
      expect(find.byKey(const Key('chat_ai_badge')), findsOneWidget);

      final badge = tester.widget<Container>(
        find.byKey(const Key('chat_ai_badge')),
      );
      final decoration = badge.decoration! as BoxDecoration;
      final theme = AppTheme.darkTheme;
      expect(decoration.color, theme.colorScheme.primary);
    });
  });

  group('AI disclaimer in ChatView', () {
    Future<void> pumpAgentChat(
      WidgetTester tester, {
      required List<MessageModel> messages,
    }) async {
      final imService = Get.find<ImService>();
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_ai_identity',
          type: 'private',
          peerId: 'agent-1',
          peerType: 2,
          peerNickname: 'Agent Bot',
          updatedAt: 0,
          lastMessageTime: 0,
        ),
      ]);
      imService.currentMessages.assignAll(messages);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_ai_identity';
      controller.chatTitle = 'Agent Bot';
      controller.chatType = 'private';
      controller.setHistoryFlagsForTest(hasOlderHistory: false);

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          theme: AppTheme.lightTheme,
          home: ChatView(),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
      controller.setHistoryFlagsForTest(hasOlderHistory: false);
      await tester.pump();
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

    testWidgets('shows one-shot disclaimer at history top for agent private', (
      tester,
    ) async {
      await pumpAgentChat(
        tester,
        messages: [
          MessageModel(
            msgId: 'a1',
            sessionId: 'session_ai_identity',
            senderId: 'agent-1',
            senderType: 2,
            content: 'First agent reply',
            createdAt: 1710000000000,
          ),
          MessageModel(
            msgId: 'a2',
            sessionId: 'session_ai_identity',
            senderId: 'agent-1',
            senderType: 2,
            content: 'Second agent reply',
            createdAt: 1710000001000,
          ),
        ],
      );

      expect(find.byKey(const Key('chat_ai_disclaimer')), findsOneWidget);
      expect(find.text('内容由 AI 生成，可能有误，请注意核实'), findsOneWidget);
      // Consecutive agent messages: badge only on the first visible avatar.
      expect(find.byKey(const Key('chat_ai_badge')), findsOneWidget);
    });

    testWidgets('delegate_origin badges owner avatar; plain owner does not', (
      tester,
    ) async {
      await pumpAgentChat(
        tester,
        messages: [
          MessageModel(
            msgId: 'd1',
            sessionId: 'session_ai_identity',
            senderId: '1001',
            senderType: 1,
            content: 'Delegated reply',
            createdAt: 1710000000000,
            extra: const {'delegate_origin': true},
            status: 'sent',
          ),
          MessageModel(
            msgId: 'h1',
            sessionId: 'session_ai_identity',
            senderId: '1001',
            senderType: 1,
            content: 'Typed by owner',
            createdAt: 1710000002000,
            status: 'sent',
          ),
        ],
      );

      expect(find.byKey(const Key('chat_ai_badge')), findsOneWidget);
    });

    testWidgets('empty agent private chat shows disclaimer in empty state', (
      tester,
    ) async {
      await pumpAgentChat(tester, messages: const []);

      expect(find.byKey(const Key('chat_ai_disclaimer')), findsOneWidget);
      expect(find.text('内容由 AI 生成，可能有误，请注意核实'), findsOneWidget);
      expect(find.text('chat_empty'.tr), findsOneWidget);
    });

    // Group-chat disclaimer path is covered by shouldShowChatAiDisclaimer unit
    // tests above (always-on for groups). Full ChatView coverage lives in the
    // agent-private case; FakeSessionService session_type defaults can flip
    // chatType during onReady and are out of scope for this minimal test.
  });
}
