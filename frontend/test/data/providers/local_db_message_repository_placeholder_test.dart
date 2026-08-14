import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // 验证：本地消息窗口查询必须排除 msg_type=4 流式占位行。
  // 这些占位（孤儿、内容为空）只可能来自历史接口下发，若被当作已封板消息
  // 渲染会显示为空白气泡。
  test(
    'local message window queries exclude msg_type=4 placeholders',
    () async {
      final userId =
          'placeholder-filter-${DateTime.now().microsecondsSinceEpoch}';
      const sessionId = 's-placeholder';
      await LocalDb.initDatabaseFactory();
      await LocalDb.setActiveUser(userId);
      try {
        await LocalDb.batchInsertMessages([
          {
            'msg_id': '1001',
            'session_id': sessionId,
            'sender_id': 'a1',
            'sender_type': 2,
            'msg_type': 1,
            'content': '已封板回复',
            'created_at': 1000,
          },
          {
            'msg_id': '1002',
            'session_id': sessionId,
            'sender_id': 'a1',
            'sender_type': 2,
            'msg_type': 4,
            'content': '',
            'created_at': 2000,
          },
          {
            'msg_id': '1003',
            'session_id': sessionId,
            'sender_id': 'a1',
            'sender_type': 2,
            'msg_type': 1,
            'content': '另一条已封板回复',
            'created_at': 3000,
          },
        ]);

        final latest = await LocalDb.getLatestMessages(sessionId, limit: 60);
        expect(latest.map((m) => m['msg_id']), ['1001', '1003']);

        final before = await LocalDb.getMessagesBefore(
          sessionId,
          beforeCreatedAt: 3000,
          beforeMsgId: '1003',
          limit: 20,
        );
        expect(before.map((m) => m['msg_id']), ['1001']);

        final after = await LocalDb.getMessagesAfter(
          sessionId,
          afterCreatedAt: 1000,
          afterMsgId: '1001',
          limit: 20,
        );
        expect(after.map((m) => m['msg_id']), ['1003']);

        final lastBySession = await LocalDb.getLastMessages();
        expect(lastBySession[sessionId]?['msg_id'], '1003');
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'batchUpsertMessages updates existing rows and inserts new rows',
    () async {
      final userId = 'batch-upsert-${DateTime.now().microsecondsSinceEpoch}';
      const sessionId = 's-batch-upsert';
      await LocalDb.initDatabaseFactory();
      await LocalDb.setActiveUser(userId);
      try {
        await LocalDb.batchInsertMessages([
          {
            'msg_id': 'edit-existing',
            'session_id': sessionId,
            'sender_id': 'u1',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'before',
            'created_at': 1000,
          },
        ]);

        await LocalDb.batchUpsertMessages([
          {'msg_id': 'edit-existing', 'content': 'after'},
          {
            'msg_id': 'edit-new',
            'session_id': sessionId,
            'sender_id': 'u1',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'inserted',
            'created_at': 2000,
          },
        ]);

        final updated = await LocalDb.getMessageByMsgId('edit-existing');
        expect(updated?['content'], 'after');
        expect(updated?['session_id'], sessionId);
        expect(updated?['created_at'], 1000000);

        final inserted = await LocalDb.getMessageByMsgId('edit-new');
        expect(inserted?['content'], 'inserted');
        expect(inserted?['session_id'], sessionId);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'batchUpsertMessages handles duplicate new ids without clearing fields',
    () async {
      final userId =
          'batch-upsert-dupe-${DateTime.now().microsecondsSinceEpoch}';
      const sessionId = 's-batch-upsert-dupe';
      await LocalDb.initDatabaseFactory();
      await LocalDb.setActiveUser(userId);
      try {
        await LocalDb.batchUpsertMessages([
          {
            'msg_id': 'edit-dupe-new',
            'session_id': sessionId,
            'sender_id': 'u1',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'first',
            'created_at': 2000,
          },
          {'msg_id': 'edit-dupe-new', 'content': 'second'},
        ]);

        final row = await LocalDb.getMessageByMsgId('edit-dupe-new');
        expect(row?['content'], 'second');
        expect(row?['session_id'], sessionId);
        expect(row?['sender_id'], 'u1');
        expect(row?['created_at'], 2000000);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test('batchUpsertMessages merges duplicate new ids before insert', () async {
    final userId =
        'batch-upsert-dupe-merge-${DateTime.now().microsecondsSinceEpoch}';
    const sessionId = 's-batch-upsert-dupe-merge';
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(userId);
    try {
      await LocalDb.batchUpsertMessages([
        {'msg_id': 'edit-dupe-new-reversed', 'content': 'first'},
        {
          'msg_id': 'edit-dupe-new-reversed',
          'session_id': sessionId,
          'sender_id': 'u1',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'second',
          'created_at': 3000,
        },
      ]);

      final row = await LocalDb.getMessageByMsgId('edit-dupe-new-reversed');
      expect(row?['content'], 'second');
      expect(row?['session_id'], sessionId);
      expect(row?['sender_id'], 'u1');
      expect(row?['created_at'], 3000000);
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });

  test(
    'batchUpsertMessages merges duplicate existing ids before update',
    () async {
      final userId =
          'batch-upsert-dupe-existing-${DateTime.now().microsecondsSinceEpoch}';
      const sessionId = 's-batch-upsert-dupe-existing';
      await LocalDb.initDatabaseFactory();
      await LocalDb.setActiveUser(userId);
      try {
        await LocalDb.batchInsertMessages([
          {
            'msg_id': 'edit-dupe-existing',
            'session_id': sessionId,
            'sender_id': 'u1',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'before',
            'created_at': 1000,
          },
        ]);

        await LocalDb.batchUpsertMessages([
          {'msg_id': 'edit-dupe-existing', 'content': 'after'},
          {
            'msg_id': 'edit-dupe-existing',
            'extra': {'edited': true},
          },
        ]);

        final row = await LocalDb.getMessageByMsgId('edit-dupe-existing');
        expect(row?['content'], 'after');
        expect(row?['extra'], '{"edited":true}');
        expect(row?['session_id'], sessionId);
        expect(row?['created_at'], 1000000);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );
}
