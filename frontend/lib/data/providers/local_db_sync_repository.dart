part of 'local_db.dart';

class LocalSyncState {
  const LocalSyncState({
    this.streamName = 'chat',
    this.committedCursor = 0,
    this.serverHeadCursor = 0,
    this.generation = '',
    this.bootstrapCursor = 0,
  });

  final String streamName;
  final int committedCursor;
  final int serverHeadCursor;
  final String generation;
  final int bootstrapCursor;
}

class LocalOutboxCommand {
  const LocalOutboxCommand({
    required this.commandId,
    required this.commandKind,
    required this.payload,
    required this.attemptCount,
    required this.nextAttemptAt,
  });

  final String commandId;
  final String commandKind;
  final Map<String, dynamic> payload;
  final int attemptCount;
  final int nextAttemptAt;
}

class LocalSyncApplyResult {
  const LocalSyncApplyResult({
    this.persisted = true,
    this.committedCursor = 0,
    this.changedMessageRows = const <Map<String, dynamic>>[],
    this.deletedMessageIds = const <String>[],
    this.deletedMessagesBySession = const <String, List<String>>{},
    this.changedSessionIds = const <String>[],
    this.deletedSessionIds = const <String>[],
    this.accessRevokedSessionIds = const <String>[],
    this.membershipChangedSessionIds = const <String>[],
    this.acknowledgedCommandIds = const <String>[],
    this.unchangedEvents = 0,
  });

  final bool persisted;
  final int committedCursor;
  final List<Map<String, dynamic>> changedMessageRows;
  final List<String> deletedMessageIds;
  final Map<String, List<String>> deletedMessagesBySession;
  final List<String> changedSessionIds;
  final List<String> deletedSessionIds;
  final List<String> accessRevokedSessionIds;
  final List<String> membershipChangedSessionIds;
  final List<String> acknowledgedCommandIds;
  final int unchangedEvents;

  bool get hasChanges =>
      changedMessageRows.isNotEmpty ||
      deletedMessageIds.isNotEmpty ||
      changedSessionIds.isNotEmpty ||
      deletedSessionIds.isNotEmpty;
}

class LocalArchiveApplyResult {
  const LocalArchiveApplyResult({
    this.persisted = true,
    this.changedRows = const <Map<String, dynamic>>[],
    this.deletedMessageIds = const <String>[],
    this.unchangedCount = 0,
  });

  final bool persisted;
  final List<Map<String, dynamic>> changedRows;
  final List<String> deletedMessageIds;
  final int unchangedCount;

  bool get hasChanges => changedRows.isNotEmpty || deletedMessageIds.isNotEmpty;
}

class _EntityVersion {
  const _EntityVersion({
    required this.exists,
    required this.version,
    required this.tombstone,
    required this.lastEventCursor,
  });

  const _EntityVersion.none()
    : exists = false,
      version = -1,
      tombstone = false,
      lastEventCursor = 0;

  final bool exists;
  final int version;
  final bool tombstone;
  final int lastEventCursor;
}

class LocalDbSyncRepository {
  static const String _streamName = 'chat';
  static const Map<String, dynamic> _sessionInsertDefaults = {
    'title': '',
    'type': 'private',
    'peer_id': '',
    'peer_type': 0,
    'peer_nickname': '',
    'peer_username': '',
    'updated_at': 0,
    'is_pinned': 0,
    'is_muted': 0,
    'pinned_at': 0,
    'friend_is_pinned': 0,
    'friend_pinned_at': 0,
    'friend_is_muted': 0,
    'unread_count': 0,
  };

  static Future<LocalSyncState> getSyncState({
    String streamName = _streamName,
  }) {
    return LocalDb._withDatabaseOr<LocalSyncState>(
      LocalSyncState(streamName: streamName),
      (db) async {
        final rows = await db.query(
          'sync_state',
          where: 'stream_name = ?',
          whereArgs: [streamName],
          limit: 1,
        );
        if (rows.isEmpty) {
          return LocalSyncState(streamName: streamName);
        }
        final row = rows.first;
        return LocalSyncState(
          streamName: streamName,
          committedCursor: _int(row['committed_cursor']),
          serverHeadCursor: _int(row['server_head_cursor']),
          generation: row['generation']?.toString() ?? '',
          bootstrapCursor: _int(row['bootstrap_cursor']),
        );
      },
    );
  }

  static Future<void> prepareGeneration(
    String generation, {
    String streamName = _streamName,
  }) async {
    _requireActiveDatabase();
    final normalized = generation.trim();
    if (normalized.isEmpty) {
      throw ArgumentError.value(generation, 'generation');
    }
    await LocalDb._withDatabase<void>((db) async {
      final now = DateTime.now().millisecondsSinceEpoch;
      await db.transaction((txn) async {
        final rows = await txn.query(
          'sync_state',
          where: 'stream_name = ?',
          whereArgs: [streamName],
          limit: 1,
        );
        if (rows.isEmpty) {
          await txn.insert('sync_state', {
            'user_id': LocalDb.activeUserId ?? '',
            'stream_name': streamName,
            'committed_cursor': 0,
            'server_head_cursor': 0,
            'generation': normalized,
            'bootstrap_cursor': 0,
            'updated_at': now,
          });
          return;
        }
        if (rows.first['generation']?.toString() == normalized) return;
        await txn.update(
          'sync_state',
          {'generation': normalized, 'updated_at': now},
          where: 'stream_name = ?',
          whereArgs: [streamName],
        );
      });
    });
  }

  static Future<void> markBootstrapComplete(
    int bootstrapCursor, {
    required int committedCursor,
    String streamName = _streamName,
  }) async {
    _requireActiveDatabase();
    final cursor = bootstrapCursor <= 0 ? 1 : bootstrapCursor;
    if (committedCursor < 0) {
      throw ArgumentError.value(committedCursor, 'committedCursor');
    }
    await LocalDb._withDatabase<void>((db) async {
      await db.update(
        'sync_state',
        {
          'bootstrap_cursor': cursor,
          'committed_cursor': committedCursor,
          'server_head_cursor': committedCursor,
          'updated_at': DateTime.now().millisecondsSinceEpoch,
        },
        where: 'stream_name = ?',
        whereArgs: [streamName],
      );
    });
  }

  static Future<LocalSyncApplyResult> applyBatch(
    Map<String, dynamic> batch,
  ) async {
    final generation = batch['generation']?.toString().trim() ?? '';
    final fromCursor = _int(batch['from_cursor']);
    final nextCursor = _int(batch['next_cursor']);
    final headCursor = _int(batch['head_cursor']);
    if (generation.isEmpty ||
        fromCursor < 0 ||
        nextCursor < fromCursor ||
        headCursor < nextCursor) {
      throw const FormatException('invalid sync_v2 batch envelope');
    }
    final rawEvents = batch['events'];
    if (rawEvents is! List) {
      throw const FormatException('sync_v2 events must be a list');
    }
    if (rawEvents.any((event) => event is! Map)) {
      throw const FormatException('sync_v2 event must be an object');
    }

    return LocalDb._withDatabaseOr<
      LocalSyncApplyResult
    >(LocalSyncApplyResult(persisted: false, committedCursor: fromCursor), (
      db,
    ) async {
      return db.transaction((txn) async {
        final stateRows = await txn.query(
          'sync_state',
          where: 'stream_name = ?',
          whereArgs: [_streamName],
          limit: 1,
        );
        if (stateRows.isEmpty) {
          throw StateError('sync_v2 generation was not prepared');
        }
        final state = stateRows.first;
        final localGeneration = state['generation']?.toString() ?? '';
        final committed = _int(state['committed_cursor']);
        if (localGeneration != generation) {
          throw StateError('stale sync_v2 generation');
        }

        // A duplicate delivery after the durable commit but before the ACK
        // has an older from_cursor and is an idempotent no-op. A zero-progress
        // batch at the current cursor is different: it can carry the final
        // unread snapshot and must still be reduced transactionally.
        if (nextCursor < committed ||
            (nextCursor == committed && fromCursor < committed)) {
          return LocalSyncApplyResult(committedCursor: committed);
        }
        if (fromCursor != committed) {
          throw StateError(
            'sync_v2 cursor mismatch local=$committed from=$fromCursor',
          );
        }

        final events =
            rawEvents
                .cast<Map>()
                .map((event) => Map<String, dynamic>.from(event))
                .toList(growable: false)
              ..sort((a, b) => _int(a['cursor']).compareTo(_int(b['cursor'])));

        var lastEventCursor = fromCursor;
        var unchangedEvents = 0;
        final changedMessageRows = <Map<String, dynamic>>[];
        final deletedMessageIds = <String>[];
        final deletedMessagesBySession = <String, List<String>>{};
        final changedSessionIds = <String>{};
        final deletedSessionIds = <String>[];
        final accessRevokedSessionIds = <String>[];
        final membershipChangedSessionIds = <String>{};
        final acknowledgedCommandIds = <String>{};

        for (final event in events) {
          final cursor = _int(event['cursor']);
          if (cursor != lastEventCursor + 1 || cursor > nextCursor) {
            throw const FormatException('sync_v2 event cursor is invalid');
          }
          lastEventCursor = cursor;
          final kind = event['kind']?.toString().trim() ?? '';
          final entityType = event['entity_type']?.toString().trim() ?? '';
          final entityId = event['entity_id']?.toString().trim() ?? '';
          final version = _int(event['entity_version']);
          final tombstone = _bool(event['tombstone']);
          final commandId = event['command_id']?.toString().trim() ?? '';
          final payload = _map(event['payload']);
          if (kind.isEmpty ||
              entityType.isEmpty ||
              entityId.isEmpty ||
              version < 0) {
            throw const FormatException('invalid sync_v2 event');
          }

          final versionRows = await txn.query(
            'sync_entity_versions',
            where: 'entity_type = ? AND entity_id = ?',
            whereArgs: [entityType, entityId],
            limit: 1,
          );
          final previousVersion = versionRows.isEmpty
              ? -1
              : _int(versionRows.first['state_version']);
          final previousTombstone =
              versionRows.isNotEmpty && _bool(versionRows.first['tombstone']);
          if (version < previousVersion ||
              (version == previousVersion && previousTombstone && !tombstone)) {
            unchangedEvents++;
            if (commandId.isNotEmpty) {
              await _acknowledgeOutboxTx(txn, commandId);
              acknowledgedCommandIds.add(commandId);
            }
            continue;
          }

          var changed = false;
          switch (kind) {
            case 'message.upsert':
              if (_bool(payload['is_revoked']) || tombstone) {
                final result = await _deleteMessageTx(
                  txn,
                  entityId,
                  payload['session_id']?.toString() ?? '',
                );
                changed = result.changed;
                if (result.changed) {
                  deletedMessageIds.add(entityId);
                  if (result.sessionId.isNotEmpty) {
                    changedSessionIds.add(result.sessionId);
                    deletedMessagesBySession
                        .putIfAbsent(result.sessionId, () => <String>[])
                        .add(entityId);
                  }
                }
              } else {
                final row = _messageRow(payload);
                final written = await _upsertRowTx(
                  txn,
                  table: 'messages',
                  primaryKey: 'msg_id',
                  primaryValue: entityId,
                  values: row,
                );
                changed = written;
                if (written) {
                  changedMessageRows.add(row);
                  final sid = row['session_id']?.toString().trim() ?? '';
                  if (sid.isNotEmpty) changedSessionIds.add(sid);
                }
              }
              break;
            case 'message.revoke':
              final result = await _deleteMessageTx(
                txn,
                entityId,
                payload['session_id']?.toString() ?? '',
              );
              changed = result.changed;
              if (result.changed) {
                deletedMessageIds.add(entityId);
                if (result.sessionId.isNotEmpty) {
                  changedSessionIds.add(result.sessionId);
                  deletedMessagesBySession
                      .putIfAbsent(result.sessionId, () => <String>[])
                      .add(entityId);
                }
              }
              break;
            case 'session.upsert':
              changed = await _upsertSessionTx(txn, payload, entityId);
              if (changed) changedSessionIds.add(entityId);
              break;
            case 'session.remove':
              final reason = payload['reason']?.toString().trim() ?? '';
              final isHistoryReset =
                  reason == 'history_reset' ||
                  (reason.isEmpty && payload.containsKey('deleted_at'));
              changed = await _deleteSessionTx(
                txn,
                entityId,
                deleteMessages: isHistoryReset,
              );
              if (changed) {
                deletedSessionIds.add(entityId);
                changedSessionIds.add(entityId);
              }
              if (!isHistoryReset) accessRevokedSessionIds.add(entityId);
              break;
            case 'session.unread_set':
              final sid = payload['session_id']?.toString().trim() ?? '';
              final target = sid.isEmpty ? entityId : sid;
              if (await _sessionProjectionIsTombstonedTx(txn, target)) {
                break;
              }
              changed = await _setSessionValuesTx(txn, target, {
                'unread_count': _int(payload['unread_count']).clamp(0, 1 << 31),
              });
              if (changed) changedSessionIds.add(target);
              break;
            case 'session.read_state':
              final sid = payload['session_id']?.toString().trim() ?? '';
              // Only the current user's own member projection uses the bare
              // session id. Peer read receipts have a composite entity id
              // and remain event metadata until a local receipt table exists.
              if (sid.isNotEmpty && entityId == sid) {
                if (await _sessionProjectionIsTombstonedTx(txn, sid)) {
                  break;
                }
                changed = await _setSessionValuesTx(txn, sid, {
                  'unread_count': _int(
                    payload['unread_count'],
                  ).clamp(0, 1 << 31),
                });
                if (changed) changedSessionIds.add(sid);
              }
              break;
            case 'session.pin_changed':
              changed = await _applyPinMuteTx(txn, payload, pin: true);
              if (changed) {
                changedSessionIds.addAll(
                  await _affectedSessionIds(txn, payload),
                );
              }
              break;
            case 'session.mute_changed':
              changed = await _applyPinMuteTx(txn, payload, pin: false);
              if (changed) {
                changedSessionIds.addAll(
                  await _affectedSessionIds(txn, payload),
                );
              }
              break;
            case 'membership.changed':
              membershipChangedSessionIds.add(entityId);
              final nestedSession = _map(payload['session']);
              if (nestedSession.isNotEmpty && !tombstone) {
                changed = await _upsertSessionTx(txn, {
                  ...nestedSession,
                  ..._membershipSessionProjection(payload),
                }, entityId);
                if (changed) changedSessionIds.add(entityId);
              }
              break;
            default:
              // Unknown additive events still advance the stream cursor.
              // Their version/tombstone is retained so an old projection
              // can never overwrite a newer state after an app upgrade.
              break;
          }

          if (versionRows.isEmpty) {
            await txn.insert('sync_entity_versions', {
              'entity_type': entityType,
              'entity_id': entityId,
              'state_version': version,
              'tombstone': tombstone ? 1 : 0,
              'last_event_cursor': cursor,
            });
          } else if (version > previousVersion ||
              tombstone != previousTombstone ||
              _int(versionRows.first['last_event_cursor']) < cursor) {
            await txn.update(
              'sync_entity_versions',
              {
                'state_version': version,
                'tombstone': tombstone ? 1 : 0,
                'last_event_cursor': cursor,
              },
              where: 'entity_type = ? AND entity_id = ?',
              whereArgs: [entityType, entityId],
            );
          }
          if (!changed) unchangedEvents++;
          if (commandId.isNotEmpty) {
            await _acknowledgeOutboxTx(txn, commandId);
            acknowledgedCommandIds.add(commandId);
          }
        }
        if (lastEventCursor != nextCursor) {
          throw const FormatException('sync_v2 batch cursor is not contiguous');
        }

        final snapshot = _map(batch['final_state_snapshot']);
        final unreadRaw = snapshot['unread_by_session'];
        if (unreadRaw is Map) {
          final unread = <String, int>{};
          unreadRaw.forEach((key, value) {
            final sid = key.toString().trim();
            if (sid.isNotEmpty) unread[sid] = _int(value).clamp(0, 1 << 31);
          });
          changedSessionIds.addAll(await _replaceUnreadSnapshotTx(txn, unread));
        }

        await _refreshAccountCountersTx(txn);
        final now = DateTime.now().millisecondsSinceEpoch;
        await txn.update(
          'sync_state',
          {
            'committed_cursor': nextCursor,
            'server_head_cursor': headCursor,
            'generation': generation,
            'updated_at': now,
          },
          where: 'stream_name = ?',
          whereArgs: [_streamName],
        );

        return LocalSyncApplyResult(
          committedCursor: nextCursor,
          changedMessageRows: changedMessageRows,
          deletedMessageIds: deletedMessageIds,
          deletedMessagesBySession: deletedMessagesBySession,
          changedSessionIds: changedSessionIds.toList(growable: false),
          deletedSessionIds: deletedSessionIds,
          accessRevokedSessionIds: accessRevokedSessionIds,
          membershipChangedSessionIds: membershipChangedSessionIds.toList(
            growable: false,
          ),
          acknowledgedCommandIds: acknowledgedCommandIds.toList(
            growable: false,
          ),
          unchangedEvents: unchangedEvents,
        );
      });
    });
  }

  /// Applies archive/bootstrap rows through the same entity-version barrier as
  /// realtime sync. A stale history page can therefore never resurrect a
  /// revoked message or overwrite a newer edit already received via sync_v2.
  static Future<LocalArchiveApplyResult> applyArchiveMessages(
    List<Map<String, dynamic>> messages,
  ) {
    if (messages.isEmpty) {
      return Future.value(const LocalArchiveApplyResult());
    }
    return LocalDb._withDatabaseOr<LocalArchiveApplyResult>(
      const LocalArchiveApplyResult(persisted: false),
      (db) async {
        return db.transaction((txn) async {
          final changedRows = <Map<String, dynamic>>[];
          final deletedMessageIds = <String>[];
          var unchangedCount = 0;
          for (final raw in messages) {
            final messageId = raw['msg_id']?.toString().trim() ?? '';
            if (messageId.isEmpty) {
              unchangedCount++;
              continue;
            }
            final version = _int(raw['state_version']);
            final tombstone = _bool(raw['is_revoked']);
            final versionRows = await txn.query(
              'sync_entity_versions',
              where: 'entity_type = ? AND entity_id = ?',
              whereArgs: ['message', messageId],
              limit: 1,
            );
            final previousVersion = versionRows.isEmpty
                ? -1
                : _int(versionRows.first['state_version']);
            final previousTombstone =
                versionRows.isNotEmpty && _bool(versionRows.first['tombstone']);
            if (version < previousVersion ||
                (version == previousVersion &&
                    previousTombstone &&
                    !tombstone)) {
              unchangedCount++;
              continue;
            }

            var changed = false;
            if (tombstone) {
              final deleted = await _deleteMessageTx(
                txn,
                messageId,
                raw['session_id']?.toString() ?? '',
              );
              changed = deleted.changed;
              if (changed) deletedMessageIds.add(messageId);
            } else {
              final row = _messageRow(raw);
              changed = await _upsertRowTx(
                txn,
                table: 'messages',
                primaryKey: 'msg_id',
                primaryValue: messageId,
                values: row,
              );
              if (changed) changedRows.add(row);
            }

            final previousCursor = versionRows.isEmpty
                ? 0
                : _int(versionRows.first['last_event_cursor']);
            if (versionRows.isEmpty) {
              await txn.insert('sync_entity_versions', {
                'entity_type': 'message',
                'entity_id': messageId,
                'state_version': version,
                'tombstone': tombstone ? 1 : 0,
                'last_event_cursor': 0,
              });
            } else if (version > previousVersion ||
                tombstone != previousTombstone) {
              await txn.update(
                'sync_entity_versions',
                {
                  'state_version': version,
                  'tombstone': tombstone ? 1 : 0,
                  'last_event_cursor': previousCursor,
                },
                where: 'entity_type = ? AND entity_id = ?',
                whereArgs: ['message', messageId],
              );
            }
            if (!changed) unchangedCount++;
          }
          return LocalArchiveApplyResult(
            changedRows: changedRows,
            deletedMessageIds: deletedMessageIds,
            unchangedCount: unchangedCount,
          );
        });
      },
    );
  }

  static Future<bool> applySessionSnapshot({
    required Map<String, dynamic> session,
    required int sessionStateVersion,
    required int memberStateVersion,
  }) {
    _requireActiveDatabase();
    final sid = session['session_id']?.toString().trim() ?? '';
    if (sid.isEmpty) return Future.value(false);
    return LocalDb._withDatabaseOr<bool>(false, (db) async {
      return db.transaction((txn) async {
        final sessionVersion = await _loadEntityVersionTx(txn, 'session', sid);
        final memberVersion = await _loadEntityVersionTx(
          txn,
          'session_member',
          sid,
        );
        final allowSession = _projectionMayApply(
          incomingVersion: sessionStateVersion,
          previous: sessionVersion,
        );
        if (!allowSession && sessionVersion.tombstone) return false;
        final allowMember = _projectionMayApply(
          incomingVersion: memberStateVersion,
          previous: memberVersion,
        );

        var changed = false;
        if (allowSession) {
          changed =
              await _upsertRowTx(
                txn,
                table: 'sessions',
                primaryKey: 'session_id',
                primaryValue: sid,
                values: {
                  'session_id': sid,
                  for (final field in const [
                    'type',
                    'peer_id',
                    'peer_type',
                    'peer_nickname',
                    'peer_username',
                    'updated_at',
                    'last_message',
                    'last_message_time',
                  ])
                    if (session.containsKey(field)) field: session[field],
                },
                insertDefaults: _sessionInsertDefaults,
              ) ||
              changed;
          await _recordProjectionVersionTx(
            txn,
            entityType: 'session',
            entityId: sid,
            incomingVersion: sessionStateVersion,
            previous: sessionVersion,
          );
        }
        if (allowMember) {
          changed =
              await _upsertRowTx(
                txn,
                table: 'sessions',
                primaryKey: 'session_id',
                primaryValue: sid,
                values: {
                  'session_id': sid,
                  for (final field in const [
                    'title',
                    'is_pinned',
                    'is_muted',
                    'pinned_at',
                    'unread_count',
                  ])
                    if (session.containsKey(field)) field: session[field],
                },
                insertDefaults: _sessionInsertDefaults,
              ) ||
              changed;
          await _recordProjectionVersionTx(
            txn,
            entityType: 'session_member',
            entityId: sid,
            incomingVersion: memberStateVersion,
            previous: memberVersion,
          );
        }

        // Peer preference versions are not yet exposed by the snapshot API.
        // They may initialize a missing projection but may not overwrite a
        // peer event that the v2 reducer has already versioned.
        final peerId = session['peer_id']?.toString().trim() ?? '';
        final peerVersion = peerId.isEmpty
            ? const _EntityVersion.none()
            : await _loadEntityVersionTx(txn, 'peer', peerId);
        if (!peerVersion.exists) {
          changed =
              await _upsertRowTx(
                txn,
                table: 'sessions',
                primaryKey: 'session_id',
                primaryValue: sid,
                values: {
                  'session_id': sid,
                  for (final field in const [
                    'friend_is_pinned',
                    'friend_pinned_at',
                    'friend_is_muted',
                  ])
                    if (session.containsKey(field)) field: session[field],
                },
                insertDefaults: _sessionInsertDefaults,
              ) ||
              changed;
        }
        return changed;
      });
    });
  }

  static Future<void> enqueueOutboxCommand({
    required String commandId,
    required String commandKind,
    required Map<String, dynamic> payload,
  }) async {
    _requireActiveDatabase();
    final id = commandId.trim();
    final kind = commandKind.trim();
    if (id.isEmpty || kind.isEmpty) return;
    await LocalDb._withDatabase<void>((db) async {
      final now = DateTime.now().millisecondsSinceEpoch;
      await db.insert('outbox', {
        'command_id': id,
        'command_kind': kind,
        'payload': jsonEncode(payload),
        'state': 'pending',
        'attempt_count': 0,
        'next_attempt_at': 0,
        'created_at': now,
        'updated_at': now,
      }, conflictAlgorithm: ConflictAlgorithm.ignore);
    });
  }

  static Future<void> applySessionCommandWithOutbox({
    required String sessionId,
    required Map<String, dynamic> sessionValues,
    required String commandId,
    required String commandKind,
    required Map<String, dynamic> payload,
  }) async {
    _requireActiveDatabase();
    final sid = sessionId.trim();
    final id = commandId.trim();
    final kind = commandKind.trim();
    if (sid.isEmpty || id.isEmpty || kind.isEmpty) {
      throw ArgumentError('session/outbox command fields must not be empty');
    }
    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final baseVersion = await _loadEntityVersionTx(
          txn,
          'session_member',
          sid,
        );
        await _upsertRowTx(
          txn,
          table: 'sessions',
          primaryKey: 'session_id',
          primaryValue: sid,
          values: {'session_id': sid, ...sessionValues},
          insertDefaults: _sessionInsertDefaults,
        );
        final now = DateTime.now().millisecondsSinceEpoch;
        await txn.insert('outbox', {
          'command_id': id,
          'command_kind': kind,
          'payload': jsonEncode({
            ...payload,
            '_optimistic_entity_type': 'session_member',
            '_optimistic_entity_id': sid,
            '_optimistic_base_version': baseVersion.version,
          }),
          'state': 'pending',
          'attempt_count': 0,
          'next_attempt_at': 0,
          'created_at': now,
          'updated_at': now,
        }, conflictAlgorithm: ConflictAlgorithm.ignore);
      });
    });
  }

  static Future<void> applyPeerCommandWithOutbox({
    required List<String> sessionIds,
    required Map<String, dynamic> sessionValues,
    required String commandId,
    required String commandKind,
    required Map<String, dynamic> payload,
  }) async {
    _requireActiveDatabase();
    final ids = sessionIds
        .map((id) => id.trim())
        .where((id) => id.isNotEmpty)
        .toSet();
    final id = commandId.trim();
    final kind = commandKind.trim();
    final peerId = payload['peer_user_id']?.toString().trim() ?? '';
    if (ids.isEmpty || id.isEmpty || kind.isEmpty || peerId.isEmpty) {
      throw ArgumentError('peer/outbox command fields must not be empty');
    }
    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final baseVersion = await _loadEntityVersionTx(txn, 'peer', peerId);
        for (final sid in ids) {
          await _upsertRowTx(
            txn,
            table: 'sessions',
            primaryKey: 'session_id',
            primaryValue: sid,
            values: {'session_id': sid, ...sessionValues},
            insertDefaults: _sessionInsertDefaults,
          );
        }
        final now = DateTime.now().millisecondsSinceEpoch;
        await txn.insert('outbox', {
          'command_id': id,
          'command_kind': kind,
          'payload': jsonEncode({
            ...payload,
            '_optimistic_entity_type': 'peer',
            '_optimistic_entity_id': peerId,
            '_optimistic_base_version': baseVersion.version,
          }),
          'state': 'pending',
          'attempt_count': 0,
          'next_attempt_at': 0,
          'created_at': now,
          'updated_at': now,
        }, conflictAlgorithm: ConflictAlgorithm.ignore);
      });
    });
  }

  static Future<void> insertLocalStubWithOutbox({
    required Map<String, dynamic> message,
    required String commandId,
    required String commandKind,
    required Map<String, dynamic> payload,
  }) async {
    _requireActiveDatabase();
    final id = commandId.trim();
    final kind = commandKind.trim();
    if (id.isEmpty || kind.isEmpty) {
      throw ArgumentError('outbox command id/kind must not be empty');
    }
    final filtered = LocalDbLifecycle._filterMessageColumns(message);
    final messageId = filtered['msg_id']?.toString().trim() ?? '';
    if (messageId.isEmpty) {
      throw ArgumentError('local message stub must have msg_id');
    }
    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        await txn.insert(
          'messages',
          filtered,
          conflictAlgorithm: ConflictAlgorithm.replace,
        );
        final now = DateTime.now().millisecondsSinceEpoch;
        await txn.insert('outbox', {
          'command_id': id,
          'command_kind': kind,
          'payload': jsonEncode(payload),
          'state': 'pending',
          'attempt_count': 0,
          'next_attempt_at': 0,
          'created_at': now,
          'updated_at': now,
        }, conflictAlgorithm: ConflictAlgorithm.ignore);
      });
    });
  }

  static Future<List<LocalOutboxCommand>> getPendingOutboxCommands({
    int limit = 100,
  }) {
    final safeLimit = limit.clamp(1, 100);
    return LocalDb._withDatabaseOr<List<LocalOutboxCommand>>(
      const <LocalOutboxCommand>[],
      (db) async {
        final now = DateTime.now().millisecondsSinceEpoch;
        final rows = await db.query(
          'outbox',
          where: "state = 'pending' AND next_attempt_at <= ?",
          whereArgs: [now],
          orderBy: 'created_at ASC',
          limit: safeLimit,
        );
        return rows
            .map((row) {
              Map<String, dynamic> payload = const <String, dynamic>{};
              try {
                final decoded = jsonDecode(row['payload']?.toString() ?? '{}');
                if (decoded is Map) {
                  payload = Map<String, dynamic>.from(decoded);
                }
              } catch (_) {}
              return LocalOutboxCommand(
                commandId: row['command_id']?.toString() ?? '',
                commandKind: row['command_kind']?.toString() ?? '',
                payload: payload,
                attemptCount: _int(row['attempt_count']),
                nextAttemptAt: _int(row['next_attempt_at']),
              );
            })
            .toList(growable: false);
      },
    );
  }

  /// Moves messages created by pre-v19 clients into the durable outbox.
  /// The command id is the existing local_seq, so retries remain idempotent.
  static Future<int> backfillPendingMessageOutbox() {
    return LocalDb._withDatabaseOr<int>(0, (db) async {
      return db.transaction((txn) async {
        final rows = await txn.query(
          'messages',
          where: 'local_seq IS NOT NULL AND status = ?',
          whereArgs: ['sending'],
          orderBy: 'created_at ASC',
        );
        var inserted = 0;
        final now = DateTime.now().millisecondsSinceEpoch;
        for (final row in rows) {
          final commandId = row['local_seq']?.toString().trim() ?? '';
          final sessionId = row['session_id']?.toString().trim() ?? '';
          final content = row['content']?.toString() ?? '';
          if (commandId.isEmpty || sessionId.isEmpty || content.isEmpty) {
            continue;
          }
          final payload = <String, dynamic>{
            'session_id': sessionId,
            'client_msg_id': commandId,
            'msg_type': _int(row['msg_type']) == 0 ? 1 : _int(row['msg_type']),
            'content': content,
          };
          final quoted = row['quoted_message_id']?.toString().trim() ?? '';
          if (quoted.isNotEmpty) payload['quoted_message_id'] = quoted;
          for (final field in const ['extra', 'visible_to']) {
            final raw = row[field]?.toString().trim() ?? '';
            if (raw.isEmpty) continue;
            try {
              final decoded = jsonDecode(raw);
              if (field == 'extra' && decoded is Map) {
                payload[field] = Map<String, dynamic>.from(decoded);
              } else if (field == 'visible_to' && decoded is List) {
                payload[field] = decoded;
              }
            } catch (_) {}
          }
          inserted += await txn.insert('outbox', {
            'command_id': commandId,
            'command_kind': 'send_msg',
            'payload': jsonEncode(payload),
            'state': 'pending',
            'attempt_count': 0,
            'next_attempt_at': 0,
            'created_at': _int(row['created_at']) == 0
                ? now
                : _int(row['created_at']),
            'updated_at': now,
          }, conflictAlgorithm: ConflictAlgorithm.ignore);
        }
        return inserted;
      });
    });
  }

  static Future<void> completeSendAck(
    String commandId,
    String msgId,
    int inboxSeq, {
    int? createdAt,
  }) async {
    final id = commandId.trim();
    final serverId = msgId.trim();
    if (id.isEmpty || serverId.isEmpty) return;
    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        await LocalDbMessageRepository._updateAckMsgTx(
          txn,
          id,
          serverId,
          inboxSeq,
          createdAt: createdAt,
        );
        await _acknowledgeOutboxTx(txn, id);
      });
    });
  }

  static Future<bool> tryAcquireWriterLease({
    required String ownerId,
    Duration ttl = const Duration(seconds: 15),
    int? nowMs,
  }) {
    final owner = ownerId.trim();
    if (owner.isEmpty || ttl <= Duration.zero) {
      throw ArgumentError('writer lease owner/ttl must be valid');
    }
    return LocalDb._withDatabaseOr<bool>(false, (db) async {
      return db.transaction((txn) async {
        final accountId = LocalDb.activeUserId ?? '';
        if (accountId.isEmpty) return false;
        final now = nowMs ?? DateTime.now().millisecondsSinceEpoch;
        final rows = await txn.query(
          'sync_writer_lease',
          where: 'account_id = ?',
          whereArgs: [accountId],
          limit: 1,
        );
        if (rows.isNotEmpty) {
          final existingOwner = rows.first['owner_id']?.toString() ?? '';
          final expiresAt = _int(rows.first['expires_at']);
          if (existingOwner != owner && expiresAt > now) return false;
        }
        await txn.insert('sync_writer_lease', {
          'account_id': accountId,
          'owner_id': owner,
          'expires_at': now + ttl.inMilliseconds,
          'updated_at': now,
        }, conflictAlgorithm: ConflictAlgorithm.replace);
        return true;
      });
    });
  }

  static Future<bool> renewWriterLease({
    required String ownerId,
    Duration ttl = const Duration(seconds: 15),
    int? nowMs,
  }) {
    final owner = ownerId.trim();
    if (owner.isEmpty || ttl <= Duration.zero) return Future.value(false);
    return LocalDb._withDatabaseOr<bool>(false, (db) async {
      final accountId = LocalDb.activeUserId ?? '';
      if (accountId.isEmpty) return false;
      final now = nowMs ?? DateTime.now().millisecondsSinceEpoch;
      final count = await db.update(
        'sync_writer_lease',
        {'expires_at': now + ttl.inMilliseconds, 'updated_at': now},
        where: 'account_id = ? AND owner_id = ? AND expires_at > ?',
        whereArgs: [accountId, owner, now],
      );
      return count == 1;
    });
  }

  static Future<void> releaseWriterLease({required String ownerId}) async {
    final owner = ownerId.trim();
    if (owner.isEmpty) return;
    await LocalDb._withDatabase<void>((db) async {
      final accountId = LocalDb.activeUserId ?? '';
      if (accountId.isEmpty) return;
      await db.delete(
        'sync_writer_lease',
        where: 'account_id = ? AND owner_id = ?',
        whereArgs: [accountId, owner],
      );
    });
  }

  static Future<void> markOutboxAttempt(
    String commandId, {
    required int nextAttemptAt,
  }) async {
    final id = commandId.trim();
    if (id.isEmpty) return;
    await LocalDb._withDatabase<void>((db) async {
      await db.rawUpdate(
        'UPDATE outbox SET attempt_count = attempt_count + 1, '
        'next_attempt_at = ?, updated_at = ? '
        "WHERE command_id = ? AND state = 'pending'",
        [nextAttemptAt, DateTime.now().millisecondsSinceEpoch, id],
      );
    });
  }

  static Future<void> acknowledgeOutboxCommand(String commandId) async {
    final id = commandId.trim();
    if (id.isEmpty) return;
    await LocalDb._withDatabase<void>((db) async {
      await _acknowledgeOutboxTx(db, id);
    });
  }

  /// Removes a terminally rejected command and restores its optimistic local
  /// projection in the same transaction, so a crash cannot leave one side
  /// applied without the other.
  static Future<void> rejectOutboxCommand(LocalOutboxCommand command) async {
    final id = command.commandId.trim();
    if (id.isEmpty) return;
    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final payload = command.payload;
        final sid = payload['session_id']?.toString().trim() ?? '';
        final projectionUnchanged = await _optimisticProjectionUnchangedTx(
          txn,
          payload,
        );
        switch (command.commandKind) {
          case 'session.pin':
            if (sid.isNotEmpty && projectionUnchanged) {
              await txn.update(
                'sessions',
                {
                  'is_pinned': _bool(payload['previous_is_pinned']) ? 1 : 0,
                  'pinned_at': _int(payload['previous_pinned_at']),
                },
                where: 'session_id = ?',
                whereArgs: [sid],
              );
            }
            break;
          case 'session.mute':
            if (sid.isNotEmpty && projectionUnchanged) {
              await txn.update(
                'sessions',
                {'is_muted': _bool(payload['previous_is_muted']) ? 1 : 0},
                where: 'session_id = ?',
                whereArgs: [sid],
              );
            }
            break;
          case 'peer.pin':
          case 'peer.mute':
            if (!projectionUnchanged) break;
            final rawStates = payload['previous_states'];
            if (rawStates is List) {
              for (final raw in rawStates) {
                if (raw is! Map) continue;
                final previousSid = raw['session_id']?.toString().trim() ?? '';
                if (previousSid.isEmpty) continue;
                if (command.commandKind == 'peer.pin') {
                  await txn.update(
                    'sessions',
                    {
                      'friend_is_pinned': _bool(raw['is_pinned']) ? 1 : 0,
                      'friend_pinned_at': _int(raw['pinned_at']),
                    },
                    where: 'session_id = ?',
                    whereArgs: [previousSid],
                  );
                } else {
                  await txn.update(
                    'sessions',
                    {'friend_is_muted': _bool(raw['is_muted']) ? 1 : 0},
                    where: 'session_id = ?',
                    whereArgs: [previousSid],
                  );
                }
              }
            }
            break;
          default:
            break;
        }
        await txn.delete('outbox', where: 'command_id = ?', whereArgs: [id]);
        await _refreshAccountCountersTx(txn);
      });
    });
  }

  static Future<Map<String, int>> getAccountCounters() {
    return LocalDb._withDatabaseOr<Map<String, int>>(
      const <String, int>{
        'total_unread': 0,
        'notification_unread': 0,
        'muted_unread': 0,
        'mention_unread': 0,
      },
      (db) async {
        final rows = await db.query(
          'account_counters',
          where: 'counter_id = 1',
          limit: 1,
        );
        if (rows.isEmpty) return const <String, int>{};
        final row = rows.first;
        return {
          'total_unread': _int(row['total_unread']),
          'notification_unread': _int(row['notification_unread']),
          'muted_unread': _int(row['muted_unread']),
          'mention_unread': _int(row['mention_unread']),
        };
      },
    );
  }

  static Future<_EntityVersion> _loadEntityVersionTx(
    DatabaseExecutor txn,
    String entityType,
    String entityId,
  ) async {
    final rows = await txn.query(
      'sync_entity_versions',
      where: 'entity_type = ? AND entity_id = ?',
      whereArgs: [entityType, entityId],
      limit: 1,
    );
    if (rows.isEmpty) return const _EntityVersion.none();
    final row = rows.first;
    return _EntityVersion(
      exists: true,
      version: _int(row['state_version']),
      tombstone: _bool(row['tombstone']),
      lastEventCursor: _int(row['last_event_cursor']),
    );
  }

  static Future<bool> _optimisticProjectionUnchangedTx(
    DatabaseExecutor txn,
    Map<String, dynamic> payload,
  ) async {
    final entityType =
        payload['_optimistic_entity_type']?.toString().trim() ?? '';
    final entityId = payload['_optimistic_entity_id']?.toString().trim() ?? '';
    if (entityType.isEmpty ||
        entityId.isEmpty ||
        !payload.containsKey('_optimistic_base_version')) {
      // Pending commands created before this guard was introduced retain the
      // original rollback behavior so an upgrade cannot strand their
      // optimistic projection forever.
      return true;
    }
    final current = await _loadEntityVersionTx(txn, entityType, entityId);
    return current.version == _int(payload['_optimistic_base_version']);
  }

  static bool _projectionMayApply({
    required int incomingVersion,
    required _EntityVersion previous,
  }) {
    if (!previous.exists) return true;
    if (incomingVersion < previous.version) return false;
    if (incomingVersion == previous.version && previous.tombstone) return false;
    return true;
  }

  static Future<void> _recordProjectionVersionTx(
    DatabaseExecutor txn, {
    required String entityType,
    required String entityId,
    required int incomingVersion,
    required _EntityVersion previous,
  }) async {
    if (!previous.exists) {
      await txn.insert('sync_entity_versions', {
        'entity_type': entityType,
        'entity_id': entityId,
        'state_version': incomingVersion,
        'tombstone': 0,
        'last_event_cursor': 0,
      });
      return;
    }
    if (incomingVersion <= previous.version && !previous.tombstone) return;
    await txn.update(
      'sync_entity_versions',
      {
        'state_version': incomingVersion,
        'tombstone': 0,
        'last_event_cursor': previous.lastEventCursor,
      },
      where: 'entity_type = ? AND entity_id = ?',
      whereArgs: [entityType, entityId],
    );
  }

  static Future<bool> _upsertSessionTx(
    DatabaseExecutor txn,
    Map<String, dynamic> payload,
    String fallbackId,
  ) async {
    final sid = payload['session_id']?.toString().trim().isNotEmpty == true
        ? payload['session_id'].toString().trim()
        : fallbackId;
    if (sid.isEmpty) return false;
    final sessionType = _int(payload['session_type']);
    final updatedAt = _timestampMs(payload['updated_at']);
    final groupName = payload['group_name']?.toString().trim() ?? '';
    final projectedTitle = payload['title']?.toString().trim() ?? '';
    final projectedType = payload['type']?.toString().trim() ?? '';
    final peerId = payload['peer_id']?.toString().trim() ?? '';
    final peerType = _int(payload['peer_type']);
    final values = <String, dynamic>{
      'session_id': sid,
      if (groupName.isNotEmpty)
        'title': groupName
      else if (projectedTitle.isNotEmpty)
        'title': projectedTitle,
      if (sessionType > 0)
        'type': sessionType == 2 ? 'group' : 'private'
      else if (projectedType.isNotEmpty)
        'type': projectedType,
      if (peerId.isNotEmpty) 'peer_id': peerId,
      if (peerType > 0) 'peer_type': peerType,
      if (updatedAt > 0) 'updated_at': updatedAt,
      if ((payload['last_msg_summary']?.toString() ?? '').isNotEmpty)
        'last_message': payload['last_msg_summary'].toString(),
      if (updatedAt > 0) 'last_message_time': updatedAt,
    };
    return _upsertRowTx(
      txn,
      table: 'sessions',
      primaryKey: 'session_id',
      primaryValue: sid,
      values: values,
      insertDefaults: const <String, dynamic>{
        'title': '',
        'type': 'private',
        'peer_id': '',
        'peer_type': 0,
        'peer_nickname': '',
        'peer_username': '',
        'is_pinned': 0,
        'is_muted': 0,
        'pinned_at': 0,
        'friend_is_pinned': 0,
        'friend_pinned_at': 0,
        'friend_is_muted': 0,
        'unread_count': 0,
      },
    );
  }

  static Map<String, dynamic> _membershipSessionProjection(
    Map<String, dynamic> payload,
  ) {
    final recipient = payload['recipient_user_id']?.toString().trim() ?? '';
    final session = _map(payload['session']);
    if (recipient.isEmpty || _int(session['session_type']) != 1) {
      return const <String, dynamic>{};
    }
    final rawMembers = payload['members'];
    if (rawMembers is! List) return const <String, dynamic>{};
    Map<String, dynamic>? self;
    Map<String, dynamic>? peer;
    for (final raw in rawMembers) {
      final member = _map(raw);
      if (member.isEmpty || _bool(member['is_tombstone'])) continue;
      final memberId = member['member_id']?.toString().trim() ?? '';
      if (memberId.isEmpty) continue;
      if (memberId == recipient && _int(member['member_type']) == 1) {
        self = member;
      } else {
        peer ??= member;
      }
    }
    if (peer == null) return const <String, dynamic>{};
    final customTitle = self?['custom_title']?.toString().trim() ?? '';
    final changeTitle =
        _map(payload['change'])['title']?.toString().trim() ?? '';
    return <String, dynamic>{
      'type': 'private',
      'peer_id': peer['member_id']?.toString() ?? '',
      'peer_type': _int(peer['member_type']),
      if (customTitle.isNotEmpty)
        'title': customTitle
      else if (changeTitle.isNotEmpty)
        'title': changeTitle,
    };
  }

  static Future<bool> _setSessionValuesTx(
    DatabaseExecutor txn,
    String sessionId,
    Map<String, dynamic> values,
  ) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    return _upsertRowTx(
      txn,
      table: 'sessions',
      primaryKey: 'session_id',
      primaryValue: sid,
      values: {'session_id': sid, ...values},
      insertDefaults: {
        'title': '',
        'type': 'private',
        'peer_id': '',
        'peer_type': 0,
        'peer_nickname': '',
        'peer_username': '',
        'updated_at': DateTime.now().millisecondsSinceEpoch,
        'is_pinned': 0,
        'is_muted': 0,
        'pinned_at': 0,
        'friend_is_pinned': 0,
        'friend_pinned_at': 0,
        'friend_is_muted': 0,
        'unread_count': 0,
      },
    );
  }

  static Future<bool> _applyPinMuteTx(
    DatabaseExecutor txn,
    Map<String, dynamic> payload, {
    required bool pin,
  }) async {
    final sid = payload['session_id']?.toString().trim() ?? '';
    final peerId = payload['peer_user_id']?.toString().trim() ?? '';
    final enabled = _bool(payload[pin ? 'is_pinned' : 'is_muted']);
    if (sid.isNotEmpty) {
      if (await _sessionProjectionIsTombstonedTx(txn, sid)) return false;
      return _setSessionValuesTx(txn, sid, {
        pin ? 'is_pinned' : 'is_muted': enabled ? 1 : 0,
        if (pin) 'pinned_at': enabled ? _timestampMs(payload['pinned_at']) : 0,
      });
    }
    if (peerId.isEmpty) return false;
    final rows = await txn.query(
      'sessions',
      columns: [
        'session_id',
        pin ? 'friend_is_pinned' : 'friend_is_muted',
        if (pin) 'friend_pinned_at',
      ],
      where: 'peer_id = ?',
      whereArgs: [peerId],
    );
    var changed = false;
    for (final row in rows) {
      final values = <String, dynamic>{
        pin ? 'friend_is_pinned' : 'friend_is_muted': enabled ? 1 : 0,
        if (pin)
          'friend_pinned_at': enabled ? _timestampMs(payload['pinned_at']) : 0,
      };
      final different = values.entries.any(
        (entry) => !_valueEquals(row[entry.key], entry.value),
      );
      if (!different) continue;
      await txn.update(
        'sessions',
        values,
        where: 'session_id = ?',
        whereArgs: [row['session_id']],
      );
      changed = true;
    }
    return changed;
  }

  static Future<List<String>> _affectedSessionIds(
    DatabaseExecutor txn,
    Map<String, dynamic> payload,
  ) async {
    final sid = payload['session_id']?.toString().trim() ?? '';
    if (sid.isNotEmpty) return [sid];
    final peerId = payload['peer_user_id']?.toString().trim() ?? '';
    if (peerId.isEmpty) return const <String>[];
    final rows = await txn.query(
      'sessions',
      columns: ['session_id'],
      where: 'peer_id = ?',
      whereArgs: [peerId],
    );
    return rows
        .map((row) => row['session_id']?.toString() ?? '')
        .where((value) => value.isNotEmpty)
        .toList(growable: false);
  }

  static Future<bool> _deleteSessionTx(
    DatabaseExecutor txn,
    String sessionId, {
    required bool deleteMessages,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    final deletedMessages = deleteMessages
        ? await txn.delete(
            'messages',
            where: 'session_id = ?',
            whereArgs: [sid],
          )
        : 0;
    final deletedSession = await txn.delete(
      'sessions',
      where: 'session_id = ?',
      whereArgs: [sid],
    );
    return deletedMessages > 0 || deletedSession > 0;
  }

  static Future<_DeletedMessageResult> _deleteMessageTx(
    DatabaseExecutor txn,
    String messageId,
    String sessionHint,
  ) async {
    final mid = messageId.trim();
    if (mid.isEmpty) return const _DeletedMessageResult();
    var sid = sessionHint.trim();
    if (sid.isEmpty) {
      final rows = await txn.query(
        'messages',
        columns: ['session_id'],
        where: 'msg_id = ?',
        whereArgs: [mid],
        limit: 1,
      );
      if (rows.isNotEmpty) sid = rows.first['session_id']?.toString() ?? '';
    }
    final deleted = await txn.delete(
      'messages',
      where: 'msg_id = ?',
      whereArgs: [mid],
    );
    if (deleted > 0 && sid.isNotEmpty) {
      final latest = await txn.rawQuery(
        'SELECT content, created_at FROM messages '
        'WHERE session_id = ? AND (msg_type IS NULL OR msg_type != 4) '
        "AND (status IS NULL OR TRIM(status) != 'error') "
        "AND NOT (TRIM(content) GLOB '[[]*](grix://card/*)') "
        'ORDER BY created_at DESC, msg_id DESC LIMIT 1',
        [sid],
      );
      await txn.update(
        'sessions',
        latest.isEmpty
            ? {'last_message': '', 'last_message_time': 0}
            : {
                'last_message': latest.first['content']?.toString() ?? '',
                'last_message_time': _int(latest.first['created_at']),
              },
        where: 'session_id = ?',
        whereArgs: [sid],
      );
    }
    return _DeletedMessageResult(changed: deleted > 0, sessionId: sid);
  }

  static Future<bool> _upsertRowTx(
    DatabaseExecutor txn, {
    required String table,
    required String primaryKey,
    required String primaryValue,
    required Map<String, dynamic> values,
    Map<String, dynamic> insertDefaults = const <String, dynamic>{},
  }) async {
    final incoming = <String, dynamic>{
      for (final entry in values.entries)
        entry.key: entry.value is bool
            ? (entry.value as bool ? 1 : 0)
            : entry.value,
    };
    incoming[primaryKey] = primaryValue;
    final rows = await txn.query(
      table,
      where: '$primaryKey = ?',
      whereArgs: [primaryValue],
      limit: 1,
    );
    if (rows.isEmpty) {
      await txn.insert(table, {...insertDefaults, ...incoming});
      return true;
    }
    final existing = rows.first;
    final changes = <String, dynamic>{};
    for (final entry in incoming.entries) {
      if (entry.key == primaryKey) continue;
      if (!_valueEquals(existing[entry.key], entry.value)) {
        changes[entry.key] = entry.value;
      }
    }
    if (changes.isEmpty) return false;
    await txn.update(
      table,
      changes,
      where: '$primaryKey = ?',
      whereArgs: [primaryValue],
    );
    return true;
  }

  static Map<String, dynamic> _messageRow(Map<String, dynamic> payload) {
    final row = Map<String, dynamic>.from(payload);
    row['msg_id'] = payload['msg_id']?.toString() ?? '';
    row['session_id'] = payload['session_id']?.toString() ?? '';
    row['sender_id'] = payload['sender_id']?.toString() ?? '';
    row['created_at'] = _timestampMs(payload['created_at']);
    row['status'] = row['status']?.toString().trim().isNotEmpty == true
        ? row['status']
        : 'sent';
    return LocalDbLifecycle._filterMessageColumns(row);
  }

  static Future<Set<String>> _replaceUnreadSnapshotTx(
    DatabaseExecutor txn,
    Map<String, int> unread,
  ) async {
    final changed = <String>{};
    final tombstoneRows = await txn.query(
      'sync_entity_versions',
      columns: ['entity_id'],
      where: "entity_type = 'session' AND tombstone = 1",
    );
    final tombstonedSessions = tombstoneRows
        .map((row) => row['entity_id']?.toString() ?? '')
        .where((id) => id.isNotEmpty)
        .toSet();
    final rows = await txn.query(
      'sessions',
      columns: ['session_id', 'unread_count'],
    );
    final existingIds = <String>{};
    for (final row in rows) {
      final sid = row['session_id']?.toString() ?? '';
      if (sid.isEmpty) continue;
      if (tombstonedSessions.contains(sid)) continue;
      existingIds.add(sid);
      final target = unread[sid] ?? 0;
      if (_int(row['unread_count']) == target) continue;
      await txn.update(
        'sessions',
        {'unread_count': target},
        where: 'session_id = ?',
        whereArgs: [sid],
      );
      changed.add(sid);
    }
    for (final entry in unread.entries) {
      if (entry.value <= 0 ||
          existingIds.contains(entry.key) ||
          tombstonedSessions.contains(entry.key)) {
        continue;
      }
      await _setSessionValuesTx(txn, entry.key, {'unread_count': entry.value});
      changed.add(entry.key);
    }
    return changed;
  }

  static Future<bool> _sessionProjectionIsTombstonedTx(
    DatabaseExecutor txn,
    String sessionId,
  ) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    final rows = await txn.query(
      'sync_entity_versions',
      columns: ['tombstone'],
      where: "entity_type = 'session' AND entity_id = ?",
      whereArgs: [sid],
      limit: 1,
    );
    return rows.isNotEmpty && _bool(rows.first['tombstone']);
  }

  static Future<void> _refreshAccountCountersTx(DatabaseExecutor txn) async {
    final totals = await txn.rawQuery(
      'SELECT '
      'COALESCE(SUM(unread_count), 0) AS total_unread, '
      'COALESCE(SUM(CASE WHEN is_muted = 1 OR friend_is_muted = 1 '
      'THEN unread_count ELSE 0 END), 0) AS muted_unread '
      'FROM sessions',
    );
    final total = totals.isEmpty ? 0 : _int(totals.first['total_unread']);
    final muted = totals.isEmpty ? 0 : _int(totals.first['muted_unread']);
    final notification = (total - muted).clamp(0, 1 << 31).toInt();
    final rows = await txn.query(
      'account_counters',
      where: 'counter_id = 1',
      limit: 1,
    );
    final values = <String, dynamic>{
      'total_unread': total,
      'notification_unread': notification,
      'muted_unread': muted,
      'updated_at': DateTime.now().millisecondsSinceEpoch,
    };
    if (rows.isEmpty) {
      await txn.insert('account_counters', {
        'counter_id': 1,
        'mention_unread': 0,
        ...values,
      });
      return;
    }
    if (_int(rows.first['total_unread']) == total &&
        _int(rows.first['notification_unread']) == notification &&
        _int(rows.first['muted_unread']) == muted) {
      return;
    }
    await txn.update('account_counters', values, where: 'counter_id = 1');
  }

  static Future<void> _acknowledgeOutboxTx(
    DatabaseExecutor txn,
    String commandId,
  ) async {
    final rows = await txn.query(
      'outbox',
      columns: ['command_kind'],
      where: 'command_id = ?',
      whereArgs: [commandId],
      limit: 1,
    );
    if (rows.isEmpty) return;
    if (rows.first['command_kind'] == 'session_history_reset') {
      // The local deleted-session marker intentionally survives restarts and
      // asks for reconciliation on every connection. Keep one compact receipt
      // so it cannot recreate and resend the same reset indefinitely.
      await txn.update(
        'outbox',
        {
          'state': 'acknowledged',
          'updated_at': DateTime.now().millisecondsSinceEpoch,
        },
        where: "command_id = ? AND state != 'acknowledged'",
        whereArgs: [commandId],
      );
      return;
    }
    // All other commands have either a committed stream receipt or a
    // committed v1 REST response. Their server-side idempotency receipt is
    // durable, so retaining them locally would make the outbox grow forever.
    await txn.delete('outbox', where: 'command_id = ?', whereArgs: [commandId]);
  }

  static bool _valueEquals(Object? left, Object? right) {
    if (left == right) return true;
    if (left == null || right == null) return false;
    final leftInt = StrictIntParser.tryParse(left);
    final rightInt = StrictIntParser.tryParse(right);
    if (leftInt != null && rightInt != null) return leftInt == rightInt;
    return left.toString() == right.toString();
  }

  static int _int(Object? value) => StrictIntParser.tryParse(value) ?? 0;

  static bool _bool(Object? value) {
    if (value is bool) return value;
    if (value is num) return value != 0;
    final normalized = value?.toString().trim().toLowerCase() ?? '';
    return normalized == '1' || normalized == 'true';
  }

  static Map<String, dynamic> _map(Object? value) {
    if (value is Map<String, dynamic>) return value;
    if (value is Map) return Map<String, dynamic>.from(value);
    return const <String, dynamic>{};
  }

  static int _timestampMs(Object? value) {
    final direct = StrictIntParser.tryParse(value);
    if (direct != null) {
      return LocalDbLifecycle._normalizeCreatedAt(direct);
    }
    final parsed = DateTime.tryParse(value?.toString() ?? '');
    return parsed?.millisecondsSinceEpoch ?? 0;
  }

  static void _requireActiveDatabase() {
    if (!LocalDb.hasActiveUser) {
      throw StateError('transactional sync requires an active user database');
    }
  }
}

class _DeletedMessageResult {
  const _DeletedMessageResult({this.changed = false, this.sessionId = ''});

  final bool changed;
  final String sessionId;
}
