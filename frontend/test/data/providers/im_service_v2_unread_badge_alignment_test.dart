import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/conversation_summary_model.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  _FakeAuthService(this.userIdValue);

  final String userIdValue;

  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => userIdValue;

  @override
  String? get token => 'test_access_token';
}

class _FakeSessionService extends SessionService {
  @override
  bool get isInitialized => true;

  ConversationPageResult pageResult = const ConversationPageResult(
    success: false,
  );

  @override
  Future<ConversationPageResult> fetchConversationPage({
    int limit = 30,
    String cursor = '',
  }) async {
    return pageResult;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late String userId;
  late ImService imService;
  late _FakeSessionService sessionService;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    ConversationsController.useConversationListApiForTest = true;
    userId = 'unread_badge_${DateTime.now().microsecondsSinceEpoch}';
    SharedPreferences.setMockInitialValues({});
    Get.put<AuthService>(_FakeAuthService(userId));
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(userId);
    imService = ImService();
    Get.put<ImService>(imService);
    sessionService = _FakeSessionService();
    Get.put<SessionService>(sessionService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
  });

  tearDown(() async {
    ConversationsController.useConversationListApiForTest = null;
    imService.onClose();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  Future<int> _listBadgeTotal(ConversationsController controller) async {
    await controller.refreshSessionsOnPageVisible();
    return controller.groupedSessions.fold<int>(
      0,
      (sum, item) => sum + item.badgeUnreadCount,
    );
  }

  test(
    'v2 unread_set outside the list and for deleted sessions keep badge==list',
    () async {
      // Visible list sessions sum to 12. Ghost unread_set (+3) and deleted
      // unread_set (+3) must not push the app/tab badge to 15/18.
      await LocalDb.upsertSession({
        'session_id': 's-alice',
        'title': 'Alice',
        'type': 'private',
        'peer_id': '2001',
        'peer_type': 1,
        'unread_count': 5,
        'updated_at': 1700000000000,
        'last_message': 'a',
        'last_message_time': 1700000000000,
      });
      await LocalDb.upsertSession({
        'session_id': 's-bob',
        'title': 'Bob',
        'type': 'private',
        'peer_id': '2002',
        'peer_type': 1,
        'unread_count': 7,
        'updated_at': 1700000001000,
        'last_message': 'b',
        'last_message_time': 1700000001000,
      });
      await LocalDb.upsertSession({
        'session_id': 's-deleted',
        'title': 'Gone',
        'type': 'private',
        'peer_id': '2003',
        'peer_type': 1,
        'unread_count': 0,
        'updated_at': 1700000000000,
      });

      await imService.loadSessions(
        refreshFromServer: false,
        backfillMissingPeerIdentities: false,
      );
      await imService.deleteConversation('s-deleted');

      sessionService.pageResult = const ConversationPageResult(
        items: [
          ConversationSummaryModel(
            groupKey: 'private:1:2001',
            conversationType: 'private',
            latestSessionId: 's-alice',
            title: 'Alice',
            peerId: '2001',
            peerType: 1,
            peerNickname: 'Alice',
            lastMsg: 'a',
            unread: 5,
            badgeUnread: 5,
            latestActiveAt: 1700000000000,
            threadCount: 1,
          ),
          ConversationSummaryModel(
            groupKey: 'private:1:2002',
            conversationType: 'private',
            latestSessionId: 's-bob',
            title: 'Bob',
            peerId: '2002',
            peerType: 1,
            peerNickname: 'Bob',
            lastMsg: 'b',
            unread: 7,
            badgeUnread: 7,
            latestActiveAt: 1700000001000,
            threadCount: 1,
          ),
        ],
      );

      await LocalDb.prepareSyncGeneration('badge-align');
      await LocalDb.applySyncBatch({
        'generation': 'badge-align',
        'from_cursor': '0',
        'next_cursor': '2',
        'head_cursor': '2',
        'has_more': false,
        'events': [
          {
            'cursor': '1',
            'kind': 'session.unread_set',
            'entity_type': 'session_member',
            'entity_id': 'ghost-outside-list',
            'entity_version': '1',
            'payload': {
              'session_id': 'ghost-outside-list',
              'unread_count': 3,
            },
          },
          {
            'cursor': '2',
            'kind': 'session.unread_set',
            'entity_type': 'session_member',
            'entity_id': 's-deleted',
            'entity_version': '1',
            'payload': {'session_id': 's-deleted', 'unread_count': 3},
          },
        ],
        'final_state_snapshot': {
          'unread_by_session': {
            's-alice': 5,
            's-bob': 7,
            'ghost-outside-list': 3,
            's-deleted': 3,
          },
        },
      });

      await imService.loadSessions(
        refreshFromServer: false,
        backfillMissingPeerIdentities: false,
      );

      final controller = Get.put(ConversationsController());
      final listTotal = await _listBadgeTotal(controller);

      expect(imService.notificationUnread, 12);
      expect(listTotal, 12);
      expect(listTotal, imService.notificationUnread);
      expect(
        (await LocalDb.getSessions()).map((row) => row['session_id']).toSet(),
        {'s-alice', 's-bob'},
      );
    },
  );
}
