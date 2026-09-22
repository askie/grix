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
    final sent = _sendPacket({
      'cmd': 'sync_resume',
      'seq': _nextActionSeq(),
      'payload': {
        'generation': generation,
        'committed_cursor': state.committedCursor.toString(),
        'capabilities': const ['sync_v2'],
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
      if (result.changedSessionIds.isNotEmpty ||
          result.deletedSessionIds.isNotEmpty) {
        _scheduleSyncV2SessionReload();
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

  void _scheduleSyncV2SessionReload() {
    _syncV2SessionReloadRequested = true;
    if (_syncV2SessionReloadInFlight) return;
    _syncV2SessionReloadInFlight = true;
    unawaited(() async {
      try {
        do {
          _syncV2SessionReloadRequested = false;
          await loadSessions(
            refreshFromServer: false,
            backfillMissingPeerIdentities: false,
          );
          await _syncDeferredSystemUnreadBadgeAfterAuthoritativeRefresh();
        } while (_syncV2SessionReloadRequested);
      } finally {
        _syncV2SessionReloadInFlight = false;
      }
    }());
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
      final dispatch = await _dispatchSyncOutboxCommand(command);
      if (dispatch.terminal) {
        await LocalDb.rejectOutboxCommand(command);
        _clearRejectedOutboxOverrides(command);
        await loadSessions(
          refreshFromServer: false,
          backfillMissingPeerIdentities: false,
        );
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
