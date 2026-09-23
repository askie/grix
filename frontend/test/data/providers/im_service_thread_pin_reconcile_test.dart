import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';

Map<String, dynamic> _row(String sid, {bool pinned = false, int pinnedAt = 0}) {
  return {
    'session_id': sid,
    'title': sid,
    'type': 'private',
    'peer_id': 'agent-1',
    'peer_type': 2,
    'updated_at': 1700000000000,
    'is_pinned': pinned ? 1 : 0,
    'pinned_at': pinnedAt,
    'unread_count': 0,
  };
}

SessionModel _thread(String sid, {bool pinned = false, int pinnedAt = 0}) {
  return SessionModel(
    sessionId: sid,
    title: sid,
    type: 'private',
    peerId: 'agent-1',
    peerType: 2,
    updatedAt: 1700000000000,
    lastMessageTime: 1700000000000,
    isPinned: pinned,
    pinnedAt: pinnedAt,
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late String userId;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    userId = 'thread_pin_${DateTime.now().microsecondsSinceEpoch}';
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(userId);
  });

  tearDown(() async {
    Get.reset();
    await LocalDb.setActiveUser(null);
  });

  test('server thread rows overwrite stale local session pins', () async {
    await LocalDb.upsertSession(
      _row('stale-pinned', pinned: true, pinnedAt: 5),
    );
    await LocalDb.upsertSession(_row('missing-pin'));
    await LocalDb.upsertSession(_row('untouched', pinned: true, pinnedAt: 7));
    final service = ImService();
    service.sessions.value = [
      _thread('stale-pinned', pinned: true, pinnedAt: 5),
      _thread('missing-pin'),
      _thread('untouched', pinned: true, pinnedAt: 7),
    ];

    await service.reconcileSessionPinsFromThreads([
      _thread('stale-pinned'),
      _thread('missing-pin', pinned: true, pinnedAt: 1700000009000),
      _thread('unknown-session', pinned: true, pinnedAt: 1),
    ]);

    final byId = {for (final s in service.sessions) s.sessionId: s};
    expect(byId['stale-pinned']!.isPinned, isFalse);
    expect(byId['missing-pin']!.isPinned, isTrue);
    expect(byId['missing-pin']!.pinnedAt, 1700000009000);
    expect(byId['untouched']!.isPinned, isTrue);
    expect(byId.containsKey('unknown-session'), isFalse);

    final rows = {
      for (final r in await LocalDb.getSessions())
        r['session_id'].toString(): r,
    };
    expect(rows['stale-pinned']!['is_pinned'], 0);
    expect(rows['missing-pin']!['is_pinned'], 1);
    expect(rows['missing-pin']!['pinned_at'], 1700000009000);
    expect(rows['untouched']!['is_pinned'], 1);
  });
}
