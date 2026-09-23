import 'dart:convert';

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
    'history reset lookup is per session and rejection keeps a receipt',
    () async {
      Future<void> enqueueReset(String sid, int deletedAt) =>
          LocalDb.enqueueOutboxCommand(
            commandId: 'history_reset:$sid:$deletedAt',
            commandKind: 'session_history_reset',
            payload: {'session_id': sid, 'deleted_at': deletedAt},
          );
      await enqueueReset('ab', 100);
      await enqueueReset('abc', 100);
      await enqueueReset('ab', 200);
      await LocalDb.enqueueOutboxCommand(
        commandId: 'read:ab:9',
        commandKind: 'session_read',
        payload: {'session_id': 'ab', 'last_read_msg_id': '9'},
      );
      // Waiting out a retry backoff must not hide a command from the lookup.
      await LocalDb.markOutboxAttempt(
        'history_reset:ab:200',
        nextAttemptAt: DateTime.now().millisecondsSinceEpoch + 60000,
      );

      final forAb = await LocalDb.getPendingSessionHistoryResetCommands('ab');
      expect(forAb.map((c) => c.commandId), [
        'history_reset:ab:100',
        'history_reset:ab:200',
      ]);

      await LocalDb.rejectOutboxCommand(forAb.first);
      final read = (await LocalDb.getPendingOutboxCommands()).singleWhere(
        (c) => c.commandId == 'read:ab:9',
      );
      await LocalDb.rejectOutboxCommand(read);

      final db = await LocalDb.database;
      final rows = await db.query('outbox', orderBy: 'command_id ASC');
      expect(
        {for (final row in rows) row['command_id']: row['state']},
        {
          'history_reset:ab:100': 'rejected',
          'history_reset:ab:200': 'pending',
          'history_reset:abc:100': 'pending',
        },
      );
      expect(
        (await LocalDb.getPendingSessionHistoryResetCommands(
          'ab',
        )).map((c) => c.commandId),
        ['history_reset:ab:200'],
      );
      // Re-enqueueing a rejected reset is a no-op.
      await enqueueReset('ab', 100);
      expect(
        (await db.query(
          'outbox',
          where: 'command_id = ?',
          whereArgs: ['history_reset:ab:100'],
        )).single['state'],
        'rejected',
      );
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

  test(
    'string-encoded message.upsert for unknown group lands message and read tip',
    () async {
      // Defense in depth: _map decodes a JSON-string payload. Production
      // sync_v2 delivers json.RawMessage objects (not a root cause of the
      // bootstrap watermark hole), but string payloads must not become {}.
      const sid = '49dc128a-1c7c-4750-b739-d0d4076ea1b5';
      const oldMsg = '2102253625862000640';
      const newMsg = '2102391970839662592';

      await LocalDb.upsertSession({
        'session_id': sid,
        'title': 'Stale Group',
        'type': 'group',
        'unread_count': 0,
        'updated_at': 1774500000000,
        'last_message': 'morning',
        'last_message_time': 1774500000000,
      });
      await LocalDb.batchInsertMessages([
        {
          'msg_id': oldMsg,
          'session_id': sid,
          'sender_id': '2030840865701756928',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'morning',
          'created_at': 1774500000000,
          'status': 'sent',
        },
      ]);
      await LocalDb.prepareSyncGeneration('string-payload-generation');

      final messagePayload = jsonEncode({
        'msg_id': newMsg,
        'session_id': sid,
        'sender_id': '2057219032343379968',
        'sender_type': 2,
        'msg_type': 1,
        'content': 'agent reply after v2 enable',
        'extra': <String, dynamic>{'k': 'v'},
        'state_version': '1',
        'is_deleted': false,
        'is_revoked': false,
        'created_at': '2026-09-22T13:38:01.059767921Z',
      });

      final result = await LocalDb.applySyncBatch({
        'generation': 'string-payload-generation',
        'from_cursor': '0',
        'next_cursor': '3',
        'head_cursor': '3',
        'has_more': false,
        'events': [
          {
            'cursor': '1',
            'kind': 'message.upsert',
            'entity_type': 'message',
            'entity_id': newMsg,
            'entity_version': '1',
            'payload': messagePayload,
          },
          {
            'cursor': '2',
            'kind': 'session.upsert',
            'entity_type': 'session',
            'entity_id': sid,
            'entity_version': '1',
            'payload': {
              'session_id': sid,
              'session_type': 2,
              'group_name': 'Stale Group',
              'last_msg_summary': 'agent reply after v2 enable',
              'updated_at': '2026-09-22T13:38:01.059767921Z',
            },
          },
          {
            'cursor': '3',
            'kind': 'session.unread_set',
            'entity_type': 'session_member',
            'entity_id': sid,
            'entity_version': '4',
            'payload': {
              'session_id': sid,
              'unread_count': 3,
              'last_read_msg_id': oldMsg,
            },
          },
        ],
        'final_state_snapshot': {
          'unread_by_session': {sid: 3},
        },
      });

      expect(result.persisted, isTrue);
      expect(result.committedCursor, 3);

      final messages = await LocalDb.getLatestMessages(sid);
      expect(
        messages.map((row) => row['msg_id']?.toString()),
        contains(newMsg),
      );
      // Opening the chat reports the latest server tip as the read boundary.
      expect(await LocalDb.getLatestServerMessageId(sid), newMsg);

      final session = (await LocalDb.getSessions()).singleWhere(
        (row) => row['session_id'] == sid,
      );
      expect(session['unread_count'], 3);

      await LocalDb.applySyncBatch({
        'generation': 'string-payload-generation',
        'from_cursor': '3',
        'next_cursor': '4',
        'head_cursor': '4',
        'has_more': false,
        'events': [
          {
            'cursor': '4',
            'kind': 'session.unread_set',
            'entity_type': 'session_member',
            'entity_id': sid,
            'entity_version': '5',
            'payload': {'session_id': sid, 'unread_count': 0},
          },
        ],
        'final_state_snapshot': {'unread_by_session': <String, int>{}},
      });
      expect(
        (await LocalDb.getSessions()).singleWhere(
          (row) => row['session_id'] == sid,
        )['unread_count'],
        0,
      );

      final replay = await LocalDb.applySyncBatch({
        'generation': 'string-payload-generation',
        'from_cursor': '0',
        'next_cursor': '4',
        'head_cursor': '4',
        'has_more': false,
        'events': const [],
      });
      expect(replay.committedCursor, 4);
      expect(
        (await LocalDb.getSessions()).singleWhere(
          (row) => row['session_id'] == sid,
        )['unread_count'],
        0,
      );
      expect(await LocalDb.getLatestServerMessageId(sid), newMsg);
    },
  );

  test(
    'message.upsert missing session_id skips without refusing later events',
    () async {
      // Throwing on a missing session_id used to disconnect without ACK;
      // resume then replayed the same cursor and spun forever. Skip the
      // orphan, advance the cursor, and keep applying the rest of the batch.
      const sid = 'after-orphan-session';
      const goodMsg = '2102404346213302001';
      await LocalDb.prepareSyncGeneration('missing-session-id-generation');

      final result = await LocalDb.applySyncBatch({
        'generation': 'missing-session-id-generation',
        'from_cursor': '0',
        'next_cursor': '2',
        'head_cursor': '2',
        'has_more': false,
        'events': [
          {
            'cursor': '1',
            'kind': 'message.upsert',
            'entity_type': 'message',
            'entity_id': '2102404346213302000',
            'entity_version': '1',
            'payload': {
              'msg_id': '2102404346213302000',
              // intentionally no session_id
              'sender_id': '2001',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'orphan',
              'created_at': 1700000000000,
            },
          },
          {
            'cursor': '2',
            'kind': 'message.upsert',
            'entity_type': 'message',
            'entity_id': goodMsg,
            'entity_version': '1',
            'payload': {
              'msg_id': goodMsg,
              'session_id': sid,
              'sender_id': '2001',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'kept after orphan skip',
              'created_at': 1700000001000,
            },
          },
        ],
      });

      expect(result.persisted, isTrue);
      expect(result.committedCursor, 2);
      expect(result.changedMessageRows, hasLength(1));
      expect(result.changedMessageRows.single['msg_id'], goodMsg);

      final db = await LocalDb.database;
      final orphan = await db.query(
        'messages',
        where: 'msg_id = ?',
        whereArgs: ['2102404346213302000'],
      );
      expect(orphan, isEmpty);
      final goodRows = await LocalDb.getLatestMessages(sid);
      expect(goodRows.map((row) => row['msg_id']?.toString()), [goodMsg]);
    },
  );

  test(
    'unread_set for unknown session does not create a list-invisible stub',
    () async {
      // Reproduces iOS 3019: badge 15 vs list sum 12. Three unread_set events
      // for sessions never bootstrapped into the local conversation list used
      // to upsert stub rows that only inflated account/badge totals.
      await LocalDb.upsertSession({
        'session_id': 'visible-a',
        'title': 'Alice',
        'type': 'private',
        'peer_id': '2001',
        'peer_type': 1,
        'unread_count': 5,
        'updated_at': 1700000000000,
      });
      await LocalDb.upsertSession({
        'session_id': 'visible-b',
        'title': 'Bob',
        'type': 'private',
        'peer_id': '2002',
        'peer_type': 1,
        'unread_count': 7,
        'updated_at': 1700000001000,
      });
      await LocalDb.prepareSyncGeneration('unread-outside-list');

      final result = await LocalDb.applySyncBatch({
        'generation': 'unread-outside-list',
        'from_cursor': '0',
        'next_cursor': '1',
        'head_cursor': '1',
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
        ],
        'final_state_snapshot': {
          'unread_by_session': {
            'visible-a': 5,
            'visible-b': 7,
            'ghost-outside-list': 3,
          },
        },
      });

      expect(result.persisted, isTrue);
      final sessions = await LocalDb.getSessions();
      expect(
        sessions.map((row) => row['session_id']?.toString()).toSet(),
        {'visible-a', 'visible-b'},
      );
      final counters = await LocalDb.getAccountCounters();
      expect(counters['total_unread'], 12);
      expect(counters['notification_unread'], 12);
    },
  );

  test(
    'unread_set for a locally deleted session does not resurrect unread',
    () async {
      await LocalDb.upsertSession({
        'session_id': 'kept-session',
        'title': 'Kept',
        'type': 'group',
        'unread_count': 12,
        'updated_at': 1700000000000,
      });
      await LocalDb.upsertSession({
        'session_id': 'deleted-session',
        'title': 'Deleted',
        'type': 'group',
        'unread_count': 0,
        'updated_at': 1700000000000,
      });
      await LocalDb.deleteConversation('deleted-session');
      expect(
        (await LocalDb.getSessions()).map((row) => row['session_id']),
        ['kept-session'],
      );
      await LocalDb.prepareSyncGeneration('unread-deleted');

      final result = await LocalDb.applySyncBatch({
        'generation': 'unread-deleted',
        'from_cursor': '0',
        'next_cursor': '1',
        'head_cursor': '1',
        'has_more': false,
        'events': [
          {
            'cursor': '1',
            'kind': 'session.unread_set',
            'entity_type': 'session_member',
            'entity_id': 'deleted-session',
            'entity_version': '1',
            'payload': {
              'session_id': 'deleted-session',
              'unread_count': 3,
            },
          },
        ],
        'final_state_snapshot': {
          'unread_by_session': {
            'kept-session': 12,
            'deleted-session': 3,
          },
        },
      });

      expect(result.persisted, isTrue);
      final sessions = await LocalDb.getSessions();
      expect(
        sessions.map((row) => row['session_id']?.toString()).toSet(),
        {'kept-session'},
      );
      expect(sessions.single['unread_count'], 12);
      final counters = await LocalDb.getAccountCounters();
      expect(counters['total_unread'], 12);
      expect(counters['notification_unread'], 12);
    },
  );

  test(
    'message.upsert version skip without a bound local row refuses the batch',
    () async {
      // Reproduces the private-chat sample hole: a prior corrupt apply can
      // record a high entity version (or an unbound orphan row) so the real
      // message.upsert is skipped while session.unread_set in the same triple
      // still lands. Refuse to advance the cursor past that gap.
      await LocalDb.prepareSyncGeneration('version-skip-missing-body');
      final db = await LocalDb.database;
      await db.insert('sync_entity_versions', {
        'entity_type': 'message',
        'entity_id': '2102391295594467328',
        'state_version': 5,
        'tombstone': 0,
        'last_event_cursor': 1,
      });
      await LocalDb.upsertSession({
        'session_id': 'e08d82a0-407e-4ee2-96b2-68b5c5e0a54a',
        'title': 'Private',
        'type': 'private',
        'peer_id': '2001',
        'peer_type': 2,
        'unread_count': 0,
        'updated_at': 1700000000000,
      });

      await expectLater(
        LocalDb.applySyncBatch({
          'generation': 'version-skip-missing-body',
          'from_cursor': '0',
          'next_cursor': '3',
          'head_cursor': '3',
          'has_more': false,
          'events': [
            {
              'cursor': '1',
              'kind': 'message.upsert',
              'entity_type': 'message',
              'entity_id': '2102391295594467328',
              'entity_version': '1',
              'payload': {
                'msg_id': '2102391295594467328',
                'session_id': 'e08d82a0-407e-4ee2-96b2-68b5c5e0a54a',
                'sender_id': '2001',
                'sender_type': 2,
                'msg_type': 1,
                'content': 'agent reply',
                'created_at': '2026-09-22T13:35:20Z',
              },
            },
            {
              'cursor': '2',
              'kind': 'session.upsert',
              'entity_type': 'session',
              'entity_id': 'e08d82a0-407e-4ee2-96b2-68b5c5e0a54a',
              'entity_version': '1',
              'payload': {
                'session_id': 'e08d82a0-407e-4ee2-96b2-68b5c5e0a54a',
                'session_type': 1,
                'last_msg_summary': 'agent reply',
                'updated_at': '2026-09-22T13:35:20Z',
              },
            },
            {
              'cursor': '3',
              'kind': 'session.unread_set',
              'entity_type': 'session_member',
              'entity_id': 'e08d82a0-407e-4ee2-96b2-68b5c5e0a54a',
              'entity_version': '1',
              'payload': {
                'session_id': 'e08d82a0-407e-4ee2-96b2-68b5c5e0a54a',
                'unread_count': 3,
              },
            },
          ],
        }),
        throwsA(isA<StateError>()),
      );

      expect((await LocalDb.getSyncState()).committedCursor, 0);
      expect(
        (await LocalDb.getSessions()).singleWhere(
          (row) =>
              row['session_id'] == 'e08d82a0-407e-4ee2-96b2-68b5c5e0a54a',
        )['unread_count'],
        0,
      );
      expect(
        await LocalDb.getLatestMessages(
          'e08d82a0-407e-4ee2-96b2-68b5c5e0a54a',
        ),
        isEmpty,
      );
    },
  );

  group('compound_v1', () {
    /// Applies [pages] to a fresh user database and returns what they
    /// persisted, which must not depend on how the server encoded the events.
    Future<(Map<String, Object?>, LocalSyncApplyResult)> applyToFreshDb(
      String label,
      List<Map<String, dynamic>> pages, {
      Future<void> Function()? arrange,
    }) async {
      await LocalDb.setActiveUser('${userId}_$label');
      await LocalDb.prepareSyncGeneration(_compoundGeneration);
      if (arrange != null) await arrange();
      late LocalSyncApplyResult result;
      for (final page in pages) {
        result = await LocalDb.applySyncBatch(page);
        expect(result.persisted, isTrue, reason: label);
      }
      final db = await LocalDb.database;
      final persisted = <String, Object?>{
        'committed_cursor': (await LocalDb.getSyncState()).committedCursor,
        'sessions': await db.query('sessions', orderBy: 'session_id'),
        'messages': await db.query('messages', orderBy: 'msg_id'),
        'versions': await db.query(
          'sync_entity_versions',
          columns: const [
            'entity_type',
            'entity_id',
            'state_version',
            'tombstone',
          ],
          orderBy: 'entity_type, entity_id',
        ),
        'counters': await db.query(
          'account_counters',
          columns: const [
            'total_unread',
            'notification_unread',
            'muted_unread',
          ],
        ),
        'outbox': await db.query(
          'outbox',
          columns: const ['command_id', 'state'],
          orderBy: 'command_id',
        ),
      };
      return (persisted, result);
    }

    Future<void> enqueueSend(String commandId) => LocalDb.enqueueOutboxCommand(
      commandId: commandId,
      commandKind: 'send_msg',
      payload: {
        'session_id': _compoundSid,
        'client_msg_id': commandId,
        'msg_type': 1,
        'content': 'user reply',
      },
    );

    test('one compound event persists exactly like its three events', () async {
      final message = _serverMessage(_msgA, 1, 'compound hello', 1);
      final session = _serverSession(12, 'compound hello', 1);
      final unread = _serverUnread(34, 5);

      final (threeRows, threeResult) = await applyToFreshDb('three', [
        _syncPage(0, 3, [
          _syncEvent(
            1,
            'message.upsert',
            'message',
            _msgA,
            1,
            message,
            commandId: 'client-msg-1',
          ),
          _syncEvent(2, 'session.upsert', 'session', _compoundSid, 12, session),
          _syncEvent(
            3,
            'session.unread_set',
            'session_member',
            _compoundSid,
            34,
            unread,
          ),
        ]),
      ], arrange: () => enqueueSend('client-msg-1'));
      final (compoundRows, compoundResult) = await applyToFreshDb('compound', [
        _syncPage(0, 3, [
          _syncEvent(
            3,
            'message.upsert',
            'message',
            _msgA,
            1,
            {...message, 'session': session, 'unread': unread},
            firstCursor: 1,
            commandId: 'client-msg-1',
          ),
        ]),
      ], arrange: () => enqueueSend('client-msg-1'));

      expect(compoundRows, equals(threeRows));
      expect(_resultFields(compoundResult), equals(_resultFields(threeResult)));
      // The comparison must not pass on two empty projections.
      final row = (threeRows['sessions']! as List).single as Map;
      expect(row['title'], 'Release room');
      expect(row['type'], 'group');
      expect(row['last_message'], 'compound hello');
      expect(row['unread_count'], 5);
      expect(threeRows['outbox'], isEmpty);
      expect(threeResult.changedSessionIds, [_compoundSid]);
      expect(
        threeResult.changedMessageRows.single['content'],
        'compound hello',
      );
      expect(threeResult.acknowledgedCommandIds, ['client-msg-1']);
    });

    test(
      'a compound without session or unread reduces like its events',
      () async {
        // Unread never materializes a session row, so the row exists first,
        // exactly as a standalone session.unread_set requires.
        final bootstrap = _syncPage(0, 1, [
          _syncEvent(
            1,
            'session.upsert',
            'session',
            _compoundSid,
            11,
            _serverSession(11, 'earlier', 0),
          ),
        ]);
        final message = _serverMessage(_msgA, 1, 'degraded', 1);
        final session = _serverSession(12, 'degraded', 1);
        final unread = _serverUnread(34, 2);
        final cases =
            <
              (
                String,
                Map<String, dynamic>,
                List<Map<String, dynamic>>,
                String,
                int,
              )
            >[
              (
                'session-only',
                {'session': session},
                [
                  _syncEvent(
                    3,
                    'session.upsert',
                    'session',
                    _compoundSid,
                    12,
                    session,
                  ),
                ],
                'degraded',
                0,
              ),
              // The contract names unread_count/state_version/last_read_msg_id
              // only; the message's session_id then identifies the member row.
              (
                'unread-only',
                {'unread': Map.of(unread)..remove('session_id')},
                [
                  _syncEvent(
                    3,
                    'session.unread_set',
                    'session_member',
                    _compoundSid,
                    34,
                    unread,
                  ),
                ],
                'earlier',
                2,
              ),
              ('neither', {'session': null, 'unread': null}, [], 'earlier', 0),
            ];

        for (final (label, nested, companions, summary, unreadCount) in cases) {
          final next = 2 + companions.length;
          final (eventRows, eventResult) = await applyToFreshDb(
            '$label-events',
            [
              bootstrap,
              _syncPage(1, next, [
                _syncEvent(2, 'message.upsert', 'message', _msgA, 1, message),
                ...companions,
              ]),
            ],
          );
          final (compoundRows, compoundResult) = await applyToFreshDb(
            '$label-compound',
            [
              bootstrap,
              _syncPage(1, next, [
                _syncEvent(
                  next,
                  'message.upsert',
                  'message',
                  _msgA,
                  1,
                  {...message, ...nested},
                  firstCursor: next == 2 ? null : 2,
                ),
              ]),
            ],
          );

          expect(compoundRows, equals(eventRows), reason: label);
          expect(
            _resultFields(compoundResult),
            equals(_resultFields(eventResult)),
            reason: label,
          );
          final row = (compoundRows['sessions']! as List).single as Map;
          expect(row['last_message'], summary, reason: label);
          expect(row['unread_count'], unreadCount, reason: label);
          expect(
            (compoundRows['messages']! as List).single,
            containsPair('content', 'degraded'),
            reason: label,
          );
        }
      },
    );

    test(
      'a stale compound message still applies its session and unread',
      () async {
        // History already delivered a newer edit of the same message.
        Future<void> arrange() => LocalDb.applyArchiveMessages([
          _serverMessage(_msgA, 2, 'edited later', 1),
        ]);
        final message = _serverMessage(_msgA, 1, 'original', 1);
        final session = _serverSession(12, 'original', 1);
        final unread = _serverUnread(34, 3);

        final (eventRows, eventResult) = await applyToFreshDb('stale-events', [
          _syncPage(0, 3, [
            _syncEvent(1, 'message.upsert', 'message', _msgA, 1, message),
            _syncEvent(
              2,
              'session.upsert',
              'session',
              _compoundSid,
              12,
              session,
            ),
            _syncEvent(
              3,
              'session.unread_set',
              'session_member',
              _compoundSid,
              34,
              unread,
            ),
          ]),
        ], arrange: arrange);
        final (compoundRows, compoundResult) = await applyToFreshDb(
          'stale-compound',
          [
            _syncPage(0, 3, [
              _syncEvent(3, 'message.upsert', 'message', _msgA, 1, {
                ...message,
                'session': session,
                'unread': unread,
              }, firstCursor: 1),
            ]),
          ],
          arrange: arrange,
        );

        expect(compoundRows, equals(eventRows));
        expect(
          _resultFields(compoundResult),
          equals(_resultFields(eventResult)),
        );
        expect(
          (compoundRows['messages']! as List).single,
          containsPair('content', 'edited later'),
        );
        final row = (compoundRows['sessions']! as List).single as Map;
        expect(row['last_message'], 'original');
        expect(row['unread_count'], 3);
      },
    );

    test('a folded page lands the same rows as the page it folds', () async {
      Map<String, dynamic> sessionEvent(
        int cursor,
        int version,
        String summary,
        int minute,
      ) => _syncEvent(
        cursor,
        'session.upsert',
        'session',
        _compoundSid,
        version,
        _serverSession(version, summary, minute),
      );
      Map<String, dynamic> unreadEvent(int cursor, int version, int count) =>
          _syncEvent(
            cursor,
            'session.unread_set',
            'session_member',
            _compoundSid,
            version,
            _serverUnread(version, count),
          );
      Map<String, dynamic> messageEvent(
        int cursor,
        String msgId,
        int version,
        String content,
        int minute, {
        int? firstCursor,
        String commandId = '',
        Map<String, dynamic> nested = const {},
      }) => _syncEvent(
        cursor,
        'message.upsert',
        'message',
        msgId,
        version,
        {..._serverMessage(msgId, version, content, minute), ...nested},
        firstCursor: firstCursor,
        commandId: commandId,
      );
      Map<String, dynamic> revokeD({int? firstCursor}) => _syncEvent(
        11,
        'message.revoke',
        'message',
        _msgD,
        2,
        _serverMessage(_msgD, 2, 'oops', 4, revoked: true),
        firstCursor: firstCursor,
        tombstone: true,
      );

      // An agent answer streamed as two versions, the user's reply (a send
      // receipt), a message revoked right after it was sent, a follow-up.
      final (unfoldedRows, unfoldedResult) = await applyToFreshDb('unfolded', [
        _syncPage(0, 16, [
          messageEvent(1, _msgA, 1, 'draft', 1),
          sessionEvent(2, 10, 'draft', 1),
          unreadEvent(3, 20, 1),
          messageEvent(4, _msgA, 2, 'final answer', 1),
          sessionEvent(5, 11, 'final answer', 2),
          messageEvent(6, _msgB, 1, 'user reply', 3, commandId: 'client-msg-1'),
          sessionEvent(7, 12, 'user reply', 3),
          messageEvent(8, _msgD, 1, 'oops', 4),
          sessionEvent(9, 13, 'oops', 4),
          unreadEvent(10, 21, 2),
          revokeD(),
          unreadEvent(12, 22, 1),
          sessionEvent(13, 14, 'user reply', 5),
          messageEvent(14, _msgC, 1, 'agent follow-up', 6),
          sessionEvent(15, 15, 'agent follow-up', 6),
          unreadEvent(16, 23, 2),
        ]),
      ], arrange: () => enqueueSend('client-msg-1'));
      // A keeps only its last upsert and D only its revoke; B's receipt
      // survives; no session.upsert companion is left except the session and
      // unread nested in C's compound.
      final (foldedRows, foldedResult) = await applyToFreshDb('folded', [
        _syncPage(0, 16, [
          messageEvent(4, _msgA, 2, 'final answer', 1, firstCursor: 1),
          messageEvent(
            6,
            _msgB,
            1,
            'user reply',
            3,
            firstCursor: 5,
            commandId: 'client-msg-1',
          ),
          revokeD(firstCursor: 7),
          messageEvent(
            16,
            _msgC,
            1,
            'agent follow-up',
            6,
            firstCursor: 12,
            nested: {
              'session': _serverSession(15, 'agent follow-up', 6),
              'unread': _serverUnread(23, 2),
            },
          ),
        ]),
      ], arrange: () => enqueueSend('client-msg-1'));

      expect(foldedRows, equals(unfoldedRows));
      expect(foldedResult.changedSessionIds, unfoldedResult.changedSessionIds);
      expect(foldedResult.acknowledgedCommandIds, ['client-msg-1']);
      expect(
        (foldedRows['messages']! as List).map((row) => (row as Map)['content']),
        ['final answer', 'user reply', 'agent follow-up'],
      );
      final row = (foldedRows['sessions']! as List).single as Map;
      expect(row['last_message'], 'agent follow-up');
      expect(row['unread_count'], 2);
      expect(foldedRows['outbox'], isEmpty);
      expect(
        foldedRows['versions'],
        contains(
          allOf(
            containsPair('entity_id', _msgD),
            containsPair('state_version', 2),
            containsPair('tombstone', 1),
          ),
        ),
      );
    });

    test('a folded older receipt acks without regressing newer data', () async {
      // Folding keeps every receipt, even when a newer version of the same
      // message follows it in the page or is already local from history.
      final page = _syncPage(0, 3, [
        _syncEvent(
          1,
          'message.upsert',
          'message',
          _msgB,
          1,
          _serverMessage(_msgB, 1, 'as sent', 2),
          commandId: 'client-msg-2',
        ),
        _syncEvent(
          3,
          'message.upsert',
          'message',
          _msgB,
          3,
          _serverMessage(_msgB, 3, 'edited twice', 2),
          firstCursor: 2,
        ),
      ]);
      final arrangements = <(String, Future<void> Function())>[
        ('receipt-then-newer', () => enqueueSend('client-msg-2')),
        (
          'newer-already-local',
          () async {
            await enqueueSend('client-msg-2');
            await LocalDb.applyArchiveMessages([
              _serverMessage(_msgB, 3, 'edited twice', 2),
            ]);
          },
        ),
      ];

      for (final (label, arrange) in arrangements) {
        final (rows, result) = await applyToFreshDb(label, [
          page,
        ], arrange: arrange);
        expect(result.acknowledgedCommandIds, ['client-msg-2'], reason: label);
        expect(rows['outbox'], isEmpty, reason: label);
        expect(
          (rows['messages']! as List).single,
          containsPair('content', 'edited twice'),
          reason: label,
        );
        expect(
          rows['versions'],
          contains(
            allOf(
              containsPair('entity_id', _msgB),
              containsPair('state_version', 3),
            ),
          ),
          reason: label,
        );
      }
    });

    test('cursor gaps pass only when first_cursor covers them', () async {
      Map<String, dynamic> event(int cursor, {int? firstCursor}) => _syncEvent(
        cursor,
        'message.upsert',
        'message',
        'msg-$cursor',
        1,
        _serverMessage('msg-$cursor', 1, 'cursor $cursor', cursor),
        firstCursor: firstCursor,
      );
      await LocalDb.prepareSyncGeneration(_compoundGeneration);

      // Old-server semantics stay intact: any uncovered gap is a lost event.
      final refused = <String, List<Map<String, dynamic>>>{
        'gap without first_cursor': [event(1), event(3)],
        'page ends before next_cursor': [event(1), event(2)],
        'first_cursor leaves a gap': [event(1), event(3, firstCursor: 3)],
        'cursor before first_cursor': [
          event(0, firstCursor: 1),
          event(3, firstCursor: 1),
        ],
      };
      for (final entry in refused.entries) {
        await expectLater(
          LocalDb.applySyncBatch(_syncPage(0, 3, entry.value)),
          throwsA(isA<FormatException>()),
          reason: entry.key,
        );
      }
      expect((await LocalDb.getSyncState()).committedCursor, 0);
      expect(await LocalDb.getLatestMessages(_compoundSid), isEmpty);

      final folded = await LocalDb.applySyncBatch(
        _syncPage(0, 3, [event(1), event(3, firstCursor: 2)]),
      );
      expect(folded.committedCursor, 3);
      expect((await LocalDb.getSyncState()).committedCursor, 3);
      expect(
        (await LocalDb.getLatestMessages(
          _compoundSid,
        )).map((row) => row['msg_id']),
        unorderedEquals(['msg-1', 'msg-3']),
      );
    });
  });
}

const _compoundGeneration = 'compound-generation';
const _compoundSid = '7c1b2d9e-4f3a-4b8e-9d21-3a6f0c5e8b17';
const _msgA = '2102897316570075136';
const _msgB = '2102897316570075137';
const _msgC = '2102897316570075138';
const _msgD = '2102897316570075139';

// Shapes as the server marshals them: model.Message and model.Session encode
// int64 ids and state_version as strings and times as RFC 3339, while the
// session_member unread payload is a Go map whose numbers stay numbers.
Map<String, dynamic> _serverMessage(
  String msgId,
  int version,
  String content,
  int minute, {
  bool revoked = false,
}) => {
  'msg_id': msgId,
  'session_id': _compoundSid,
  'sender_id': '2030840865701756928',
  'sender_type': 1,
  'msg_type': 1,
  'content': content,
  'extra': <String, dynamic>{},
  'is_deleted': false,
  'is_revoked': revoked,
  'state_version': '$version',
  'created_at': '2026-09-24T07:${_twoDigits(minute)}:04.976123+08:00',
};

Map<String, dynamic> _serverSession(int version, String summary, int minute) =>
    {
      'session_id': _compoundSid,
      'owner_id': '2030840865701756928',
      'session_type': 2,
      'group_name': 'Release room',
      'allow_member_invite': true,
      'all_members_muted': false,
      'last_msg_id': _msgA,
      'last_msg_summary': summary,
      'moderation_status': 1,
      'banned_reason': '',
      'is_deleted': false,
      'state_version': '$version',
      'created_at': '2026-09-01T08:00:00+08:00',
      'updated_at': '2026-09-24T07:${_twoDigits(minute)}:05.123456+08:00',
    };

Map<String, dynamic> _serverUnread(int version, int unreadCount) => {
  'session_id': _compoundSid,
  'unread_count': unreadCount,
  'last_read_msg_id': 2102896936847151104,
  'state_version': version,
};

String _twoDigits(int value) => value.toString().padLeft(2, '0');

Map<String, dynamic> _syncEvent(
  int cursor,
  String kind,
  String entityType,
  String entityId,
  int version,
  Map<String, dynamic> payload, {
  int? firstCursor,
  bool tombstone = false,
  String commandId = '',
}) => {
  'cursor': '$cursor',
  if (firstCursor != null) 'first_cursor': '$firstCursor',
  'kind': kind,
  'entity_type': entityType,
  'entity_id': entityId,
  'entity_version': '$version',
  if (tombstone) 'tombstone': true,
  if (commandId.isNotEmpty) 'command_id': commandId,
  'payload': payload,
};

/// A catch-up page without a final unread snapshot, so unread values land
/// only through the events themselves.
Map<String, dynamic> _syncPage(
  int from,
  int next,
  List<Map<String, dynamic>> events,
) => {
  'generation': _compoundGeneration,
  'from_cursor': '$from',
  'next_cursor': '$next',
  'head_cursor': '${next + 1}',
  'has_more': true,
  'events': events,
};

Map<String, Object?> _resultFields(LocalSyncApplyResult result) => {
  'committed_cursor': result.committedCursor,
  'changed_message_rows': result.changedMessageRows,
  'deleted_message_ids': result.deletedMessageIds,
  'deleted_messages_by_session': result.deletedMessagesBySession,
  'changed_session_ids': result.changedSessionIds,
  'deleted_session_ids': result.deletedSessionIds,
  'access_revoked_session_ids': result.accessRevokedSessionIds,
  'membership_changed_session_ids': result.membershipChangedSessionIds,
  'acknowledged_command_ids': result.acknowledgedCommandIds,
  'unchanged_events': result.unchangedEvents,
};

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
