import 'package:flutter_test/flutter_test.dart';

import 'package:grix/data/providers/local_db.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late String userId;

  setUp(() async {
    userId = 'sync_v2_${DateTime.now().microsecondsSinceEpoch}';
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(userId);
  });

  tearDown(() async {
    await LocalDb.setActiveUser(null);
  });

  test('v19 creates transactional sync and outbox tables', () async {
    final db = await LocalDb.database;
    final rows = await db.rawQuery(
      "SELECT name FROM sqlite_master WHERE type = 'table'",
    );
    final names = rows.map((row) => row['name']?.toString()).toSet();

    expect(names, contains('sync_state'));
    expect(names, contains('sync_entity_versions'));
    expect(names, contains('outbox'));
    expect(names, contains('account_counters'));
    expect(names, contains('sync_writer_lease'));
  });

  test('bootstrap establishes the transactional event cursor', () async {
    await LocalDb.prepareSyncGeneration('bootstrap-generation');
    await LocalDb.markSyncBootstrapComplete(1700000000, committedCursor: 87);

    final state = await LocalDb.getSyncState();
    expect(state.bootstrapCursor, 1700000000);
    expect(state.committedCursor, 87);
    expect(state.serverHeadCursor, 87);
  });

  test('zero-progress batch still applies its final unread snapshot', () async {
    await LocalDb.upsertSession({
      'session_id': 'snapshot-only-session',
      'title': 'Snapshot only',
      'type': 'group',
      'unread_count': 4,
      'updated_at': 1700000000000,
    });
    await LocalDb.prepareSyncGeneration('snapshot-only-generation');

    final result = await LocalDb.applySyncBatch({
      'generation': 'snapshot-only-generation',
      'from_cursor': '0',
      'next_cursor': '0',
      'head_cursor': '0',
      'has_more': false,
      'events': const <Map<String, dynamic>>[],
      'final_state_snapshot': {
        'unread_by_session': {'snapshot-only-session': 1},
      },
    });

    expect(result.persisted, isTrue);
    expect(result.committedCursor, 0);
    final session = (await LocalDb.getSessions()).singleWhere(
      (row) => row['session_id'] == 'snapshot-only-session',
    );
    expect(session['unread_count'], 1);
  });

  test(
    'batch reducer commits projections cursor and replay is a no-op',
    () async {
      await LocalDb.prepareSyncGeneration('generation-1');
      final result = await LocalDb.applySyncBatch({
        'generation': 'generation-1',
        'from_cursor': '0',
        'next_cursor': '3',
        'head_cursor': '3',
        'has_more': false,
        'events': [
          {
            'cursor': '1',
            'kind': 'message.upsert',
            'entity_type': 'message',
            'entity_id': '101',
            'entity_version': '1',
            'payload': {
              'msg_id': '101',
              'session_id': 'session-1',
              'sender_id': '8',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'hello',
              'extra': <String, dynamic>{},
              'created_at': '2026-09-22T00:00:00Z',
            },
          },
          {
            'cursor': '2',
            'kind': 'session.upsert',
            'entity_type': 'session',
            'entity_id': 'session-1',
            'entity_version': '1',
            'payload': {
              'session_id': 'session-1',
              'session_type': 2,
              'group_name': 'Group',
              'last_msg_summary': 'hello',
              'updated_at': '2026-09-22T00:00:00Z',
            },
          },
          {
            'cursor': '3',
            'kind': 'session.unread_set',
            'entity_type': 'session_member',
            'entity_id': 'session-1',
            'entity_version': '4',
            'payload': {'session_id': 'session-1', 'unread_count': 7},
          },
        ],
        'final_state_snapshot': {
          'unread_by_session': {'session-1': 7},
        },
      });

      expect(result.persisted, isTrue);
      expect(result.committedCursor, 3);
      expect(result.changedMessageRows, hasLength(1));
      final messages = await LocalDb.getLatestMessages('session-1');
      final sessions = await LocalDb.getSessions();
      final state = await LocalDb.getSyncState();
      final counters = await LocalDb.getAccountCounters();
      expect(messages.single['content'], 'hello');
      expect(sessions.single['title'], 'Group');
      expect(sessions.single['unread_count'], 7);
      expect(state.committedCursor, 3);
      expect(counters['total_unread'], 7);
      expect(counters['notification_unread'], 7);

      final replay = await LocalDb.applySyncBatch({
        'generation': 'generation-1',
        'from_cursor': '0',
        'next_cursor': '3',
        'head_cursor': '3',
        'has_more': false,
        'events': const [],
      });
      expect(replay.committedCursor, 3);
      expect(replay.hasChanges, isFalse);
      expect((await LocalDb.getLatestMessages('session-1')), hasLength(1));
    },
  );

  test('newer tombstone blocks stale history resurrection', () async {
    await LocalDb.prepareSyncGeneration('generation-2');
    await LocalDb.applySyncBatch(
      _singleMessageBatch(
        generation: 'generation-2',
        from: 0,
        next: 1,
        version: 2,
        content: 'new',
      ),
    );
    await LocalDb.applySyncBatch({
      'generation': 'generation-2',
      'from_cursor': '1',
      'next_cursor': '2',
      'head_cursor': '2',
      'has_more': false,
      'events': [
        {
          'cursor': '2',
          'kind': 'message.revoke',
          'entity_type': 'message',
          'entity_id': '201',
          'entity_version': '3',
          'tombstone': true,
          'payload': {'msg_id': '201', 'session_id': 'session-2'},
        },
      ],
    });
    final stale = await LocalDb.applySyncBatch(
      _singleMessageBatch(
        generation: 'generation-2',
        from: 2,
        next: 3,
        version: 2,
        content: 'stale history',
      ),
    );

    expect(stale.hasChanges, isFalse);
    expect(await LocalDb.getLatestMessages('session-2'), isEmpty);
    expect((await LocalDb.getSyncState()).committedCursor, 3);
  });

  test('archive pages obey realtime versions and tombstones', () async {
    await LocalDb.prepareSyncGeneration('generation-archive');
    await LocalDb.applySyncBatch(
      _singleMessageBatch(
        generation: 'generation-archive',
        from: 0,
        next: 1,
        version: 3,
        content: 'newest edit',
      ),
    );

    final staleEdit = await LocalDb.applyArchiveMessages([
      {
        'msg_id': '201',
        'session_id': 'session-2',
        'sender_id': '8',
        'msg_type': 1,
        'content': 'stale archive',
        'state_version': '2',
        'created_at': 1700000000000,
      },
    ]);
    expect(staleEdit.hasChanges, isFalse);
    expect(
      (await LocalDb.getLatestMessages('session-2')).single['content'],
      'newest edit',
    );

    final revoked = await LocalDb.applyArchiveMessages([
      {
        'msg_id': '201',
        'session_id': 'session-2',
        'state_version': '4',
        'is_revoked': true,
        'created_at': 1700000000000,
      },
    ]);
    expect(revoked.deletedMessageIds, ['201']);
    expect(await LocalDb.getLatestMessages('session-2'), isEmpty);

    final staleResurrection = await LocalDb.applyArchiveMessages([
      {
        'msg_id': '201',
        'session_id': 'session-2',
        'sender_id': '8',
        'msg_type': 1,
        'content': 'must stay deleted',
        'state_version': '3',
        'created_at': 1700000000000,
      },
    ]);
    expect(staleResurrection.hasChanges, isFalse);
    expect(await LocalDb.getLatestMessages('session-2'), isEmpty);
  });

  test('access revoke keeps history while history reset deletes it', () async {
    await LocalDb.prepareSyncGeneration('generation-session-remove');
    await LocalDb.applySyncBatch({
      'generation': 'generation-session-remove',
      'from_cursor': '0',
      'next_cursor': '4',
      'head_cursor': '4',
      'has_more': false,
      'events': [
        for (final entry in const [
          ('access-session', '301'),
          ('reset-session', '302'),
        ]) ...[
          {
            'cursor': entry.$2 == '301' ? '1' : '3',
            'kind': 'session.upsert',
            'entity_type': 'session',
            'entity_id': entry.$1,
            'entity_version': '1',
            'payload': {
              'session_id': entry.$1,
              'session_type': 2,
              'group_name': entry.$1,
              'updated_at': '2026-09-22T00:00:00Z',
            },
          },
          {
            'cursor': entry.$2 == '301' ? '2' : '4',
            'kind': 'message.upsert',
            'entity_type': 'message',
            'entity_id': entry.$2,
            'entity_version': '1',
            'payload': {
              'msg_id': entry.$2,
              'session_id': entry.$1,
              'sender_id': '8',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'kept until explicitly reset',
              'extra': <String, dynamic>{},
              'created_at': '2026-09-22T00:00:00Z',
            },
          },
        ],
      ],
    });

    final result = await LocalDb.applySyncBatch({
      'generation': 'generation-session-remove',
      'from_cursor': '4',
      'next_cursor': '6',
      'head_cursor': '6',
      'has_more': false,
      'events': [
        {
          'cursor': '5',
          'kind': 'session.remove',
          'entity_type': 'session',
          'entity_id': 'access-session',
          'entity_version': '2',
          'tombstone': true,
          'payload': {'session_id': 'access-session', 'reason': 'revoked'},
        },
        {
          'cursor': '6',
          'kind': 'session.remove',
          'entity_type': 'session',
          'entity_id': 'reset-session',
          'entity_version': '2',
          'tombstone': true,
          'payload': {
            'session_id': 'reset-session',
            'deleted_at': '2026-09-22T01:00:00Z',
          },
        },
      ],
      'final_state_snapshot': {
        'unread_by_session': {'access-session': 4, 'reset-session': 5},
      },
    });

    expect(result.accessRevokedSessionIds, ['access-session']);
    expect(
      result.deletedSessionIds,
      containsAll(['access-session', 'reset-session']),
    );
    expect(await LocalDb.getLatestMessages('access-session'), hasLength(1));
    expect(await LocalDb.getLatestMessages('reset-session'), isEmpty);
    expect(await LocalDb.getSessions(), isEmpty);
  });

  test(
    'versioned session bootstrap rejects older stream projections',
    () async {
      await LocalDb.applySessionSnapshot(
        session: {
          'session_id': 'session-bootstrap',
          'title': 'new title',
          'type': 'group',
          'updated_at': 1700000005000,
          'unread_count': 9,
        },
        sessionStateVersion: 5,
        memberStateVersion: 7,
      );
      await LocalDb.prepareSyncGeneration('generation-bootstrap');
      final result = await LocalDb.applySyncBatch({
        'generation': 'generation-bootstrap',
        'from_cursor': '0',
        'next_cursor': '2',
        'head_cursor': '2',
        'has_more': false,
        'events': [
          {
            'cursor': '1',
            'kind': 'session.upsert',
            'entity_type': 'session',
            'entity_id': 'session-bootstrap',
            'entity_version': '4',
            'payload': {
              'session_id': 'session-bootstrap',
              'session_type': 2,
              'group_name': 'old title',
              'updated_at': 1700000001000,
            },
          },
          {
            'cursor': '2',
            'kind': 'session.unread_set',
            'entity_type': 'session_member',
            'entity_id': 'session-bootstrap',
            'entity_version': '6',
            'payload': {'session_id': 'session-bootstrap', 'unread_count': 2},
          },
        ],
      });

      expect(result.hasChanges, isFalse);
      final session = (await LocalDb.getSessions()).singleWhere(
        (row) => row['session_id'] == 'session-bootstrap',
      );
      expect(session['title'], 'new title');
      expect(session['unread_count'], 9);
      expect((await LocalDb.getSyncState()).committedCursor, 2);
    },
  );

  test('membership event creates a complete private peer projection', () async {
    await LocalDb.prepareSyncGeneration('generation-membership');
    await LocalDb.applySyncBatch({
      'generation': 'generation-membership',
      'from_cursor': '0',
      'next_cursor': '1',
      'head_cursor': '1',
      'has_more': false,
      'events': [
        {
          'cursor': '1',
          'kind': 'membership.changed',
          'entity_type': 'membership',
          'entity_id': 'private-session',
          'entity_version': '1',
          'payload': {
            'recipient_user_id': '1001',
            'change': {'action': 'add', 'title': 'Agent task'},
            'session': {
              'session_id': 'private-session',
              'session_type': 1,
              'updated_at': '2026-09-22T00:00:00Z',
            },
            'members': [
              {
                'member_id': '1001',
                'member_type': 1,
                'custom_title': 'My agent task',
              },
              {'member_id': '2002', 'member_type': 2},
            ],
          },
        },
      ],
    });

    final session = (await LocalDb.getSessions()).single;
    expect(session['session_id'], 'private-session');
    expect(session['type'], 'private');
    expect(session['peer_id'], '2002');
    expect(session['peer_type'], 2);
    expect(session['title'], 'My agent task');
  });

  test('invalid event rolls back entity and cursor together', () async {
    await LocalDb.prepareSyncGeneration('generation-3');
    await expectLater(
      LocalDb.applySyncBatch({
        'generation': 'generation-3',
        'from_cursor': '0',
        'next_cursor': '2',
        'head_cursor': '2',
        'has_more': false,
        'events': [
          {
            'cursor': '2',
            'kind': 'message.upsert',
            'entity_type': 'message',
            'entity_id': '301',
            'entity_version': '1',
            'payload': {
              'msg_id': '301',
              'session_id': 'session-3',
              'sender_id': '8',
              'msg_type': 1,
              'content': 'must roll back',
              'created_at': 1700000000000,
            },
          },
          {
            'cursor': '2',
            'kind': 'session.unread_set',
            'entity_type': 'session_member',
            'entity_id': 'session-3',
            'entity_version': '1',
            'payload': {'session_id': 'session-3', 'unread_count': 1},
          },
        ],
      }),
      throwsA(isA<FormatException>()),
    );

    expect((await LocalDb.getSyncState()).committedCursor, 0);
    expect(await LocalDb.getLatestMessages('session-3'), isEmpty);
  });

  test('optimistic message and outbox command persist atomically', () async {
    await LocalDb.insertLocalStubWithOutbox(
      message: {
        'msg_id': 'temp-command-1',
        'session_id': 'session-4',
        'sender_id': 'me',
        'msg_type': 1,
        'content': 'pending',
        'status': 'sending',
        'local_seq': 'command-1',
        'created_at': 1700000000000,
      },
      commandId: 'command-1',
      commandKind: 'send_msg',
      payload: {
        'session_id': 'session-4',
        'client_msg_id': 'command-1',
        'msg_type': 1,
        'content': 'pending',
      },
    );

    expect(await LocalDb.getPendingOutboxCommands(), hasLength(1));
    expect(await LocalDb.getPendingOutboundMessages(), hasLength(1));
    await LocalDb.completeOutboxSendAck('command-1', 'server-401', 9);
    expect(await LocalDb.getPendingOutboxCommands(), isEmpty);
    final persisted = await LocalDb.getLatestMessages('session-4');
    expect(persisted.single['msg_id'], 'server-401');
    expect(persisted.single['status'], 'success');
    expect(persisted.single['local_seq'], isNull);
  });

  test(
    'optimistic session projection and command persist atomically',
    () async {
      await LocalDb.applySessionCommandWithOutbox(
        sessionId: 'session-command',
        sessionValues: {'is_muted': 1},
        commandId: 'mute-command',
        commandKind: 'session.mute',
        payload: {'session_id': 'session-command', 'is_muted': true},
      );

      final session = (await LocalDb.getSessions()).singleWhere(
        (row) => row['session_id'] == 'session-command',
      );
      expect(session['is_muted'], 1);
      final commands = await LocalDb.getPendingOutboxCommands();
      expect(commands.single.commandId, 'mute-command');
      expect(commands.single.commandKind, 'session.mute');
    },
  );

  test(
    'terminal outbox failure rolls back projection and removes command',
    () async {
      await LocalDb.applySessionCommandWithOutbox(
        sessionId: 'session-terminal',
        sessionValues: {'is_muted': 1},
        commandId: 'terminal-command',
        commandKind: 'session.mute',
        payload: {
          'session_id': 'session-terminal',
          'is_muted': true,
          'previous_is_muted': false,
        },
      );
      final command = (await LocalDb.getPendingOutboxCommands()).single;

      await LocalDb.rejectOutboxCommand(command);

      expect(await LocalDb.getPendingOutboxCommands(), isEmpty);
      final session = (await LocalDb.getSessions()).singleWhere(
        (row) => row['session_id'] == 'session-terminal',
      );
      expect(session['is_muted'], 0);
    },
  );

  test(
    'terminal rollback does not overwrite a newer authoritative projection',
    () async {
      await LocalDb.prepareSyncGeneration('terminal-version-generation');
      await LocalDb.applySyncBatch({
        'generation': 'terminal-version-generation',
        'from_cursor': '0',
        'next_cursor': '1',
        'head_cursor': '1',
        'has_more': false,
        'events': [
          {
            'cursor': '1',
            'kind': 'session.mute_changed',
            'entity_type': 'session_member',
            'entity_id': 'session-versioned-terminal',
            'entity_version': '1',
            'payload': {
              'session_id': 'session-versioned-terminal',
              'is_muted': false,
            },
          },
        ],
      });
      await LocalDb.applySessionCommandWithOutbox(
        sessionId: 'session-versioned-terminal',
        sessionValues: {'is_muted': 1},
        commandId: 'versioned-terminal-command',
        commandKind: 'session.mute',
        payload: {
          'session_id': 'session-versioned-terminal',
          'is_muted': true,
          'previous_is_muted': false,
        },
      );
      final command = (await LocalDb.getPendingOutboxCommands()).single;
      await LocalDb.applySyncBatch({
        'generation': 'terminal-version-generation',
        'from_cursor': '1',
        'next_cursor': '2',
        'head_cursor': '2',
        'has_more': false,
        'events': [
          {
            'cursor': '2',
            'kind': 'session.mute_changed',
            'entity_type': 'session_member',
            'entity_id': 'session-versioned-terminal',
            'entity_version': '2',
            'payload': {
              'session_id': 'session-versioned-terminal',
              'is_muted': true,
            },
          },
        ],
      });

      await LocalDb.rejectOutboxCommand(command);

      expect(await LocalDb.getPendingOutboxCommands(), isEmpty);
      final session = (await LocalDb.getSessions()).singleWhere(
        (row) => row['session_id'] == 'session-versioned-terminal',
      );
      expect(session['is_muted'], 1);
    },
  );

  test(
    'pre-v19 pending messages are backfilled into the outbox once',
    () async {
      await LocalDb.insertLocalStub({
        'msg_id': 'temp-legacy-command',
        'session_id': 'session-legacy',
        'sender_id': 'me',
        'msg_type': 1,
        'content': 'legacy pending',
        'status': 'sending',
        'local_seq': 'legacy-command',
        'extra': {'source': 'upgrade'},
        'created_at': 1700000000000,
      });

      expect(await LocalDb.backfillPendingMessageOutbox(), 1);
      expect(await LocalDb.backfillPendingMessageOutbox(), 0);
      final commands = await LocalDb.getPendingOutboxCommands();
      expect(commands, hasLength(1));
      expect(commands.single.commandId, 'legacy-command');
      expect(commands.single.payload['content'], 'legacy pending');
      expect(commands.single.payload['extra'], {'source': 'upgrade'});
    },
  );

  test(
    'writer lease prevents two active sync writers and permits expiry',
    () async {
      expect(
        await LocalDb.tryAcquireSyncWriterLease(
          ownerId: 'tab-a',
          ttl: const Duration(seconds: 10),
          nowMs: 1000,
        ),
        isTrue,
      );
      expect(
        await LocalDb.tryAcquireSyncWriterLease(
          ownerId: 'tab-b',
          ttl: const Duration(seconds: 10),
          nowMs: 5000,
        ),
        isFalse,
      );
      expect(
        await LocalDb.tryAcquireSyncWriterLease(
          ownerId: 'tab-b',
          ttl: const Duration(seconds: 10),
          nowMs: 11001,
        ),
        isTrue,
      );
      expect(
        await LocalDb.renewSyncWriterLease(
          ownerId: 'tab-a',
          ttl: const Duration(seconds: 10),
          nowMs: 11002,
        ),
        isFalse,
      );
      expect(
        await LocalDb.renewSyncWriterLease(
          ownerId: 'tab-b',
          ttl: const Duration(seconds: 10),
          nowMs: 11002,
        ),
        isTrue,
      );
      await LocalDb.releaseSyncWriterLease(ownerId: 'tab-b');
      expect(
        await LocalDb.tryAcquireSyncWriterLease(ownerId: 'tab-a', nowMs: 11003),
        isTrue,
      );
    },
  );
}

Map<String, dynamic> _singleMessageBatch({
  required String generation,
  required int from,
  required int next,
  required int version,
  required String content,
}) {
  return {
    'generation': generation,
    'from_cursor': from.toString(),
    'next_cursor': next.toString(),
    'head_cursor': next.toString(),
    'has_more': false,
    'events': [
      {
        'cursor': next.toString(),
        'kind': 'message.upsert',
        'entity_type': 'message',
        'entity_id': '201',
        'entity_version': version.toString(),
        'payload': {
          'msg_id': '201',
          'session_id': 'session-2',
          'sender_id': '8',
          'sender_type': 1,
          'msg_type': 1,
          'content': content,
          'extra': <String, dynamic>{},
          'created_at': 1700000000000,
        },
      },
    ],
  };
}
