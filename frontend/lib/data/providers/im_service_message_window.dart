part of 'im_service.dart';

extension _ImServiceMessageWindow on ImService {
  void _beginSessionWindowSync() {
    if (_sessionWindowSyncInflight <= 0) {
      _sessionWindowSyncInflightSinceMs = DateTime.now().millisecondsSinceEpoch;
    }
    _sessionWindowSyncInflight++;
  }

  void _endSessionWindowSync() {
    if (_sessionWindowSyncInflight <= 0) {
      _sessionWindowSyncInflight = 0;
      _sessionWindowSyncInflightSinceMs = 0;
      return;
    }
    _sessionWindowSyncInflight--;
    if (_sessionWindowSyncInflight <= 0) {
      _sessionWindowSyncInflight = 0;
      _sessionWindowSyncInflightSinceMs = 0;
    }
  }

  void _setInitialHistoryReadyIfCurrent(String sessionId, bool ready) {
    if (_currentSessionId.value != sessionId) return;
    if (initialHistoryReady.value == ready) return;
    initialHistoryReady.value = ready;
  }

  void _cacheCurrentSessionWindow(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty || currentMessages.isEmpty) return;

    final nowMs = _sessionWindowCacheNowMs();
    _purgeExpiredSessionWindowCaches(nowMs);
    final selected = <MessageModel>[];
    var contentChars = 0;
    for (
      var index = currentMessages.length - 1;
      index >= 0 && selected.length < ImService._sessionWindowCacheMessageLimit;
      index--
    ) {
      final message = currentMessages[index];
      final remainingChars =
          ImService._sessionWindowCacheMaxContentChars - contentChars;
      final messageChars = _estimateCachedMessageChars(message, remainingChars);
      if (messageChars > ImService._sessionWindowCacheMaxContentChars ||
          messageChars > remainingChars) {
        break;
      }
      selected.add(message);
      contentChars += messageChars;
    }
    if (selected.isEmpty) {
      _cachedSessionWindows.remove(sid);
      return;
    }
    final cachedMessages = selected.reversed.toList(growable: false);

    _MessageCursor? oldestCursor;
    _MessageCursor? newestCursor;
    for (final message in cachedMessages) {
      final cursor = _cursorFromMessage(message);
      if (cursor == null) continue;
      oldestCursor ??= cursor;
      newestCursor = cursor;
    }

    _cachedSessionWindows.remove(sid);
    _cachedSessionWindows[sid] = _CachedSessionWindowState(
      sessionId: sid,
      messages: List<MessageModel>.of(cachedMessages),
      oldestCursor: oldestCursor,
      newestCursor: newestCursor,
      hasOlder:
          _hasOlderMessages || cachedMessages.length < currentMessages.length,
      hasNewer: _hasNewerMessages,
      cachedAtMs: nowMs,
    );
    while (_cachedSessionWindows.length >
        ImService._sessionWindowCacheMaxEntries) {
      _cachedSessionWindows.remove(_cachedSessionWindows.keys.first);
    }
  }

  _CachedSessionWindowState? _takeCachedSessionWindow(String sessionId) {
    final nowMs = _sessionWindowCacheNowMs();
    _purgeExpiredSessionWindowCaches(nowMs);
    return _cachedSessionWindows.remove(sessionId.trim());
  }

  void _purgeExpiredSessionWindowCaches(int nowMs) {
    final ttlMs = ImService._sessionWindowCacheTtl.inMilliseconds;
    _cachedSessionWindows.removeWhere((_, cached) {
      final ageMs = nowMs - cached.cachedAtMs;
      return ageMs < 0 || ageMs > ttlMs;
    });
  }

  int _sessionWindowCacheNowMs() {
    return ImService.sessionWindowCacheNowMsForTest ??
        DateTime.now().millisecondsSinceEpoch;
  }

  int _estimateCachedMessageChars(MessageModel message, int limit) {
    var size =
        message.content.length +
        message.msgId.length +
        message.sessionId.length +
        message.senderId.length +
        (message.clientMsgId?.length ?? 0) +
        (message.status?.length ?? 0) +
        (message.agentDeliveryStatus?.length ?? 0) +
        (message.quotedMessageId?.length ?? 0) +
        64;
    if (size > limit) return size;
    size += _estimateCachedDynamicValue(message.extra, limit - size);
    if (size > limit) return size;
    size += _estimateCachedDynamicValue(message.visibleTo, limit - size);
    return size;
  }

  int _estimateCachedDynamicValue(Object? value, int limit) {
    if (limit < 0) return 1;
    if (value == null) return 0;
    if (value is String) return value.length;
    if (value is num || value is bool) return 8;

    var size = 16;
    if (value is List) {
      for (final item in value) {
        size += 8 + _estimateCachedDynamicValue(item, limit - size);
        if (size > limit) return size;
      }
      return size;
    }
    if (value is Map) {
      for (final entry in value.entries) {
        size +=
            8 +
            _estimateCachedDynamicValue(entry.key, limit - size) +
            _estimateCachedDynamicValue(entry.value, limit - size);
        if (size > limit) return size;
      }
      return size;
    }
    return value.toString().length;
  }

  void _enterSessionImpl(
    String sessionId, {
    Duration initialLoadDelay = Duration.zero,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (_currentSessionId.value != sid) {
      // 切换到不同会话：自动缓存当前活跃会话的消息窗口快照，
      // 使后续 re-enter 能瞬间恢复（chat→chat push 场景）。
      final leavingSid = (_currentSessionId.value ?? '').trim();
      _cacheCurrentSessionWindow(leavingSid);
      _stopSessionComposing(_currentSessionId.value ?? '', notifyRemote: true);
      _stopSessionViewing(_currentSessionId.value ?? '', notifyRemote: true);
    }
    _sessionWindowSyncPriorityUntilMs =
        DateTime.now().millisecondsSinceEpoch + 3000;
    // Use clearUnread instead of manual DB+in-memory clearing so that
    // _localUnreadOverride is set — this prevents _applyUnreadSync from
    // restoring a stale server unread count before the read receipt arrives.
    clearUnread(sid);
    _requestSessionActivityList(sid);
    _requestAgentOutputSnapshot(sid);
    _requestAgentToolbarSnapshot(sid);
    // 进入会话时主动拉一次队列快照，覆盖本地缓存。push 通道在 connector 重启 /
    // idle evict / 客户端短暂离线等边界场景会丢消息，靠这条 query/pull 兜底。
    _sendQueueSnapshotQueryImpl(sessionId: sid);
    _startSessionViewing(sid);

    // 先同步激活当前会话，避免首帧前到达的消息被误计入未读。
    _currentSessionId.value = sid;
    unawaited(PushFilterService.setActiveSessionID(sid));

    // Start DB change subscription for the new session (feature-gated).
    _startDbChangeSubscription();

    // If we have a cached window for this exact session, restore instantly
    // and do a background refresh — avoids the white-screen flash on re-entry.
    final cached = _takeCachedSessionWindow(sid);
    debugPrint(
      '🔵 _enterSessionImpl sid=$sid cached=${cached != null} '
      'cachedMsgs=${cached?.messages.length}',
    );
    if (cached != null && cached.messages.isNotEmpty) {
      _restoreSessionFromCache(cached);
      _setInitialHistoryReadyIfCurrent(sid, true);
      debugPrint(
        '🟢 CACHE HIT: restored ${cached.messages.length} messages for $sid',
      );
      Timer.run(() {
        if (_currentSessionId.value != sid) return;
        unawaited(_loadInitialMessages(sid));
      });
      return;
    }
    initialHistoryReady.value = false;
    debugPrint('🔴 CACHE MISS: full load for $sid');

    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_currentSessionId.value != sid) {
        Sentry.addBreadcrumb(
          Breadcrumb(
            category: 'msg_window',
            message: 'postFrameCallback: session changed, skipping load',
            data: {'expected': sid, 'current': _currentSessionId.value},
            level: SentryLevel.warning,
          ),
        );
        return;
      }
      _initialSessionLoadTimer?.cancel();
      if (initialLoadDelay <= Duration.zero) {
        _startInitialSessionMessageLoad(sid);
        return;
      }
      _initialSessionLoadTimer = Timer(initialLoadDelay, () {
        if (_currentSessionId.value != sid) {
          Sentry.addBreadcrumb(
            Breadcrumb(
              category: 'msg_window',
              message: 'timer: session changed, skipping load',
              data: {'expected': sid, 'current': _currentSessionId.value},
              level: SentryLevel.warning,
            ),
          );
          return;
        }
        _startInitialSessionMessageLoad(sid);
      });
    });
  }

  void _startInitialSessionMessageLoad(String sessionId) {
    initialHistoryReady.value = false;
    _resetMessageWindowState();
    _clearStreamDiagnostics(reason: 'enter_session');
    currentMessages.clear();
    _clearCurrentMessageIndexes();
    _initialLoadRetryTimer?.cancel();
    _initialLoadRetryTimer = null;
    _initialLoadRetryCount = 0;
    unawaited(_loadInitialMessages(sessionId));
  }

  void _restoreSessionFromCache(_CachedSessionWindowState cached) {
    _oldestHistoryCursor = cached.oldestCursor;
    _newestHistoryCursor = cached.newestCursor;
    _hasOlderMessages = cached.hasOlder;
    _hasNewerMessages = cached.hasNewer;
    _isLoadingOlderMessages = false;
    _isLoadingNewerMessages = false;
    _clearStreamDiagnostics(reason: 'enter_session_cache');
    currentMessages.assignAll(cached.messages);
    _rebuildCurrentMessageIndexes();
  }

  Future<void> _loadOlderForCurrentSessionImpl() async {
    final sid = currentSessionId;
    if (sid == null || _isLoadingOlderMessages || !_hasOlderMessages) return;
    _isLoadingOlderMessages = true;
    try {
      await _loadOlderMessages(sid);
    } finally {
      _isLoadingOlderMessages = false;
    }
  }

  Future<void> _loadNewerForCurrentSessionImpl() async {
    final sid = currentSessionId;
    if (sid == null || _isLoadingNewerMessages || !_hasNewerMessages) return;
    _isLoadingNewerMessages = true;
    try {
      await _loadNewerMessages(sid);
    } finally {
      _isLoadingNewerMessages = false;
    }
  }

  Future<void> _forceReloadSessionWindowImpl(
    String sessionId, {
    bool triggerPullSync = true,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    _sessionWindowSyncPriorityUntilMs =
        DateTime.now().millisecondsSinceEpoch + 3000;

    if (triggerPullSync && _isConnected.value && _isAuthenticated.value) {
      _triggerPullSyncThrottled();
      await Future<void>.delayed(const Duration(milliseconds: 220));
    }

    await _syncSessionHistoryBackfill(
      sessionId: sid,
      limit: ImService._messagePageSize,
    );

    if (_currentSessionId.value == sid) {
      await _loadInitialMessages(sid);
    }
  }

  Future<void> _loadInitialMessages(String sessionId) async {
    _beginSessionWindowSync();
    try {
      final dbMsgs = await LocalDb.getLatestMessages(
        sessionId,
        limit: ImService._initialMessageLimit + 1,
      );
      if (sessionId != currentSessionId) {
        Sentry.addBreadcrumb(
          Breadcrumb(
            category: 'msg_window',
            message: 'loadInitialMessages: session changed during local load',
            data: {'expected': sessionId, 'current': currentSessionId},
            level: SentryLevel.warning,
          ),
        );
        return;
      }

      // Render the local DB snapshot immediately. Remote backfill, if needed,
      // is handled in the background and always writes through LocalDb first.
      await _applyInitialMessageRows(
        sessionId: sessionId,
        dbMsgs: dbMsgs,
        remoteHasMore: false,
        remoteSyncFailed: false,
        remoteSyncSkipped: false,
        phase: 'local_snapshot',
      );

      // If local DB is empty, fire a non-blocking backfill. The backfill writes
      // to DB silently (no bus event); after completion, _reloadWindowFromDb
      // replaces the empty window with the fresh data from DB.
      //
      // If local DB is non-empty, still fire a non-blocking per-session history
      // reconcile against the server. This single path covers every "missing
      // message" case the older code split apart: a stale tail (newest local
      // message older than the server), a preview ahead of the messages table
      // (push_msg guardian timeout or an unfinalized type-4 streaming
      // placeholder), and — the case this replaced — a hole in the middle of
      // the window left by a dropped realtime push. The bus subscriber
      // incrementally appends whatever rows were missing.
      final localIsEmpty = dbMsgs.isEmpty;
      if (localIsEmpty) {
        late final Future<void> backfill;
        backfill = _backfillEmptyInitialWindow(sessionId).whenComplete(() {
          if (identical(_pendingInitialWindowBackfill, backfill)) {
            _pendingInitialWindowBackfill = null;
          }
        });
        _pendingInitialWindowBackfill = backfill;
        unawaited(backfill);
      } else {
        // 本地非空：进会话时拉取该会话最新一页与服务端对账，修复实时 push 抖动
        // 丢失留下的空洞——无论是末尾滞后还是窗口中间的断档。
        //
        // 注意：inbox_seq 是「按用户跨所有会话」的全局流水号，同一会话的相邻
        // 消息在全局序列里天然不连续（中间夹着其它会话的消息），因此无法用
        // 单会话内部的 seq 差来判断空洞——那样会每次进会话都误判、反复触发
        // 全量补齐却永远补不平。这里改为按 session_id 直取服务端权威列表，
        // 幂等写库后经变更总线增量补齐 UI（已渲染的本地快照不受影响，重复进
        // 会话只是幂等回写、不抖动）。
        unawaited(
          _syncSessionHistoryBackfill(
            sessionId: sessionId,
            limit: ImService._initialMessageLimit,
            // emitBusEvent: true — subscriber incrementally appends missing rows.
          ).catchError((Object e, StackTrace st) {
            Sentry.captureException(e, stackTrace: st);
            debugPrint('Session history reconcile backfill error: $e');
            return null;
          }),
        );
      }
    } catch (e, st) {
      Sentry.captureException(e, stackTrace: st);
      if (sessionId == currentSessionId) {
        _scheduleInitialLoadRetry(sessionId);
      }
    } finally {
      _endSessionWindowSync();
    }
  }

  Future<void> _backfillEmptyInitialWindow(String sessionId) async {
    try {
      final synced = await _syncSessionHistoryBackfill(
        sessionId: sessionId,
        limit: ImService._initialMessageLimit,
        emitBusEvent: false, // _reloadWindowFromDb handles window update.
      );
      if (_currentSessionId.value != sessionId) return;

      if (synced == null || synced.requestFailed) {
        _scheduleInitialLoadRetry(sessionId);
        return;
      }

      await _reloadWindowFromDb(sessionId);
    } catch (e, st) {
      Sentry.captureException(e, stackTrace: st);
      if (_currentSessionId.value == sessionId) {
        _scheduleInitialLoadRetry(sessionId);
      }
    }
  }

  /// Reload the current window entirely from local DB (used after backfill).
  Future<void> _reloadWindowFromDb(String sessionId) async {
    if (_currentSessionId.value != sessionId) return;
    final dbMsgs = await LocalDb.getLatestMessages(
      sessionId,
      limit: ImService._initialMessageLimit + 1,
    );
    if (_currentSessionId.value != sessionId) return;
    await _applyInitialMessageRows(
      sessionId: sessionId,
      dbMsgs: dbMsgs,
      remoteHasMore: false,
      remoteSyncFailed: false,
      remoteSyncSkipped: false,
      phase: 'backfill_reload',
    );
  }

  Future<void> _applyInitialMessageRows({
    required String sessionId,
    required List<Map<String, dynamic>> dbMsgs,
    required bool remoteHasMore,
    required bool remoteSyncFailed,
    required bool remoteSyncSkipped,
    required String phase,
  }) async {
    final hasLocalOverflow = dbMsgs.length > ImService._initialMessageLimit;
    final hasOlder = hasLocalOverflow || remoteHasMore || remoteSyncFailed;
    final visibleRows = hasLocalOverflow ? dbMsgs.sublist(1) : dbMsgs;
    final initialRenderContents = visibleRows
        .take(ImService._initialRenderCacheHydrationLimit)
        .map((row) => row['content']?.toString() ?? '')
        .where((content) => content.trim().isNotEmpty)
        .toList(growable: false);
    final newMsgs = visibleRows.map((m) => MessageModel.fromJson(m)).toList();
    for (final msg in newMsgs) {
      _reconcileActiveStreamingStateForUiMessage(msg);
    }
    // Snapshot streaming placeholders before assignAll replaces everything.
    // Concurrent events (agent_output_get_resp, stream_finish) processed at
    // await points above may have cleared _activeStreamingMsgIds, making
    // _restoreStreamingPlaceholdersForSession unable to recover them.
    // Capturing from both currentMessages and _streamingPlaceholders ensures
    // survival regardless of that state, even when currentMessages was
    // cleared before _loadInitialMessages (e.g. loadInitialWindowForTest).
    final seenStreamingMsgIds = <String>{};
    final preExistingStreamingPlaceholders = <MessageModel>[];
    for (final msg in currentMessages) {
      if (msg.msgType == 4 &&
          msg.msgId.isNotEmpty &&
          msg.sessionId.trim() == sessionId) {
        seenStreamingMsgIds.add(msg.msgId);
        preExistingStreamingPlaceholders.add(msg);
      }
    }
    for (final msg in _streamingPlaceholders.values) {
      if (msg.msgId.isNotEmpty &&
          msg.sessionId.trim() == sessionId &&
          !seenStreamingMsgIds.contains(msg.msgId)) {
        preExistingStreamingPlaceholders.add(msg);
      }
    }
    currentMessages.value = List<MessageModel>.of(newMsgs);
    Sentry.addBreadcrumb(
      Breadcrumb(
        category: 'msg_window',
        message: 'loadInitialMessages complete',
        data: {
          'session_id': sessionId,
          'msg_count': newMsgs.length,
          'db_count': dbMsgs.length,
          'remote_sync_failed': remoteSyncFailed,
          'remote_sync_skipped': remoteSyncSkipped,
          'phase': phase,
        },
        level: newMsgs.isEmpty ? SentryLevel.warning : SentryLevel.info,
      ),
    );
    _rebuildCurrentMessageIndexes();
    _restoreStreamingPlaceholdersForSession(sessionId);
    _restorePreExistingStreamingPlaceholders(
      preExistingStreamingPlaceholders,
      sessionId: sessionId,
    );
    _syncHistoryWindowAnchorsFromCurrent();
    _hasOlderMessages = hasOlder;
    _hasNewerMessages = false;
    unawaited(_queueSessionReadByKnownBoundary(sessionId));

    // Disk hydration is an optimization, not a prerequisite for first paint.
    // Run it after publishing the local snapshot so slow Windows storage never
    // holds the chat page blank. The initial window is deliberately small, so
    // an uncached first render remains bounded.
    _scheduleInitialRenderCacheHydration(sessionId, initialRenderContents);

    // 远程同步失败或被跳过，且本地无消息时，安排重试。
    if ((remoteSyncFailed || remoteSyncSkipped) && newMsgs.isEmpty) {
      Sentry.captureMessage(
        'MsgWindow: no messages after load, scheduling retry, '
        'sid=$sessionId failed=$remoteSyncFailed skipped=$remoteSyncSkipped',
        level: SentryLevel.warning,
      );
      _scheduleInitialLoadRetry(sessionId);
    } else if (newMsgs.isNotEmpty) {
      _initialLoadRetryCount = 0;
      _setInitialHistoryReadyIfCurrent(sessionId, true);
    } else if (phase == 'backfill_reload') {
      // 空窗 remote backfill 已结束（仍可能为空），此时才能判定真·空会话。
      _setInitialHistoryReadyIfCurrent(sessionId, true);
    }
  }

  void _scheduleInitialRenderCacheHydration(
    String sessionId,
    List<String> contents,
  ) {
    if (contents.isEmpty) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      // onClose/reset clears _currentSessionId, so this also fences callbacks
      // scheduled by a disposed or superseded chat session.
      if (_currentSessionId.value != sessionId) return;
      ImService.initialRenderHydrationStartedForTest?.call();
      unawaited(
        MessageBubble.hydrateFinalRenderStatesFromDisk(
          contents,
          maxEntries: ImService._initialRenderCacheHydrationLimit,
        ).catchError((Object error, StackTrace stackTrace) {
          Sentry.captureException(error, stackTrace: stackTrace);
        }),
      );
    });
  }

  void _scheduleInitialLoadRetry(String sessionId) {
    if (_initialLoadRetryCount >= ImService._maxInitialLoadRetries) {
      debugPrint(
        '⚠️ 初始消息加载重试已达上限 '
        '($_initialLoadRetryCount/${ImService._maxInitialLoadRetries})，'
        '放弃重试 session=$sessionId',
      );
      _setInitialHistoryReadyIfCurrent(sessionId, true);
      return;
    }
    _initialLoadRetryCount++;
    debugPrint(
      '🔄 安排初始消息加载重试 '
      '第$_initialLoadRetryCount次 session=$sessionId',
    );
    _initialLoadRetryTimer?.cancel();
    _initialLoadRetryTimer = Timer(ImService._initialLoadRetryDelay, () {
      _initialLoadRetryTimer = null;
      if (_currentSessionId.value != sessionId) return;
      unawaited(_loadInitialMessages(sessionId));
    });
  }

  Future<void> _loadOlderMessages(String sessionId) async {
    final cursor = _oldestHistoryCursor;
    if (cursor == null) {
      _hasOlderMessages = false;
      return;
    }

    try {
      var dbMsgs = await LocalDb.getMessagesBefore(
        sessionId,
        beforeCreatedAt: cursor.createdAt,
        beforeMsgId: cursor.msgId,
        limit: ImService._messagePageSize + 1,
      );
      if (dbMsgs.isEmpty) {
        unawaited(
          _backfillOlderWindow(sessionId: sessionId, beforeMsgId: cursor.msgId),
        );
      }
      if (sessionId != currentSessionId) return;

      if (dbMsgs.isEmpty) {
        return;
      }

      final hasLocalOverflow = dbMsgs.length > ImService._messagePageSize;
      final visibleRows = hasLocalOverflow ? dbMsgs.sublist(1) : dbMsgs;

      // Batch prepend: all loaded rows are older than the current window,
      // so parse once, filter duplicates, and insert at the front
      // in a single operation instead of per-message upsert.
      final newMsgs = visibleRows
          .map((row) => MessageModel.fromJson(row))
          .where(
            (msg) =>
                msg.msgId.isNotEmpty && !_currentMessageIds.contains(msg.msgId),
          )
          .toList();
      for (final msg in newMsgs) {
        _reconcileActiveStreamingStateForUiMessage(msg);
      }
      if (newMsgs.isNotEmpty) {
        currentMessages.insertAll(0, newMsgs);
        _rebuildCurrentMessageIndexes();
        for (final msg in newMsgs) {
          _cacheStreamingPlaceholder(msg);
        }
      }

      _trimCurrentMessagesFromBottom();
      _syncHistoryWindowAnchorsFromCurrent();

      if (hasLocalOverflow) {
        // Local cache still buffers older messages; keep paginating locally.
        _hasOlderMessages = true;
      } else {
        // Reached the bottom of the locally cached window. This is only a
        // recent slice of history (on Web the local DB starts fresh every
        // session), so do NOT conclude the conversation ends here. Consult
        // the server: the backfill fetches older rows into the local DB and
        // updates _hasOlderMessages from the server's authoritative hasMore.
        _hasOlderMessages = true;
        final floorMsgId = _oldestHistoryCursor?.msgId ?? cursor.msgId;
        unawaited(
          _backfillOlderWindow(sessionId: sessionId, beforeMsgId: floorMsgId),
        );
      }
    } catch (e) {
      debugPrint('Load older messages error: $e');
    }
  }

  Future<void> _backfillOlderWindow({
    required String sessionId,
    required String beforeMsgId,
  }) async {
    final sid = sessionId.trim();
    final before = beforeMsgId.trim();
    if (sid.isEmpty || before.isEmpty) return;

    final key = '$sid:$before';
    if (!_pendingOlderBackfillKeys.add(key)) return;
    try {
      final synced = await _syncSessionHistoryBackfill(
        sessionId: sid,
        beforeMsgId: before,
        limit: ImService._messagePageSize,
      );
      if (_currentSessionId.value != sid) return;
      if (synced == null || synced.requestFailed) {
        _hasOlderMessages = true;
        return;
      }
      _hasOlderMessages = synced.hasMore;
    } catch (e, st) {
      Sentry.captureException(e, stackTrace: st);
      debugPrint('Older message backfill error: $e');
      if (_currentSessionId.value == sid) {
        _hasOlderMessages = true;
      }
    } finally {
      _pendingOlderBackfillKeys.remove(key);
    }
  }

  Future<void> _loadNewerMessages(String sessionId) async {
    final cursor = _newestHistoryCursor;
    if (cursor == null) {
      _hasNewerMessages = false;
      return;
    }

    try {
      final dbMsgs = await LocalDb.getMessagesAfter(
        sessionId,
        afterCreatedAt: cursor.createdAt,
        afterMsgId: cursor.msgId,
        limit: ImService._messagePageSize + 1,
      );
      if (sessionId != currentSessionId) return;

      if (dbMsgs.isEmpty) {
        _hasNewerMessages = false;
        return;
      }

      final hasNewer = dbMsgs.length > ImService._messagePageSize;
      final visibleRows = hasNewer
          ? dbMsgs.sublist(0, ImService._messagePageSize)
          : dbMsgs;

      // Batch append: all loaded rows are newer than the current window.
      final newMsgs = visibleRows
          .map((row) => MessageModel.fromJson(row))
          .where(
            (msg) =>
                msg.msgId.isNotEmpty && !_currentMessageIds.contains(msg.msgId),
          )
          .toList();
      for (final msg in newMsgs) {
        _reconcileActiveStreamingStateForUiMessage(msg);
      }
      if (newMsgs.isNotEmpty) {
        currentMessages.addAll(newMsgs);
        _rebuildCurrentMessageIndexes();
        for (final msg in newMsgs) {
          _cacheStreamingPlaceholder(msg);
        }
      }
      _hasNewerMessages = hasNewer;
      _trimCurrentMessagesFromTop();
      _syncHistoryWindowAnchorsFromCurrent();
      unawaited(_queueSessionReadByKnownBoundary(sessionId));
    } catch (e) {
      debugPrint('Load newer messages error: $e');
    }
  }

  void _leaveSessionImpl([String? explicitSessionId]) {
    final leavingSessionId =
        (explicitSessionId ?? _currentSessionId.value ?? '').trim();
    Sentry.addBreadcrumb(
      Breadcrumb(
        category: 'msg_window',
        message: 'leaveSession',
        data: {
          'leaving': leavingSessionId,
          'current': _currentSessionId.value,
          'match': _currentSessionId.value == leavingSessionId,
        },
        level: SentryLevel.info,
      ),
    );
    _stopSessionComposing(leavingSessionId, notifyRemote: true);
    _stopSessionViewing(leavingSessionId, notifyRemote: true);
    if (leavingSessionId.isNotEmpty) {
      clearUnread(leavingSessionId);
    }
    // Only clear shared state if the active session hasn't been taken over by
    // a newer controller (e.g. Chat→Chat navigation via Get.offNamed).
    if (_currentSessionId.value == leavingSessionId) {
      // Cache window state before clearing so re-entry can restore instantly.
      debugPrint(
        '🟡 _leaveSessionImpl leaving=$leavingSessionId '
        'currentMsgs=${currentMessages.length}',
      );
      _cacheCurrentSessionWindow(leavingSessionId);
      _initialSessionLoadTimer?.cancel();
      _initialSessionLoadTimer = null;
      _initialLoadRetryTimer?.cancel();
      _initialLoadRetryTimer = null;
      _cancelDbChangeSubscription();
      _currentSessionId.value = null;
      unawaited(PushFilterService.setActiveSessionID(null));
      _resetMessageWindowState();
      initialHistoryReady.value = false;
      _clearStreamDiagnostics(reason: 'leave_session');
      currentMessages.clear();
      _clearCurrentMessageIndexes();
    }
  }

  void _restoreCurrentSessionRealtimeState() {
    final sid = _currentSessionId.value?.trim() ?? '';
    if (sid.isEmpty) {
      return;
    }
    _bumpSessionMemberEventVersion(sid);
    _requestSessionActivityList(sid);
    _requestAgentOutputSnapshot(sid);
    _requestAgentToolbarSnapshot(sid);
    // 主动拉一次队列快照，覆盖本地缓存。push 通道在 connector 重启 / idle evict /
    // 客户端短暂离线等边界场景会丢消息，靠这条 query/pull 兜底，确保前端永远不会卡在
    // stale 的"队列里有任务"状态。
    _sendQueueSnapshotQueryImpl(sessionId: sid);
    _startSessionViewing(sid);
    if (_composingActive && _composingSessionId == sid) {
      // 这里只补发一次 active:true 重建服务端状态，有意不重启续期/空闲
      // 定时器：断连已把它们取消，服务端 TTL 会自然兜底；下一次文本变化
      // 会经 shouldSendImmediately 重建两个定时器。
      _sendSessionActivitySet(sid, kind: 'composing', active: true);
    }
  }

  void _appendUIMessage(MessageModel msg) {
    _upsertUIMessageInOrder(msg);
    if (msg.sessionId == currentSessionId) {
      _trimCurrentMessagesFromTop();
      _syncHistoryWindowAnchorsFromCurrent();
    }
  }

  Future<bool> _consumeIncomingRevokedMessage(
    Map<String, dynamic> msgDict, {
    required String dbOpLabel,
    bool reloadSessions = true,
  }) async {
    if (!_toBool(msgDict['is_revoked'])) {
      return false;
    }

    final sid = msgDict['session_id']?.toString().trim() ?? '';
    final mid = msgDict['msg_id']?.toString().trim() ?? '';
    if (sid.isEmpty || mid.isEmpty) {
      return true;
    }

    return _applyLocalMessageRevokeImpl(
      sessionId: sid,
      msgId: mid,
      dbOpLabel: dbOpLabel,
      reloadSessions: reloadSessions,
    );
  }

  Future<bool> _applyLocalMessageRevokeImpl({
    required String sessionId,
    required String msgId,
    String dbOpLabel = 'deleteMessage(local_revoke)',
    bool reloadSessions = true,
    int? authoritativeUnreadCount,
  }) async {
    final sid = sessionId.trim();
    final mid = msgId.trim();
    if (sid.isEmpty || mid.isEmpty) {
      return true;
    }

    // Remove from active streaming state synchronously *before* the first
    // await so that concurrent stream_finish/stream_error handlers that arrive
    // during the DB operation see the correct state and skip re-inserting the
    // message into the UI via their _activeStreamingMsgIds guard.
    _activeStreamingMsgIds.remove(mid);
    _discardStreamingSessionPreview(mid);
    _clearStreamChunkGapTrackingForMessage(mid);
    MessageStreamController.finish(mid, '');

    final persisted = await _guardDbWrite(
      () => LocalDb.deleteMessage(mid),
      op: dbOpLabel,
    );
    if (!persisted) {
      return false;
    }
    LocalDbChangeBus.instance.emitMessageChange(
      LocalMessageRevoked(sessionId: sid, msgId: mid),
    );
    _removeUIMessage(mid);
    _clearAgentOutputStateForStreamMessage(sessionId: sid, msgId: mid);
    if (authoritativeUnreadCount != null) {
      await _setSessionUnreadCountLocal(sid, authoritativeUnreadCount);
    }

    if (reloadSessions) {
      // 撤回：本地已无可预览消息时必须清空摘要，避免被撤回的内容仍留在会话列表。
      await _refreshSessionPreviewFromLocal(sid, allowClearPreview: true);
    }
    return true;
  }

  void _removeUIMessage(String msgId) {
    final idx = currentMessages.indexWhere((m) => m.msgId == msgId);
    if (idx != -1) {
      currentMessages.removeAt(idx);
      _currentMessageIds.remove(msgId);
      _rebuildCurrentMessageIndexes();
      _syncHistoryWindowAnchorsFromCurrent();
    }
    if (msgId.isNotEmpty) {
      _streamingPlaceholders.remove(msgId);
    }
  }

  void _removeMessageFromCurrentSessionImpl(String msgId) {
    _removeUIMessage(msgId);
  }

  /// Update agentDeliveryStatus on a window message in-place.
  /// Used as a reliable fallback alongside bus events for status changes
  /// (e.g. retry_msg_ack) where DB row may not be available.
  void _updateAgentDeliveryStatusInWindow(String msgId, String status) {
    final idx = currentMessages.indexWhere((m) => m.msgId == msgId);
    if (idx != -1) {
      currentMessages[idx] = currentMessages[idx].copyWith(
        agentDeliveryStatus: status,
      );
    }
  }

  void _ackUIMessage(String clientMsgId, String msgId, [int? createdAt]) {
    final idx = currentMessages.indexWhere((e) => e.clientMsgId == clientMsgId);
    if (idx != -1) {
      // ACK confirms canonical server time; update UI ordering immediately to
      // avoid cross-device divergence caused by client clocks.
      final canonicalCreatedAt = (createdAt != null && createdAt > 0)
          ? createdAt
          : currentMessages[idx].createdAt;
      final merged = currentMessages[idx].copyWith(
        msgId: msgId,
        status: 'success',
        createdAt: canonicalCreatedAt,
      );
      _upsertUIMessageInOrder(merged);
    }
  }

  void _upsertUIMessageForTestImpl(MessageModel msg) {
    _upsertUIMessageInOrder(msg);
  }

  MessageModel? _messageInCurrentWindowOrPlaceholder(String msgId) {
    final normalizedMsgId = msgId.trim();
    if (normalizedMsgId.isEmpty) {
      return null;
    }
    final idx = currentMessages.indexWhere((m) => m.msgId == normalizedMsgId);
    if (idx != -1) {
      return currentMessages[idx];
    }
    return _streamingPlaceholders[normalizedMsgId];
  }

  void _normalizeCurrentMessageOrder() {
    final sorted = currentMessages.toList()
      ..sort((a, b) => _compareMessageOrder(a, b));
    currentMessages.assignAll(sorted);
    _rebuildCurrentMessageIndexes();
  }

  void _upsertUIMessageInOrder(MessageModel msg) {
    _reconcileActiveStreamingStateForUiMessage(msg);
    final msgId = msg.msgId;
    final clientMsgId = msg.clientMsgId ?? '';
    final hasKnownMsgId =
        msgId.isNotEmpty && _currentMessageIds.contains(msgId);
    final hasKnownClientId =
        clientMsgId.isNotEmpty &&
        _currentClientMessageIds.contains(clientMsgId);
    final maybeExisting = hasKnownMsgId || hasKnownClientId;

    if (!maybeExisting) {
      if (currentMessages.isEmpty) {
        currentMessages.add(msg);
        _trackMessageIndexes(msg);
        _cacheStreamingPlaceholder(msg);
        return;
      }
      if (_compareMessageOrder(currentMessages.last, msg) <= 0) {
        currentMessages.add(msg);
        _trackMessageIndexes(msg);
        _cacheStreamingPlaceholder(msg);
        return;
      }
      if (_compareMessageOrder(msg, currentMessages.first) <= 0) {
        currentMessages.insert(0, msg);
        _trackMessageIndexes(msg);
        _cacheStreamingPlaceholder(msg);
        return;
      }
      final fastInsertAt = _findInsertIndex(msg);
      currentMessages.insert(fastInsertAt, msg);
      _trackMessageIndexes(msg);
      _cacheStreamingPlaceholder(msg);
      return;
    }

    final removeSet = <int>{};
    if (msgId.isNotEmpty) {
      final idxByMsgId = currentMessages.indexWhere((e) => e.msgId == msgId);
      if (idxByMsgId != -1) {
        removeSet.add(idxByMsgId);
      }
    }
    if (clientMsgId.isNotEmpty) {
      final idxByClient = currentMessages.indexWhere(
        (e) => e.clientMsgId == clientMsgId,
      );
      if (idxByClient != -1) {
        removeSet.add(idxByClient);
      }
    }

    final mergedMsg = _mergeWithExistingUiMessageState(
      msg,
      removeSet,
      messages: currentMessages,
    );

    if (removeSet.length == 1) {
      final idx = removeSet.first;
      final prevValid =
          idx == 0 ||
          _compareMessageOrder(currentMessages[idx - 1], mergedMsg) <= 0;
      final nextValid =
          idx == currentMessages.length - 1 ||
          _compareMessageOrder(mergedMsg, currentMessages[idx + 1]) <= 0;
      if (prevValid && nextValid) {
        _untrackMessageIndexes(currentMessages[idx]);
        currentMessages[idx] = mergedMsg;
        _trackMessageIndexes(mergedMsg);
        _cacheStreamingPlaceholder(mergedMsg);
        return;
      }
    }

    if (removeSet.isNotEmpty) {
      final removeIndexes = removeSet.toList()..sort((a, b) => b.compareTo(a));
      for (final idx in removeIndexes) {
        _untrackMessageIndexes(currentMessages[idx]);
        currentMessages.removeAt(idx);
      }
    }

    if (currentMessages.isEmpty) {
      currentMessages.add(mergedMsg);
      _trackMessageIndexes(mergedMsg);
      _cacheStreamingPlaceholder(mergedMsg);
      return;
    }
    if (_compareMessageOrder(currentMessages.last, mergedMsg) <= 0) {
      currentMessages.add(mergedMsg);
      _trackMessageIndexes(mergedMsg);
      _cacheStreamingPlaceholder(mergedMsg);
      return;
    }
    if (_compareMessageOrder(mergedMsg, currentMessages.first) <= 0) {
      currentMessages.insert(0, mergedMsg);
      _trackMessageIndexes(mergedMsg);
      _cacheStreamingPlaceholder(mergedMsg);
      return;
    }
    final insertAt = _findInsertIndex(mergedMsg);
    currentMessages.insert(insertAt, mergedMsg);
    _trackMessageIndexes(mergedMsg);
    _cacheStreamingPlaceholder(mergedMsg);
  }

  void _reconcileActiveStreamingStateForUiMessage(MessageModel msg) {
    if (msg.msgType == 4) {
      return;
    }
    _clearActiveStreamingStateForMessage(msg.msgId);
  }

  void _clearActiveStreamingStateForMessage(String msgId) {
    final normalizedMsgId = msgId.trim();
    if (normalizedMsgId.isEmpty) {
      return;
    }
    _activeStreamingMsgIds.remove(normalizedMsgId);
    _discardStreamingSessionPreview(normalizedMsgId);
    _clearStreamChunkGapTrackingForMessage(normalizedMsgId);
  }

  void _clearActiveStreamingStateForSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty || _activeStreamingMsgIds.isEmpty) {
      return;
    }

    final trackedMsgIds = <String>{};
    for (final msg in currentMessages) {
      final msgId = msg.msgId.trim();
      if (msgId.isEmpty || msg.sessionId.trim() != sid) {
        continue;
      }
      trackedMsgIds.add(msgId);
    }
    for (final msg in _streamingPlaceholders.values) {
      final msgId = msg.msgId.trim();
      if (msgId.isEmpty || msg.sessionId.trim() != sid) {
        continue;
      }
      trackedMsgIds.add(msgId);
    }
    for (final msgId in trackedMsgIds) {
      _activeStreamingMsgIds.remove(msgId);
      _discardStreamingSessionPreview(msgId);
      _clearStreamChunkGapTrackingForMessage(msgId);
    }
  }

  void _observeStreamChunkGap({required String msgId, required int chunkSeq}) {
    final normalizedMsgId = msgId.trim();
    if (normalizedMsgId.isEmpty || chunkSeq <= 0) {
      return;
    }

    final pending = _streamPendingChunkSeqByMsg.putIfAbsent(
      normalizedMsgId,
      () => <int>{},
    );
    if (!pending.add(chunkSeq)) {
      return;
    }

    var expected = _streamExpectedChunkSeqByMsg[normalizedMsgId] ?? 1;
    while (pending.remove(expected)) {
      expected++;
    }
    _streamExpectedChunkSeqByMsg[normalizedMsgId] = expected;

    if (pending.isEmpty) {
      _streamGapRecoveryAttemptsByMsg.remove(normalizedMsgId);
      _streamGapRecoveryTimersByMsg.remove(normalizedMsgId)?.cancel();
      return;
    }

    _scheduleStreamChunkGapRecovery(normalizedMsgId);
  }

  void _scheduleStreamChunkGapRecovery(String msgId) {
    if (msgId.isEmpty) {
      return;
    }
    if (_streamGapRecoveryTimersByMsg.containsKey(msgId)) {
      return;
    }
    final attempts = _streamGapRecoveryAttemptsByMsg[msgId] ?? 0;
    if (attempts >= ImService._streamGapRecoveryMaxAttempts) {
      return;
    }

    _streamGapRecoveryTimersByMsg[msgId] = Timer(
      ImService._streamGapRecoveryDelay,
      () {
        _streamGapRecoveryTimersByMsg.remove(msgId);
        if (!_isTrackedStreamingMessage(msgId)) {
          _clearStreamChunkGapTrackingForMessage(msgId);
          return;
        }

        final pending = _streamPendingChunkSeqByMsg[msgId];
        if (pending == null || pending.isEmpty) {
          _streamGapRecoveryAttemptsByMsg.remove(msgId);
          return;
        }

        final currentAttempts =
            (_streamGapRecoveryAttemptsByMsg[msgId] ?? 0) + 1;
        _streamGapRecoveryAttemptsByMsg[msgId] = currentAttempts;
        if (_lastInboxSeqCursor > 0) {
          _triggerPullSyncThrottled(cursorOverride: _lastInboxSeqCursor);
        } else {
          _triggerPullSyncThrottled();
        }

        if (_isTrackedStreamingMessage(msgId)) {
          _scheduleStreamChunkGapRecovery(msgId);
        } else {
          _clearStreamChunkGapTrackingForMessage(msgId);
        }
      },
    );
  }

  void _clearStreamChunkGapTrackingForMessage(String msgId) {
    final normalizedMsgId = msgId.trim();
    if (normalizedMsgId.isEmpty) {
      return;
    }
    _streamExpectedChunkSeqByMsg.remove(normalizedMsgId);
    _streamPendingChunkSeqByMsg.remove(normalizedMsgId);
    _streamGapRecoveryAttemptsByMsg.remove(normalizedMsgId);
    _streamGapRecoveryTimersByMsg.remove(normalizedMsgId)?.cancel();
  }

  void _clearStreamChunkGapTrackingState() {
    for (final timer in _streamGapRecoveryTimersByMsg.values) {
      timer.cancel();
    }
    _streamGapRecoveryTimersByMsg.clear();
    _streamExpectedChunkSeqByMsg.clear();
    _streamPendingChunkSeqByMsg.clear();
    _streamGapRecoveryAttemptsByMsg.clear();
  }

  bool _hasActiveLocalStreamForSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return false;
    }
    return _localInferenceInFlight.contains(sid) ||
        _localStreamRenderMsgIds.containsKey(sid) ||
        _localStreamServerMsgIds.containsKey(sid);
  }

  void _cacheStreamingPlaceholder(MessageModel msg) {
    final msgId = msg.msgId.trim();
    if (msgId.isEmpty) {
      return;
    }
    if (msg.msgType == 4 && msg.sessionId.trim().isNotEmpty) {
      _streamingPlaceholders[msgId] = msg;
      return;
    }
    _streamingPlaceholders.remove(msgId);
  }

  MessageModel _buildStreamingPlaceholderFromPayload(
    Map payload, {
    required String msgId,
    required String sessionId,
  }) {
    // 流式期就带上引用目标:后端随 stream_chunk 下发 quoted_message_id,
    // 据此渲染"引用回复"。缺省/0 视为无引用。
    final rawQuoted = payload['quoted_message_id']?.toString().trim() ?? '';
    final quotedMessageId = (rawQuoted.isEmpty || rawQuoted == '0')
        ? null
        : rawQuoted;
    return MessageModel(
      msgId: msgId,
      sessionId: sessionId,
      senderId: payload['sender_id']?.toString() ?? '',
      senderType: _toInt(payload['sender_type']) > 0
          ? _toInt(payload['sender_type'])
          : 1,
      msgType: 4,
      content: '',
      createdAt: _resolveStreamChunkCreatedAt(
        msgId: msgId,
        incomingCreatedAt: payload['created_at'],
      ),
      isThinking: _toBool(payload['is_thinking']),
      quotedMessageId: quotedMessageId,
      // 流式期就带上 visible_to:隐藏消息的回复从首个 chunk 起即渲染锁标记,
      // 而非等 stream_finish 收尾才补上。
      visibleTo: MessageModel.readVisibleTo(payload['visible_to']),
    );
  }

  void _restoreStreamingPlaceholdersForSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty || _streamingPlaceholders.isEmpty) {
      return;
    }
    final active =
        _streamingPlaceholders.values
            .where((msg) {
              return msg.sessionId == sid &&
                  _activeStreamingMsgIds.contains(msg.msgId);
            })
            .toList(growable: false)
          ..sort((a, b) => _compareMessageOrder(a, b));
    for (final msg in active) {
      if (_currentMessageIds.contains(msg.msgId)) {
        continue;
      }
      _upsertUIMessageInOrder(msg);
    }
  }

  /// Restores streaming placeholders that existed in the window before
  /// [currentMessages.assignAll] replaced it. Unlike
  /// [_restoreStreamingPlaceholdersForSession], this does NOT depend on
  /// [_activeStreamingMsgIds] — it re-inserts any placeholder that isn't
  /// already represented in the new window (by msgId or clientMsgId),
  /// preventing data loss when a concurrent event (e.g. terminal
  /// agent_output_get_resp) clears the active streaming set during the
  /// async [_loadInitialMessages] execution.
  void _restorePreExistingStreamingPlaceholders(
    List<MessageModel> placeholders, {
    required String sessionId,
  }) {
    if (placeholders.isEmpty) return;
    final sid = sessionId.trim();
    for (final msg in placeholders) {
      if (msg.sessionId.trim() != sid) continue;
      if (_currentMessageIds.contains(msg.msgId)) continue;
      final clientId = msg.clientMsgId?.trim() ?? '';
      if (clientId.isNotEmpty && _currentClientMessageIds.contains(clientId)) {
        continue;
      }
      _upsertUIMessageInOrder(msg);
    }
  }

  int _resolveStreamChunkCreatedAt({
    required String msgId,
    required dynamic incomingCreatedAt,
  }) {
    final incoming = _normalizeMessageCreatedAt(_toInt(incomingCreatedAt));
    if (incoming > 0) {
      return incoming;
    }
    final existing = _streamingPlaceholders[msgId]?.createdAt ?? 0;
    if (existing > 0) {
      return existing;
    }
    final idx = currentMessages.indexWhere((m) => m.msgId == msgId);
    if (idx != -1) {
      final currentCreatedAt = currentMessages[idx].createdAt;
      if (currentCreatedAt > 0) {
        return currentCreatedAt;
      }
    }
    return DateTime.now().millisecondsSinceEpoch;
  }

  MessageModel _mergeWithExistingUiMessageState(
    MessageModel incoming,
    Set<int> removeSet, {
    required List<MessageModel> messages,
  }) {
    if (removeSet.isEmpty) {
      return incoming;
    }

    MessageModel merged = incoming;
    for (final idx in removeSet) {
      final existing = messages[idx];
      if (merged.sessionId.trim().isEmpty &&
          existing.sessionId.trim().isNotEmpty) {
        merged = merged.copyWith(sessionId: existing.sessionId);
      }
      if (merged.content.isEmpty && existing.content.isNotEmpty) {
        merged = merged.copyWith(content: existing.content);
      }
      if ((merged.agentDeliveryStatus ?? '').trim().isEmpty &&
          (existing.agentDeliveryStatus ?? '').trim().isNotEmpty) {
        merged = merged.copyWith(
          agentDeliveryStatus: existing.agentDeliveryStatus,
        );
      }
      if ((merged.status ?? '').trim().isEmpty &&
          (existing.status ?? '').trim().isNotEmpty) {
        if (_shouldPreserveExistingUiStatus(
          existing: existing,
          incoming: merged,
        )) {
          merged = merged.copyWith(status: existing.status);
        }
      }
      if ((merged.clientMsgId ?? '').trim().isEmpty &&
          (existing.clientMsgId ?? '').trim().isNotEmpty) {
        merged = merged.copyWith(clientMsgId: existing.clientMsgId);
      }
      if (merged.extra.isEmpty && existing.extra.isNotEmpty) {
        merged = merged.copyWith(
          extra: Map<String, dynamic>.from(existing.extra),
        );
      }
      if ((merged.quotedMessageId ?? '').trim().isEmpty &&
          (existing.quotedMessageId ?? '').trim().isNotEmpty) {
        merged = merged.copyWith(quotedMessageId: existing.quotedMessageId);
      }
      // visible_to 只增不减:窗口内已带锁标记的消息,不因后续某条缺省
      // visible_to 的上行数据(快照/兜底路径)覆盖而丢锁。
      if ((merged.visibleTo == null || merged.visibleTo!.isEmpty) &&
          (existing.visibleTo?.isNotEmpty ?? false)) {
        merged = merged.copyWith(visibleTo: existing.visibleTo);
      }
      // isThinking is UI-only and is not persisted in LocalDb. A history row
      // for the same active type-4 placeholder must not erase that marker.
      if (merged.msgType == 4 && !merged.isThinking && existing.isThinking) {
        merged = merged.copyWith(isThinking: true);
      }
    }
    return merged;
  }

  bool _shouldPreserveExistingUiStatus({
    required MessageModel existing,
    required MessageModel incoming,
  }) {
    final existingStatus = (existing.status ?? '').trim();
    if (existingStatus.isEmpty) {
      return false;
    }
    if (!_isTransientLocalUiStatus(existingStatus)) {
      return true;
    }
    return !_isAuthoritativeServerMessage(incoming);
  }

  bool _isTransientLocalUiStatus(String status) {
    final normalized = status.trim();
    return normalized.startsWith('sending') || normalized.startsWith('failed');
  }

  bool _isAuthoritativeServerMessage(MessageModel message) {
    final msgId = message.msgId.trim();
    return msgId.isNotEmpty && !msgId.startsWith('temp_');
  }

  void _trackMessageIndexes(MessageModel msg) {
    if (msg.msgId.isNotEmpty) {
      _currentMessageIds.add(msg.msgId);
    }
    final clientMsgId = msg.clientMsgId;
    if (clientMsgId != null && clientMsgId.isNotEmpty) {
      _currentClientMessageIds.add(clientMsgId);
    }
  }

  void _untrackMessageIndexes(MessageModel msg) {
    if (msg.msgId.isNotEmpty) {
      _currentMessageIds.remove(msg.msgId);
    }
    final clientMsgId = msg.clientMsgId;
    if (clientMsgId != null && clientMsgId.isNotEmpty) {
      _currentClientMessageIds.remove(clientMsgId);
    }
  }

  void _rebuildCurrentMessageIndexes() {
    _currentMessageIds.clear();
    _currentClientMessageIds.clear();
    for (final msg in currentMessages) {
      _trackMessageIndexes(msg);
    }
  }

  void _clearCurrentMessageIndexes() {
    _currentMessageIds.clear();
    _currentClientMessageIds.clear();
  }

  void _resetMessageWindowState() {
    _oldestHistoryCursor = null;
    _newestHistoryCursor = null;
    _hasOlderMessages = true;
    _hasNewerMessages = false;
    _isLoadingOlderMessages = false;
    _isLoadingNewerMessages = false;
  }

  _MessageCursor? _cursorFromMessage(MessageModel msg) {
    if (msg.msgId.isEmpty) return null;
    if (msg.msgType == 4) return null;
    return _MessageCursor(createdAt: msg.createdAt, msgId: msg.msgId);
  }

  void _syncHistoryWindowAnchorsFromCurrent() {
    _MessageCursor? firstCursor;
    _MessageCursor? lastCursor;
    for (final msg in currentMessages) {
      final cursor = _cursorFromMessage(msg);
      if (cursor == null) continue;
      firstCursor ??= cursor;
      lastCursor = cursor;
    }
    _oldestHistoryCursor = firstCursor;
    _newestHistoryCursor = lastCursor;
    if (firstCursor == null) {
      _hasOlderMessages = false;
      _hasNewerMessages = false;
    }
  }

  void _trimCurrentMessagesFromTop() {
    if (currentMessages.length <= ImService._residentMessageCap) return;
    final overflow = currentMessages.length - ImService._residentMessageCap;
    if (overflow <= 0) return;
    currentMessages.removeRange(0, overflow);
    _rebuildCurrentMessageIndexes();
    _hasOlderMessages = true;
  }

  void _trimCurrentMessagesFromBottom() {
    if (currentMessages.length <= ImService._residentMessageCap) return;
    final overflow = currentMessages.length - ImService._residentMessageCap;
    if (overflow <= 0) return;
    currentMessages.removeRange(
      currentMessages.length - overflow,
      currentMessages.length,
    );
    _rebuildCurrentMessageIndexes();
    _hasNewerMessages = true;
  }

  int _findInsertIndex(MessageModel target) {
    var low = 0;
    var high = currentMessages.length;
    while (low < high) {
      final mid = low + ((high - low) >> 1);
      final current = currentMessages[mid];
      if (_compareMessageOrder(current, target) <= 0) {
        low = mid + 1;
      } else {
        high = mid;
      }
    }
    return low;
  }

  int _compareMessageOrder(MessageModel a, MessageModel b) {
    final byCreatedAt = a.createdAt.compareTo(b.createdAt);
    if (byCreatedAt != 0) {
      return byCreatedAt;
    }
    final keyA = _messageOrderKey(a);
    final keyB = _messageOrderKey(b);
    return keyA.compareTo(keyB);
  }

  String _messageOrderKey(MessageModel msg) {
    final clientMsgId = msg.clientMsgId ?? '';
    if (clientMsgId.isNotEmpty) {
      return 'c:$clientMsgId';
    }
    if (msg.msgId.isNotEmpty) {
      return 'm:${msg.msgId}';
    }
    return 's:${msg.senderType}:${msg.senderId}:${msg.content.hashCode}';
  }

  int _normalizeMessageCreatedAt(int createdAt) {
    if (createdAt > 0 && createdAt < 10000000000) {
      return createdAt * 1000;
    }
    return createdAt;
  }

  String _resolveStreamingFinalContent({
    required String msgId,
    required String incomingContent,
  }) {
    if (incomingContent.isNotEmpty || msgId.isEmpty) {
      return incomingContent;
    }

    final streamedContent = MessageStreamController.peekContent(msgId);
    if (streamedContent.isNotEmpty) {
      return streamedContent;
    }

    final idx = currentMessages.indexWhere((m) => m.msgId == msgId);
    if (idx != -1) {
      final existingContent = currentMessages[idx].content;
      if (existingContent.isNotEmpty) {
        return existingContent;
      }
    }

    return incomingContent;
  }

  String _resolveStreamingFinalizeSessionId({
    required String msgId,
    required String incomingSessionId,
  }) {
    final normalizedIncoming = incomingSessionId.trim();
    if (normalizedIncoming.isNotEmpty) {
      return normalizedIncoming;
    }
    if (msgId.isEmpty) {
      return '';
    }

    final idx = currentMessages.indexWhere((m) => m.msgId == msgId);
    if (idx != -1) {
      final existingSessionId = currentMessages[idx].sessionId.trim();
      if (existingSessionId.isNotEmpty) {
        return existingSessionId;
      }
    }
    final placeholderSessionId =
        _streamingPlaceholders[msgId]?.sessionId.trim() ?? '';
    if (placeholderSessionId.isNotEmpty) {
      return placeholderSessionId;
    }
    return '';
  }

  bool _hasMessageInCurrentWindow(String msgId) {
    if (msgId.isEmpty) {
      return false;
    }
    return currentMessages.any((message) => message.msgId == msgId);
  }

  bool _hasStreamingPlaceholderInCurrentWindow(String msgId) {
    if (msgId.isEmpty) {
      return false;
    }
    return currentMessages.any(
      (message) => message.msgId == msgId && message.msgType == 4,
    );
  }

  bool _isTrackedStreamingMessage(String msgId) {
    final normalizedMsgId = msgId.trim();
    if (normalizedMsgId.isEmpty) {
      return false;
    }
    return _activeStreamingMsgIds.contains(normalizedMsgId) ||
        _locallyStoppedStreamMsgIds.contains(normalizedMsgId) ||
        _streamingPlaceholders.containsKey(normalizedMsgId) ||
        _hasStreamingPlaceholderInCurrentWindow(normalizedMsgId);
  }

  int _resolveStreamFinalizeCreatedAt({
    required String msgId,
    required dynamic incomingCreatedAt,
  }) {
    final incoming = _normalizeMessageCreatedAt(_toInt(incomingCreatedAt));
    if (incoming > 0) {
      return incoming;
    }
    final idx = currentMessages.indexWhere((m) => m.msgId == msgId);
    if (idx != -1) {
      final existing = currentMessages[idx].createdAt;
      if (existing > 0) {
        return existing;
      }
    }
    final placeholderCreatedAt = _streamingPlaceholders[msgId]?.createdAt ?? 0;
    if (placeholderCreatedAt > 0) {
      return placeholderCreatedAt;
    }
    return DateTime.now().millisecondsSinceEpoch;
  }

  // ─── LocalDbChangeBus subscription ─────────────────────────────────────

  /// Start listening to DB change events for the current session window.
  /// Called when entering a session. Only activates if the feature flag is on.
  void _startDbChangeSubscription() {
    _cancelDbChangeSubscription();
    if (!ImService._dbChangeEventDrivenWindow) return;
    _dbChangeSubscription = LocalDbChangeBus.instance.messageChanges.listen(
      _handleDbChange,
    );
  }

  /// Stop listening to DB change events. Called when leaving a session.
  void _cancelDbChangeSubscription() {
    _dbChangeSubscription?.cancel();
    _dbChangeSubscription = null;
  }

  /// Handle a LocalDbChangeBus event. Only processes events for the current
  /// session; non-current session events are ignored (session list is already
  /// refreshed by the downstream handler).
  ///
  /// Uses event-carried data for synchronous UI updates — avoids a second DB
  /// round-trip and ensures tests see updates immediately.
  void _handleDbChange(LocalMessageChange change) {
    final currentSid = _currentSessionId.value?.trim() ?? '';
    if (currentSid.isEmpty) return;
    if (change.sessionId != currentSid) return;

    switch (change) {
      case LocalMessagesInserted(:final msgIds, :final rows):
        _handleDbMessagesInsertedSync(currentSid, msgIds, rows);
      case LocalMessageUpdated(
        :final msgId,
        :final row,
        :final clientMsgId,
        :final ackCreatedAt,
      ):
        _handleDbMessageUpdatedSync(
          currentSid,
          msgId,
          row: row,
          clientMsgId: clientMsgId,
          ackCreatedAt: ackCreatedAt,
        );
      case LocalMessageRevoked():
        // Revoke is already handled by _applyLocalMessageRevokeImpl which
        // removes from UI synchronously. The DB event is a confirmation;
        // no additional UI action needed here.
        break;
    }
  }

  /// Synchronously insert messages into the window using event-carried data.
  void _handleDbMessagesInsertedSync(
    String sessionId,
    List<String> msgIds,
    List<Map<String, dynamic>>? rows,
  ) {
    if (sessionId != _currentSessionId.value) return;
    if (msgIds.isEmpty) return;

    // If rows are provided, use them directly (no DB round-trip).
    if (rows != null && rows.isNotEmpty) {
      _mergeDbInsertedRowsAtomically(sessionId, rows);
      return;
    }

    // Fallback: async DB query (should rarely happen).
    unawaited(_handleDbMessagesInsertedAsync(sessionId, msgIds));
  }

  /// Merge a DB insert batch off the reactive list, then publish at most one
  /// new snapshot. History reconciliation commonly carries a full recent page
  /// whose rows are already visible; mutating [currentMessages] per row makes
  /// every duplicate rebuild the entire chat snapshot on the UI isolate.
  void _mergeDbInsertedRowsAtomically(
    String sessionId,
    List<Map<String, dynamic>> rows,
  ) {
    if (sessionId != _currentSessionId.value || rows.isEmpty) return;

    final beforeFirst = currentMessages.isEmpty ? null : currentMessages.first;
    final working = currentMessages.toList(growable: true);
    final mergedBatch = <MessageModel>[];

    for (final row in rows) {
      final mid = row['msg_id']?.toString().trim() ?? '';
      if (mid.isEmpty) continue;
      final incoming = MessageModel.fromJson(row);
      if (incoming.sessionId.trim() != sessionId) continue;

      _reconcileActiveStreamingStateForUiMessage(incoming);
      final removeSet = _matchingUiMessageIndexes(working, incoming);
      final merged = _mergeWithExistingUiMessageState(
        incoming,
        removeSet,
        messages: working,
      );
      if (removeSet.isNotEmpty) {
        final removeIndexes = removeSet.toList()
          ..sort((a, b) => b.compareTo(a));
        for (final index in removeIndexes) {
          working.removeAt(index);
        }
      }
      _insertMessageInOrder(working, merged);
      _cacheStreamingPlaceholder(merged);
      mergedBatch.add(merged);
    }

    if (mergedBatch.isEmpty) return;

    final allOlderThanWindow =
        beforeFirst != null &&
        mergedBatch.every(
          (message) => _compareMessageOrder(message, beforeFirst) <= 0,
        );
    var trimmedFromTop = false;
    var trimmedFromBottom = false;
    final overflow = working.length - ImService._residentMessageCap;
    if (overflow > 0) {
      if (allOlderThanWindow) {
        working.removeRange(working.length - overflow, working.length);
        trimmedFromBottom = true;
      } else {
        working.removeRange(0, overflow);
        trimmedFromTop = true;
      }
    }

    if (_messageListsEquivalent(currentMessages, working)) return;
    if (sessionId != _currentSessionId.value) return;

    // Replacing the Rx value emits once. RxList.assignAll clears and appends,
    // which can emit more than once depending on the GetX implementation.
    currentMessages.value = List<MessageModel>.of(working);
    _rebuildCurrentMessageIndexes();
    if (trimmedFromTop) {
      _hasOlderMessages = true;
    }
    if (trimmedFromBottom) {
      _hasNewerMessages = true;
    }
    _syncHistoryWindowAnchorsFromCurrent();
  }

  Set<int> _matchingUiMessageIndexes(
    List<MessageModel> messages,
    MessageModel incoming,
  ) {
    final indexes = <int>{};
    final msgId = incoming.msgId.trim();
    final clientMsgId = incoming.clientMsgId?.trim() ?? '';
    // Preserve the single-message upsert merge order: canonical msgId state
    // wins before a separate local clientMsgId stub is folded into it.
    if (msgId.isNotEmpty) {
      final msgIndex = messages.indexWhere((message) => message.msgId == msgId);
      if (msgIndex != -1) indexes.add(msgIndex);
    }
    if (clientMsgId.isNotEmpty) {
      final clientIndex = messages.indexWhere(
        (message) => message.clientMsgId == clientMsgId,
      );
      if (clientIndex != -1) indexes.add(clientIndex);
    }
    return indexes;
  }

  void _insertMessageInOrder(
    List<MessageModel> messages,
    MessageModel message,
  ) {
    if (messages.isEmpty || _compareMessageOrder(messages.last, message) <= 0) {
      messages.add(message);
      return;
    }
    if (_compareMessageOrder(message, messages.first) <= 0) {
      messages.insert(0, message);
      return;
    }

    var low = 0;
    var high = messages.length;
    while (low < high) {
      final mid = low + ((high - low) >> 1);
      if (_compareMessageOrder(messages[mid], message) <= 0) {
        low = mid + 1;
      } else {
        high = mid;
      }
    }
    messages.insert(low, message);
  }

  bool _messageListsEquivalent(
    List<MessageModel> current,
    List<MessageModel> next,
  ) {
    if (current.length != next.length) return false;
    for (var index = 0; index < current.length; index++) {
      if (!_messagesUiEquivalent(current[index], next[index])) return false;
    }
    return true;
  }

  bool _messagesUiEquivalent(MessageModel left, MessageModel right) {
    return left.msgId == right.msgId &&
        left.sessionId == right.sessionId &&
        left.senderId == right.senderId &&
        left.senderType == right.senderType &&
        left.msgType == right.msgType &&
        left.content == right.content &&
        _deepMessageValueEquals(left.extra, right.extra) &&
        left.isDeleted == right.isDeleted &&
        left.isRevoked == right.isRevoked &&
        left.createdAt == right.createdAt &&
        left.clientMsgId == right.clientMsgId &&
        left.status == right.status &&
        left.agentDeliveryStatus == right.agentDeliveryStatus &&
        left.quotedMessageId == right.quotedMessageId &&
        _deepMessageValueEquals(left.visibleTo, right.visibleTo) &&
        left.isThinking == right.isThinking;
  }

  bool _deepMessageValueEquals(Object? left, Object? right) {
    if (identical(left, right)) return true;
    if (left is List && right is List) {
      if (left.length != right.length) return false;
      for (var index = 0; index < left.length; index++) {
        if (!_deepMessageValueEquals(left[index], right[index])) return false;
      }
      return true;
    }
    if (left is Map && right is Map) {
      if (left.length != right.length) return false;
      for (final entry in left.entries) {
        if (!right.containsKey(entry.key) ||
            !_deepMessageValueEquals(entry.value, right[entry.key])) {
          return false;
        }
      }
      return true;
    }
    return left == right;
  }

  /// Async fallback: query DB for inserted messages.
  Future<void> _handleDbMessagesInsertedAsync(
    String sessionId,
    List<String> msgIds,
  ) async {
    if (sessionId != _currentSessionId.value) return;
    for (final mid in msgIds) {
      final row = await _guardDbOp<Map<String, dynamic>?>(
        LocalDb.getMessageByMsgId(mid),
        op: 'getMessageByMsgId(db_change_bus)',
      );
      if (row == null) continue;
      if (sessionId != _currentSessionId.value) return;
      final msg = MessageModel.fromJson(row);
      final beforeFirst = currentMessages.isEmpty
          ? null
          : currentMessages.first;
      _upsertUIMessageInOrder(msg);
      _finishDbInsertedWindowUpdate([msg], beforeFirst: beforeFirst);
    }
  }

  void _finishDbInsertedWindowUpdate(
    List<MessageModel> inserted, {
    required MessageModel? beforeFirst,
  }) {
    if (inserted.isEmpty) return;

    final allOlderThanWindow =
        beforeFirst != null &&
        inserted.every((msg) => _compareMessageOrder(msg, beforeFirst) <= 0);
    if (allOlderThanWindow) {
      _trimCurrentMessagesFromBottom();
    } else {
      _trimCurrentMessagesFromTop();
    }
    _syncHistoryWindowAnchorsFromCurrent();
  }

  /// Synchronously update a message in the window using event-carried data.
  void _handleDbMessageUpdatedSync(
    String sessionId,
    String msgId, {
    Map<String, dynamic>? row,
    String? clientMsgId,
    int? ackCreatedAt,
  }) {
    if (sessionId != _currentSessionId.value) return;

    // send_ack path: window entry tracked by clientMsgId.
    if (clientMsgId != null &&
        clientMsgId.isNotEmpty &&
        _currentClientMessageIds.contains(clientMsgId)) {
      _ackUIMessage(clientMsgId, msgId, ackCreatedAt ?? 0);
      return;
    }

    // Standard update path: message tracked by msgId.
    if (_hasMessageInCurrentWindow(msgId) && row != null) {
      final msg = MessageModel.fromJson(row);
      _updateUIMessage(msgId, msg);
      return;
    }

    // Fallback: async DB query if no row data provided.
    if (_hasMessageInCurrentWindow(msgId)) {
      unawaited(_handleDbMessageUpdatedAsync(sessionId, msgId));
    }
  }

  /// Async fallback: query DB for updated message.
  Future<void> _handleDbMessageUpdatedAsync(
    String sessionId,
    String msgId,
  ) async {
    if (sessionId != _currentSessionId.value) return;
    final row = await _guardDbOp<Map<String, dynamic>?>(
      LocalDb.getMessageByMsgId(msgId),
      op: 'getMessageByMsgId(db_change_bus_update)',
    );
    if (row == null) return;
    if (sessionId != _currentSessionId.value) return;
    final msg = MessageModel.fromJson(row);
    _updateUIMessage(msgId, msg);
  }

  /// On reconnect (auth success), do a lightweight history backfill for the
  /// active chat session. This catches messages that were pushed over the old
  /// WS connection but not fully processed (e.g. app was in background) and
  /// whose inbox_seq was already observed, causing pull_sync to skip them.
  Future<void> refreshActiveSessionOnReconnect() async {
    final sid = _currentSessionId.value?.trim() ?? '';
    if (sid.isEmpty) return;

    await _syncSessionHistoryBackfill(
      sessionId: sid,
      limit: ImService._messagePageSize,
      emitBusEvent: true,
    );
  }
}
