import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/bindings/chat_binding.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:grix/modules/chat/services/chat_message_window_owners.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Mirrors the shared-window contract of the real service: entering a session
/// takes the window over, leaving it clears the window only while it is still
/// the current one.
class _FakeImService extends ImService {
  final List<String> enterCalls = <String>[];
  final List<String?> leaveCalls = <String?>[];
  String? _current;

  @override
  String? get currentSessionId => _current;

  @override
  void enterSession(
    String sessionId, {
    Duration initialLoadDelay = Duration.zero,
  }) {
    enterCalls.add(sessionId);
    _current = sessionId;
  }

  @override
  void leaveSession([String? explicitSessionId]) {
    leaveCalls.add(explicitSessionId);
    if (_current == explicitSessionId) {
      _current = null;
    }
  }

  @override
  void connect(String wsUrl) {}

  @override
  void ensureConnected() {}

  @override
  void refreshDelegateStates() {}
}

class _FakeAuthService extends AuthService {
  String? id = '42';

  @override
  bool get isLoggedIn => id != null;

  @override
  String? get userId => id;
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

  late _FakeImService imService;
  late _FakeAuthService authService;

  String tagFor(String sessionId) =>
      ChatBinding.controllerTagForSession(sessionId);

  Future<ChatController> openChat(WidgetTester tester, String sessionId) async {
    final controller = Get.put<ChatController>(
      ChatController(
        routeArguments: <String, dynamic>{
          'session_id': sessionId,
          'title': sessionId,
          'type': 'private',
        },
      ),
      tag: tagFor(sessionId),
    );
    // onReady runs after the next frame and enters the session in a microtask;
    // pumping a widget is what schedules that frame in the test binding.
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    return controller;
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
    ChatMessageWindowOwners.resetForTest();
    SharedPreferences.setMockInitialValues({});
    imService = _FakeImService();
    authService = _FakeAuthService();
    Get.put<ImService>(imService);
    Get.put<AuthService>(authService);
    Get.put<AgentService>(_FakeAgentService());
    Get.put<SessionService>(_FakeSessionService());
    Get.put<OssService>(_FakeOssService());
  });

  tearDown(() async {
    Get.reset();
    ChatMessageWindowOwners.resetForTest();
  });

  testWidgets(
    'closing a chat opened on top hands the message window back to the '
    'previous live chat',
    (WidgetTester tester) async {
      final pane = await openChat(tester, 'pane');
      await openChat(tester, 'nested');
      expect(imService.enterCalls, ['pane', 'nested']);
      expect(imService.currentSessionId, 'nested');

      Get.delete<ChatController>(tag: tagFor('nested'));
      await tester.pump();

      expect(imService.leaveCalls, ['nested']);
      expect(imService.enterCalls, ['pane', 'nested', 'pane']);
      expect(imService.currentSessionId, 'pane');
      expect(pane.isClosed, isFalse);
    },
  );

  testWidgets(
    'closing a chat that no longer owns the window leaves the current chat '
    'alone',
    (WidgetTester tester) async {
      await openChat(tester, 'pane');
      await openChat(tester, 'nested');

      // Desktop pane switch: the replaced chat is deleted after the new one
      // took the window over.
      Get.delete<ChatController>(tag: tagFor('pane'));
      await tester.pump();

      expect(imService.leaveCalls, ['pane']);
      expect(imService.enterCalls, ['pane', 'nested']);
      expect(imService.currentSessionId, 'nested');
    },
  );

  testWidgets(
    'no extra re-entry when the previous chat already re-owned the window',
    (WidgetTester tester) async {
      await openChat(tester, 'pane');
      await openChat(tester, 'nested');

      // ChatRouteNavigator.toChat restores the previous chat as soon as the
      // nested route pops, before its controller is deleted.
      imService.enterSession('pane');
      Get.delete<ChatController>(tag: tagFor('nested'));
      await tester.pump();

      expect(imService.leaveCalls, ['nested']);
      expect(imService.enterCalls, ['pane', 'nested', 'pane']);
      expect(imService.currentSessionId, 'pane');
    },
  );

  testWidgets('closing the last chat does not enter anything', (
    WidgetTester tester,
  ) async {
    await openChat(tester, 'only');

    Get.delete<ChatController>(tag: tagFor('only'));
    await tester.pump();

    expect(imService.leaveCalls, ['only']);
    expect(imService.enterCalls, ['only']);
    expect(imService.currentSessionId, isNull);
  });

  testWidgets(
    'a chat whose session was removed before it closed still hands the '
    'window back',
    (WidgetTester tester) async {
      await openChat(tester, 'pane');
      await openChat(tester, 'nested');

      // ImService leaves a removed session itself, before the page closes.
      imService.leaveSession('nested');
      expect(imService.currentSessionId, isNull);

      Get.delete<ChatController>(tag: tagFor('nested'));
      await tester.pump();

      expect(imService.enterCalls, ['pane', 'nested', 'pane']);
      expect(imService.currentSessionId, 'pane');
    },
  );

  testWidgets('nothing is re-entered after the account signed out', (
    WidgetTester tester,
  ) async {
    await openChat(tester, 'pane');
    await openChat(tester, 'nested');

    authService.id = null;
    Get.delete<ChatController>(tag: tagFor('nested'));
    await tester.pump();

    expect(imService.enterCalls, ['pane', 'nested']);
    expect(imService.currentSessionId, isNull);
  });

  testWidgets('chats recorded for another account are not re-entered', (
    WidgetTester tester,
  ) async {
    await openChat(tester, 'pane');
    await openChat(tester, 'nested');

    authService.id = '7';
    Get.delete<ChatController>(tag: tagFor('nested'));
    await tester.pump();

    expect(imService.enterCalls, ['pane', 'nested']);
    expect(imService.currentSessionId, isNull);
  });

  testWidgets(
    'restoreSharedMessageWindow reloads the chat still on screen after a '
    'local data reset',
    (WidgetTester tester) async {
      await openChat(tester, 'pane');

      // A runtime reset clears the window without closing the page.
      imService.leaveSession('pane');
      expect(imService.currentSessionId, isNull);

      ChatController.restoreSharedMessageWindow();

      expect(imService.enterCalls, ['pane', 'pane']);
      expect(imService.currentSessionId, 'pane');

      // Still owned: a second call must not enter again.
      ChatController.restoreSharedMessageWindow();
      expect(imService.enterCalls, ['pane', 'pane']);
    },
  );
}
