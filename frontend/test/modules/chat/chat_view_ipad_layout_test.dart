import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
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
  Map<String, dynamic> get _detail => {
    'session_type': 1,
    'member_count': 0,
    'members': const [],
  };

  @override
  Future<Map<String, dynamic>?> fetchSessionDetail(String sessionId) async =>
      _detail;

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return SessionDetailResult(data: _detail);
  }
}

class _FakeOssService extends OssService {}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

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

  Future<void> pumpChatAt(WidgetTester tester, Size size) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    final imService = Get.find<ImService>();
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_ipad_layout_test',
        type: 'private',
        peerId: 'peer',
        peerType: 1,
        peerNickname: 'Peer User',
        updatedAt: 0,
        lastMessageTime: 0,
      ),
    ]);
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'peer-msg',
        sessionId: 'session_ipad_layout_test',
        senderId: 'peer',
        senderType: 1,
        content: 'Hello from peer',
        createdAt: 1710000000000,
      ),
      MessageModel(
        msgId: 'my-msg',
        sessionId: 'session_ipad_layout_test',
        senderId: '1001',
        senderType: 1,
        // 长内容：在最窄的 Stage Manager 宽度下最容易压出横向溢出。
        content:
            'Hello from me with a deliberately long single line of content '
            'so that the bubble has to wrap instead of overflowing.',
        createdAt: 1710000060000,
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_ipad_layout_test';
    controller.chatTitle = 'Peer User';
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
  }

  // iPad Pro 13 英寸的逻辑尺寸，以及 Stage Manager 能拖到的最窄宽度。
  const sizes = <String, Size>{
    'iPad portrait 1024x1366': Size(1024, 1366),
    'iPad landscape 1366x1024': Size(1366, 1024),
    'Stage Manager narrow 320x1024': Size(320, 1024),
  };

  sizes.forEach((name, size) {
    testWidgets('chat view lays out without overflow at $name', (tester) async {
      await pumpChatAt(tester, size);

      expect(find.byType(ChatView), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  });

  testWidgets('chat view survives a Stage Manager resize', (tester) async {
    await pumpChatAt(tester, const Size(1366, 1024));

    tester.view.physicalSize = const Size(320, 1024);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));
    expect(tester.takeException(), isNull);

    tester.view.physicalSize = const Size(1024, 1366);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));
    expect(tester.takeException(), isNull);
  });
}
