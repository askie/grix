import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/shared/models/session_avatar_member.dart';

/// batchApplySessionDeltas 的行为契约。
/// 回归背景：af943cb5 曾往不存在的列 raw_unread_inc 写值，导致"批次里带
/// 未读增量的已存在会话"触发整个事务回滚——该批所有会话的预览/时间/未读
/// 全部静默丢失，长期靠会话列表整刷兜底。本组用例锁死修复后的正确行为，
/// 尤其是带未读增量的更新路径必须真实落库。
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<Map<String, dynamic>?> sessionRow(String sid) async {
    final sessions = await LocalDb.getSessions();
    for (final s in sessions) {
      if (s['session_id']?.toString() == sid) return Map.of(s);
    }
    return null;
  }

  setUp(() async {
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(
      'deltas-${DateTime.now().microsecondsSinceEpoch}',
    );
  });

  tearDown(() async {
    await LocalDb.setActiveUser(null);
  });

  test('已存在会话 + 未读增量：预览/时间/未读全部落库（回归 raw_unread_inc）', () async {
    await LocalDb.upsertSession({
      'session_id': 's1',
      'title': 't',
      'type': 'private',
      'peer_id': '',
      'peer_type': 0,
      'peer_nickname': '',
      'peer_username': '',
      'updated_at': 1000,
      'is_pinned': false,
      'is_muted': false,
      'pinned_at': 0,
      'unread_count': 2,
      'last_message': 'old',
      'last_message_time': 1000,
    });

    await LocalDb.batchApplySessionDeltas(
      {
        's1': {'last_content': 'new', 'last_created_at': 2000, 'unread_inc': 3},
      },
      {'s1': 'private'},
    );

    final row = await sessionRow('s1');
    expect(row, isNotNull);
    expect(row!['last_message'], 'new');
    expect(row['last_message_time'], 2000);
    expect(row['updated_at'], 2000);
    expect(row['unread_count'], 5, reason: '2 + 增量 3');
  });

  test('同批多个会话，其中一个带未读增量：互不拖累全部落库', () async {
    await LocalDb.upsertSession({
      'session_id': 's1',
      'title': '',
      'type': 'private',
      'peer_id': '',
      'peer_type': 0,
      'peer_nickname': '',
      'peer_username': '',
      'updated_at': 1000,
      'is_pinned': false,
      'is_muted': false,
      'pinned_at': 0,
      'unread_count': 0,
      'last_message': 'old-1',
      'last_message_time': 1000,
    });

    await LocalDb.batchApplySessionDeltas(
      {
        's1': {'last_content': 'new-1', 'last_created_at': 2000, 'unread_inc': 1},
        's2': {'last_content': 'new-2', 'last_created_at': 3000, 'unread_inc': 4},
      },
      {'s1': 'private', 's2': 'group'},
    );

    final r1 = await sessionRow('s1');
    expect(r1?['last_message'], 'new-1');
    expect(r1?['unread_count'], 1);

    // s2 不存在 → 走插入路径，带上未读
    final r2 = await sessionRow('s2');
    expect(r2, isNotNull);
    expect(r2!['last_message'], 'new-2');
    expect(r2['unread_count'], 4);
    expect(r2['type'], 'group');
  });

  test('无未读增量：只更新预览与时间，未读不变', () async {
    await LocalDb.upsertSession({
      'session_id': 's1',
      'title': '',
      'type': 'private',
      'peer_id': '',
      'peer_type': 0,
      'peer_nickname': '',
      'peer_username': '',
      'updated_at': 1000,
      'is_pinned': false,
      'is_muted': false,
      'pinned_at': 0,
      'unread_count': 7,
      'last_message': 'old',
      'last_message_time': 1000,
    });

    await LocalDb.batchApplySessionDeltas(
      {
        's1': {'last_content': 'new', 'last_created_at': 2000, 'unread_inc': 0},
      },
      {'s1': 'private'},
    );

    final row = await sessionRow('s1');
    expect(row?['last_message'], 'new');
    expect(row?['unread_count'], 7);
  });

  test('last_content 为空（纯卡片批次）：保留原预览文本，仅推进时间', () async {
    await LocalDb.upsertSession({
      'session_id': 's1',
      'title': '',
      'type': 'private',
      'peer_id': '',
      'peer_type': 0,
      'peer_nickname': '',
      'peer_username': '',
      'updated_at': 1000,
      'is_pinned': false,
      'is_muted': false,
      'pinned_at': 0,
      'unread_count': 0,
      'last_message': 'keep-me',
      'last_message_time': 1000,
    });

    await LocalDb.batchApplySessionDeltas(
      {
        's1': {'last_content': '', 'last_created_at': 2000, 'unread_inc': 1},
      },
      {'s1': 'private'},
    );

    final row = await sessionRow('s1');
    expect(row?['last_message'], 'keep-me');
    expect(row?['last_message_time'], 2000);
    expect(row?['unread_count'], 1);
  });

  test('delta 带对端身份：新建会话直接落 peer_id/peer_type', () async {
    await LocalDb.batchApplySessionDeltas(
      {
        's-peer': {
          'last_content': 'hi',
          'last_created_at': 2000,
          'unread_inc': 1,
          'peer_id': '8001',
          'peer_type': 1,
        },
      },
      {'s-peer': 'private'},
    );

    final row = await sessionRow('s-peer');
    expect(row, isNotNull);
    expect(row!['peer_id'].toString(), '8001');
    expect(row['peer_type'], 1);
  });

  test('delta 带对端身份：补缺不覆盖已有 peer', () async {
    // 现有记录 peer 为空（历史占位行）→ 补齐
    await LocalDb.upsertSession({
      'session_id': 's-peerless',
      'title': '',
      'type': 'private',
      'peer_id': '',
      'peer_type': 0,
      'peer_nickname': '',
      'peer_username': '',
      'updated_at': 1000,
      'is_pinned': false,
      'is_muted': false,
      'pinned_at': 0,
      'unread_count': 1,
      'last_message': 'old',
      'last_message_time': 1000,
    });
    await LocalDb.upsertSession({
      'session_id': 's-has-peer',
      'title': '',
      'type': 'private',
      'peer_id': '7001',
      'peer_type': 1,
      'peer_nickname': '',
      'peer_username': '',
      'updated_at': 1000,
      'is_pinned': false,
      'is_muted': false,
      'pinned_at': 0,
      'unread_count': 1,
      'last_message': 'old',
      'last_message_time': 1000,
    });

    await LocalDb.batchApplySessionDeltas(
      {
        's-peerless': {
          'last_content': 'new',
          'last_created_at': 2000,
          'unread_inc': 1,
          'peer_id': '8002',
          'peer_type': 2,
        },
        's-has-peer': {
          'last_content': 'new',
          'last_created_at': 2000,
          'unread_inc': 1,
          'peer_id': '9999',
          'peer_type': 2,
        },
      },
      {'s-peerless': 'private', 's-has-peer': 'private'},
    );

    final peerless = await sessionRow('s-peerless');
    expect(peerless?['peer_id'].toString(), '8002');
    expect(peerless?['peer_type'], 2);

    final hasPeer = await sessionRow('s-has-peer');
    expect(hasPeer?['peer_id'].toString(), '7001', reason: '已有 peer 不被覆盖');
    expect(hasPeer?['peer_type'], 1);
  });

  test('群头像缓存只更新已有会话，不创建空会话行', () async {
    await LocalDb.upsertSessionGroupAvatarMembers('missing-group', const [
      SessionAvatarMember(
        memberId: '1001',
        memberType: 1,
        displayName: 'A',
        avatarUrl: 'https://example.com/a.png',
      ),
    ]);

    expect(await sessionRow('missing-group'), isNull);
  });

  test('群头像缓存更新保留已有会话字段', () async {
    await LocalDb.upsertSession({
      'session_id': 'group-avatar-existing',
      'title': '真实群名',
      'type': 'group',
      'peer_id': '',
      'peer_type': 0,
      'peer_nickname': '',
      'peer_username': '',
      'updated_at': 1000,
      'is_pinned': true,
      'is_muted': true,
      'pinned_at': 900,
      'unread_count': 3,
      'last_message': 'keep preview',
      'last_message_time': 1000,
    });

    await LocalDb.upsertSessionGroupAvatarMembers(
      'group-avatar-existing',
      const [
        SessionAvatarMember(
          memberId: '1001',
          memberType: 1,
          displayName: '成员A',
          avatarUrl: 'https://example.com/a.png',
        ),
      ],
    );

    final row = await sessionRow('group-avatar-existing');
    expect(row, isNotNull);
    expect(row!['title'], '真实群名');
    expect(row['last_message'], 'keep preview');
    expect(row['unread_count'], 3);
    expect(row['is_pinned'], 1);
    expect(row['is_muted'], 1);
    expect(row['group_avatar_members']?.toString(), contains('成员A'));
  });
}
