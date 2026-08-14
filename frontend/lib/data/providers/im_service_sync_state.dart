part of 'im_service.dart';

extension _ImServiceSyncState on ImService {
  void _sendPushAck(String msgId) {
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    final req = {
      'cmd': 'push_ack',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'msg_id': msgId},
    };
    _sendPacket(req, requireAuthenticated: true);
  }

  void _triggerFriendSync() async {
    if (_friendSyncInFlight) return;
    await _ensureFriendEventSeqLoaded();
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    _friendSyncInFlight = true;
    final req = {
      'cmd': 'friend_sync',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'last_event_seq': _lastFriendEventSeq.toString()},
    };
    final sent = _sendPacket(req, requireAuthenticated: true);
    if (!sent) {
      _friendSyncInFlight = false;
    }
  }

  int _resolvePullSyncCursor(int localMaxInboxSeq) {
    final candidate = localMaxInboxSeq > _lastInboxSeqCursor
        ? localMaxInboxSeq
        : _lastInboxSeqCursor;
    _observeInboxSeq(candidate);
    return candidate;
  }

  void _observeInboxSeq(dynamic seq) {
    final n = _toInt(seq);
    if (n <= _lastInboxSeqCursor || n <= 0) return;
    _lastInboxSeqCursor = n;
    _clearPersistFailPullSyncBackoff();
    _persistInboxSeqCursor();
  }

  Future<void> _ensureInboxSeqCursorLoaded() async {
    if (_inboxSeqCursorLoaded) return;

    final localMaxInboxSeq = await LocalDb.getMaxInboxSeq();
    final localStorageSummary = await LocalDb.getStorageSummary();
    await _ensureBootstrapInboxSeqFloorLoaded();
    int persisted = 0;
    if (Get.isRegistered<AuthService>()) {
      final uid = Get.find<AuthService>().userId;
      if (uid != null && uid.isNotEmpty) {
        final prefs = await _safeGetPrefs();
        persisted =
            prefs?.getInt('${ImService._inboxSeqCursorKeyPrefix}$uid') ?? 0;
      }
    }

    _lastInboxSeqCursor = _resolveInitialInboxSeqCursor(
      localMaxInboxSeq: localMaxInboxSeq,
      persistedInboxSeq: persisted,
      localMessageCount: localStorageSummary.messageCount,
      bootstrapInboxSeqFloor: _bootstrapInboxSeqFloor,
    );
    _inboxSeqCursorLoaded = true;
    _persistInboxSeqCursor();
  }

  Future<void> _ensureSessionSyncCursorLoaded() async {
    if (_sessionSyncCursorLoaded) return;
    int persisted = 0;
    if (Get.isRegistered<AuthService>()) {
      final uid = Get.find<AuthService>().userId;
      if (uid != null && uid.isNotEmpty) {
        final prefs = await _safeGetPrefs();
        persisted =
            prefs?.getInt('${ImService._sessionSyncCursorKeyPrefix}$uid') ?? 0;
      }
    }
    _lastSessionSyncCursor = persisted;
    _sessionSyncCursorLoaded = true;
  }

  void _observeSessionSyncCursor(int cursor) {
    if (cursor <= 0 || cursor <= _lastSessionSyncCursor) return;
    _lastSessionSyncCursor = cursor;
    _persistSessionSyncCursor();
  }

  void _persistSessionSyncCursor() {
    if (!_sessionSyncCursorLoaded) return;
    if (!Get.isRegistered<AuthService>()) return;
    final uid = Get.find<AuthService>().userId;
    if (uid == null || uid.isEmpty) return;
    unawaited(_persistSessionSyncCursorAsync(uid, _lastSessionSyncCursor));
  }

  Future<void> _persistSessionSyncCursorAsync(String uid, int value) async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    final key = '${ImService._sessionSyncCursorKeyPrefix}$uid';
    if (value <= 0) {
      await prefs.remove(key);
      return;
    }
    await prefs.setInt(key, value);
  }

  int _resolveInitialInboxSeqCursor({
    required int localMaxInboxSeq,
    required int persistedInboxSeq,
    required int localMessageCount,
    int bootstrapInboxSeqFloor = 0,
  }) {
    final hasBootstrapFloor = bootstrapInboxSeqFloor > 0;
    if (localMaxInboxSeq <= 0 &&
        localMessageCount <= 0 &&
        persistedInboxSeq > 0 &&
        !hasBootstrapFloor) {
      debugPrint(
        'ImService inbox cursor reset: local message store is empty but persisted cursor=$persistedInboxSeq',
      );
      return 0;
    }
    var candidate = persistedInboxSeq > localMaxInboxSeq
        ? persistedInboxSeq
        : localMaxInboxSeq;
    if (bootstrapInboxSeqFloor > candidate) {
      candidate = bootstrapInboxSeqFloor;
    }
    return candidate;
  }

  void _observeBootstrapInboxSeqFloor(dynamic seq) {
    final n = _toInt(seq);
    if (n <= 0 || n <= _bootstrapInboxSeqFloor) {
      return;
    }
    _bootstrapInboxSeqFloor = n;
    _bootstrapInboxSeqFloorLoaded = true;
    _persistBootstrapInboxSeqFloor();
  }

  Future<void> _ensureBootstrapInboxSeqFloorLoaded() async {
    if (_bootstrapInboxSeqFloorLoaded) return;
    _bootstrapInboxSeqFloor = 0;
    if (!Get.isRegistered<AuthService>()) {
      _bootstrapInboxSeqFloorLoaded = true;
      return;
    }
    final uid = Get.find<AuthService>().userId;
    if (uid == null || uid.isEmpty) {
      _bootstrapInboxSeqFloorLoaded = true;
      return;
    }
    final prefs = await _safeGetPrefs();
    _bootstrapInboxSeqFloor =
        prefs?.getInt('${ImService._bootstrapInboxSeqFloorKeyPrefix}$uid') ?? 0;
    _bootstrapInboxSeqFloorLoaded = true;
  }

  Future<void> _ensureFriendEventSeqLoaded() async {
    if (_friendEventSeqLoaded) return;
    final authService = Get.find<AuthService>();
    final uid = authService.userId;
    if (uid == null) {
      _lastFriendEventSeq = 0;
      _friendEventSeqLoaded = true;
      return;
    }
    final prefs = await _safeGetPrefs();
    _lastFriendEventSeq =
        prefs?.getInt('${ImService._friendEventSeqKeyPrefix}$uid') ?? 0;
    _friendEventSeqLoaded = true;
  }

  Future<void> _ensurePendingReadStatesLoaded() async {
    if (_pendingReadStatesLoaded) return;
    if (!Get.isRegistered<AuthService>()) {
      _pendingReadStatesBySession.clear();
      _pendingReadStatesLoaded = true;
      return;
    }
    final uid = Get.find<AuthService>().userId;
    if (uid == null) {
      _pendingReadStatesBySession.clear();
      _pendingReadStatesLoaded = true;
      return;
    }

    final prefs = await _safeGetPrefs();
    final key = '${ImService._pendingReadStatesKeyPrefix}$uid';
    final raw = prefs?.getString(key);
    _pendingReadStatesBySession.clear();

    if (raw != null && raw.trim().isNotEmpty) {
      try {
        final decoded = jsonDecode(raw);
        if (decoded is Map) {
          decoded.forEach((k, v) {
            final sid = k.toString().trim();
            if (sid.isEmpty) return;
            final lastReadMsgId = v?.toString().trim() ?? '';
            if (!_isValidMsgId(lastReadMsgId)) return;
            _pendingReadStatesBySession[sid] = _PendingReadState(
              lastReadMsgId: lastReadMsgId,
            );
          });
        }
      } catch (e) {
        debugPrint('Load pending read states failed: $e');
      }
    }
    _pendingReadStatesLoaded = true;
  }

  Future<void> _ensureDeletedSessionsLoaded() async {
    if (_deletedSessionsLoaded) return;
    final uid = Get.find<AuthService>().userId;
    if (uid == null || uid.isEmpty) {
      _locallyDeletedSessions.clear();
      _deletedSessionsLoaded = true;
      return;
    }

    final prefs = await _safeGetPrefs();
    final key = '${ImService._deletedSessionsKeyPrefix}$uid';
    final raw = prefs?.getString(key);
    _locallyDeletedSessions.clear();

    if (raw == null || raw.trim().isEmpty) {
      _deletedSessionsLoaded = true;
      return;
    }

    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map) {
        decoded.forEach((k, v) {
          final sid = k.toString().trim();
          if (sid.isEmpty) return;
          final ts = _toInt(v);
          if (ts > 0) {
            _locallyDeletedSessions[sid] = ts;
          }
        });
      }
    } catch (e) {
      debugPrint('Load deleted sessions failed: $e');
    }
    _deletedSessionsLoaded = true;
  }

  Future<void> _ensureRevokedSessionsLoaded() async {
    if (_revokedSessionsLoaded) return;
    final uid = Get.find<AuthService>().userId;
    if (uid == null || uid.isEmpty) {
      _locallyRevokedSessions.clear();
      _revokedSessionsLoaded = true;
      return;
    }

    final prefs = await _safeGetPrefs();
    final key = '${ImService._revokedSessionsKeyPrefix}$uid';
    final raw = prefs?.getString(key);
    _locallyRevokedSessions.clear();

    if (raw == null || raw.trim().isEmpty) {
      _revokedSessionsLoaded = true;
      return;
    }

    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map) {
        decoded.forEach((k, v) {
          final sid = k.toString().trim();
          if (sid.isEmpty) return;
          final ts = _toInt(v);
          if (ts > 0) {
            _locallyRevokedSessions[sid] = ts;
          }
        });
      }
    } catch (e) {
      debugPrint('Load revoked sessions failed: $e');
    }
    _revokedSessionsLoaded = true;
  }

  void _updateFriendEventSeq(dynamic seq) {
    final n = _toInt(seq);
    if (n <= _lastFriendEventSeq) return;
    _lastFriendEventSeq = n;
    _persistFriendEventSeq();
  }

  void _persistFriendEventSeq() {
    final uid = Get.find<AuthService>().userId;
    if (uid == null) return;
    unawaited(_persistFriendEventSeqAsync(uid, _lastFriendEventSeq));
  }

  void _persistInboxSeqCursor() {
    if (!_inboxSeqCursorLoaded) return;
    if (!Get.isRegistered<AuthService>()) return;
    final uid = Get.find<AuthService>().userId;
    if (uid == null || uid.isEmpty) return;
    final value = _lastInboxSeqCursor;
    unawaited(_persistInboxSeqCursorAsync(uid, value));
  }

  void _persistBootstrapInboxSeqFloor() {
    if (!_bootstrapInboxSeqFloorLoaded) return;
    if (!Get.isRegistered<AuthService>()) return;
    final uid = Get.find<AuthService>().userId;
    if (uid == null || uid.isEmpty) return;
    final value = _bootstrapInboxSeqFloor;
    unawaited(_persistBootstrapInboxSeqFloorAsync(uid, value));
  }

  void _persistPendingReadStates() {
    if (!_pendingReadStatesLoaded) return;
    if (!Get.isRegistered<AuthService>()) return;
    final uid = Get.find<AuthService>().userId;
    if (uid == null) return;

    final key = '${ImService._pendingReadStatesKeyPrefix}$uid';
    final payload = <String, String>{};
    final entries = _pendingReadStatesBySession.entries.toList()
      ..sort((a, b) => a.key.compareTo(b.key));
    for (final entry in entries) {
      payload[entry.key] = entry.value.lastReadMsgId;
    }
    unawaited(_persistPendingReadStatesAsync(key, payload));
  }

  void _persistDeletedSessions() {
    if (!_deletedSessionsLoaded) return;
    final uid = Get.find<AuthService>().userId;
    if (uid == null || uid.isEmpty) return;

    final key = '${ImService._deletedSessionsKeyPrefix}$uid';
    final payload = <String, int>{}..addAll(_locallyDeletedSessions);
    unawaited(_persistDeletedSessionsAsync(key, payload));
  }

  void _persistRevokedSessions() {
    if (!_revokedSessionsLoaded) return;
    final uid = Get.find<AuthService>().userId;
    if (uid == null || uid.isEmpty) return;

    final key = '${ImService._revokedSessionsKeyPrefix}$uid';
    final payload = <String, int>{}..addAll(_locallyRevokedSessions);
    unawaited(_persistDeletedSessionsAsync(key, payload));
  }

  Future<void> _persistFriendEventSeqAsync(String uid, int seq) async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    await prefs.setInt('${ImService._friendEventSeqKeyPrefix}$uid', seq);
  }

  Future<void> _persistInboxSeqCursorAsync(String uid, int value) async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    await prefs.setInt('${ImService._inboxSeqCursorKeyPrefix}$uid', value);
  }

  Future<void> _persistBootstrapInboxSeqFloorAsync(
    String uid,
    int value,
  ) async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    final key = '${ImService._bootstrapInboxSeqFloorKeyPrefix}$uid';
    if (value <= 0) {
      await prefs.remove(key);
      return;
    }
    await prefs.setInt(key, value);
  }

  Future<void> _clearBootstrapInboxSeqFloorForCurrentUser() async {
    var uid = '';
    if (Get.isRegistered<AuthService>()) {
      uid = Get.find<AuthService>().userId?.trim() ?? '';
    }
    if (uid.isEmpty) {
      uid = LocalDb.activeUserId?.trim() ?? '';
    }
    if (uid.isEmpty) {
      return;
    }
    final prefs = await _safeGetPrefs();
    if (prefs == null) {
      return;
    }
    await prefs.remove('${ImService._bootstrapInboxSeqFloorKeyPrefix}$uid');
  }

  Future<void> _persistPendingReadStatesAsync(
    String key,
    Map<String, String> payload,
  ) async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    if (payload.isEmpty) {
      await prefs.remove(key);
    } else {
      await prefs.setString(key, jsonEncode(payload));
    }
  }

  Future<void> _persistDeletedSessionsAsync(
    String key,
    Map<String, int> payload,
  ) async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    if (payload.isEmpty) {
      await prefs.remove(key);
    } else {
      await prefs.setString(key, jsonEncode(payload));
    }
  }

  bool _shouldSuppressDeletedSession(String sessionId, int updatedAtMs) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    if (updatedAtMs < 0) return false;
    return _locallyDeletedSessions.containsKey(sid);
  }

  bool _shouldSuppressAccessRevokedSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    return _locallyRevokedSessions.containsKey(sid);
  }

  bool _shouldAcceptDeletedSessionActivity(String sessionId, int activityAtMs) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    final deletedAt = _locallyDeletedSessions[sid];
    if (deletedAt == null) {
      return true;
    }
    if (activityAtMs <= 0 || activityAtMs <= deletedAt) {
      return false;
    }
    _clearSessionLocalDeleteMark(sid);
    return true;
  }

  void _clearSessionLocalDeleteMark(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (_locallyDeletedSessions.remove(sid) == null) return;
    _persistDeletedSessions();
  }

  void _clearSessionLocalRevokedMark(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (_locallyRevokedSessions.remove(sid) == null) return;
    _persistRevokedSessions();
  }

  void _clearSessionAccessRevokedReason(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    _sessionAccessRevokedReasons.remove(sid);
  }

  Future<void> _removeSessionLocally(
    String sessionId, {
    required bool preserveMessages,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (preserveMessages) {
      await LocalDb.deleteSessionRecord(sid);
    } else {
      await LocalDb.deleteConversation(sid);
    }
    sessions.removeWhere((s) => s.sessionId == sid);
    sessionActivities.remove(sid);

    if (_currentSessionId.value == sid) {
      leaveSession();
    }
  }

  Future<SharedPreferences?> _safeGetPrefs() async {
    try {
      return await SharedPreferences.getInstance();
    } on MissingPluginException catch (e) {
      _logPrefsUnavailable(e);
      return null;
    } on PlatformException catch (e) {
      _logPrefsUnavailable(e);
      return null;
    }
  }

  void _logPrefsUnavailable(Object error) {
    if (_prefsUnavailableLogged) return;
    _prefsUnavailableLogged = true;
    debugPrint('SharedPreferences unavailable in ImService: $error');
  }

  Map<String, int> _parseUnreadSnapshot(dynamic raw) {
    final map = <String, int>{};
    if (raw is! Map) return map;
    raw.forEach((key, value) {
      final sid = key?.toString().trim() ?? '';
      if (sid.isEmpty) return;
      final n = _toInt(value);
      if (n >= 0) {
        map[sid] = n;
      }
    });
    return map;
  }

  Map<String, int> _normalizeUnreadSnapshot(Map<String, int> rawSnapshot) {
    if (rawSnapshot.isEmpty) return const <String, int>{};
    final normalized = <String, int>{};
    rawSnapshot.forEach((key, value) {
      final sid = key.trim();
      if (sid.isEmpty) return;
      final unread = _normalizeUnreadForSession(sid, value);
      if (unread > 0) {
        normalized[sid] = unread;
      }
    });
    return normalized;
  }

  bool _isMessageFromCurrentUser(String senderId) {
    final normalizedSenderID = senderId.trim();
    if (normalizedSenderID.isEmpty) {
      return false;
    }
    if (!Get.isRegistered<AuthService>()) {
      return false;
    }
    final myUserID = Get.find<AuthService>().userId?.trim() ?? '';
    if (myUserID.isEmpty) {
      return false;
    }
    return normalizedSenderID == myUserID;
  }

  void _markCurrentSessionSeenFromSender(
    String sessionId,
    String senderId, {
    String msgId = '',
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty || !_isCurrentSession(sid)) {
      return;
    }
    if (_isMessageFromCurrentUser(senderId)) {
      return;
    }

    LocalDb.clearUnread(sid);
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx != -1 && sessions[idx].unreadCount != 0) {
      sessions[idx] = sessions[idx].copyWith(unreadCount: 0);
    }

    final lastReadMsgId = _parseServerMessageId(msgId);
    if (_isValidMsgId(lastReadMsgId)) {
      _markSessionReadRemote(sid, lastReadMsgId: lastReadMsgId);
      return;
    }

    unawaited(_queueSessionReadByKnownBoundary(sid));
  }

  void _markSessionReadRemote(
    String sessionId, {
    required String lastReadMsgId,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty || !_isValidMsgId(lastReadMsgId)) return;
    if (!_pendingReadStatesLoaded) {
      _ensurePendingReadStatesLoaded().then((_) {
        _markSessionReadRemote(sid, lastReadMsgId: lastReadMsgId);
      });
      return;
    }
    final existing = _pendingReadStatesBySession[sid];
    if (existing != null &&
        _compareMsgId(existing.lastReadMsgId, lastReadMsgId) >= 0) {
      return;
    }
    _pendingReadStatesBySession[sid] = _PendingReadState(
      lastReadMsgId: lastReadMsgId,
      retryCount: 0,
      nextSendMs: 0,
      lastSentMsgId: '',
    );
    _persistPendingReadStates();
    _flushPendingSessionReads();
  }

  void _handleSessionReadAck(
    String sessionId, {
    required int code,
    required String lastReadMsgId,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;

    final state = _pendingReadStatesBySession[sid];
    if (state == null) {
      return;
    }

    if (code == 4001 || code == 4003) {
      _pendingReadStatesBySession.remove(sid);
      _localUnreadOverrides.remove(sid);
      _persistPendingReadStates();
      _schedulePendingReadRetry();
      return;
    }

    if (code != 0) {
      return;
    }

    final effectiveAckMsgId = _isValidMsgId(lastReadMsgId)
        ? lastReadMsgId
        : state.lastSentMsgId;
    if (_compareMsgId(effectiveAckMsgId, state.lastReadMsgId) >= 0) {
      _pendingReadStatesBySession.remove(sid);
      _localUnreadOverrides.remove(sid);
      _persistPendingReadStates();
      _schedulePendingReadRetry();
      return;
    }

    if (_isValidMsgId(state.lastSentMsgId) &&
        _compareMsgId(effectiveAckMsgId, state.lastSentMsgId) >= 0) {
      _pendingReadStatesBySession[sid] = state.copyWith(
        retryCount: 0,
        nextSendMs: 0,
        lastSentMsgId: '',
      );
      _persistPendingReadStates();
      _flushPendingSessionReads();
      return;
    }

    _schedulePendingReadRetry();
  }

  void _handleSessionHistoryResetAck(
    Map<String, dynamic> payload, {
    int seq = 0,
  }) {
    final sid = payload['session_id']?.toString().trim() ?? '';
    final code = _toInt(payload['code']);
    if (sid.isEmpty) {
      return;
    }
    final inFlightSeq = _sessionHistoryResetInFlightAtMs[sid] ?? 0;
    if (seq > 0 && inFlightSeq > 0 && seq != inFlightSeq) {
      debugPrint(
        'session_history_reset_ack stale sid=$sid seq=$seq current=$inFlightSeq',
      );
      return;
    }
    _sessionHistoryResetInFlightAtMs.remove(sid);
    _sessionHistoryResetInFlightDeletedAtMs.remove(sid);
    _scheduleSessionHistoryResetRetry();
    if (code == 0) {
      return;
    }

    if (code == 4003) {
      _clearSessionLocalDeleteMark(sid);
    }

    final msg = payload['msg']?.toString() ?? '';
    debugPrint('session_history_reset_ack failed sid=$sid code=$code msg=$msg');
  }

  void _handleSessionHistoryResetSync(Map<String, dynamic> payload) {
    final sid = payload['session_id']?.toString().trim() ?? '';
    final deletedAtMs = _toInt(payload['deleted_at']);
    if (sid.isEmpty || deletedAtMs <= 0) return;

    final existing = _locallyDeletedSessions[sid] ?? 0;
    if (deletedAtMs < existing) return;

    _locallyDeletedSessions[sid] = deletedAtMs;
    _persistDeletedSessions();
    unawaited(_removeSessionLocally(sid, preserveMessages: false));
  }

  void _handleSessionHistoryResetsQueryAck(Map<String, dynamic> payload) {
    final resets = payload['resets'];
    if (resets is! List) return;

    var changed = false;
    for (final entry in resets) {
      if (entry is! Map<String, dynamic>) continue;
      final sid = entry['session_id']?.toString().trim() ?? '';
      final deletedAtMs = _toInt(entry['deleted_at']);
      if (sid.isEmpty || deletedAtMs <= 0) continue;

      final existing = _locallyDeletedSessions[sid] ?? 0;
      if (deletedAtMs > existing) {
        _locallyDeletedSessions[sid] = deletedAtMs;
        changed = true;
        unawaited(_removeSessionLocally(sid, preserveMessages: false));
      }
    }
    if (changed) {
      _persistDeletedSessions();
    }
  }

  int _nextPendingReadDelayMs(int attempt) {
    final safeAttempt = attempt < 1 ? 1 : attempt;
    var delay = ImService._pendingReadBaseRetryMs * (1 << (safeAttempt - 1));
    if (delay > ImService._pendingReadMaxRetryMs) {
      delay = ImService._pendingReadMaxRetryMs;
    }
    final jitter = DateTime.now().millisecondsSinceEpoch % 300;
    return delay + jitter;
  }

  void _syncDeletedSessionHistoryResets() {
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    _querySessionHistoryResets();
    if (_locallyDeletedSessions.isEmpty) {
      return;
    }
    final entries = _locallyDeletedSessions.entries.toList()
      ..sort((a, b) => a.key.compareTo(b.key));
    for (final entry in entries) {
      _sendSessionHistoryReset(entry.key, entry.value);
    }
  }

  void _querySessionHistoryResets() {
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    final req = {
      'cmd': 'session_history_resets_query',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': <String, dynamic>{},
    };
    _sendPacket(req, requireAuthenticated: true);
  }

  void _queueDeletedSessionReadClears() {
    if (_locallyDeletedSessions.isEmpty) {
      return;
    }
    final sessionIDs = _locallyDeletedSessions.keys.toList()..sort();
    for (final sid in sessionIDs) {
      unawaited(_queueSessionReadByKnownBoundary(sid));
    }
  }

  void _sendSessionHistoryReset(String sessionId, int deletedAtMs) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final deletedAt = deletedAtMs > 0 ? deletedAtMs : nowMs;
    if (_hasRecentSessionHistoryResetInFlight(
      sid,
      nowMs: nowMs,
      deletedAtMs: deletedAt,
    )) {
      _scheduleSessionHistoryResetRetry(nowMs: nowMs);
      return;
    }
    final seq = _nextSessionHistoryResetSeq(nowMs);
    final req = {
      'cmd': 'session_history_reset',
      'seq': seq,
      'payload': {'session_id': sid, 'deleted_at': deletedAt},
    };
    if (_sendPacket(req, requireAuthenticated: true)) {
      _sessionHistoryResetInFlightAtMs[sid] = seq;
      _sessionHistoryResetInFlightDeletedAtMs[sid] = deletedAt;
      _scheduleSessionHistoryResetRetry(nowMs: nowMs);
    }
  }

  int _nextSessionHistoryResetSeq(int nowMs) {
    if (nowMs > _lastSessionHistoryResetSeq) {
      _lastSessionHistoryResetSeq = nowMs;
    } else {
      _lastSessionHistoryResetSeq++;
    }
    return _lastSessionHistoryResetSeq;
  }

  bool _hasRecentSessionHistoryResetInFlight(
    String sessionId, {
    int? nowMs,
    int deletedAtMs = 0,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    final inFlightAtMs = _sessionHistoryResetInFlightAtMs[sid] ?? 0;
    if (inFlightAtMs <= 0) return false;
    final inFlightDeletedAtMs =
        _sessionHistoryResetInFlightDeletedAtMs[sid] ?? 0;
    if (deletedAtMs > inFlightDeletedAtMs) {
      return false;
    }
    final effectiveNowMs = nowMs ?? DateTime.now().millisecondsSinceEpoch;
    return effectiveNowMs - inFlightAtMs < _effectiveSessionHistoryResetRetryMs;
  }

  int get _effectiveSessionHistoryResetRetryMs =>
      ImService.sessionHistoryResetRetryMsForTest ??
      ImService._sessionHistoryResetRetryMs;

  void _scheduleSessionHistoryResetRetry({int? nowMs}) {
    _sessionHistoryResetRetryTimer?.cancel();
    _sessionHistoryResetRetryTimer = null;
    if (_sessionHistoryResetInFlightAtMs.isEmpty) return;
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }

    final effectiveNowMs = nowMs ?? DateTime.now().millisecondsSinceEpoch;
    int? earliestDueMs;
    for (final sentAtMs in _sessionHistoryResetInFlightAtMs.values) {
      final dueMs = sentAtMs + _effectiveSessionHistoryResetRetryMs;
      if (earliestDueMs == null || dueMs < earliestDueMs) {
        earliestDueMs = dueMs;
      }
    }
    if (earliestDueMs == null) return;
    final delayMs = earliestDueMs <= effectiveNowMs
        ? 1
        : earliestDueMs - effectiveNowMs;
    _sessionHistoryResetRetryTimer = Timer(
      Duration(milliseconds: delayMs),
      _retrySessionHistoryResets,
    );
  }

  void _retrySessionHistoryResets() {
    _sessionHistoryResetRetryTimer = null;
    if (_sessionHistoryResetInFlightAtMs.isEmpty) return;
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    final entries = _sessionHistoryResetInFlightAtMs.keys.toList()..sort();
    for (final sid in entries) {
      final deletedAt =
          _locallyDeletedSessions[sid] ??
          _sessionHistoryResetInFlightDeletedAtMs[sid] ??
          0;
      _sendSessionHistoryReset(sid, deletedAt);
    }
  }

  void _schedulePendingReadRetry() {
    _pendingReadRetryTimer?.cancel();
    if (_pendingReadStatesBySession.isEmpty) return;

    final now = DateTime.now().millisecondsSinceEpoch;
    int? earliest;
    for (final state in _pendingReadStatesBySession.values) {
      final nextAt = state.nextSendMs;
      if (nextAt <= now) {
        earliest = now;
        break;
      }
      if (earliest == null || nextAt < earliest) {
        earliest = nextAt;
      }
    }
    if (earliest == null) return;

    final waitMs = (earliest - now).clamp(0, ImService._pendingReadMaxRetryMs);
    _pendingReadRetryTimer = Timer(Duration(milliseconds: waitMs), () {
      _flushPendingSessionReads();
    });
  }

  void _flushPendingSessionReads() {
    if (!_pendingReadStatesLoaded) {
      _ensurePendingReadStatesLoaded().then((_) {
        _flushPendingSessionReads();
      });
      return;
    }
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      _schedulePendingReadRetry();
      return;
    }
    if (_pendingReadStatesBySession.isEmpty) {
      _pendingReadRetryTimer?.cancel();
      _pendingReadRetryTimer = null;
      return;
    }
    final toSend = _pendingReadStatesBySession.entries.toList()
      ..sort((a, b) => a.key.compareTo(b.key));
    final now = DateTime.now().millisecondsSinceEpoch;
    for (final entry in toSend) {
      final sid = entry.key;
      final state = entry.value;
      final nextAt = state.nextSendMs;
      if (nextAt > now) {
        continue;
      }
      final req = {
        'cmd': 'session_read',
        'seq': DateTime.now().millisecondsSinceEpoch,
        'payload': {'session_id': sid, 'last_read_msg_id': state.lastReadMsgId},
      };
      final sent = _sendPacket(req, requireAuthenticated: true);
      if (!sent) {
        _schedulePendingReadRetry();
        return;
      }

      final attempt = state.retryCount + 1;
      _pendingReadStatesBySession[sid] = state.copyWith(
        retryCount: attempt,
        nextSendMs: now + _nextPendingReadDelayMs(attempt),
        lastSentMsgId: state.lastReadMsgId,
      );
    }
    _persistPendingReadStates();
    _schedulePendingReadRetry();
  }
}
