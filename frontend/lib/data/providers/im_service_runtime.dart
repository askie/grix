part of 'im_service.dart';

extension _ImServiceRuntime on ImService {
  bool isCurrentSession(String sessionId) {
    return _isCurrentSession(sessionId);
  }

  bool get shouldShowConnectionBanner {
    _connectionBannerVisibilityTick.value;
    _syncConnectionBannerDelayForStage(_connectionStage.value);
    if (_isInitialConnectionBannerDelayActive()) {
      return false;
    }
    if (_isConnectionBannerSuppressedNow()) {
      return false;
    }
    if (_isConnectionLossBannerDelayActive()) {
      return false;
    }
    switch (_connectionStage.value) {
      case ImConnectionStage.connecting:
      case ImConnectionStage.authenticating:
      case ImConnectionStage.reconnecting:
      case ImConnectionStage.disconnected:
        return true;
      case ImConnectionStage.connected:
      case ImConnectionStage.kicked:
      case ImConnectionStage.authFailed:
        return false;
    }
  }

  String get connectionBannerTextKey {
    switch (_connectionStage.value) {
      case ImConnectionStage.connecting:
        return 'connection_connecting';
      case ImConnectionStage.authenticating:
        return 'connection_authenticating';
      case ImConnectionStage.reconnecting:
        return 'chat_connection_lost';
      case ImConnectionStage.disconnected:
        return 'connection_disconnected';
      case ImConnectionStage.connected:
      case ImConnectionStage.kicked:
      case ImConnectionStage.authFailed:
        return 'chat_connection_lost';
    }
  }

  void suppressConnectionBannerTemporarily({
    Duration duration = ImService._transientConnectionBannerSuppressDuration,
  }) {
    final durationMs = duration.inMilliseconds;
    if (durationMs <= 0) return;
    final nowMs = ImService.nowMsProvider();
    final nextUntilMs = nowMs + durationMs;
    if (nextUntilMs <= _connectionBannerSuppressedUntilMs) {
      return;
    }
    _connectionBannerSuppressedUntilMs = nextUntilMs;
    _connectionBannerVisibilityTick.value++;
    _rescheduleConnectionBannerSuppressionTimer();
  }

  bool _isConnectionBannerSuppressedNow() {
    final untilMs = _connectionBannerSuppressedUntilMs;
    if (untilMs <= 0) return false;
    final nowMs = ImService.nowMsProvider();
    if (nowMs < untilMs) {
      return true;
    }
    _connectionBannerSuppressedUntilMs = 0;
    _connectionBannerSuppressionTimer?.cancel();
    _connectionBannerSuppressionTimer = null;
    return false;
  }

  void _rescheduleConnectionBannerSuppressionTimer() {
    _connectionBannerSuppressionTimer?.cancel();
    final untilMs = _connectionBannerSuppressedUntilMs;
    if (untilMs <= 0) {
      _connectionBannerSuppressionTimer = null;
      return;
    }
    final nowMs = ImService.nowMsProvider();
    final remainingMs = untilMs - nowMs;
    if (remainingMs <= 0) {
      _connectionBannerSuppressedUntilMs = 0;
      _connectionBannerSuppressionTimer = null;
      _connectionBannerVisibilityTick.value++;
      return;
    }
    _connectionBannerSuppressionTimer = Timer(
      Duration(milliseconds: remainingMs),
      () {
        _connectionBannerSuppressionTimer = null;
        final deadlineMs = _connectionBannerSuppressedUntilMs;
        final currentMs = ImService.nowMsProvider();
        if (deadlineMs > currentMs) {
          _rescheduleConnectionBannerSuppressionTimer();
          return;
        }
        _connectionBannerSuppressedUntilMs = 0;
        _connectionBannerVisibilityTick.value++;
      },
    );
  }

  int get totalUnread => _countUnread(includeMuted: true);

  int get notificationUnread => _countUnread(includeMuted: false);

  int totalUnreadForSession(SessionModel session) {
    return _countUnreadForSession(session, includeMuted: true);
  }

  int notificationUnreadForSession(SessionModel session) {
    return _countUnreadForSession(session, includeMuted: false);
  }

  int _countUnread({required bool includeMuted}) {
    int count = 0;
    for (final session in sessions) {
      final unread = _countUnreadForSession(
        session,
        includeMuted: includeMuted,
      );
      if (unread > 0) count += unread;
    }
    return count;
  }

  int _countUnreadForSession(
    SessionModel session, {
    required bool includeMuted,
  }) {
    if (!includeMuted && session.isMuted) {
      return 0;
    }
    if (!includeMuted &&
        (session.friendIsMuted || isPeerMuted(session.peerId))) {
      return 0;
    }
    return _normalizeUnreadForSession(session.sessionId, session.unreadCount);
  }

  bool _isCurrentSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    final current = _currentSessionId.value?.trim() ?? '';
    if (current.isEmpty) return false;
    return sid == current;
  }

  int _normalizeUnreadForSession(String sessionId, int unreadCount) {
    final safeUnread = unreadCount < 0 ? 0 : unreadCount;
    if (_isCurrentSession(sessionId)) {
      return 0;
    }
    return safeUnread;
  }

  void deferSystemUnreadBadgeSyncUntilAuthoritativeRefresh() {
    _deferSystemUnreadBadgeSync = true;
  }

  Future<void> syncSystemUnreadBadgeNow({
    bool force = false,
    bool authoritative = false,
  }) async {
    if (_deferSystemUnreadBadgeSync && !authoritative) {
      return;
    }
    if (authoritative) {
      _deferSystemUnreadBadgeSync = false;
    }
    await AppBadgeService.syncUnreadBadge(notificationUnread, force: force);
  }

  void _syncSystemUnreadBadge() {
    unawaited(syncSystemUnreadBadgeNow());
  }

  Future<void> _syncDeferredSystemUnreadBadgeAfterAuthoritativeRefresh() async {
    if (!_deferSystemUnreadBadgeSync) {
      return;
    }
    await syncSystemUnreadBadgeNow(force: true, authoritative: true);
  }

  Future<ImService> init() async {
    try {
      // 在任何保活逻辑触发之前，从持久化存储预设 WS 端点。
      // 防止 _ensureRealtimeConnectedAsync 在 splash 设置正确端点之前
      // 以编译时默认值（国区 URL）发起错误连接，导致全球区用户被踢出登录。
      if (!kIsWeb) {
        final savedWsEndpoint = await AppStorageService.loadWsEndpoint();
        if (savedWsEndpoint != null && savedWsEndpoint.isNotEmpty) {
          _wsUrl = savedWsEndpoint;
        } else {
          // 存储为空（老账号升级 / 数据丢失）：按保存的区域偏好推导，
          // 确保 token 有效时 ensureConnected() 不会因 _wsUrl==null 而 no-op。
          _wsUrl = resolveRegionWsUrl(await resolveInitialRegion());
        }
      }
      final authService = Get.find<AuthService>();
      if (authService.isLoggedIn && authService.userId != null) {
        deferSystemUnreadBadgeSyncUntilAuthoritativeRefresh();
        await LocalDb.setActiveUser(authService.userId);
        await _ensureInboxSeqCursorLoaded();
        await _ensurePendingReadStatesLoaded();
        await _ensureDeletedSessionsLoaded();
        await _ensureRevokedSessionsLoaded();
        _queueDeletedSessionReadClears();
        await loadSessions();
      } else {
        _clearRuntimeState();
      }
      _syncSystemUnreadBadge();
    } catch (e) {
      debugPrint('ImService init error: $e');
    }
    return this;
  }

  void suspendForAppBackground() {
    _isSuspendedForAppBackground = true;
    _setRealtimeAppStateImpl('background');
    final hasRealtimeWork =
        _isConnected.value ||
        _isConnecting ||
        _channel != null ||
        (_heartbeatTimer?.isActive ?? false) ||
        (_reconnectTimer?.isActive ?? false) ||
        (_composingRenewTimer?.isActive ?? false) ||
        (_composingIdleTimer?.isActive ?? false) ||
        (_sessionViewingRenewTimer?.isActive ?? false);
    if (!hasRealtimeWork) {
      return;
    }

    final currentSessionId = _currentSessionId.value?.trim() ?? '';
    _stopSessionComposing(currentSessionId, notifyRemote: true);
    _stopSessionViewing(currentSessionId, notifyRemote: true);
    disconnect(stage: ImConnectionStage.disconnected);
  }

  void _redirectToLoginImpl() {
    if (Get.currentRoute != AppRoutes.login) {
      RootRouteNavigator.toLogin();
    }
  }

  void _redirectToHomeAfterAuthSuccessImpl() {
    if (!Get.isRegistered<AuthService>()) {
      return;
    }
    final authService = Get.find<AuthService>();
    if (!authService.isLoggedIn) {
      return;
    }
    if (Get.currentRoute == AppRoutes.login) {
      RootRouteNavigator.toHome();
    }
  }

  void _updateUIMessageImpl(String msgId, MessageModel msg) {
    _upsertUIMessageInOrder(msg);
  }

  void _recordPongReceiptImpl() {
    _lastPongAtMs = DateTime.now().millisecondsSinceEpoch;
  }

  int _toIntImpl(dynamic v) {
    if (v is int) return v;
    if (v is num) return v.toInt();
    if (v is String) return int.tryParse(v.trim()) ?? 0;
    return int.tryParse(v?.toString() ?? '') ?? 0;
  }

  bool _toBoolImpl(dynamic v) {
    if (v is bool) return v;
    if (v is num) return v != 0;
    final normalized = v?.toString().trim().toLowerCase() ?? '';
    return normalized == 'true' || normalized == '1';
  }

  List<int> _resolveMentionUserIdsImpl(
    dynamic rawMentionUserIds,
    String content,
  ) {
    final uniq = <int>[];
    final seen = <int>{};

    void add(dynamic value) {
      final id = _toInt(value);
      if (id <= 0 || seen.contains(id)) return;
      seen.add(id);
      uniq.add(id);
    }

    if (rawMentionUserIds is List) {
      for (final item in rawMentionUserIds) {
        add(item);
      }
    }

    if (content.length > 1) {
      for (var i = 0; i < content.length; i++) {
        if (content.codeUnitAt(i) != 64) {
          continue;
        }
        var j = i + 1;
        while (j < content.length) {
          final code = content.codeUnitAt(j);
          if (code < 48 || code > 57) {
            break;
          }
          j++;
        }
        if (j > i + 1) {
          add(content.substring(i + 1, j));
        }
      }
    }

    return uniq;
  }

  Map<String, dynamic>? _decodeExtraMapImpl(dynamic rawExtra) {
    if (rawExtra is Map) {
      return Map<String, dynamic>.from(rawExtra);
    }
    if (rawExtra is String) {
      final trimmed = rawExtra.trim();
      if (trimmed.isEmpty) {
        return null;
      }
      try {
        final decoded = jsonDecode(trimmed);
        if (decoded is Map) {
          return Map<String, dynamic>.from(decoded);
        }
      } catch (_) {
        return null;
      }
    }
    return null;
  }

  String _toIdImpl(dynamic v) {
    return v?.toString().trim() ?? '';
  }

  int _requireIntLikeImpl(dynamic v, {required String fieldName}) {
    try {
      return StrictIntParser.parse(v, fieldName: fieldName);
    } on FormatException {
      throw StateError('$fieldName must be integer number');
    }
  }

  void _syncNowImpl() {
    _isSuspendedForAppBackground = false;
    // _wsUrl 已由 connect() 在登录/启动时设置；此处不回落 defaultWsUrl，
    // 避免全球区用户在 _wsUrl 尚未初始化的极短窗口内被意外写入 CN 地址。
    // _scheduleReconnect() 内已有 `_wsUrl == null` 守卫，可安全 no-op。
    if (_isConnected.value && _isAuthenticated.value) {
      _triggerPullSyncThrottled();
      return;
    }
    _allowReconnect = true;
    _beginInitialConnectionBannerDelayCycle();
    _setConnectionStage(ImConnectionStage.reconnecting);
    // immediate：这是用户点"重试"或网络恢复的显式触发，必须绕开当前退避窗口
    // 立刻连一次，否则退避到 30 秒上限后按钮会毫无反应。
    _scheduleReconnect(immediate: true);
  }

  void _disconnectImpl({
    ImConnectionStage stage = ImConnectionStage.disconnected,
  }) {
    _allowReconnect = false;
    _hasPendingInitialConnection = false;
    _settlePendingAgentToolbarActionAcks(false);
    _connectEpoch++;
    _isConnecting = false;
    _lastPongAtMs = 0;
    _heartbeatTimer?.cancel();
    _heartbeatTimer = null;
    _authHandshakeTimer?.cancel();
    _authHandshakeTimer = null;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _pendingReadRetryTimer?.cancel();
    _pendingReadRetryTimer = null;
    _pullSyncThrottleTimer?.cancel();
    _pullSyncThrottleTimer = null;
    _sessionHistoryResetRetryTimer?.cancel();
    _sessionHistoryResetRetryTimer = null;
    _editPreviewFlushTimer?.cancel();
    _editPreviewFlushTimer = null;
    _editPreviewFlushCompleter = null;
    _pendingEditPreviewBySessionId.clear();
    _wsSubscription?.cancel();
    _wsSubscription = null;
    final oldChannel = _channel;
    if (oldChannel != null) {
      try {
        oldChannel.sink.close();
      } catch (e) {
        debugPrint('WebSocket close error: $e');
      }
    }
    _channel = null;
    // _wsUrl 故意保留：断线不清除目标端点，让 ensureConnected() 在切后台再回前台时
    // 能复用正确的区域地址，而非回落到编译时国区默认值。
    // 重新登录时 connect() 会覆写 _wsUrl；logout 后 isLoggedIn=false 阻止误连。
    for (final c in _fileListPending.values) {
      if (!c.isCompleted) c.complete([]);
    }
    _fileListPending.clear();
    for (final c in _createFolderPending.values) {
      if (!c.isCompleted) {
        c.completeError(Exception('im_disconnected'.tr));
      }
    }
    _createFolderPending.clear();
    for (final c in _skillUploadPending.values) {
      if (!c.isCompleted) {
        c.completeError(Exception('im_disconnected'.tr));
      }
    }
    _skillUploadPending.clear();
    for (final c in _skillEnablePending.values) {
      if (!c.isCompleted) {
        c.completeError(Exception('im_disconnected'.tr));
      }
    }
    _skillEnablePending.clear();
    for (final c in _skillDisablePending.values) {
      if (!c.isCompleted) {
        c.completeError(Exception('im_disconnected'.tr));
      }
    }
    _skillDisablePending.clear();
    for (final c in _sessionBindPending.values) {
      if (!c.isCompleted) {
        c.completeError(Exception('im_disconnected'.tr));
      }
    }
    _sessionBindPending.clear();
    _reconnectAttempts = 0;
    _isConnected.value = false;
    _isAuthenticated.value = false;
    _composingRenewTimer?.cancel();
    _composingRenewTimer = null;
    _composingIdleTimer?.cancel();
    _composingIdleTimer = null;
    _sessionViewingRenewTimer?.cancel();
    _sessionViewingRenewTimer = null;
    _initialSessionLoadTimer?.cancel();
    _initialSessionLoadTimer = null;
    _initialLoadRetryTimer?.cancel();
    _initialLoadRetryTimer = null;
    _staleAgentOutputTimer?.cancel();
    _staleAgentOutputTimer = null;
    _setConnectionStage(stage);
    _clearSendAckTimers();
  }

  Future<void> _resetForAccountSwitchImpl() async {
    disconnect();
    await _clearBootstrapInboxSeqFloorForCurrentUser();
    _lastFriendEventSeq = 0;
    _lastInboxSeqCursor = 0;
    _bootstrapInboxSeqFloor = 0;
    _lastSessionSyncCursor = 0;
    _friendEventSeqLoaded = false;
    _inboxSeqCursorLoaded = false;
    _bootstrapInboxSeqFloorLoaded = false;
    _sessionSyncCursorLoaded = false;
    _sessionSyncIncrementalInFlight = false;
    _friendSyncInFlight = false;
    _pendingResendInFlight = false;
    _lastPullSyncRequestMs = 0;
    _pendingPullSyncCursorFloor = 0;
    _persistFailPullSyncStreak = 0;
    _lastPersistFailPullSyncScheduleMs = 0;
    _pendingPersistFailPullSync = false;
    _sessionWindowPaginationHasMore = false;
    _sessionWindowPaginationNextOffset = 0;
    _sessionWindowPaginationInFlight = false;
    _sessionWindowPaginationNextAllowedAtMs = 0;
    _lastSessionWindowRefreshAttemptAtMs = 0;
    _sessionTypeHints.clear();
    _locallyDeletedSessions.clear();
    _locallyRevokedSessions.clear();
    _sessionHistoryResetInFlightAtMs.clear();
    _sessionHistoryResetInFlightDeletedAtMs.clear();
    _lastSessionHistoryResetSeq = 0;
    _downstreamLagLastLogAtMsByCmd.clear();
    _downstreamLagSuppressedByCmd.clear();
    _deletedSessionsLoaded = false;
    _revokedSessionsLoaded = false;
    _inflightSessionAccessProbe.clear();
    _activeStreamingMsgIds.clear();
    _clearAllStreamingSessionPreviews();
    _clearStreamChunkGapTrackingState();
    _locallyStoppedStreamMsgIds.clear();
    _hiddenAgentOutputMessages.clear();
    _pendingLocalStopStateBySession.clear();
    _pendingLocalStopStreamMsgIdBySession.clear();
    _cancelDbChangeSubscription();
    _agentStateExpiryTimer?.cancel();
    _agentStateExpiryTimer = null;
    _agentStateExpiryTick.value = 0;
    _clearSendAckTimers();
    _clearRuntimeState();
  }

  void _onCloseImpl() {
    disconnect();
    _sessionsBadgeWorker?.dispose();
    _sessionsBadgeWorker = null;
    _currentSessionBadgeWorker?.dispose();
    _currentSessionBadgeWorker = null;
    _initialConnectionBannerDelayTimer?.cancel();
    _initialConnectionBannerDelayTimer = null;
    _connectionLossBannerDelayTimer?.cancel();
    _connectionLossBannerDelayTimer = null;
    _connectionBannerSuppressionTimer?.cancel();
    _connectionBannerSuppressionTimer = null;
    _authHandshakeTimer?.cancel();
    _authHandshakeTimer = null;
    _agentStateExpiryTimer?.cancel();
    _agentStateExpiryTimer = null;
    _sessionViewingRenewTimer?.cancel();
    _sessionViewingRenewTimer = null;
    _initialSessionLoadTimer?.cancel();
    _initialSessionLoadTimer = null;
    _initialLoadRetryTimer?.cancel();
    _initialLoadRetryTimer = null;
    _localStreamThrottleTimer?.cancel();
    _localStreamThrottleTimer = null;
    _streamingWatchdogTimer?.cancel();
    _streamingWatchdogTimer = null;
    _clearRuntimeState();
  }

  Future<void> _loadSessionsForCurrentUserImpl() async {
    _clearRuntimeState();
    if (LocalDb.hasActiveUser) {
      deferSystemUnreadBadgeSyncUntilAuthoritativeRefresh();
      await _ensurePendingReadStatesLoaded();
      await _ensureDeletedSessionsLoaded();
      await _ensureRevokedSessionsLoaded();
      _queueDeletedSessionReadClears();
      await loadSessions();
    }
  }

  void _clearRuntimeState() {
    _downstreamQueue = Future.value();
    _streamDownstreamQueue = Future.value();
    _clearStreamDiagnostics(reason: 'clear_runtime_state');
    _hasEstablishedRealtimeSession = false;
    _hasPendingInitialConnection = false;
    _currentSessionId.value = null;
    _resetMessageWindowState();
    initialHistoryReady.value = false;
    _friendSyncInFlight = false;
    _pendingResendInFlight = false;
    _pendingReadRetryTimer?.cancel();
    _pendingReadRetryTimer = null;
    _pullSyncThrottleTimer?.cancel();
    _pullSyncThrottleTimer = null;
    _sessionHistoryResetRetryTimer?.cancel();
    _sessionHistoryResetRetryTimer = null;
    _authHandshakeTimer?.cancel();
    _authHandshakeTimer = null;
    _sessionActivityCleanupTimer?.cancel();
    _sessionActivityCleanupTimer = null;
    _composingRenewTimer?.cancel();
    _composingRenewTimer = null;
    _composingIdleTimer?.cancel();
    _composingIdleTimer = null;
    _sessionViewingRenewTimer?.cancel();
    _sessionViewingRenewTimer = null;
    _initialSessionLoadTimer?.cancel();
    _initialSessionLoadTimer = null;
    _initialLoadRetryTimer?.cancel();
    _initialLoadRetryTimer = null;
    _composingSessionId = '';
    _composingActive = false;
    _viewingSessionId = '';
    _viewingActive = false;
    _pendingReadStatesBySession.clear();
    _localUnreadOverrides.clear();
    _pendingReadStatesLoaded = false;
    _inboxSeqCursorLoaded = false;
    _bootstrapInboxSeqFloorLoaded = false;
    _sessionSyncCursorLoaded = false;
    _sessionSyncIncrementalInFlight = false;
    _deletedSessionsLoaded = false;
    _revokedSessionsLoaded = false;
    _deferSystemUnreadBadgeSync = false;
    _lastInboxSeqCursor = 0;
    _lastSessionSyncCursor = 0;
    _bootstrapInboxSeqFloor = 0;
    _lastPullSyncRequestMs = 0;
    _pendingPullSyncCursorFloor = 0;
    _persistFailPullSyncStreak = 0;
    _lastPersistFailPullSyncScheduleMs = 0;
    _pendingPersistFailPullSync = false;
    _sessionWindowPaginationHasMore = false;
    _sessionWindowPaginationNextOffset = 0;
    _sessionWindowPaginationInFlight = false;
    _sessionWindowPaginationNextAllowedAtMs = 0;
    _lastSessionWindowRefreshAttemptAtMs = 0;
    _sessionTypeHints.clear();
    _locallyDeletedSessions.clear();
    _locallyRevokedSessions.clear();
    _sessionHistoryResetInFlightAtMs.clear();
    _sessionHistoryResetInFlightDeletedAtMs.clear();
    _lastSessionHistoryResetSeq = 0;
    _downstreamLagLastLogAtMsByCmd.clear();
    _downstreamLagSuppressedByCmd.clear();
    _inflightSessionAccessProbe.clear();
    _peerIdentityBackfillAttempted.clear();
    _peerIdentityBackfillRearmCount.clear();
    _peerIdentityBackfillInFlight = false;
    _activeStreamingMsgIds.clear();
    _streamingActivityAtByMsgId.clear();
    _streamingWatchdogTimer?.cancel();
    _streamingWatchdogTimer = null;
    _clearStreamChunkGapTrackingState();
    _locallyStoppedStreamMsgIds.clear();
    _streamingPlaceholders.clear();
    _hiddenAgentOutputMessages.clear();
    _pendingLocalStopStateBySession.clear();
    _pendingLocalStopStreamMsgIdBySession.clear();
    _agentStateExpiryTimer?.cancel();
    _agentStateExpiryTimer = null;
    _agentStateExpiryTick.value = 0;
    sessionActivities.clear();
    _setConnectionStage(ImConnectionStage.disconnected);
    _clearSendAckTimers();
    currentMessages.clear();
    _clearCurrentMessageIndexes();
    sessions.clear();
    delegateStates.clear();
    voiceDelegateStates.clear();
    agentOutputStates.clear();
    for (final entry in _pendingDeliveryTimeoutTimers.values) {
      entry.timer?.cancel();
    }
    _pendingDeliveryTimeoutTimers.clear();
    agentToolbars.clear();
    eventLifecycleQueues.clear();
    _agentToolbarLoadingItemBySession.clear();
    _agentToolbarPendingActionBySession.clear();
    _agentToolbarTargetAgentIdBySession.clear();
    agentStates.clear();
    sessionMemberEventVersions.clear();
    sessionAccessRevokedVersions.clear();
    _sessionAccessRevokedReasons.clear();
    sessionReadVersions.clear();
    _sessionReadCursorBySession.clear();
  }
}
