import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<int> totalChanges() async {
    final db = await LocalDb.database;
    final rows = await db.rawQuery('SELECT total_changes()');
    return rows.single.values.single as int;
  }

  test(
    'message write result distinguishes unavailable database from no-op',
    () async {
      await LocalDb.setActiveUser(null);

      final result = await LocalDb.batchInsertMessagesWithResult([
        {
          'msg_id': 'not-persisted',
          'session_id': 'missing-db',
          'content': 'must not advance cursor',
        },
      ]);

      expect(result.persisted, isFalse);
      expect(result.hasChanges, isFalse);
    },
  );

  test(
    'message writes skip rows whose persisted values are unchanged',
    () async {
      final userId =
          'message-write-dedup-${DateTime.now().microsecondsSinceEpoch}';
      await LocalDb.initDatabaseFactory();
      await LocalDb.setActiveUser(userId);
      try {
        final message = <String, dynamic>{
          'msg_id': 'dedup-message',
          'session_id': 'dedup-session',
          'sender_id': 'u1',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'unchanged',
          'extra': {'source': 'sync'},
          'inbox_seq': 42,
          'created_at': 1000,
        };

        final inserted = await LocalDb.batchInsertMessagesWithResult([message]);
        expect(inserted.insertedRows, hasLength(1));
        expect(inserted.updatedRows, isEmpty);
        expect(inserted.unchangedCount, 0);

        var before = await totalChanges();
        final unchanged = await LocalDb.batchInsertMessagesWithResult([
          message,
        ]);
        expect(await totalChanges(), before);
        expect(unchanged.hasChanges, isFalse);
        expect(unchanged.unchangedCount, 1);

        before = await totalChanges();
        final unchangedPartial = await LocalDb.batchUpsertMessagesWithResult([
          {'msg_id': 'dedup-message', 'content': 'unchanged'},
        ]);
        expect(await totalChanges(), before);
        expect(unchangedPartial.hasChanges, isFalse);
        expect(unchangedPartial.unchangedCount, 1);

        before = await totalChanges();
        await LocalDb.upsertMessage({
          'msg_id': 'dedup-message',
          'content': 'unchanged',
        });
        expect(await totalChanges(), before);

        before = await totalChanges();
        await LocalDb.upsertMessage({
          'msg_id': 'dedup-message',
          'content': null,
        });
        expect(await totalChanges(), before + 1);

        before = await totalChanges();
        await LocalDb.upsertMessage({
          'msg_id': 'dedup-message',
          'content': null,
        });
        expect(await totalChanges(), before);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'message writes update only changed values and preserve other fields',
    () async {
      final userId =
          'message-write-diff-${DateTime.now().microsecondsSinceEpoch}';
      await LocalDb.initDatabaseFactory();
      await LocalDb.setActiveUser(userId);
      try {
        await LocalDb.batchInsertMessages([
          {
            'msg_id': 'changed-message',
            'session_id': 'changed-session',
            'sender_id': 'u1',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'before',
            'created_at': 1000,
          },
        ]);

        final before = await totalChanges();
        final updated = await LocalDb.batchInsertMessagesWithResult([
          {'msg_id': 'changed-message', 'content': 'after'},
        ]);
        expect(await totalChanges(), before + 1);
        expect(updated.insertedRows, isEmpty);
        expect(updated.updatedRows, hasLength(1));

        final row = await LocalDb.getMessageByMsgId('changed-message');
        expect(row?['content'], 'after');
        expect(row?['session_id'], 'changed-session');
        expect(row?['sender_id'], 'u1');
        expect(row?['created_at'], 1000000);

        final beforeDuplicateBatch = await totalChanges();
        await LocalDb.batchInsertMessages([
          {
            'msg_id': 'duplicate-in-batch',
            'session_id': 'changed-session',
            'sender_id': 'u1',
            'content': 'first',
            'created_at': 2000,
          },
          {'msg_id': 'duplicate-in-batch', 'content': 'second'},
        ]);
        expect(await totalChanges(), beforeDuplicateBatch + 1);

        final coalesced = await LocalDb.getMessageByMsgId('duplicate-in-batch');
        expect(coalesced?['content'], 'second');
        expect(coalesced?['session_id'], 'changed-session');
        expect(coalesced?['created_at'], 2000000);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );
}
