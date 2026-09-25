part of 'im_service.dart';

class _SyncOutboxDispatchResult {
  const _SyncOutboxDispatchResult.accepted()
    : accepted = true,
      terminal = false;

  const _SyncOutboxDispatchResult.retryable()
    : accepted = false,
      terminal = false;

  const _SyncOutboxDispatchResult.terminal()
    : accepted = false,
      terminal = true;

  final bool accepted;
  final bool terminal;
}

extension _ImServiceSyncV2 on ImService {
  Future<bool> _ensureSyncWriterLease() async {
    if (!kIsWeb) return true;
    final acquired = await LocalDb.tryAcquireSyncWriterLease(
      ownerId: _syncWriterLeaseOwnerId,
    );
    if (!acquired) {
      _isReadOnlySyncFollower.value = true;
      _syncWriterLeaseRenewTimer?.cancel();
      _syncWriterLeaseRenewTimer = null;
      return false;
    }
    _isReadOnlySyncFollower.value = false;
    _syncWriterLeaseRenewTimer ??= Timer.periodic(
      const Duration(seconds: 5),
      (_) => unawaited(_renewSyncWriterLease()),
    );
    return true;
  }

  Future<void> _renewSyncWriterLease() async {
    if (!kIsWeb || _syncWriterLeaseRenewing) return;
    _syncWriterLeaseRenewing = true;
    try {
      final renewed = await LocalDb.renewSyncWriterLease(
        ownerId: _syncWriterLeaseOwnerId,
      );
      if (renewed) return;
      _syncWriterLeaseRenewTimer?.cancel();
      _syncWriterLeaseRenewTimer = null;
      _isReadOnlySyncFollower.value = true;
      _allowReconnect = false;
      _handleDisconnect(finalStage: ImConnectionStage.disconnected);
    } finally {
      _syncWriterLeaseRenewing = false;
    }
  }

  Future<void> _releaseSyncWriterLease() async {
    _syncWriterLeaseRenewTimer?.cancel();
    _syncWriterLeaseRenewTimer = null;
    _syncWriterLeaseRenewing = false;
    if (kIsWeb) {
      await LocalDb.releaseSyncWriterLease(ownerId: _syncWriterLeaseOwnerId);
    }
    _isReadOnlySyncFollower.value = false;
  }

  Future<void> _handleSyncV2AuthSuccess() async {
    await _runPostAuthSuccessStep(
      'ensure_deleted_sessions_loaded',
      _ensureDeletedSessionsLoaded,
    );
    await _runPostAuthSuccessStep(
      'ensure_pending_read_states_loaded',
      _ensurePendingReadStatesLoaded,
    );
    await _runPostAuthSuccessStep(
      'sync_deleted_session_history_resets',
      _syncDeletedSessionHistoryResets,
    );
    await _runPostAuthSuccessStep(
      'queue_deleted_session_read_clears',
      _queueDeletedSessionReadClears,
    );

    final generation = const Uuid().v4();
    await LocalDb.prepareSyncGeneration(generation);
    var state = await LocalDb.getSyncState();
    if (state.bootstrapCursor <= 0) {
      final bootstrapped = await _bootstrapSessionsForSyncV2();
      if (!bootstrapped) {
        _allowReconnect = true;
        _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
        return;
      }
      state = await LocalDb.getSyncState();
    }
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    _syncV2Generation = generation;
    // The server answers every resume with at least one batch, so the window
    // always closes on a has_more=false batch of this generation.
    _syncV2CatchingUp = true;
    final sent = _sendPacket({
      'cmd': 'sync_resume',
      'seq': _nextActionSeq(),
      'payload': {
        'generation': generation,
        'committed_cursor': state.committedCursor.toString(),
        // compound_v1 lets the server nest session/unread into message.upsert
        // and fold replay pages; LocalDb.applySyncBatch reduces both alike.
        'capabilities': const ['sync_v2', 'compound_v1'],
      },
    }, requireAuthenticated: true);
    if (!sent) return;

    await _runPostAuthSuccessStep(
      'backfill_pending_message_outbox',
      LocalDb.backfillPendingMessageOutbox,
    );

    await _runPostAuthSuccessStep(
      'flush_pending_session_reads',
      _flushPendingSessionReads,
    );
    await _runPostAuthSuccessStep('flush_sync_outbox', _flushSyncOutbox);
    await _runPostAuthSuccessStep(
      'trigger_delegate_list',
      _triggerDelegateList,
    );
    _runPostAuthSuccessStepInBackground('trigger_friend_sync', () async {
      // Friend requests are not a chat-domain durable event yet. Keep their
      // independent cursor until the server event catalog includes them.
      _triggerFriendSync();
    });
    await _runPostAuthSuccessStep(
      'restore_current_session_realtime_state',
      _restoreCurrentSessionRealtimeState,
    );
  }

  Future<bool> _bootstrapSessionsForSyncV2() async {
    final sessionService = _sessionServiceOrNull();
    if (sessionService == null) return false;
    final result = await sessionService.fetchSyncV2BootstrapSnapshotsResult(
      // Bootstrap must capture one stable ID window behind one primary head.
      // Offset pagination across requests can shift under concurrent inserts
      // and permanently omit a pre-head session, so v2 intentionally uses one
      // bounded snapshot request and rejects accounts above the bound.
      limit: ImService._syncV2BootstrapSessionLimit,
    );
    if (!result.success || result.hasMore) return false;
    await _upsertSessionsFromServerSnapshots(result.snapshots);
    await _applySyncV2BootstrapRecentMessages(result.snapshots);
    await _removeSessionsMissingFromServerSnapshots(result.snapshots);
    await LocalDb.markSyncBootstrapComplete(
      result.cursor,
      committedCursor: result.syncHeadCursor,
    );
    await loadSessions(
      refreshFromServer: false,
      backfillMissingPeerIdentities: false,
    );
    return true;
  }

  /// Persists the recent_messages a sync_head snapshot attached to each
  /// session, so the first enterSession renders completely from LocalDb
  /// instead of waiting for the history backfill.
  ///
  /// Best-effort on purpose: these messages sit before the bootstrap head and
  /// never replay as sync events, but a write failure must not fail the
  /// bootstrap itself — the session upsert and the cursor commit keep their
  /// original order, and the enterSession history reconcile/backfill remains
  /// the fallback for anything that did not land here.
  Future<void> _applySyncV2BootstrapRecentMessages(
    List<SessionSnapshot> snapshots,
  ) async {
    final messages = <Map<String, dynamic>>[];
    for (final snapshot in snapshots) {
      if (snapshot.recentMessages.isEmpty) continue;
      final sid = snapshot.sessionId.trim();
      if (sid.isEmpty) continue;
      // Mirror the session upsert suppression: writing messages for a locally
      // deleted session would resurrect it through the message-only
      // projection path.
      if (_shouldSuppressDeletedSession(sid, snapshot.updatedAt)) continue;
      messages.addAll(snapshot.recentMessages);
    }
    if (messages.isEmpty) return;
    try {
      // One call, one transaction: msg_id-keyed upserts behind the entity
      // version barrier make bootstrap retries and later sync events
      // idempotent, so the input order (msg_id DESC per session) is
      // irrelevant to the outcome.
      final result = await LocalDb.applyArchiveMessages(messages);
      if (!result.persisted) {
        debugPrint('sync_v2 bootstrap recent_messages not persisted');
      }
    } catch (e) {
      debugPrint('sync_v2 bootstrap recent_messages apply failed: $e');
    }
  }

  Future<void> _handleSyncV2Batch(Map<String, dynamic> payload) async {
    if (_activeSyncMode != 'v2') return;
    final generation = payload['generation']?.toString().trim() ?? '';
    if (generation.isEmpty || generation != _syncV2Generation) {
      debugPrint(
        'sync_v2 ignored stale generation=$generation active=$_syncV2Generation',
      );
      return;
    }
    if (_syncV2ApplyingBatch) {
      // The server guarantees one unacknowledged batch. Receiving another is
      // a protocol violation; reconnect from the durable local cursor.
      debugPrint('sync_v2 received concurrent batch; reconnecting');
      _allowReconnect = true;
      _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
      return;
    }

    _syncV2ApplyingBatch = true;
    try {
      final result = await LocalDb.applySyncBatch(payload);
      if (!result.persisted) {
        throw StateError('sync_v2 local database unavailable');
      }
      _publishSyncV2Changes(result);
      final sessionsChanged =
          result.changedSessionIds.isNotEmpty ||
          result.deletedSessionIds.isNotEmpty;
      final hasMore = _toBool(payload['has_more']);
      if (hasMore) _syncV2CatchingUp = true;
      if (_syncV2CatchingUp) {
        if (sessionsChanged) _syncV2SessionReloadDeferred = true;
        // A resume replays the durable event log batch by batch, and that log
        // carries every historical unread_set/read_state value. Publishing the
        // session projection after each batch makes the tab badge walk through
        // that history (3, 4, 0, 5, ...). Only the batch with has_more=false
        // carries the authoritative final snapshot, so the projection is
        // published once there; the bounded cap keeps a very long catch-up
        // from hiding progress entirely.
        if (hasMore &&
            _syncV2DeferredBatchCount < ImService._syncV2MaxDeferredBatches) {
          _syncV2DeferredBatchCount++;
        } else {
          _syncV2DeferredBatchCount = 0;
          if (!hasMore) _syncV2CatchingUp = false;
          if (_syncV2SessionReloadDeferred) {
            _syncV2SessionReloadDeferred = false;
            _scheduleSyncV2SessionReload(
              backfillMissingPeerIdentities: !hasMore,
            );
          }
        }
      } else if (sessionsChanged) {
        // Caught up: a live batch names every session it touched, so only
        // those rows are re-projected. A full reload per live batch decoded
        // every session row and latest message and kept an idle device busy.
        await _applySyncV2SessionDelta(result);
      }
      if (!_isConnected.value ||
          !_isAuthenticated.value ||
          _channel == null ||
          generation != _syncV2Generation) {
        return;
      }
      _sendPacket({
        'cmd': 'sync_ack',
        'seq': _nextActionSeq(),
        'payload': {
          'generation': generation,
          'committed_cursor': result.committedCursor.toString(),
        },
      }, requireAuthenticated: true);
      _syncOutboxRetryStreak = 0;
      unawaited(_flushSyncOutbox());
    } catch (e, st) {
      debugPrint('sync_v2 batch apply failed: $e\n$st');
      // Do not ACK. A fresh connection resumes from the transactionally
      // committed cursor and deterministically replays this batch.
      _allowReconnect = true;
      _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
    } finally {
      _syncV2ApplyingBatch = false;
    }
  }

  /// Flushes a deferred projection reload when the catch-up cannot finish on
  /// this connection, so LocalDb never stays ahead of the in-memory sessions
  /// for the whole offline period.
  void _flushDeferredSyncV2SessionReload() {
    _syncV2DeferredBatchCount = 0;
    _syncV2CatchingUp = false;
    if (!_syncV2SessionReloadDeferred) return;
    _syncV2SessionReloadDeferred = false;
    _scheduleSyncV2SessionReload();
  }

  void _scheduleSyncV2SessionReload({
    bool backfillMissingPeerIdentities = false,
  }) {
    _syncV2SessionReloadRequested = true;
    if (backfillMissingPeerIdentities) {
      _syncV2SessionReloadBackfillRequested = true;
    }
    if (_syncV2SessionReloadInFlight) return;
    _syncV2SessionReloadInFlight = true;
    unawaited(() async {
      try {
        do {
          _syncV2SessionReloadRequested = false;
          final backfill = _syncV2SessionReloadBackfillRequested;
          _syncV2SessionReloadBackfillRequested = false;
          // Private rows that arrived through v2 events carry no peer identity
          // (the event payload is the bare session model). The backfill is
          // already limited to unread private rows without a peer and to one
          // attempt per session, so running it once per finished catch-up
          // adds no periodic traffic.
          await loadSessions(
            refreshFromServer: false,
            backfillMissingPeerIdentities: backfill,
          );
          await _syncDeferredSystemUnreadBadgeAfterAuthoritativeRefresh();
        } while (_syncV2SessionReloadRequested);
      } finally {
        _syncV2SessionReloadInFlight = false;
      }
    }());
  }

  /// Re-projects only the sessions a live batch changed: two point reads per
  /// session through the same normalization as [loadSessions], one sort and
  /// one publish. Deleted ids are re-read too, so a session the batch removed
  /// drops out while one it removed and then refilled stays, as a full reload
  /// would decide.
  Future<void> _applySyncV2SessionDelta(LocalSyncApplyResult result) async {
    final sids = <String>{
      for (final sid in result.changedSessionIds) sid.trim(),
      for (final sid in result.deletedSessionIds) sid.trim(),
    }..remove('');
    if (sids.isEmpty) return;
    try {
      await _ensureDeletedSessionsLoaded();
      await _ensureRevokedSessionsLoaded();
      final projected = <String, SessionModel?>{};
      final suppressedDeleted = <String>{};
      final suppressedRevoked = <String>{};
      for (final sid in sids) {
        final sessionRow = await LocalDb.getSessionRecord(sid);
        final previewMessage = await LocalDb.getLatestPreviewableMessage(sid);
        projected[sid] = _sessionFromLocalRows(
          sid,
          sessionRow: sessionRow,
          previewMessage: previewMessage,
          suppressedDeleted: suppressedDeleted,
          suppressedRevoked: suppressedRevoked,
        );
      }

      final previousUnread = <String, int>{};
      final next = <SessionModel>[];
      for (final session in sessions) {
        final sid = session.sessionId.trim();
        if (projected.containsKey(sid)) {
          previousUnread[sid] = session.unreadCount;
        } else {
          next.add(session);
        }
      }
      for (final session in projected.values) {
        if (session != null) next.add(session);
      }
      next.sort(SessionModel.compareByPriority);
      // Published right after the last read, like loadSessions: LocalDb runs
      // serially, so publishes land in read order and an older full snapshot
      // can never overwrite these rows.
      _applyLoadedSessionsSnapshot(next);

      // Rows that arrive through sync events carry no peer identity; ask for
      // it the way live V1 messages do, only when a peerless private
      // session's unread actually grew.
      for (final entry in projected.entries) {
        final session = entry.value;
        if (session == null ||
            session.type != 'private' ||
            session.isVisitor ||
            session.peerId.trim().isNotEmpty ||
            session.unreadCount <= (previousUnread[entry.key] ?? 0)) {
          continue;
        }
        _schedulePeerIdentityBackfillForPeerlessSession(entry.key);
      }
      await _purgeSuppressedLocalSessions(suppressedDeleted, suppressedRevoked);
      await _syncDeferredSystemUnreadBadgeAfterAuthoritativeRefresh();
    } catch (e) {
      // The batch is already durable; never fail its ACK over the projection.
      debugPrint('sync_v2 session delta failed: $e');
    }
  }

  void _publishSyncV2Changes(LocalSyncApplyResult result) {
    final bySession = <String, List<Map<String, dynamic>>>{};
    for (final row in result.changedMessageRows) {
      final sid = row['session_id']?.toString().trim() ?? '';
      if (sid.isEmpty) continue;
      bySession.putIfAbsent(sid, () => <Map<String, dynamic>>[]).add(row);
    }
    for (final entry in bySession.entries) {
      final ids = entry.value
          .map((row) => row['msg_id']?.toString().trim() ?? '')
          .where((id) => id.isNotEmpty)
          .toList(growable: false);
      if (ids.isEmpty) continue;
      final maxCreatedAt = entry.value.fold<int>(0, (current, row) {
        final value = _normalizeMessageCreatedAt(_toInt(row['created_at']));
        return value > current ? value : current;
      });
      LocalDbChangeBus.instance.emitMessageChange(
        LocalMessagesInserted(
          sessionId: entry.key,
          msgIds: ids,
          maxCreatedAt: maxCreatedAt,
          rows: entry.value,
        ),
      );
      // Drop any cached window so a later re-enter loads from LocalDb
      // instead of restoring a pre-upsert snapshot. An open chat for this
      // session already merged via the bus event above; this only affects
      // idle / previously-viewed sessions (V1 push_msg has the same bus
      // publish, but leaveSession caches before the next enter).
      _cachedSessionWindows.remove(entry.key);
    }

    final currentSid = _currentSessionId.value?.trim() ?? '';
    for (final entry in result.deletedMessagesBySession.entries) {
      for (final msgId in entry.value) {
        LocalDbChangeBus.instance.emitMessageChange(
          LocalMessageRevoked(sessionId: entry.key, msgId: msgId),
        );
        if (entry.key == currentSid) {
          removeMessageFromCurrentSession(msgId);
        }
      }
    }
    for (final sid in result.changedSessionIds) {
      if (sid.isEmpty) continue;
      LocalDbChangeBus.instance.emitSessionChange(
        LocalSessionChanged(sessionId: sid),
      );
    }
    for (final sid in result.membershipChangedSessionIds) {
      _bumpSessionMemberEventVersion(sid);
    }
    for (final sid in result.accessRevokedSessionIds) {
      _markSessionAccessRevoked(sid, reason: 'sync_v2');
      unawaited(revokeSessionAccess(sid));
    }
  }

  Future<void> _flushSyncOutbox() async {
    if (_syncOutboxFlushInFlight) {
      _syncOutboxFlushRequested = true;
      return;
    }
    _syncOutboxFlushInFlight = true;
    try {
      do {
        _syncOutboxFlushRequested = false;
        await _flushSyncOutboxPass();
      } while (_syncOutboxFlushRequested);
    } finally {
      _syncOutboxFlushInFlight = false;
    }
  }

  Future<void> _flushSyncOutboxPass() async {
    _syncOutboxRetryTimer?.cancel();
    _syncOutboxRetryTimer = null;
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    final commands = await LocalDb.getPendingOutboxCommands();
    if (commands.isEmpty) return;
    var sentAny = false;
    for (final command in commands) {
      if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
        break;
      }
      if (_activeSyncMode != 'v2' &&
          !ImService._v1RestOutboxCommands.contains(command.commandKind)) {
        continue;
      }
      if (command.attemptCount >= ImService._syncOutboxMaxAttempts) {
        // A command that no receipt settled after this many sends will not be
        // settled by the next one either; stop the endless 30s resend loop.
        debugPrint(
          'sync outbox rejected after ${command.attemptCount} attempts '
          'kind=${command.commandKind} id=${command.commandId}',
        );
        await _rejectSyncOutboxCommand(command);
        continue;
      }
      final dispatch = await _dispatchSyncOutboxCommand(command);
      if (dispatch.terminal) {
        await _rejectSyncOutboxCommand(command);
        continue;
      }
      if (dispatch.accepted && _activeSyncMode != 'v2') {
        // These REST responses are returned only after the backend mutation
        // transaction commits. v1 has no sync event receipt, so the response
        // is the terminal durable acknowledgement for rollback compatibility.
        await LocalDb.acknowledgeOutboxCommand(command.commandId);
        continue;
      }
      final attempt = command.attemptCount + 1;
      final backoffSeconds = attempt <= 1
          ? 2
          : attempt == 2
          ? 5
          : attempt == 3
          ? 15
          : 30;
      await LocalDb.markOutboxAttempt(
        command.commandId,
        nextAttemptAt:
            DateTime.now().millisecondsSinceEpoch + backoffSeconds * 1000,
      );
      sentAny = true;
      if (!dispatch.accepted) break;
    }
    if (sentAny) {
      _syncOutboxRetryStreak = (_syncOutboxRetryStreak + 1).clamp(0, 4);
      final retrySeconds = _syncOutboxRetryStreak <= 1
          ? 2
          : _syncOutboxRetryStreak == 2
          ? 5
          : _syncOutboxRetryStreak == 3
          ? 15
          : 30;
      _syncOutboxRetryTimer = Timer(
        Duration(seconds: retrySeconds),
        () => unawaited(_flushSyncOutbox()),
      );
    }
  }

  Future<void> _rejectSyncOutboxCommand(LocalOutboxCommand command) async {
    await LocalDb.rejectOutboxCommand(command);
    _clearRejectedOutboxOverrides(command);
    await loadSessions(
      refreshFromServer: false,
      backfillMissingPeerIdentities: false,
    );
  }

  Future<void> _enqueueSyncOutboxAndFlush({
    required String commandId,
    required String commandKind,
    required Map<String, dynamic> payload,
  }) async {
    await LocalDb.enqueueOutboxCommand(
      commandId: commandId,
      commandKind: commandKind,
      payload: payload,
    );
    await _flushSyncOutbox();
  }

  Future<_SyncOutboxDispatchResult> _dispatchSyncOutboxCommand(
    LocalOutboxCommand command,
  ) async {
    final payload = Map<String, dynamic>.from(command.payload);
    final sessionService = _sessionServiceOrNull();
    switch (command.commandKind) {
      case 'session.pin':
        if (sessionService == null) {
          return const _SyncOutboxDispatchResult.retryable();
        }
        final result = await sessionService.setSessionPinnedResult(
          payload['session_id']?.toString() ?? '',
          isPinned: _toBool(payload['is_pinned']),
          commandId: command.commandId,
        );
        return _classifyOutboxHttpResult(
          success: result.code == 0,
          httpStatus: result.httpStatus,
          networkError: result.networkError,
        );
      case 'session.mute':
        if (sessionService == null) {
          return const _SyncOutboxDispatchResult.retryable();
        }
        final result = await sessionService.setSessionMutedResult(
          payload['session_id']?.toString() ?? '',
          isMuted: _toBool(payload['is_muted']),
          commandId: command.commandId,
        );
        return _classifyOutboxHttpResult(
          success: result.code == 0,
          httpStatus: result.httpStatus,
          networkError: result.networkError,
        );
      case 'message.revoke':
        if (sessionService == null) {
          return const _SyncOutboxDispatchResult.retryable();
        }
        final result = await sessionService.deleteMessageCommandResult(
          sessionId: payload['session_id']?.toString() ?? '',
          msgId: payload['msg_id']?.toString() ?? '',
          commandId: command.commandId,
        );
        return _classifyOutboxHttpResult(
          success: result.success,
          httpStatus: result.httpStatus,
          networkError: result.networkError,
        );
      case 'peer.pin':
        if (!Get.isRegistered<FriendService>()) {
          return const _SyncOutboxDispatchResult.retryable();
        }
        final result = await Get.find<FriendService>()
            .setFriendPinnedCommandResult(
              friendUserId: payload['peer_user_id']?.toString() ?? '',
              isPinned: _toBool(payload['is_pinned']),
              commandId: command.commandId,
            );
        return _classifyOutboxHttpResult(
          success: result.success,
          httpStatus: result.httpStatus,
          networkError: result.networkError,
        );
      case 'peer.mute':
        if (!Get.isRegistered<FriendService>()) {
          return const _SyncOutboxDispatchResult.retryable();
        }
        final result = await Get.find<FriendService>()
            .setFriendMutedCommandResult(
              friendUserId: payload['peer_user_id']?.toString() ?? '',
              isMuted: _toBool(payload['is_muted']),
              commandId: command.commandId,
            );
        return _classifyOutboxHttpResult(
          success: result.success,
          httpStatus: result.httpStatus,
          networkError: result.networkError,
        );
      default:
        payload['command_id'] = command.commandId;
        final sent = _sendPacket({
          'cmd': command.commandKind,
          'seq': _nextActionSeq(),
          'payload': payload,
        }, requireAuthenticated: true);
        return sent
            ? const _SyncOutboxDispatchResult.accepted()
            : const _SyncOutboxDispatchResult.retryable();
    }
  }

  _SyncOutboxDispatchResult _classifyOutboxHttpResult({
    required bool success,
    required int httpStatus,
    required bool networkError,
  }) {
    if (success) return const _SyncOutboxDispatchResult.accepted();
    if (networkError ||
        httpStatus == 0 ||
        httpStatus == 408 ||
        httpStatus == 429 ||
        httpStatus >= 500) {
      return const _SyncOutboxDispatchResult.retryable();
    }
    return const _SyncOutboxDispatchResult.terminal();
  }

  void _clearRejectedOutboxOverrides(LocalOutboxCommand command) {
    final payload = command.payload;
    final sid = payload['session_id']?.toString().trim() ?? '';
    if (sid.isNotEmpty) {
      _localPinOverrides.remove(sid);
    }
    final peerId = payload['peer_user_id']?.toString().trim() ?? '';
    if (peerId.isNotEmpty) {
      _peerMuteOverrides.remove(peerId);
      _peerMuteState.remove(peerId);
    }
    final sessionIds = payload['session_ids'];
    if (sessionIds is List) {
      for (final raw in sessionIds) {
        _localPinOverrides.remove(raw?.toString().trim() ?? '');
      }
    }
  }

  Future<bool> _revokeMessageThroughSync(String sessionId, String msgId) async {
    final sid = sessionId.trim();
    final mid = msgId.trim();
    if (sid.isEmpty || mid.isEmpty) return false;
    if (_activeSyncMode == 'v2') {
      final commandId = const Uuid().v4();
      await LocalDb.enqueueOutboxCommand(
        commandId: commandId,
        commandKind: 'message.revoke',
        payload: {'session_id': sid, 'msg_id': mid},
      );
      unawaited(_flushSyncOutbox());
      return true;
    }
    final sessionService = _sessionServiceOrNull();
    if (sessionService == null) return false;
    final success = await sessionService.deleteMessage(
      sessionId: sid,
      msgId: mid,
    );
    if (!success) return false;
    await applyLocalMessageRevoke(
      sessionId: sid,
      msgId: mid,
      dbOpLabel: 'deleteMessage(chat_revoke_success)',
    );
    return true;
  }
}
