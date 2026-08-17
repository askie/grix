part of 'im_service.dart';

extension _ImServiceConnection on ImService {
  void _cancelAuthHandshakeTimer() {
    _authHandshakeTimer?.cancel();
    _authHandshakeTimer = null;
  }

  void _armAuthHandshakeTimer({required String phase}) {
    _cancelAuthHandshakeTimer();
    _authHandshakeTimer = Timer(ImService._authHandshakeTimeout, () {
      _authHandshakeTimer = null;
      _handleAuthHandshakeTimeout(phase: phase);
    });
  }

  void _handleAuthHandshakeTimeout({required String phase}) {
    if (!_isConnected.value) {
      return;
    }
    debugPrint('⚠️ $phase handshake timeout, reconnecting...');
    _allowReconnect = true;
    _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
  }

  void _connectImpl(String wsUrl) {
    _allowReconnect = true;
    _isSuspendedForAppBackground = false;
    _wsUrl = wsUrl;
    if (_isConnected.value || _isConnecting) return;

    _beginInitialConnectionBannerDelayCycle();
    _setConnectionStage(ImConnectionStage.connecting);
    _reconnectAttempts = 0;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    unawaited(_doConnect());
  }

  void _setConnectionStage(ImConnectionStage stage) {
    if (_connectionStage.value == stage) return;
    _connectionStage.value = stage;
    switch (stage) {
      case ImConnectionStage.connected:
        _hasEstablishedRealtimeSession = true;
        _hasPendingInitialConnection = false;
        break;
      case ImConnectionStage.kicked:
      case ImConnectionStage.authFailed:
        _hasPendingInitialConnection = false;
        break;
      case ImConnectionStage.disconnected:
      case ImConnectionStage.connecting:
      case ImConnectionStage.authenticating:
      case ImConnectionStage.reconnecting:
        break;
    }
    _syncConnectionBannerDelayForStage(stage);
  }

  void _syncConnectionBannerDelayForStage(ImConnectionStage stage) {
    _syncInitialConnectionBannerDelayForStage(stage);
    if (_hasPendingInitialConnection) {
      _clearConnectionLossBannerDelay();
      return;
    }
    switch (stage) {
      case ImConnectionStage.reconnecting:
      case ImConnectionStage.disconnected:
        _armConnectionLossBannerDelay();
        return;
      case ImConnectionStage.connecting:
      case ImConnectionStage.authenticating:
        // 重连恢复中：connecting/authenticating 是恢复链路的中间环节，
        // 必须保留已 armed 的连接丢失宽限窗口，避免在鉴权往返期间
        //（跨区高 RTT 时尤其明显，鉴权要先刷 token 再 auth 往返）把横幅
        // 闪出来。窗口仅在真正连上后清除。
        return;
      case ImConnectionStage.connected:
      case ImConnectionStage.kicked:
      case ImConnectionStage.authFailed:
        _clearConnectionLossBannerDelay();
        return;
    }
  }

  void _beginInitialConnectionBannerDelayCycle() {
    if (_hasEstablishedRealtimeSession || _hasPendingInitialConnection) {
      return;
    }
    _hasPendingInitialConnection = true;
  }

  void _syncInitialConnectionBannerDelayForStage(ImConnectionStage stage) {
    if (!_hasPendingInitialConnection ||
        !_usesInitialConnectionBannerDelay(stage)) {
      _clearInitialConnectionBannerDelay();
      return;
    }
    _armInitialConnectionBannerDelay();
  }

  bool _usesInitialConnectionBannerDelay(ImConnectionStage stage) {
    switch (stage) {
      case ImConnectionStage.connecting:
      case ImConnectionStage.authenticating:
      case ImConnectionStage.reconnecting:
        return true;
      case ImConnectionStage.disconnected:
      case ImConnectionStage.connected:
      case ImConnectionStage.kicked:
      case ImConnectionStage.authFailed:
        return false;
    }
  }

  void _armInitialConnectionBannerDelay() {
    if (_initialConnectionBannerReadyAtMs > 0) {
      return;
    }
    _initialConnectionBannerReadyAtMs =
        ImService.nowMsProvider() +
        ImService._initialConnectionBannerDelay.inMilliseconds;
    _connectionBannerVisibilityTick.value++;
    _rescheduleInitialConnectionBannerDelayTimer();
  }

  void _clearInitialConnectionBannerDelay() {
    if (_initialConnectionBannerReadyAtMs <= 0 &&
        _initialConnectionBannerDelayTimer == null) {
      return;
    }
    _initialConnectionBannerReadyAtMs = 0;
    _initialConnectionBannerDelayTimer?.cancel();
    _initialConnectionBannerDelayTimer = null;
    _connectionBannerVisibilityTick.value++;
  }

  bool _isInitialConnectionBannerDelayActive() {
    final readyAtMs = _initialConnectionBannerReadyAtMs;
    if (readyAtMs <= 0) {
      return false;
    }
    return ImService.nowMsProvider() < readyAtMs;
  }

  void _rescheduleInitialConnectionBannerDelayTimer() {
    _initialConnectionBannerDelayTimer?.cancel();
    final readyAtMs = _initialConnectionBannerReadyAtMs;
    if (readyAtMs <= 0) {
      _initialConnectionBannerDelayTimer = null;
      return;
    }
    final remainingMs = readyAtMs - ImService.nowMsProvider();
    if (remainingMs <= 0) {
      _initialConnectionBannerDelayTimer = null;
      _connectionBannerVisibilityTick.value++;
      return;
    }
    _initialConnectionBannerDelayTimer = Timer(
      Duration(milliseconds: remainingMs),
      () {
        _initialConnectionBannerDelayTimer = null;
        if (_isInitialConnectionBannerDelayActive()) {
          _rescheduleInitialConnectionBannerDelayTimer();
          return;
        }
        _connectionBannerVisibilityTick.value++;
      },
    );
  }

  void _armConnectionLossBannerDelay() {
    if (_connectionLossBannerReadyAtMs > 0) {
      return;
    }
    _connectionLossBannerReadyAtMs =
        ImService.nowMsProvider() +
        ImService._connectionLossBannerDelay.inMilliseconds;
    _connectionBannerVisibilityTick.value++;
    _rescheduleConnectionLossBannerDelayTimer();
  }

  void _clearConnectionLossBannerDelay() {
    if (_connectionLossBannerReadyAtMs <= 0 &&
        _connectionLossBannerDelayTimer == null) {
      return;
    }
    _connectionLossBannerReadyAtMs = 0;
    _connectionLossBannerDelayTimer?.cancel();
    _connectionLossBannerDelayTimer = null;
    _connectionBannerVisibilityTick.value++;
  }

  bool _isConnectionLossBannerDelayActive() {
    final readyAtMs = _connectionLossBannerReadyAtMs;
    if (readyAtMs <= 0) {
      return false;
    }
    return ImService.nowMsProvider() < readyAtMs;
  }

  void _rescheduleConnectionLossBannerDelayTimer() {
    _connectionLossBannerDelayTimer?.cancel();
    final readyAtMs = _connectionLossBannerReadyAtMs;
    if (readyAtMs <= 0) {
      _connectionLossBannerDelayTimer = null;
      return;
    }
    final remainingMs = readyAtMs - ImService.nowMsProvider();
    if (remainingMs <= 0) {
      _connectionLossBannerDelayTimer = null;
      _connectionBannerVisibilityTick.value++;
      return;
    }
    _connectionLossBannerDelayTimer = Timer(
      Duration(milliseconds: remainingMs),
      () {
        _connectionLossBannerDelayTimer = null;
        if (_isConnectionLossBannerDelayActive()) {
          _rescheduleConnectionLossBannerDelayTimer();
          return;
        }
        _connectionBannerVisibilityTick.value++;
      },
    );
  }

  bool _isActiveConnectAttempt(int attemptId) {
    return attemptId == _connectEpoch;
  }

  Future<void> _doConnect() async {
    final wsUrl = _wsUrl;
    if (wsUrl == null) return;
    if (_isConnected.value || _isConnecting) return;

    _setConnectionStage(
      _reconnectAttempts > 0
          ? ImConnectionStage.reconnecting
          : ImConnectionStage.connecting,
    );
    final attemptId = ++_connectEpoch;
    _isConnecting = true;
    debugPrint('🔌 Connecting to WebSocket: $wsUrl');

    WebSocketChannel? channel;
    StreamSubscription? subscription;
    _armConnectWatchdog(attemptId);
    try {
      final connector =
          ImService.channelConnectorForTest ?? WebSocketChannel.connect;
      channel = connector(Uri.parse(wsUrl));
      subscription = channel.stream.listen(
        (data) {
          if (!_isActiveConnectAttempt(attemptId)) return;
          _handleSocketPayload(data.toString());
        },
        onError: (e) {
          if (!_isActiveConnectAttempt(attemptId)) return;
          debugPrint('❌ WebSocket error: $e');
          _handleDisconnect();
        },
        onDone: () {
          if (!_isActiveConnectAttempt(attemptId)) return;
          debugPrint('🔌 WebSocket closed');
          _handleDisconnect();
        },
      );

      await channel.ready.timeout(ImService._connectReadyTimeout);
      if (!_isActiveConnectAttempt(attemptId)) {
        _discardConnectAttempt(subscription, channel);
        return;
      }

      _cancelConnectWatchdog();
      _channel = channel;
      _wsSubscription = subscription;
      _isConnecting = false;
      _isConnected.value = true;
      _lastPongAtMs = DateTime.now().millisecondsSinceEpoch;
      _setConnectionStage(ImConnectionStage.authenticating);

      await _triggerAuth();
      _startHeartbeat();
    } catch (e) {
      debugPrint('❌ Connect error: $e');
      // 先复位状态再做清理：休眠唤醒后底层 connect 可能长时间悬死，清理
      // 动作一旦 await 就会把 _isConnecting 卡死，自动重连和手动重试全部
      // 被守卫吞掉（横幅常驻、重试无响应）。
      final active = _isActiveConnectAttempt(attemptId);
      _discardConnectAttempt(subscription, channel);
      if (active) {
        _isConnecting = false;
        _handleDisconnect();
      }
    }
  }

  /// 丢弃一次失败/作废的建连尝试。绝不 await：cancel/close 在底层 connect
  /// 悬死时可能永不完成；也吞掉它们的异步错误，避免外溢跳过状态复位。
  void _discardConnectAttempt(
    StreamSubscription? subscription,
    WebSocketChannel? channel,
  ) {
    if (subscription != null) {
      try {
        unawaited(subscription.cancel().catchError((_) {}));
      } catch (_) {}
    }
    if (channel != null) {
      try {
        unawaited(channel.sink.close().catchError((_) {}));
      } catch (_) {}
    }
  }

  /// 建连尝试的兜底看门狗：到点仍停留在"连接中"且 epoch 未变，说明该次
  /// 尝试已悬死在未知路径上，强制作废并让重连链路继续走退避循环。
  void _armConnectWatchdog(int attemptId) {
    _connectWatchdogTimer?.cancel();
    _connectWatchdogTimer = Timer(ImService.connectAttemptHardTimeout, () {
      _connectWatchdogTimer = null;
      if (!_isConnecting || !_isActiveConnectAttempt(attemptId)) return;
      debugPrint('⚠️ Connect attempt wedged, force recycle');
      _isConnecting = false;
      _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
    });
  }

  void _cancelConnectWatchdog() {
    _connectWatchdogTimer?.cancel();
    _connectWatchdogTimer = null;
  }

  void _startHeartbeat() {
    _heartbeatTimer?.cancel();
    _heartbeatTimer = Timer.periodic(ImService._heartbeatInterval, (_) {
      if (!_isConnected.value || _channel == null) {
        return;
      }

      final now = DateTime.now().millisecondsSinceEpoch;
      if (_lastPongAtMs > 0 &&
          now - _lastPongAtMs > ImService._pongTimeout.inMilliseconds) {
        debugPrint('⚠️ Heartbeat timeout, force reconnect');
        _handleDisconnect();
        return;
      }

      final req = {'cmd': 'ping', 'seq': now, 'payload': {}};
      _sendPacket(req);
    });
  }

  void _handleDisconnect({ImConnectionStage? finalStage}) {
    _connectEpoch++;
    _isConnecting = false;
    _settlePendingAgentToolbarActionAcks(false);
    _cancelConnectWatchdog();
    _isConnected.value = false;
    _isAuthenticated.value = false;
    _composingRenewTimer?.cancel();
    _composingRenewTimer = null;
    _composingIdleTimer?.cancel();
    _composingIdleTimer = null;
    _sessionViewingRenewTimer?.cancel();
    _sessionViewingRenewTimer = null;
    _friendSyncInFlight = false;
    _pendingResendInFlight = false;
    _lastPongAtMs = 0;
    _consecutiveSendAckTimeouts = 0;
    _heartbeatTimer?.cancel();
    _heartbeatTimer = null;
    _cancelAuthHandshakeTimer();
    _pendingReadRetryTimer?.cancel();
    _pendingReadRetryTimer = null;
    _pullSyncThrottleTimer?.cancel();
    _pullSyncThrottleTimer = null;
    _sessionHistoryResetRetryTimer?.cancel();
    _sessionHistoryResetRetryTimer = null;
    _clearSendAckTimers();
    final oldSubscription = _wsSubscription;
    if (oldSubscription != null) {
      unawaited(oldSubscription.cancel().catchError((_) {}));
    }
    _wsSubscription = null;
    // Streaming state is tied to the live connection. Clear the active set so
    // the 90-second stale capsule pruner is no longer blocked by phantom
    // streaming entries, and so reconnect can get a fresh snapshot from the
    // server rather than relying on pre-disconnect state.
    _activeStreamingMsgIds.clear();
    _clearStreamChunkGapTrackingState();
    // Reset downstream queues so stale in-flight DB operations from the
    // previous connection cannot block the new auth_ack processing.
    _downstreamQueue = Future.value();
    _streamDownstreamQueue = Future.value();
    final oldChannel = _channel;
    _channel = null;
    if (oldChannel != null) {
      try {
        oldChannel.sink.close();
      } catch (e) {
        debugPrint('WebSocket close error: $e');
      }
    }

    if (_allowReconnect) {
      _setConnectionStage(ImConnectionStage.reconnecting);
      _scheduleReconnect();
      return;
    }
    _hasPendingInitialConnection = false;
    _setConnectionStage(finalStage ?? ImConnectionStage.disconnected);
  }

  /// [immediate] 用于用户手动重试、网络恢复等显式触发的重连：丢弃正在等待的退避
  /// 定时器并把退避计数归零，立刻发起一次连接。否则一旦退避涨到 30 秒上限，
  /// 手动重试会被 `_reconnectTimer` 存活检查静默吞掉，表现为"点了没反应"。
  void _scheduleReconnect({bool immediate = false}) {
    if (!_allowReconnect || _wsUrl == null) return;
    if (_isConnecting) {
      if (!immediate) return;
      // 手动重试/网络恢复必须无条件生效：在途的建连尝试可能已经悬死
      // （休眠唤醒后出现过），作废它——epoch 失配会让它所有回调变成
      // no-op——并立刻开启新一轮连接。
      _connectEpoch++;
      _isConnecting = false;
      _cancelConnectWatchdog();
    }
    if (!immediate && (_reconnectTimer?.isActive ?? false)) return;
    if (_isConnected.value) {
      if (_isAuthenticated.value) return;
      // Connection is up but auth is not. Force a fresh connect cycle
      // rather than waiting for an auth handshake that might be stuck.
      _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
      // Fall through to actual scheduling.
    }

    _setConnectionStage(ImConnectionStage.reconnecting);
    if (immediate) {
      _reconnectAttempts = 0;
    }
    _reconnectAttempts++;
    final exp = (_reconnectAttempts - 1).clamp(0, 5);
    final multiplier = 1 << exp;
    final seconds = (ImService._baseReconnectDelay.inSeconds * multiplier)
        .clamp(1, ImService._maxReconnectDelay.inSeconds);
    final jitterMs = DateTime.now().millisecondsSinceEpoch % 700;
    final delay = immediate
        ? Duration.zero
        : Duration(seconds: seconds, milliseconds: jitterMs);
    debugPrint(
      '🔄 Reconnecting in ${delay.inMilliseconds}ms (attempt $_reconnectAttempts)',
    );
    _reconnectTimer?.cancel();
    _reconnectTimer = Timer(delay, () {
      _reconnectTimer = null;
      if (!_isConnected.value && !_isConnecting) {
        unawaited(_doConnect());
      }
    });
  }

  Future<void> _triggerAuth() async {
    final authService = Get.find<AuthService>();
    final canFastPathAuth = authService.hasUsableAccessToken(
      minRemaining: ImService._realtimeAuthFastPathMinRemaining,
    );
    if (!canFastPathAuth) {
      final refreshStatus = await authService.ensureTokenFreshStatus();
      if (refreshStatus == TokenRefreshStatus.invalidSession) {
        debugPrint('❌ Session invalid before WS auth');
        authService.handleUnauthorized();
        return;
      }
      if (refreshStatus == TokenRefreshStatus.temporaryFailure) {
        debugPrint('⚠️ Token refresh unavailable before WS auth, retry later');
        _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
        return;
      }
    }
    final token = authService.token;

    if (token == null || token.isEmpty) {
      debugPrint('❌ No token available');
      // 不能挂着一条未鉴权的连接干等——那样只剩 90 秒心跳和服务端踢人这
      // 两个外部兜底。直接进入重连循环，下一轮建连前的刷 token 负责把
      // 凭证补齐。
      _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
      return;
    }

    final deviceId = await DeviceIdentity.resolveDeviceId();
    final platform = DeviceIdentity.platformLabel();

    final req = {
      'cmd': 'auth',
      'seq': 1,
      'payload': {'token': token, 'device_id': deviceId, 'platform': platform},
    };
    debugPrint('📤 Sending auth...');
    _setConnectionStage(ImConnectionStage.authenticating);
    if (_sendPacket(req, requireAuthenticated: false)) {
      _armAuthHandshakeTimer(phase: 'auth');
    }
  }

  void _reAuthWithLatestTokenImpl() {
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    final authService = Get.find<AuthService>();
    final token = authService.token;
    if (token == null || token.isEmpty) {
      return;
    }
    final req = {
      'cmd': 're_auth',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'token': token},
    };
    debugPrint('📤 Sending re_auth...');
    if (_sendPacket(req, requireAuthenticated: true)) {
      _armAuthHandshakeTimer(phase: 're_auth');
    }
  }

  Future<void> _handleWsCredentialFailure({required bool reAuth}) async {
    final authService = Get.find<AuthService>();
    final refreshStatus = await authService.ensureTokenFreshStatus(force: true);
    switch (refreshStatus) {
      case TokenRefreshStatus.ready:
        if (reAuth) {
          _reAuthWithLatestTokenImpl();
        } else {
          debugPrint('🔁 Token refreshed after auth failure, reconnecting...');
          _allowReconnect = true;
          _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
        }
        return;
      case TokenRefreshStatus.invalidSession:
        debugPrint('❌ Session invalid after WS auth failure');
        authService.handleUnauthorized();
        return;
      case TokenRefreshStatus.temporaryFailure:
        debugPrint('⚠️ WS auth refresh failed temporarily, keep session');
        _allowReconnect = true;
        _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
        return;
    }
  }

  bool _shouldPreserveLoginOnKick(String reason) {
    switch (reason) {
      case 'same_platform_login':
      case 'replaced_by_new_connection':
        return true;
      default:
        return false;
    }
  }

  void _triggerPullSync({int? cursorOverride}) async {
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    _lastPullSyncRequestMs = DateTime.now().millisecondsSinceEpoch;
    await _ensureInboxSeqCursorLoaded();
    final localMax = await LocalDb.getMaxInboxSeq();
    final maxSeq = (cursorOverride != null && cursorOverride >= 0)
        ? cursorOverride
        : _resolvePullSyncCursor(localMax);
    final req = {
      'cmd': 'pull_sync',
      'payload': {'last_inbox_seq': maxSeq.toString()},
    };
    _sendPacket(req, requireAuthenticated: true);
  }

  void _triggerDelegateList() {
    final req = {'cmd': 'delegate_list', 'payload': {}};
    _sendPacket(req, requireAuthenticated: true);
  }

  void _refreshDelegateStatesImpl() {
    if (!isConnected || !isAuthenticated || _channel == null) {
      return;
    }
    _triggerDelegateList();
  }

  void _refreshAgentOnlineStatesImpl() {
    if (!isConnected || !isAuthenticated || _channel == null) {
      return;
    }
    _reAuthWithLatestTokenImpl();
  }

  void _setRealtimeAppStateImpl(String appState) {
    final normalized = appState.trim().toLowerCase();
    if (normalized != 'foreground' && normalized != 'background') {
      return;
    }
    _realtimeAppState = normalized;
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    _sendRealtimeAppStateIfPossible();
  }

  void _sendRealtimeAppStateIfPossible() {
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    _sendPacket({
      'cmd': 'app_state_set',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {
        'app_state': _realtimeAppState,
        'updated_at': DateTime.now().millisecondsSinceEpoch,
      },
    }, requireAuthenticated: true);
  }

  bool _sendPacket(
    Map<String, dynamic> req, {
    bool requireAuthenticated = false,
  }) {
    final cmd = req['cmd']?.toString().trim() ?? '';
    final channel = _channel;
    if (!_isConnected.value || channel == null) {
      if (cmd == 'agent_output_stop') {
        debugPrint(
          '❌ agent_output_stop blocked before send '
          'connected=${_isConnected.value} has_channel=${channel != null}',
        );
      }
      return false;
    }
    if (requireAuthenticated && !_isAuthenticated.value) {
      if (cmd == 'agent_output_stop') {
        debugPrint(
          '❌ agent_output_stop blocked before send authenticated=false',
        );
      }
      return false;
    }
    try {
      channel.sink.add(jsonEncode(req));
      if (cmd == 'agent_output_stop') {
        debugPrint(
          '📡 agent_output_stop packet enqueued seq=${req['seq']} '
          'session_id=${(req['payload'] as Map?)?['session_id'] ?? "-"} '
          'run_id=${((req['payload'] as Map?)?['run_id'] ?? "").toString().trim().isEmpty ? "-" : (req['payload'] as Map?)?['run_id']}',
        );
      }
      return true;
    } catch (e) {
      debugPrint('❌ Send packet failed (${req['cmd']}): $e');
      _handleDisconnect();
      return false;
    }
  }
}
