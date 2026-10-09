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
  String title = '',
  String peerId = '',
  String peerNickname = '',
  int unreadCount = 0,
  bool isPinned = false,
  bool friendIsPinned = false,
  String lastMessage = 'hi',
  int updatedAt = 1700000000000,
}) async {
  await LocalDb.upsertSession({
    'session_id': sessionId,
    'title': title,
    'type': type,
    'peer_id': peerId,
    'peer_type': type == 'private' ? 2 : 0,
    'peer_nickname': peerNickname,
    'peer_username': '',
    'updated_at': updatedAt,
    'is_pinned': isPinned,
    'is_muted': false,
    'pinned_at': isPinned ? updatedAt : 0,
    'friend_is_pinned': friendIsPinned,
    'friend_pinned_at': friendIsPinned ? updatedAt : 0,
    'unread_count': unreadCount,
    'last_message': lastMessage,
    'last_message_time': updatedAt,
  });
}

/// SQLite counts every inserted/updated/deleted row on this connection, so a
/// no-op refresh must leave the counter untouched.
Future<int> _totalRowWrites() async {
  final db = await LocalDb.database;
  final rows = await db.rawQuery('SELECT total_changes() AS c');
  return (rows.first['c'] as int?) ?? 0;
}

void main() {
  setUp(() async {
    Get.testMode = true;
    Get.reset();
    MessageStreamController.resetForTest();
    SharedPreferences.setMockInitialValues({});
    await LocalDb.initDatabaseFactory();
    final userId = 'summary-persist-${DateTime.now().microsecondsSinceEpoch}';
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

  test('conversation summary refresh makes the session locally searchable', () async {
    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);

    expect(await LocalDb.getSessionRecord('never-synced'), isNull);

    await service.persistConversationSummaryIdentities(const [
      ConversationSummaryModel(
        groupKey: 'private:agent-tuner',
        conversationType: 'private',
        latestSessionId: 'never-synced',
        peerId: 'agent-tuner',
        peerType: 2,
        peerNickname: '声波调音台',
        peerUsername: 'sonic_tuner',
        lastMsg: '已经把均衡曲线调平了',
        lastMsgTime: 1700000009000,
        updatedAt: 1700000009000,
      ),
    ]);

    final byNickname = await LocalDb.searchSessions(const ['声波调音台']);
    expect(byNickname.map((m) => m.sessionId), contains('never-synced'));

    final byUsername = await LocalDb.searchSessions(const ['sonic_tuner']);
    expect(byUsername.map((m) => m.sessionId), contains('never-synced'));

    final byLastMessage = await LocalDb.searchSessions(const ['均衡曲线']);
    expect(byLastMessage.map((m) => m.sessionId), contains('never-synced'));

    final row = await LocalDb.getSessionRecord('never-synced');
    expect(row?['type'], 'private');
    expect(row?['peer_id'], 'agent-tuner');
    expect(row?['updated_at'], 1700000009000);
  });

  test('group summary keeps its title searchable', () async {
    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);

    await service.persistConversationSummaryIdentities(const [
      ConversationSummaryModel(
        groupKey: 'group:g-1',
        conversationType: 'group',
        latestSessionId: 'group-never-synced',
        title: '周五复盘小组',
        sessionType: 2,
        lastMsg: '纪要已发',
        lastMsgTime: 1700000008000,
        updatedAt: 1700000008000,
      ),
    ]);

    final matched = await LocalDb.searchSessions(const ['复盘小组']);
    expect(matched.map((m) => m.sessionId), contains('group-never-synced'));
    final row = await LocalDb.getSessionRecord('group-never-synced');
    expect(row?['type'], 'group');
  });

  test(
    'summary peer repairs a local latest thread in memory and storage',
    () async {
      const sid = '3a9f37ef-39bb-4fa4-bbcc-527881770601';
      await _seedSession(
        sessionId: sid,
        type: 'private',
        title: '本地任务标题',
        unreadCount: 3,
        friendIsPinned: true,
        lastMessage: '更新的本地消息',
        updatedAt: 1700000009000,
      );
      await LocalDb.upsertSession({'session_id': sid, 'peer_type': 0});
      final service = _makeImService();
      await service.loadSessions(refreshFromServer: false);
      const summaries = [
        ConversationSummaryModel(
          groupKey: 'private:2:8001',
          conversationType: 'private',
          latestSessionId: sid,
          peerId: '8001',
          peerType: 2,
          peerNickname: '程序员A',
          peerUsername: 'programmer_a',
          updatedAt: 1700000001000,
        ),
      ];

      await service.persistConversationSummaryIdentities(summaries);

      final local = service.findSessionById(sid)!;
      expect(local.peerId, '8001');
      expect(local.peerType, 2);
      expect(local.peerNickname, '程序员A');
      expect(local.peerUsername, 'programmer_a');
      expect(local.title, '本地任务标题');
      expect(local.unreadCount, 3);
      expect(local.friendIsPinned, isTrue);
      expect(local.lastMessage, '更新的本地消息');
      expect(local.updatedAt, 1700000009000);
      final row = await LocalDb.getSessionRecord(sid);
      expect(row?['peer_id'], '8001');
      expect(row?['peer_type'], 2);

      // 数据库已经是正确身份、内存仍是旧占位时也必须修复，不能被 no-op 跳过。
      service.sessions[0] = local.copyWith(peerId: '', peerType: 0);
      final writes = await _totalRowWrites();
      await service.persistConversationSummaryIdentities(summaries);
      expect(service.findSessionById(sid)?.peerId, '8001');
      expect(await _totalRowWrites(), writes);
    },
  );

  test(
    'summary repairs peer type on older matching threads without guessing unknown peers',
    () async {
      await _seedSession(
        sessionId: 'older-thread',
        type: 'private',
        peerId: '8001',
        unreadCount: 2,
      );
      await LocalDb.upsertSession({
        'session_id': 'older-thread',
        'peer_type': 0,
      });
      await _seedSession(
        sessionId: 'unknown-thread',
        type: 'private',
        unreadCount: 1,
      );
      final service = _makeImService();
      await service.loadSessions(refreshFromServer: false);
      const summaries = [
        ConversationSummaryModel(
          groupKey: 'private:2:8001',
          conversationType: 'private',
          latestSessionId: 'latest-thread',
          peerId: '8001',
          peerType: 2,
          updatedAt: 1700000000000,
        ),
      ];

      await service.persistConversationSummaryIdentities(summaries);

      expect(service.findSessionById('older-thread')?.peerType, 2);
      expect(service.findSessionById('older-thread')?.unreadCount, 2);
      expect((await LocalDb.getSessionRecord('older-thread'))?['peer_type'], 2);
      expect(service.findSessionById('unknown-thread')?.peerId, isEmpty);
      final writes = await _totalRowWrites();
      await service.persistConversationSummaryIdentities(summaries);
      expect(await _totalRowWrites(), writes);
    },
  );

  test(
    'a peer id with conflicting summary types does not identify an older thread',
    () async {
      await _seedSession(
        sessionId: 'ambiguous-thread',
        type: 'private',
        peerId: '8001',
      );
      await LocalDb.upsertSession({
        'session_id': 'ambiguous-thread',
        'peer_type': 0,
      });
      final service = _makeImService();
      await service.loadSessions(refreshFromServer: false);

      await service.persistConversationSummaryIdentities(const [
        ConversationSummaryModel(
          groupKey: 'private:2:8001',
          conversationType: 'private',
          latestSessionId: 'agent-thread',
          peerId: '8001',
          peerType: 2,
        ),
        ConversationSummaryModel(
          groupKey: 'private:1:8001',
          conversationType: 'private',
          latestSessionId: 'user-thread',
          peerId: '8001',
          peerType: 1,
        ),
      ]);

      expect(service.findSessionById('ambiguous-thread')?.peerType, 0);
      expect(
        (await LocalDb.getSessionRecord('ambiguous-thread'))?['peer_type'],
        0,
      );
    },
  );

  test('identity upsert never overwrites unread_count or pin state', () async {
    await _seedSession(
      sessionId: 'private-keep',
      type: 'private',
      peerId: 'agent-keep',
      peerNickname: '旧昵称',
      unreadCount: 7,
      friendIsPinned: true,
    );
    await _seedSession(
      sessionId: 'group-keep',
      type: 'group',
      title: '旧标题',
      unreadCount: 3,
      isPinned: true,
    );

    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);

    await service.persistConversationSummaryIdentities(const [
      ConversationSummaryModel(
        groupKey: 'private:agent-keep',
        conversationType: 'private',
        latestSessionId: 'private-keep',
        peerId: 'agent-keep',
        peerType: 2,
        peerNickname: '新昵称',
        lastMsg: '新的一条',
        lastMsgTime: 1700000005000,
        updatedAt: 1700000005000,
        unread: 0,
        isPinned: false,
      ),
      ConversationSummaryModel(
        groupKey: 'group:group-keep',
        conversationType: 'group',
        latestSessionId: 'group-keep',
        title: '新标题',
        sessionType: 2,
        lastMsg: '群里的新消息',
        lastMsgTime: 1700000006000,
        updatedAt: 1700000006000,
        unread: 0,
        isPinned: false,
      ),
    ]);

    final privateRow = await LocalDb.getSessionRecord('private-keep');
    expect(privateRow?['unread_count'], 7);
    expect(privateRow?['friend_is_pinned'], 1);
    expect(privateRow?['friend_pinned_at'], 1700000000000);
    expect(privateRow?['peer_nickname'], '新昵称');
    expect(privateRow?['last_message'], '新的一条');

    final groupRow = await LocalDb.getSessionRecord('group-keep');
    expect(groupRow?['unread_count'], 3);
    expect(groupRow?['is_pinned'], 1);
    expect(groupRow?['pinned_at'], 1700000000000);
    expect(groupRow?['title'], '新标题');
  });

  test('repeated refresh is idempotent and writes nothing the second time', () async {
    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);

    const summaries = [
      ConversationSummaryModel(
        groupKey: 'private:agent-idem',
        conversationType: 'private',
        latestSessionId: 'idem-1',
        peerId: 'agent-idem',
        peerType: 2,
        peerNickname: '重复刷新',
        lastMsg: '同一条摘要',
        lastMsgTime: 1700000007000,
        updatedAt: 1700000007000,
      ),
    ];

    await service.persistConversationSummaryIdentities(summaries);
    final firstRow = await LocalDb.getSessionRecord('idem-1');
    final writesAfterFirst = await _totalRowWrites();

    await service.persistConversationSummaryIdentities(summaries);
    await service.persistConversationSummaryIdentities(summaries);

    final rows = await LocalDb.getSessions();
    expect(rows.where((r) => r['session_id'] == 'idem-1').length, 1);
    expect(await LocalDb.getSessionRecord('idem-1'), firstRow);
    expect(await _totalRowWrites(), writesAfterFirst);
  });

  test('empty summary title does not wipe the stored title', () async {
    await _seedSession(
      sessionId: 'titled',
      type: 'private',
      title: '本地已有标题',
      peerId: 'agent-titled',
    );

    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);

    await service.persistConversationSummaryIdentities(const [
      ConversationSummaryModel(
        groupKey: 'private:agent-titled',
        conversationType: 'private',
        latestSessionId: 'titled',
        peerId: 'agent-titled',
        peerType: 2,
        updatedAt: 1700000005000,
      ),
    ]);

    final row = await LocalDb.getSessionRecord('titled');
    expect(row?['title'], '本地已有标题');
  });

  test('a stale summary does not drag the last message backwards', () async {
    await _seedSession(
      sessionId: 'fresh-local',
      type: 'private',
      peerId: 'agent-fresh',
      lastMessage: '本地更新的最后一条',
      updatedAt: 1700000009000,
    );

    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);

    await service.persistConversationSummaryIdentities(const [
      ConversationSummaryModel(
        groupKey: 'private:agent-fresh',
        conversationType: 'private',
        latestSessionId: 'fresh-local',
        peerId: 'agent-fresh',
        peerNickname: '刷新到的昵称',
        peerType: 2,
        lastMsg: '服务端还没追上的旧摘要',
        lastMsgTime: 1700000001000,
        updatedAt: 1700000001000,
      ),
    ]);

    final row = await LocalDb.getSessionRecord('fresh-local');
    expect(row?['last_message'], '本地更新的最后一条');
    expect(row?['updated_at'], 1700000009000);
    // Pure identity fields still move forward.
    expect(row?['peer_nickname'], '刷新到的昵称');
  });

  test('locally deleted conversation is not resurrected by a summary', () async {
    await _seedSession(
      sessionId: 'deleted-1',
      type: 'private',
      peerId: 'agent-deleted',
      peerNickname: '已删会话',
    );

    final service = _makeImService();
    await service.loadSessions(refreshFromServer: false);
    await service.deleteConversation('deleted-1');
    expect(await LocalDb.getSessionRecord('deleted-1'), isNull);

    await service.persistConversationSummaryIdentities(const [
      ConversationSummaryModel(
        groupKey: 'private:agent-deleted',
        conversationType: 'private',
        latestSessionId: 'deleted-1',
        peerId: 'agent-deleted',
        peerType: 2,
        peerNickname: '已删会话',
        lastMsg: '不该被写回来',
        lastMsgTime: 1700000009000,
        updatedAt: 1700000009000,
      ),
    ]);

    expect(await LocalDb.getSessionRecord('deleted-1'), isNull);
    final matched = await LocalDb.searchSessions(const ['已删会话']);
    expect(matched.map((m) => m.sessionId), isNot(contains('deleted-1')));
  });
}
