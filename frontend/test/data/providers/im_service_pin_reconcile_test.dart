import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/conversation_summary_model.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
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

  @override
  Future<void> logout({bool notifyServer = true}) async {}
}

final _trackedImServices = <ImService>[];

ImService _makeImService() {
  final service = ImService();
  _trackedImServices.add(service);
  return service;
}

Future<void> _seedSession({
  required String sessionId,
  required String type,
  String peerId = '',
  bool isPinned = false,
  bool friendIsPinned = false,
}) async {
  await LocalDb.upsertSession({
    'session_id': sessionId,
    'title': sessionId,
    'type': type,
    'peer_id': peerId,
    'peer_type': type == 'private' ? 2 : 0,
    'peer_nickname': '',
    'peer_username': '',
    'updated_at': 1700000000000,
    'is_pinned': isPinned,
    'is_muted': false,
    'pinned_at': isPinned ? 1700000000000 : 0,
    'friend_is_pinned': friendIsPinned,
    'friend_pinned_at': friendIsPinned ? 1700000000000 : 0,
    'unread_count': 0,
    'last_message': 'hi',
    'last_message_time': 1700000000000,
  });
}

void main() {
  setUp(() async {
    Get.testMode = true;
    Get.reset();
    MessageStreamController.resetForTest();
    SharedPreferences.setMockInitialValues({});
    await LocalDb.initDatabaseFactory();
    final userId = 'pin-reconcile-${DateTime.now().microsecondsSinceEpoch}';
    Get.put<AuthService>(_FakeAuthService(userId));
    await LocalDb.setActiveUser(userId);
  });

  tearDown(() async {
    for (final service in _trackedImServices.reversed) {
      service.onClose();
    }
    _trackedImServices.clear();
    await LocalDb.setActiveUser(null);
    MessageStreamController.resetForTest();
    Get.reset();
  });

  test('reconcile clears stale local friend pins once pin set is complete', () async {
    await _seedSession(
      sessionId: 'keep-agent',
      type: 'private',
      peerId: 'agent-keep',
      friendIsPinned: true,
    );
    await _seedSession(
      sessionId: 'stale-agent',
      type: 'private',
      peerId: 'agent-stale',
      friendIsPinned: true,
    );
    await _seedSession(
      sessionId: 'group-stale',
      type: 'group',
      isPinned: true,
    );

    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);

    expect(
      service.sessions.where((s) => s.friendIsPinned || s.isPinned).length,
      3,
    );

    await service.reconcilePinsFromConversationSummaries(
      const [
        ConversationSummaryModel(
          groupKey: 'private:agent-keep',
          conversationType: 'private',
          latestSessionId: 'keep-agent',
          peerId: 'agent-keep',
          peerType: 2,
          isPinned: true,
          pinnedAt: 1700000001000,
        ),
        ConversationSummaryModel(
          groupKey: 'private:other',
          conversationType: 'private',
          latestSessionId: 'other-unpinned',
          peerId: 'agent-other',
          peerType: 2,
          isPinned: false,
        ),
      ],
      hasMore: true,
    );

    final keep = service.sessions.firstWhere((s) => s.sessionId == 'keep-agent');
    final stale = service.sessions.firstWhere((s) => s.sessionId == 'stale-agent');
    final group = service.sessions.firstWhere((s) => s.sessionId == 'group-stale');
    expect(keep.friendIsPinned, isTrue);
    expect(stale.friendIsPinned, isFalse);
    expect(group.isPinned, isFalse);

    final keepRow = await LocalDb.getSessionRecord('keep-agent');
    final staleRow = await LocalDb.getSessionRecord('stale-agent');
    final groupRow = await LocalDb.getSessionRecord('group-stale');
    expect(keepRow?['friend_is_pinned'], 1);
    expect(staleRow?['friend_is_pinned'], 0);
    expect(groupRow?['is_pinned'], 0);
  });

  test('applyLocalFriendPin persists friend pin to LocalDb', () async {
    await _seedSession(
      sessionId: 'private-1',
      type: 'private',
      peerId: '1001',
    );
    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);

    await service.applyLocalFriendPin(
      sessionIds: const ['private-1'],
      isPinned: true,
      pinnedAt: 1700000002000,
    );

    expect(service.sessions.single.friendIsPinned, isTrue);
    final row = await LocalDb.getSessionRecord('private-1');
    expect(row?['friend_is_pinned'], 1);
    expect(row?['friend_pinned_at'], 1700000002000);
  });

  test('recent local friend pin override wins over stale conversations page', () async {
    await _seedSession(
      sessionId: 'private-fresh-pin',
      type: 'private',
      peerId: 'agent-fresh',
    );
    await _seedSession(
      sessionId: 'stale-other',
      type: 'private',
      peerId: 'agent-stale',
      friendIsPinned: true,
    );

    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);
    await service.applyLocalFriendPin(
      sessionIds: const ['private-fresh-pin'],
      isPinned: true,
      pinnedAt: 1700000004000,
    );

    await service.reconcilePinsFromConversationSummaries(
      const [
        ConversationSummaryModel(
          groupKey: 'private:agent-fresh',
          conversationType: 'private',
          latestSessionId: 'private-fresh-pin',
          peerId: 'agent-fresh',
          peerType: 2,
          isPinned: false,
        ),
        ConversationSummaryModel(
          groupKey: 'private:agent-other',
          conversationType: 'private',
          latestSessionId: 'other-unpinned',
          peerId: 'agent-other',
          peerType: 2,
          isPinned: false,
        ),
      ],
      hasMore: false,
    );

    final fresh = service.sessions.firstWhere(
      (s) => s.sessionId == 'private-fresh-pin',
    );
    final stale = service.sessions.firstWhere(
      (s) => s.sessionId == 'stale-other',
    );
    expect(fresh.friendIsPinned, isTrue);
    expect(stale.friendIsPinned, isFalse);

    final freshRow = await LocalDb.getSessionRecord('private-fresh-pin');
    final staleRow = await LocalDb.getSessionRecord('stale-other');
    expect(freshRow?['friend_is_pinned'], 1);
    expect(freshRow?['friend_pinned_at'], 1700000004000);
    expect(staleRow?['friend_is_pinned'], 0);
  });

  test('reconcile does not clear outside pins while first page is all pinned', () async {
    await _seedSession(
      sessionId: 'pin-a',
      type: 'group',
      isPinned: true,
    );
    await _seedSession(
      sessionId: 'pin-b',
      type: 'group',
      isPinned: true,
    );

    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);

    await service.reconcilePinsFromConversationSummaries(
      const [
        ConversationSummaryModel(
          groupKey: 'group:pin-a',
          conversationType: 'group',
          latestSessionId: 'pin-a',
          sessionType: 2,
          isPinned: true,
          pinnedAt: 1700000003000,
        ),
      ],
      hasMore: true,
    );

    final a = service.sessions.firstWhere((s) => s.sessionId == 'pin-a');
    final b = service.sessions.firstWhere((s) => s.sessionId == 'pin-b');
    expect(a.isPinned, isTrue);
    // First page is still all-pinned and hasMore, so pin-b may continue on
    // later pages — do not treat it as stale yet.
    expect(b.isPinned, isTrue);
  });

  test('reconcile skips LocalDb writes when pin state already matches', () async {
    await _seedSession(
      sessionId: 'group-already',
      type: 'group',
      isPinned: true,
    );
    await _seedSession(
      sessionId: 'private-already',
      type: 'private',
      peerId: 'agent-already',
      friendIsPinned: true,
    );

    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);
    final beforeGroup = await LocalDb.getSessionRecord('group-already');
    final beforePrivate = await LocalDb.getSessionRecord('private-already');

    await service.reconcilePinsFromConversationSummaries(
      const [
        ConversationSummaryModel(
          groupKey: 'group:group-already',
          conversationType: 'group',
          latestSessionId: 'group-already',
          sessionType: 2,
          isPinned: true,
          pinnedAt: 1700000000000,
        ),
        ConversationSummaryModel(
          groupKey: 'private:agent-already',
          conversationType: 'private',
          latestSessionId: 'private-already',
          peerId: 'agent-already',
          peerType: 2,
          isPinned: true,
          pinnedAt: 1700000000000,
        ),
        ConversationSummaryModel(
          groupKey: 'group:unpinned-tail',
          conversationType: 'group',
          latestSessionId: 'unpinned-tail',
          sessionType: 2,
          isPinned: false,
        ),
      ],
      hasMore: false,
    );

    final afterGroup = await LocalDb.getSessionRecord('group-already');
    final afterPrivate = await LocalDb.getSessionRecord('private-already');
    expect(afterGroup?['is_pinned'], beforeGroup?['is_pinned']);
    expect(afterGroup?['pinned_at'], beforeGroup?['pinned_at']);
    expect(afterPrivate?['friend_is_pinned'], beforePrivate?['friend_is_pinned']);
    expect(
      afterPrivate?['friend_pinned_at'],
      beforePrivate?['friend_pinned_at'],
    );
    expect(
      service.sessions.firstWhere((s) => s.sessionId == 'group-already').isPinned,
      isTrue,
    );
    expect(
      service.sessions
          .firstWhere((s) => s.sessionId == 'private-already')
          .friendIsPinned,
      isTrue,
    );
  });

  test('reconcile preserves private session-level pins', () async {
    await _seedSession(
      sessionId: 'private-session-pin',
      type: 'private',
      peerId: 'agent-sp',
      isPinned: true,
    );

    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);

    await service.reconcilePinsFromConversationSummaries(
      const [
        ConversationSummaryModel(
          groupKey: 'private:agent-sp',
          conversationType: 'private',
          latestSessionId: 'private-session-pin',
          peerId: 'agent-sp',
          peerType: 2,
          isPinned: false,
        ),
      ],
      hasMore: false,
    );

    // Friend-level pin is off, but the session-level pin is owned by the
    // series list / thread popup and must survive reconcile.
    final session = service.sessions.firstWhere(
      (s) => s.sessionId == 'private-session-pin',
    );
    expect(session.friendIsPinned, isFalse);
    expect(session.isPinned, isTrue);

    final row = await LocalDb.getSessionRecord('private-session-pin');
    expect(row?['friend_is_pinned'], 0);
    expect(row?['is_pinned'], 1);
  });

  test('pin override clears once isPinned matches even when pinnedAt differs',
      () async {
    await _seedSession(
      sessionId: 'private-override',
      type: 'private',
      peerId: 'agent-override',
    );

    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);
    // Local override stamps pinnedAt with the device millisecond clock.
    await service.applyLocalFriendPin(
      sessionIds: const ['private-override'],
      isPinned: true,
      pinnedAt: 1700000004123,
    );

    // Server confirms the pin with its own (second-level x1000) timestamp.
    await service.reconcilePinsFromConversationSummaries(
      const [
        ConversationSummaryModel(
          groupKey: 'private:agent-override',
          conversationType: 'private',
          latestSessionId: 'private-override',
          peerId: 'agent-override',
          peerType: 2,
          isPinned: true,
          pinnedAt: 1700000004000,
        ),
      ],
      hasMore: false,
    );

    final session = service.sessions.firstWhere(
      (s) => s.sessionId == 'private-override',
    );
    expect(session.friendIsPinned, isTrue);
    // Override cleared: the server pinnedAt replaced the local one.
    expect(session.friendPinnedAt, 1700000004000);

    final row = await LocalDb.getSessionRecord('private-override');
    expect(row?['friend_is_pinned'], 1);
    expect(row?['friend_pinned_at'], 1700000004000);
  });
}
