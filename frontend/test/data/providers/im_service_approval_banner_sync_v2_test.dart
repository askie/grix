import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/shared/services/in_app_notification_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late ImService imService;
  late InAppNotificationService notifications;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    Get.put<AuthService>(_FakeAuthService());
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(
      'approval_banner_${DateTime.now().microsecondsSinceEpoch}',
    );
    Get.put<SessionService>(SessionService());
    imService = ImService();
    Get.put<ImService>(imService);
    notifications = InAppNotificationService();
    Get.put<InAppNotificationService>(notifications);
    imService.setActiveSyncModeForTest('v2');
  });

  tearDown(() async {
    imService.onClose();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  test('live sync_v2 approval card raises the in-app banner', () async {
    imService.sessions.add(
      SessionModel(
        sessionId: 'approval-session',
        type: 'group',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    );

    await _applyApproval(
      imService,
      generation: 'banner-live',
      msgId: '2102404346213303001',
      sessionId: 'approval-session',
      senderId: '2002',
      command: 'npm test',
    );

    final notification = notifications.currentNotification.value;
    expect(notification, isNotNull);
    expect(notification!.sessionId, 'approval-session');
    expect(notification.sessionType, 'group');
    expect(notification.summary, 'npm test');
  });

  test('approval card in the open session does not raise a banner', () async {
    imService.setCurrentSessionForTest('approval-session');

    await _applyApproval(
      imService,
      generation: 'banner-current',
      msgId: '2102404346213303002',
      sessionId: 'approval-session',
      senderId: '2002',
      command: 'npm test',
    );

    expect(notifications.currentNotification.value, isNull);
  });

  test('own approval message does not raise a banner', () async {
    await _applyApproval(
      imService,
      generation: 'banner-own',
      msgId: '2102404346213303003',
      sessionId: 'approval-session',
      senderId: '1001',
      command: 'npm test',
    );

    expect(notifications.currentNotification.value, isNull);
  });

  test('catch-up replay does not raise a banner', () async {
    imService.setSyncV2CatchingUpForTest(true);

    await _applyApproval(
      imService,
      generation: 'banner-catchup',
      msgId: '2102404346213303004',
      sessionId: 'approval-session',
      senderId: '2002',
      command: 'npm test',
    );

    expect(notifications.currentNotification.value, isNull);
  });

  test(
    'replaying the same approval card does not raise the banner again',
    () async {
      await _applyApproval(
        imService,
        generation: 'banner-replay',
        msgId: '2102404346213303005',
        sessionId: 'approval-session',
        senderId: '2002',
        command: 'npm test',
      );
      notifications.dismiss();
      expect(notifications.currentNotification.value, isNull);

      await _applyApproval(
        imService,
        generation: 'banner-replay',
        msgId: '2102404346213303005',
        sessionId: 'approval-session',
        senderId: '2002',
        command: 'npm test',
        fromCursor: 1,
        nextCursor: 2,
      );

      expect(notifications.currentNotification.value, isNull);
    },
  );
}

Future<void> _applyApproval(
  ImService imService, {
  required String generation,
  required String msgId,
  required String sessionId,
  required String senderId,
  required String command,
  int fromCursor = 0,
  int? nextCursor,
}) async {
  final card = ChatMessageCardCodec.buildExecApprovalCard(
    approvalId: 'req-$msgId',
    approvalSlug: 'req-$msgId',
    approvalCommandId: 'req-$msgId',
    command: command,
    host: 'gateway',
  );
  await LocalDb.prepareSyncGeneration(generation);
  imService.setSyncV2GenerationForTest(generation);
  final cursor = nextCursor ?? fromCursor + 1;
  await imService.applySyncV2BatchForTest({
    'generation': generation,
    'from_cursor': '$fromCursor',
    'next_cursor': '$cursor',
    'head_cursor': '$cursor',
    'has_more': false,
    'events': [
      {
        'cursor': '$cursor',
        'kind': 'message.upsert',
        'entity_type': 'message',
        'entity_id': msgId,
        'entity_version': '1',
        'payload': {
          'msg_id': msgId,
          'session_id': sessionId,
          'sender_id': senderId,
          'sender_type': 2,
          'msg_type': 1,
          'content': card.content,
          'created_at': 1700000000000,
          'status': 'sent',
        },
      },
    ],
  });
}
