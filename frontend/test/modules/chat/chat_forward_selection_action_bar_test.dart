import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:grix/modules/chat/widgets/chat_forward_selection_action_bar.dart';

class _FakeImService extends ImService {
  @override
  void enterSession(String s, {Duration initialLoadDelay = Duration.zero}) {}
  @override
  void leaveSession([String? s]) {}
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
  Future<Map<String, dynamic>?> fetchSessionDetail(String s) async => {
    'session_type': 1,
    'member_count': 0,
    'members': const [],
  };
  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String s) async =>
      const SessionDetailResult(
        data: {'session_type': 1, 'member_count': 0, 'members': []},
      );
}

class _FakeOssService extends OssService {}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

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
  tearDown(Get.reset);

  testWidgets(
    'forward selection action bar renders buttons on-screen (regression 多选转发按钮消失)',
    (tester) async {
      tester.view.physicalSize = const Size(1320, 2868);
      tester.view.devicePixelRatio = 3.0;
      tester.view.padding = const FakeViewPadding(top: 177, bottom: 102);
      addTearDown(() {
        tester.view.resetPhysicalSize();
        tester.view.resetPadding();
        tester.view.resetDevicePixelRatio();
      });
      const screenH = 956.0;

      final imService = Get.find<ImService>();
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's1',
          type: 'private',
          peerId: 'peer',
          peerType: 1,
          peerNickname: 'Peer',
          updatedAt: 0,
          lastMessageTime: 0,
        ),
      ]);
      final msgs = List.generate(
        13,
        (i) => MessageModel(
          msgId: 'm$i',
          sessionId: 's1',
          senderId: i.isEven ? 'peer' : '1001',
          senderType: 1,
          content: 'message body number $i',
          createdAt: 1710000000000 + i * 1000,
        ),
      );
      imService.currentMessages.assignAll(msgs);

      final controller = Get.put(ChatController());
      controller.sessionId = 's1';
      controller.chatTitle = 'Peer';
      controller.chatType = 'private';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          theme: AppTheme.lightTheme,
          home: ChatView(),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      controller.beginForwardSelection(msgs.first);
      for (final m in msgs.skip(1)) {
        controller.toggleForwardMessageSelection(m);
      }
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      expect(find.byType(ChatForwardSelectionActionBar), findsOneWidget);

      // The two forward buttons must be present, laid out, and fully on-screen.
      for (final label in ['合并转发', '逐条转发']) {
        final f = find.text(label);
        expect(f, findsOneWidget, reason: '"$label" not found');
        final r = tester.getRect(f);
        expect(
          r.bottom <= screenH,
          isTrue,
          reason: '"$label" bottom ${r.bottom} beyond screen $screenH',
        );
      }
      final filled = find.byType(FilledButton);
      expect(filled, findsNWidgets(2));
    },
  );
}
