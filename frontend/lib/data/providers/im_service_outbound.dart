part of 'im_service.dart';

class _PendingLocalInferenceStart {
  _PendingLocalInferenceStart({
    required this.sessionId,
    required this.agentId,
    required this.triggerMsgId,
    required this.endpoint,
    required this.modelName,
    required this.systemPrompt,
    required this.messages,
    required this.quotedMessageId,
  });

  final String sessionId;
  final String agentId;
  final String triggerMsgId;
  final String endpoint;
  final String modelName;
  final String systemPrompt;
  final List<Map<String, String>> messages;
  final String quotedMessageId;
  int attemptCount = 0;

  String get key => '$sessionId::$agentId::${triggerMsgId.trim()}';
}

extension _ImServiceOutbound on ImService {
  void _startSendAckTimer(
    String clientMsgId, {
    required bool isDelegate,
    Duration timeout = ImService._sendAckTimeout,
  }) {
    _cancelSendAckTimer(clientMsgId);
    _sendAckTimers[clientMsgId] = Timer(timeout, () {
      _sendAckTimers.remove(clientMsgId);
      unawaited(_handleSendAckTimeout(clientMsgId, isDelegate: isDelegate));
    });
  }

  Future<void> _handleSendAckTimeout(
    String clientMsgId, {
    required bool isDelegate,
  }) async {
    final idx = currentMessages.indexWhere((e) => e.clientMsgId == clientMsgId);
    final memStatus = idx != -1 ? (currentMessages[idx].status ?? '') : '';
    final local = await LocalDb.getMessageByLocalSeq(clientMsgId);
    final dbStatus = local?['status']?.toString() ?? '';
    final effectiveStatus = dbStatus.isNotEmpty ? dbStatus : memStatus;
    if (!effectiveStatus.startsWith('sending')) {
      return;
    }

    final delegatePending =
        isDelegate ||
        memStatus.contains('delegate') ||
        dbStatus.contains('delegate');
    if (delegatePending) {
      await LocalDb.updateMessageStatusByLocalSeq(
        clientMsgId,
        'failed_delegate',
      );
      if (local != null) {
        final sid = local['session_id']?.toString().trim() ?? '';
        final mid = local['msg_id']?.toString().trim() ?? '';
        if (sid.isNotEmpty && mid.isNotEmpty) {
          LocalDbChangeBus.instance.emitMessageChange(
            LocalMessageUpdated(sessionId: sid, msgId: mid),
          );
        }
      }
      return;
    }

    debugPrint('⚠️ send_ack timeout client_msg_id=$clientMsgId, keep pending');
    if (!_isConnected.value || !_isAuthenticated.value) {
      return;
    }
    _consecutiveSendAckTimeouts++;
    if (_consecutiveSendAckTimeouts <
        ImService._sendAckTimeoutReconnectThreshold) {
      _startSendAckTimer(
        clientMsgId,
        isDelegate: isDelegate,
        timeout: ImService._sendAckRecoveryTimeout,
      );
      _triggerPullSyncThrottled();
      return;
    }
    debugPrint('⚠️ repeated send_ack timeout, reconnect websocket');
    _allowReconnect = true;
    _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
  }

  void _cancelSendAckTimer(String clientMsgId) {
    final timer = _sendAckTimers.remove(clientMsgId);
    timer?.cancel();
  }

  void _clearSendAckTimers() {
    for (final timer in _sendAckTimers.values) {
      timer.cancel();
    }
    _sendAckTimers.clear();
  }

  void _resetSendAckTimeoutStreak() {
    _consecutiveSendAckTimeouts = 0;
  }

  void _triggerPullSyncThrottled({int? cursorOverride}) {
    final now = DateTime.now().millisecondsSinceEpoch;
    if (cursorOverride != null && cursorOverride >= 0) {
      if (_pendingPullSyncCursorFloor <= 0 ||
          cursorOverride < _pendingPullSyncCursorFloor) {
        _pendingPullSyncCursorFloor = cursorOverride;
      }
    }

    final remainingMs =
        ImService._pullSyncThrottleWindowMs - (now - _lastPullSyncRequestMs);
    if (remainingMs > 0) {
      _pullSyncThrottleTimer ??= Timer(
        Duration(milliseconds: remainingMs),
        _flushPendingPullSync,
      );
      return;
    }
    _pullSyncThrottleTimer?.cancel();
    _pullSyncThrottleTimer = null;
    _flushPendingPullSync();
  }

  /// Pull sync after LocalDb persist failure. Keeps the cursor floor, but
  /// backs off 2s → 5s → 15s → 30s while failures continue.
  void _triggerPullSyncAfterPersistFailure({int? cursorOverride}) {
    _pendingPersistFailPullSync = true;
    final now = DateTime.now().millisecondsSinceEpoch;
    if (cursorOverride != null && cursorOverride >= 0) {
      if (_pendingPullSyncCursorFloor <= 0 ||
          cursorOverride < _pendingPullSyncCursorFloor) {
        _pendingPullSyncCursorFloor = cursorOverride;
      }
    }

    final streakIndex = _persistFailPullSyncStreak.clamp(
      0,
      ImService._persistFailPullSyncBackoffMs.length - 1,
    );
    final windowMs = ImService._persistFailPullSyncBackoffMs[streakIndex];
    if (_lastPersistFailPullSyncScheduleMs <= 0) {
      _lastPersistFailPullSyncScheduleMs = now;
    }
    final remainingMs = windowMs - (now - _lastPersistFailPullSyncScheduleMs);
    if (remainingMs > 0) {
      final existing = _pullSyncThrottleTimer;
      if (existing == null || !existing.isActive) {
        _pullSyncThrottleTimer = Timer(
          Duration(milliseconds: remainingMs),
          _flushPendingPullSync,
        );
      }
      return;
    }
    _pullSyncThrottleTimer?.cancel();
    _pullSyncThrottleTimer = null;
    _flushPendingPullSync();
  }

  void _clearPersistFailPullSyncBackoff() {
    _persistFailPullSyncStreak = 0;
    _pendingPersistFailPullSync = false;
    _lastPersistFailPullSyncScheduleMs = 0;
  }

  void _flushPendingPullSync() {
    _pullSyncThrottleTimer?.cancel();
    _pullSyncThrottleTimer = null;

    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }

    final cursorOverride = _pendingPullSyncCursorFloor > 0
        ? _pendingPullSyncCursorFloor
        : null;
    _pendingPullSyncCursorFloor = 0;
    final wasPersistFail = _pendingPersistFailPullSync;
    _pendingPersistFailPullSync = false;
    if (wasPersistFail) {
      _lastPersistFailPullSyncScheduleMs =
          DateTime.now().millisecondsSinceEpoch;
      final next = _persistFailPullSyncStreak + 1;
      _persistFailPullSyncStreak = next.clamp(
        0,
        ImService._persistFailPullSyncBackoffMs.length - 1,
      );
    }
    _triggerPullSync(cursorOverride: cursorOverride);
  }

  Future<String?> _sendMessageImpl(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) async {
    final normalizedSessionId = sessionId.trim();
    if (normalizedSessionId.isEmpty) {
      return null;
    }

    _stopSessionComposing(normalizedSessionId, notifyRemote: true);
    final clientMsgId = const Uuid().v4();
    final tempMsg = MessageModel(
      msgId: 'temp_$clientMsgId',
      sessionId: normalizedSessionId,
      senderId: 'me',
      msgType: 1,
      content: content,
      extra: extra ?? const {},
      status: 'sending',
      clientMsgId: clientMsgId,
      quotedMessageId: quotedMessageId,
      visibleTo: visibleTo,
      createdAt: DateTime.now().millisecondsSinceEpoch,
    );

    try {
      final json = tempMsg.toJson();
      json['local_seq'] = clientMsgId;
      await LocalDb.insertLocalStub(json);
      if (updateCurrentSessionUi && _isCurrentSession(normalizedSessionId)) {
        LocalDbChangeBus.instance.emitMessageChange(
          LocalMessagesInserted(
            sessionId: normalizedSessionId,
            msgIds: [tempMsg.msgId],
            maxCreatedAt: tempMsg.createdAt,
            rows: [json],
          ),
        );
      }
    } catch (e) {
      debugPrint('Local stub error: $e');
    }

    try {
      final type = resolveSessionTypeById(normalizedSessionId);
      await _touchSession(
        normalizedSessionId,
        content,
        DateTime.now().millisecondsSinceEpoch,
        type: type,
        increaseUnread: false,
      );
    } catch (e) {
      debugPrint('Local DB update session error: $e');
    }

    _startSendAckTimer(clientMsgId, isDelegate: false);
    dispatchSendMessagePacket(
      sessionId: normalizedSessionId,
      clientMsgId: clientMsgId,
      content: content,
      extra: extra,
      quotedMessageId: quotedMessageId,
      visibleTo: visibleTo,
      delegateOrigin: false,
    );
    return clientMsgId;
  }

  void _delegateStartImpl(
    String sessionId,
    String agentId, {
    int? maxConsecutiveReplies,
  }) {
    if (_isConnected.value && _channel != null && _isAuthenticated.value) {
      final normalizedMax =
          maxConsecutiveReplies != null && maxConsecutiveReplies > 0
          ? maxConsecutiveReplies
          : ImService.defaultDelegateMaxConsecutiveReplies;
      final payload = <String, dynamic>{
        'session_id': sessionId,
        'agent_id': agentId,
        'max_consecutive_replies': normalizedMax,
      };
      final req = {
        'cmd': 'delegate_start',
        'seq': DateTime.now().millisecondsSinceEpoch,
        'payload': payload,
      };
      _sendPacket(req, requireAuthenticated: true);
    }
  }

  void _delegateStopImpl(String sessionId) {
    if (_isConnected.value && _channel != null && _isAuthenticated.value) {
      final req = {
        'cmd': 'delegate_stop',
        'seq': DateTime.now().millisecondsSinceEpoch,
        'payload': {'session_id': sessionId},
      };
      _sendPacket(req, requireAuthenticated: true);
    }
  }

  bool _stopAgentOutputImpl(String sessionId, {String? runId}) {
    if (!_isConnected.value || _channel == null || !_isAuthenticated.value) {
      return false;
    }
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return false;
    }
    final currentState = agentOutputStates[sid];
    final normalizedRunId = runId?.trim() ?? '';
    final seq = DateTime.now().millisecondsSinceEpoch;
    debugPrint(
      '📤 agent_output_stop request session=$sid '
      'run_id=${normalizedRunId.isEmpty ? "-" : normalizedRunId} '
      'seq=$seq current=${_describeAgentOutputStateForTrace(currentState)} '
      'ui=${_describeStreamingUiForTrace(sid)}',
    );
    final req = {
      'cmd': 'agent_output_stop',
      'seq': seq,
      'payload': {
        'session_id': sid,
        if (normalizedRunId.isNotEmpty) 'run_id': normalizedRunId,
      },
    };
    if (_sendPacket(req, requireAuthenticated: true)) {
      debugPrint(
        '✅ agent_output_stop request sent session=$sid '
        'run_id=${normalizedRunId.isEmpty ? "-" : normalizedRunId} seq=$seq',
      );
      _markAgentOutputStoppingLocally(sid, runId: normalizedRunId);
      return true;
    }
    debugPrint(
      '❌ agent_output_stop request not sent session=$sid '
      'run_id=${normalizedRunId.isEmpty ? "-" : normalizedRunId} '
      'connected=${_isConnected.value} authenticated=${_isAuthenticated.value} '
      'has_channel=${_channel != null} '
      'current=${_describeAgentOutputStateForTrace(currentState)} '
      'ui=${_describeStreamingUiForTrace(sid)}',
    );
    return false;
  }

  Future<void> _handleLocalInference(
    Map<String, dynamic> hint,
    String triggerMsgId,
  ) async {
    final sessionId = hint['session_id']?.toString() ?? '';
    final endpoint = hint['endpoint']?.toString() ?? '';
    final modelName = hint['model_name']?.toString() ?? '';
    final agentIdStr = hint['agent_id']?.toString() ?? '';
    final systemPrompt = hint['system_prompt']?.toString() ?? '';
    final hintedTriggerMsgId = hint['trigger_msg_id']?.toString().trim() ?? '';
    final effectiveTriggerMsgId = hintedTriggerMsgId.isNotEmpty
        ? hintedTriggerMsgId
        : triggerMsgId.trim();
    final quotedMessageId = effectiveTriggerMsgId;

    if (sessionId.isEmpty ||
        endpoint.isEmpty ||
        modelName.isEmpty ||
        agentIdStr.isEmpty ||
        effectiveTriggerMsgId.isEmpty) {
      debugPrint('local_inference: invalid hint $hint');
      return;
    }
    if (_localInferenceInFlight.contains(sessionId)) {
      debugPrint('local_inference: already in flight for session=$sessionId');
      return;
    }

    _localInferenceInFlight.add(sessionId);

    final messages = await _buildLocalInferenceMessages(
      hint,
      sessionId: sessionId,
      systemPrompt: systemPrompt,
    );

    final request = _PendingLocalInferenceStart(
      sessionId: sessionId,
      agentId: agentIdStr,
      triggerMsgId: effectiveTriggerMsgId,
      endpoint: endpoint,
      modelName: modelName,
      systemPrompt: systemPrompt,
      messages: messages,
      quotedMessageId: quotedMessageId,
    );
    _pendingLocalInferenceStarts[request.key] = request;
    _dispatchPendingLocalInferenceStart(request.key);
  }

  void _dispatchPendingLocalInferenceStart(String requestKey) {
    final request = _pendingLocalInferenceStarts[requestKey];
    if (request == null) {
      return;
    }
    if (request.attemptCount >= ImService._localStartAckMaxAttempts) {
      _failPendingLocalInferenceStart(
        requestKey,
        'relay_local_stream_start ack timeout',
      );
      return;
    }

    request.attemptCount += 1;
    final req = {
      'cmd': 'relay_local_stream_start',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {
        'session_id': request.sessionId,
        'agent_id': request.agentId,
        'trigger_msg_id': request.triggerMsgId,
      },
    };
    final sent = _sendPacket(req, requireAuthenticated: true);
    if (!sent && !_isConnected.value && _wsUrl != null) {
      _scheduleReconnect();
    }
    _pendingLocalInferenceStartTimers.remove(requestKey)?.cancel();
    _pendingLocalInferenceStartTimers[requestKey] = Timer(
      ImService._localStartAckTimeout,
      () {
        _pendingLocalInferenceStartTimers.remove(requestKey);
        _dispatchPendingLocalInferenceStart(requestKey);
      },
    );
  }

  void _handleRelayLocalStreamStartAck(Map<String, dynamic> payload) {
    final code = _toInt(payload['code']);
    final sessionId = payload['session_id']?.toString().trim() ?? '';
    final agentId = payload['agent_id']?.toString().trim() ?? '';
    final triggerMsgId = payload['trigger_msg_id']?.toString().trim() ?? '';
    final msgId = payload['msg_id']?.toString() ?? '';
    if (code != 200 || msgId.isEmpty) {
      debugPrint(
        'relay_local_stream_start_ack failed: '
        'session=$sessionId agent=$agentId trigger=$triggerMsgId '
        'code=$code msg=${payload['msg']}',
      );
      final requestKey = _localInferenceRequestKey(
        sessionId,
        agentId,
        triggerMsgId,
      );
      _failPendingLocalInferenceStart(
        requestKey,
        payload['msg']?.toString() ?? '',
      );
      return;
    }

    final request = _takePendingLocalInferenceStartForAck(
      sessionId: sessionId,
      agentId: agentId,
      triggerMsgId: triggerMsgId,
    );
    if (request == null) {
      final fallbackSessionId = _resolveRelayLocalStreamStartAckSessionId(
        sessionId,
      );
      if (fallbackSessionId.isNotEmpty) {
        final previousRenderMsgId =
            _localStreamRenderMsgIds[fallbackSessionId]?.trim() ?? '';
        _localStreamServerMsgIds[fallbackSessionId] = msgId;
        _localStreamRenderMsgIds[fallbackSessionId] = msgId;
        _remapLocalStreamRenderMessage(
          sessionId: fallbackSessionId,
          fromMsgId: previousRenderMsgId,
          toMsgId: msgId,
        );
        return;
      }
      debugPrint(
        'relay_local_stream_start_ack: '
        'no pending request session=$sessionId agent=$agentId '
        'trigger=$triggerMsgId msgId=$msgId',
      );
      return;
    }

    _localStreamServerMsgIds[request.sessionId] = msgId;
    _localStreamRenderMsgIds[request.sessionId] = msgId;
    if (request.quotedMessageId.isNotEmpty) {
      _localStreamQuotedMessageIds[request.sessionId] = request.quotedMessageId;
    }
    _localStreamChunkSeqs[request.sessionId] = 0;
    _localStreamPendingDeltas[request.sessionId] = StringBuffer();
    _startAcceptedLocalInference(request, msgId);
  }

  _PendingLocalInferenceStart? _takePendingLocalInferenceStartForAck({
    required String sessionId,
    required String agentId,
    required String triggerMsgId,
  }) {
    final exactKey = _localInferenceRequestKey(
      sessionId,
      agentId,
      triggerMsgId,
    );
    if (exactKey.isNotEmpty) {
      final exact = _pendingLocalInferenceStarts.remove(exactKey);
      _pendingLocalInferenceStartTimers.remove(exactKey)?.cancel();
      if (exact != null) {
        return exact;
      }
    }

    bool matches(String value, String candidate) {
      return value.isEmpty || candidate == value;
    }

    final candidates = _pendingLocalInferenceStarts.entries.where((entry) {
      final request = entry.value;
      return matches(sessionId, request.sessionId) &&
          matches(agentId, request.agentId) &&
          matches(triggerMsgId, request.triggerMsgId);
    }).toList();
    if (candidates.length != 1) {
      return null;
    }
    final matched = candidates.single;
    _pendingLocalInferenceStarts.remove(matched.key);
    _pendingLocalInferenceStartTimers.remove(matched.key)?.cancel();
    return matched.value;
  }

  String _resolveRelayLocalStreamStartAckSessionId(String sessionId) {
    final normalizedSessionId = sessionId.trim();
    if (normalizedSessionId.isNotEmpty &&
        _localStreamRenderMsgIds.containsKey(normalizedSessionId)) {
      return normalizedSessionId;
    }

    final candidates = _localStreamRenderMsgIds.entries
        .where(
          (entry) =>
              entry.value.trim().isNotEmpty &&
              _localInferenceInFlight.contains(entry.key),
        )
        .map((entry) => entry.key)
        .toList();
    if (candidates.length == 1) {
      return candidates.single;
    }
    return '';
  }

  void _remapLocalStreamRenderMessage({
    required String sessionId,
    required String fromMsgId,
    required String toMsgId,
  }) {
    final normalizedFromMsgId = fromMsgId.trim();
    final normalizedToMsgId = toMsgId.trim();
    if (normalizedFromMsgId.isEmpty ||
        normalizedToMsgId.isEmpty ||
        normalizedFromMsgId == normalizedToMsgId) {
      return;
    }

    final existing = _messageInCurrentWindowOrPlaceholder(normalizedFromMsgId);
    if (existing != null) {
      _removeUIMessage(normalizedFromMsgId);
      _upsertUIMessageInOrder(existing.copyWith(msgId: normalizedToMsgId));
    }

    if (_activeStreamingMsgIds.remove(normalizedFromMsgId)) {
      _clearStreamChunkGapTrackingForMessage(normalizedFromMsgId);
      _markStreamingActivity(normalizedToMsgId);
    }
    if (_locallyStoppedStreamMsgIds.remove(normalizedFromMsgId)) {
      _locallyStoppedStreamMsgIds.add(normalizedToMsgId);
    }

    final hiddenMessage = _hiddenAgentOutputMessages.remove(
      normalizedFromMsgId,
    );
    if (hiddenMessage != null) {
      _hiddenAgentOutputMessages[normalizedToMsgId] = hiddenMessage.copyWith(
        msgId: normalizedToMsgId,
      );
    }

    final existingState = agentOutputStates[sessionId];
    if (existingState != null &&
        existingState['stream_msg_id']?.toString().trim() ==
            normalizedFromMsgId) {
      final next = Map<String, dynamic>.from(existingState);
      next['stream_msg_id'] = normalizedToMsgId;
      agentOutputStates[sessionId] = next;
    }
    if (_pendingLocalStopStreamMsgIdBySession[sessionId] ==
        normalizedFromMsgId) {
      _pendingLocalStopStreamMsgIdBySession[sessionId] = normalizedToMsgId;
    }

    MessageStreamController.transfer(normalizedFromMsgId, normalizedToMsgId);
    _discardStreamingSessionPreview(normalizedFromMsgId);
    _stageStreamingSessionPreview(
      msgId: normalizedToMsgId,
      sessionId: sessionId,
      activityAt: DateTime.now().millisecondsSinceEpoch,
      isThinking: false,
    );
  }

  void _startAcceptedLocalInference(
    _PendingLocalInferenceStart request,
    String serverMsgId,
  ) {
    final sessionId = request.sessionId;
    final agentId = request.agentId;
    final quotedMessageId = request.quotedMessageId;

    unawaited(
      _localLlm.streamChat(
        sessionId: sessionId,
        endpoint: request.endpoint,
        model: request.modelName,
        messages: request.messages,
        onChunk: (chunk) {
          _ensureLocalStreamPlaceholderVisible(
            sessionId: sessionId,
            msgId: serverMsgId,
            agentId: agentId,
            quotedMessageId: quotedMessageId,
          );
          if (kDebugMode) {
            debugPrint(
              '🔵 local onChunk: sid=$sessionId msgId=$serverMsgId len=${chunk.length}',
            );
          }
          // 本地流 chunk 同样刷新活动时间，避免长本地推理被看门狗误判成僵尸流。
          _markStreamingActivity(serverMsgId);
          MessageStreamController.addChunk(serverMsgId, chunk);
          _stageStreamingSessionPreview(
            msgId: serverMsgId,
            sessionId: sessionId,
            activityAt: DateTime.now().millisecondsSinceEpoch,
            isThinking: false,
          );
          _enqueueRelayChunk(sessionId, chunk);
        },
        onFinish: (fullContent) {
          if (fullContent.isNotEmpty ||
              _currentMessageIds.contains(serverMsgId)) {
            _ensureLocalStreamPlaceholderVisible(
              sessionId: sessionId,
              msgId: serverMsgId,
              agentId: agentId,
              quotedMessageId: quotedMessageId,
            );
          }
          _activeStreamingMsgIds.remove(serverMsgId);
          _clearStreamChunkGapTrackingForMessage(serverMsgId);
          MessageStreamController.finish(serverMsgId, fullContent);
          unawaited(
            _touchSession(
              sessionId,
              fullContent,
              DateTime.now().millisecondsSinceEpoch,
              type: resolveSessionTypeById(sessionId),
              increaseUnread: false,
            ).whenComplete(() => _discardStreamingSessionPreview(serverMsgId)),
          );
          _finalizeLocalStreamRenderMessage(
            sessionId: sessionId,
            msgId: serverMsgId,
            agentId: agentId,
            finalContent: fullContent,
            quotedMessageId: quotedMessageId,
            status: 'success',
          );
          _localInferenceInFlight.remove(sessionId);

          _flushRelayChunks(sessionId);
          _sendRelayLocalStreamFinish(
            sessionId: sessionId,
            msgId: serverMsgId,
            finalContent: fullContent,
          );
          _cleanupLocalStreamState(sessionId);
        },
        onError: (error) {
          _ensureLocalStreamPlaceholderVisible(
            sessionId: sessionId,
            msgId: serverMsgId,
            agentId: agentId,
            quotedMessageId: quotedMessageId,
          );
          _activeStreamingMsgIds.remove(serverMsgId);
          _clearStreamChunkGapTrackingForMessage(serverMsgId);
          MessageStreamController.finish(
            serverMsgId,
            '[Local LLM Error] $error',
          );
          final errorPreview = '[Local LLM Error] $error';
          unawaited(
            _touchSession(
              sessionId,
              errorPreview,
              DateTime.now().millisecondsSinceEpoch,
              type: resolveSessionTypeById(sessionId),
              increaseUnread: false,
            ).whenComplete(() => _discardStreamingSessionPreview(serverMsgId)),
          );
          _finalizeLocalStreamRenderMessage(
            sessionId: sessionId,
            msgId: serverMsgId,
            agentId: agentId,
            finalContent: errorPreview,
            quotedMessageId: quotedMessageId,
            status: 'error',
          );
          _localInferenceInFlight.remove(sessionId);

          _flushRelayChunks(sessionId);
          _sendRelayLocalStreamFinish(
            sessionId: sessionId,
            msgId: serverMsgId,
            finalContent: '',
          );
          _cleanupLocalStreamState(sessionId);
          debugPrint('local_inference error session=$sessionId: $error');
        },
      ),
    );
  }

  String _localInferenceRequestKey(
    String sessionId,
    String agentId,
    String triggerMsgId,
  ) {
    final sid = sessionId.trim();
    final aid = agentId.trim();
    final tid = triggerMsgId.trim();
    if (sid.isEmpty || aid.isEmpty || tid.isEmpty) {
      return '';
    }
    return '$sid::$aid::$tid';
  }

  void _failPendingLocalInferenceStart(String requestKey, String reason) {
    final normalizedKey = requestKey.trim();
    if (normalizedKey.isEmpty) {
      return;
    }

    final request = _pendingLocalInferenceStarts.remove(normalizedKey);
    _pendingLocalInferenceStartTimers.remove(normalizedKey)?.cancel();
    if (request == null) {
      return;
    }

    _localInferenceInFlight.remove(request.sessionId);
    _cleanupLocalStreamState(request.sessionId);
    debugPrint(
      'local_inference start failed '
      'session=${request.sessionId} agent=${request.agentId} '
      'trigger=${request.triggerMsgId} reason=$reason',
    );
  }

  void _enqueueRelayChunk(String sessionId, String chunk) {
    final pending = _localStreamPendingDeltas[sessionId];
    if (pending == null) return;
    pending.write(chunk);

    _localStreamThrottleTimer ??= Timer(const Duration(milliseconds: 50), () {
      _localStreamThrottleTimer = null;
      for (final sid in _localStreamPendingDeltas.keys.toList()) {
        _flushRelayChunks(sid);
      }
    });
  }

  void _flushRelayChunks(String sessionId) {
    final pending = _localStreamPendingDeltas[sessionId];
    if (pending == null || pending.isEmpty) return;
    final delta = pending.toString();

    final seq = (_localStreamChunkSeqs[sessionId] ?? 0) + 1;

    final serverMsgId = _localStreamServerMsgIds[sessionId];
    if (serverMsgId == null || serverMsgId.isEmpty) {
      return;
    }
    final sent = _sendRelayLocalStreamChunk(
      sessionId: sessionId,
      msgId: serverMsgId,
      delta: delta,
      chunkSeq: seq,
    );
    if (!sent) {
      if (!_isConnected.value || !_isAuthenticated.value) {
        if (_wsUrl != null) {
          _scheduleReconnect();
        }
      }
      return;
    }
    pending.clear();
    _localStreamChunkSeqs[sessionId] = seq;
  }

  bool _sendRelayLocalStreamChunk({
    required String sessionId,
    required String msgId,
    required String delta,
    required int chunkSeq,
  }) {
    final req = {
      'cmd': 'relay_local_stream_chunk',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {
        'session_id': sessionId,
        'msg_id': msgId,
        'delta': delta,
        'chunk_seq': chunkSeq,
      },
    };
    return _sendPacket(req, requireAuthenticated: true);
  }

  bool _sendRelayLocalStreamFinish({
    required String sessionId,
    required String msgId,
    required String finalContent,
  }) {
    final req = {
      'cmd': 'relay_local_stream_finish',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {
        'session_id': sessionId,
        'msg_id': msgId,
        'final_content': finalContent,
      },
    };
    return _sendPacket(req, requireAuthenticated: true);
  }

  void _cleanupLocalStreamState(String sessionId) {
    _localStreamRenderMsgIds.remove(sessionId);
    _localStreamServerMsgIds.remove(sessionId);
    _localStreamQuotedMessageIds.remove(sessionId);
    _localStreamChunkSeqs.remove(sessionId);
    _localStreamPendingDeltas.remove(sessionId);
  }

  void _ensureLocalStreamPlaceholderVisible({
    required String sessionId,
    required String msgId,
    required String agentId,
    String? quotedMessageId,
  }) {
    if (!_isCurrentSession(sessionId) || msgId.isEmpty) {
      return;
    }
    _markStreamingActivity(msgId);
    final normalizedQuotedMessageId =
        (quotedMessageId?.trim().isNotEmpty ?? false)
        ? quotedMessageId!.trim()
        : (_localStreamQuotedMessageIds[sessionId]?.trim() ?? '');
    if (_currentMessageIds.contains(msgId)) {
      if (normalizedQuotedMessageId.isEmpty) {
        return;
      }
      final existingIndex = currentMessages.indexWhere((m) => m.msgId == msgId);
      if (existingIndex == -1) {
        return;
      }
      final existing = currentMessages[existingIndex];
      if ((existing.quotedMessageId?.trim() ?? '') ==
          normalizedQuotedMessageId) {
        return;
      }
      _upsertUIMessageInOrder(
        existing.copyWith(quotedMessageId: normalizedQuotedMessageId),
      );
      return;
    }
    _upsertUIMessageInOrder(
      MessageModel(
        msgId: msgId,
        sessionId: sessionId,
        senderId: agentId,
        senderType: 2,
        msgType: 4,
        content: '',
        quotedMessageId: normalizedQuotedMessageId.isEmpty
            ? null
            : normalizedQuotedMessageId,
        createdAt: DateTime.now().millisecondsSinceEpoch,
      ),
    );
  }

  void _finalizeLocalStreamRenderMessage({
    required String sessionId,
    required String msgId,
    required String agentId,
    required String finalContent,
    String? quotedMessageId,
    required String status,
  }) {
    if (!_isCurrentSession(sessionId) || msgId.isEmpty) {
      return;
    }

    final resolvedQuotedMessageId =
        (quotedMessageId?.trim().isNotEmpty ?? false)
        ? quotedMessageId!.trim()
        : (_localStreamQuotedMessageIds[sessionId]?.trim() ?? '');
    final existingIndex = currentMessages.indexWhere((m) => m.msgId == msgId);
    final existing = existingIndex == -1
        ? null
        : currentMessages[existingIndex];
    final finalized =
        existing?.copyWith(
          sessionId: sessionId,
          senderId: existing.senderId.trim().isNotEmpty
              ? existing.senderId
              : agentId,
          senderType: 2,
          msgType: 1,
          content: finalContent,
          status: status,
          quotedMessageId: resolvedQuotedMessageId.isEmpty
              ? null
              : resolvedQuotedMessageId,
        ) ??
        MessageModel(
          msgId: msgId,
          sessionId: sessionId,
          senderId: agentId,
          senderType: 2,
          msgType: 1,
          content: finalContent,
          createdAt: DateTime.now().millisecondsSinceEpoch,
          quotedMessageId: resolvedQuotedMessageId.isEmpty
              ? null
              : resolvedQuotedMessageId,
          status: status,
        );

    _upsertUIMessageInOrder(finalized);
  }

  Future<List<Map<String, String>>> _buildChatHistory(
    String sessionId,
    String systemPrompt,
  ) async {
    final history = <Map<String, String>>[];
    if (systemPrompt.isNotEmpty) {
      history.add({'role': 'system', 'content': systemPrompt});
    }
    final rows = await LocalDb.getRecentMessagesForSession(
      sessionId,
      limit: ImService._llmHistoryLimit,
    );
    for (final row in rows) {
      final msg = MessageModel.fromJson(row);
      if (msg.content.isEmpty) continue;
      if (msg.msgType == 4) continue;
      final role = msg.senderType == 2 ? 'assistant' : 'user';
      history.add({'role': role, 'content': msg.content});
    }
    return history;
  }

  Future<List<Map<String, String>>> _buildLocalInferenceMessages(
    Map<String, dynamic> hint, {
    required String sessionId,
    required String systemPrompt,
  }) async {
    final rawContextMessages = hint['context_messages'];
    if (rawContextMessages is! List || rawContextMessages.isEmpty) {
      return _buildChatHistory(sessionId, systemPrompt);
    }

    final history = <Map<String, String>>[];
    if (systemPrompt.isNotEmpty) {
      history.add({'role': 'system', 'content': systemPrompt});
    }
    for (final raw in rawContextMessages) {
      if (raw is! Map) {
        continue;
      }
      final item = Map<String, dynamic>.from(raw);
      final content = (item['content'] ?? '').toString().trim();
      if (content.isEmpty) {
        continue;
      }
      final senderType = _toInt(item['sender_type']);
      history.add({
        'role': senderType == 2 ? 'assistant' : 'user',
        'content': content,
      });
    }
    if (history.isEmpty) {
      return _buildChatHistory(sessionId, systemPrompt);
    }
    return history;
  }

  void _sendMessagePacket({
    required String sessionId,
    required String clientMsgId,
    required String content,
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    required bool delegateOrigin,
  }) {
    if (_isConnected.value && _channel != null && _isAuthenticated.value) {
      final payload = <String, dynamic>{
        'session_id': sessionId,
        'client_msg_id': clientMsgId,
        'msg_type': 1,
        'content': content,
      };

      if (quotedMessageId != null && quotedMessageId.isNotEmpty) {
        payload['quoted_message_id'] = quotedMessageId;
      }

      final mergedExtra = <String, dynamic>{};
      if (extra != null) {
        mergedExtra.addAll(extra);
      }
      final isGroupChat = resolveSessionTypeById(sessionId) == 'group';
      if (isGroupChat) {
        final mentionUserIds = _resolveMentionUserIds(
          mergedExtra['mention_user_ids'],
          content,
        );
        if (mentionUserIds.isNotEmpty) {
          mergedExtra['mention_user_ids'] = mentionUserIds;
        } else {
          mergedExtra.remove('mention_user_ids');
        }
      } else {
        mergedExtra.remove('mention_user_ids');
      }
      if (delegateOrigin) {
        mergedExtra['delegate_origin'] = true;
      }
      if (mergedExtra.isNotEmpty) {
        payload['extra'] = mergedExtra;
      }
      final normalizedVisibleTo = _normalizeVisibleToForWire(visibleTo);
      if (normalizedVisibleTo.isNotEmpty) {
        payload['visible_to'] = normalizedVisibleTo;
      }
      final req = {
        'cmd': 'send_msg',
        'seq': DateTime.now().millisecondsSinceEpoch,
        'payload': payload,
      };
      if (_sendPacket(req, requireAuthenticated: true)) {
        return;
      }
    }

    if (!_isConnected.value || !_isAuthenticated.value) {
      if (_wsUrl != null) {
        _scheduleReconnect();
      }
    }
  }

  List<String> _normalizeVisibleToForWire(List<String>? visibleTo) {
    if (visibleTo == null || visibleTo.isEmpty) {
      return const <String>[];
    }
    final normalized = <String>[];
    final seen = <String>{};
    for (final raw in visibleTo) {
      final trimmed = raw.trim();
      if (trimmed.isEmpty) continue;
      // 验证是合法数字字符串
      final parsed = int.tryParse(trimmed);
      if (parsed == null || parsed <= 0) {
        continue;
      }
      if (seen.add(trimmed)) {
        normalized.add(trimmed);
      }
    }
    return normalized;
  }

  void _retryMessagePacket({required String sessionId, required String msgId}) {
    final normalizedSessionId = sessionId.trim();
    final normalizedMsgId = msgId.trim();
    if (normalizedSessionId.isEmpty || normalizedMsgId.isEmpty) {
      return;
    }

    if (_isConnected.value && _channel != null && _isAuthenticated.value) {
      final req = {
        'cmd': 'retry_msg',
        'seq': DateTime.now().millisecondsSinceEpoch,
        'payload': {
          'session_id': normalizedSessionId,
          'msg_id': normalizedMsgId,
        },
      };
      if (_sendPacket(req, requireAuthenticated: true)) {
        return;
      }
    }

    if (!_isConnected.value || !_isAuthenticated.value) {
      if (_wsUrl != null) {
        _scheduleReconnect();
      }
    }
  }

  void _resendSendingMessagesInMemory() {
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }
    for (final msg in currentMessages) {
      final status = msg.status ?? '';
      final clientMsgId = msg.clientMsgId;
      if (!status.startsWith('sending') || clientMsgId == null) {
        continue;
      }
      final isDelegate =
          status.contains('delegate') || msg.extra['delegate_origin'] == true;
      if (isDelegate) {
        unawaited(
          LocalDb.updateMessageStatusByLocalSeq(
            clientMsgId,
            'failed_delegate',
          ).then((_) {
            LocalDbChangeBus.instance.emitMessageChange(
              LocalMessageUpdated(sessionId: msg.sessionId, msgId: msg.msgId),
            );
          }),
        );
        continue;
      }
      _startSendAckTimer(clientMsgId, isDelegate: isDelegate);
      _sendMessagePacket(
        sessionId: msg.sessionId,
        clientMsgId: clientMsgId,
        content: msg.content,
        extra: msg.extra,
        quotedMessageId: msg.quotedMessageId,
        visibleTo: msg.visibleTo,
        delegateOrigin: isDelegate,
      );
    }
  }

  Future<void> _resendPendingMessagesFromDb() async {
    if (_pendingResendInFlight) return;
    if (!_isConnected.value || !_isAuthenticated.value || _channel == null) {
      return;
    }

    _pendingResendInFlight = true;
    try {
      final pending = await LocalDb.getPendingOutboundMessages();
      for (final row in pending) {
        final clientMsgId = row['local_seq']?.toString() ?? '';
        final sessionId = row['session_id']?.toString() ?? '';
        final content = row['content']?.toString() ?? '';
        final status = row['status']?.toString() ?? '';
        final extra = _decodeExtraMap(row['extra']);
        if (clientMsgId.isEmpty || sessionId.isEmpty || content.isEmpty) {
          continue;
        }
        final isDelegate = status.contains('delegate');
        if (isDelegate) {
          await LocalDb.updateMessageStatusByLocalSeq(
            clientMsgId,
            'failed_delegate',
          );
          final mid = row['msg_id']?.toString().trim() ?? '';
          if (mid.isNotEmpty) {
            LocalDbChangeBus.instance.emitMessageChange(
              LocalMessageUpdated(sessionId: sessionId, msgId: mid),
            );
          }
          continue;
        }
        _startSendAckTimer(clientMsgId, isDelegate: isDelegate);
        dispatchSendMessagePacket(
          sessionId: sessionId,
          clientMsgId: clientMsgId,
          content: content,
          extra: extra,
          quotedMessageId: row['quoted_message_id']?.toString(),
          delegateOrigin: isDelegate,
        );
      }
    } finally {
      _pendingResendInFlight = false;
    }
  }

  Future<void> _retryMessageImpl(String? clientMsgId, {String? msgId}) async {
    final normalizedClientMsgId = clientMsgId?.trim() ?? '';
    if (normalizedClientMsgId.isNotEmpty) {
      final local = await LocalDb.getMessageByLocalSeq(normalizedClientMsgId);
      if (local != null) {
        final sessionId = local['session_id']?.toString() ?? '';
        final content = local['content']?.toString() ?? '';
        final status = local['status']?.toString() ?? '';
        final extra = _decodeExtraMap(local['extra']);
        final quotedMessageId = local['quoted_message_id']?.toString();
        if (sessionId.isEmpty || content.isEmpty) return;

        final isDelegate = status.contains('delegate');
        if (isDelegate) {
          await LocalDb.updateMessageStatusByLocalSeq(
            normalizedClientMsgId,
            'failed_delegate',
          );
          final mid = local['msg_id']?.toString().trim() ?? '';
          if (sessionId.isNotEmpty && mid.isNotEmpty) {
            LocalDbChangeBus.instance.emitMessageChange(
              LocalMessageUpdated(sessionId: sessionId, msgId: mid),
            );
          }
          return;
        }
        const nextStatus = 'sending';
        final newClientMsgId = const Uuid().v4();

        await LocalDb.deleteMessageByLocalSeq(normalizedClientMsgId);

        final newRow = Map<String, dynamic>.from(local);
        newRow['local_seq'] = newClientMsgId;
        newRow['msg_id'] = 'temp_$newClientMsgId';
        newRow['status'] = nextStatus;
        await LocalDb.insertLocalStub(newRow);

        // Remove old entry (structural window op), then emit bus event for the
        // new stub. The subscriber will insert it into the window.
        _removeUIMessage('temp_$normalizedClientMsgId');
        LocalDbChangeBus.instance.emitMessageChange(
          LocalMessagesInserted(
            sessionId: sessionId,
            msgIds: ['temp_$newClientMsgId'],
            maxCreatedAt: _toInt(newRow['created_at']),
            rows: [newRow],
          ),
        );

        _startSendAckTimer(newClientMsgId, isDelegate: isDelegate);
        dispatchSendMessagePacket(
          sessionId: sessionId,
          clientMsgId: newClientMsgId,
          content: content,
          extra: extra,
          quotedMessageId: quotedMessageId,
          delegateOrigin: isDelegate,
        );
        return;
      }
    }

    final normalizedMsgId = msgId?.trim() ?? '';
    if (normalizedMsgId.isEmpty) {
      return;
    }

    final idx = currentMessages.indexWhere((e) => e.msgId == normalizedMsgId);
    if (idx == -1) {
      return;
    }

    final message = currentMessages[idx];
    if (!isAgentDeliveryStatusError(message.agentDeliveryStatus)) {
      return;
    }

    final sessionId = message.sessionId.trim();
    if (sessionId.isEmpty) {
      return;
    }

    dispatchRetryMessagePacket(sessionId: sessionId, msgId: normalizedMsgId);
  }

  int _sendAgentFileListPacket({
    required String agentId,
    required String sessionId,
    String? parentId,
    bool showHidden = false,
    List<String>? allowedExtensions,
  }) {
    final seq = _nextActionSeq();
    final payload = <String, dynamic>{
      'agent_id': agentId,
      'session_id': sessionId,
      'show_hidden': showHidden,
      if (parentId != null) 'parent_id': parentId,
      if (allowedExtensions != null && allowedExtensions.isNotEmpty)
        'allowed_extensions': allowedExtensions,
    };
    debugPrint(
      '[file-list-diag] front -> outbound seq=$seq seqType=${seq.runtimeType} '
      'payload=$payload',
    );
    final sent = _sendPacket({
      'cmd': 'agent_file_list',
      'seq': seq,
      'payload': payload,
    }, requireAuthenticated: true);
    if (!sent) {
      debugPrint(
        '[file-list-diag] front !! _sendPacket returned false seq=$seq',
      );
    }
    return sent ? seq : 0;
  }

  int _sendConversationAuditPacket({
    required String command,
    required String sessionId,
    required String msgId,
    String? agentId,
    int? revision,
    String? cursor,
    int? limit,
    String? contentId,
    int? maxBytes,
  }) {
    final seq = _nextActionSeq();
    final payload = <String, dynamic>{
      'session_id': sessionId,
      'msg_id': msgId,
      if (agentId != null && agentId.isNotEmpty) 'agent_id': agentId,
      if (revision != null) 'revision': revision,
      if (cursor != null && cursor.isNotEmpty) 'cursor': cursor,
      if (limit != null) 'limit': limit,
      if (contentId != null && contentId.isNotEmpty) 'content_id': contentId,
      if (maxBytes != null) 'max_bytes': maxBytes,
    };
    final sent = _sendPacket({
      'cmd': command,
      'seq': seq,
      'payload': payload,
    }, requireAuthenticated: true);
    return sent ? seq : 0;
  }

  int _sendAgentConnectorAdminPacket({
    required String agentId,
    required String op,
    Map<String, dynamic>? args,
  }) {
    final seq = _nextActionSeq();
    final sent = _sendPacket({
      'cmd': 'agent_connector_admin',
      'seq': seq,
      'payload': {
        'agent_id': agentId,
        'op': op,
        if (args != null && args.isNotEmpty) 'args': args,
      },
    }, requireAuthenticated: true);
    return sent ? seq : 0;
  }

  int _sendAgentSkillUploadPacket({
    required String agentId,
    required String sessionId,
    required String name,
  }) {
    final seq = _nextActionSeq();
    final sent = _sendPacket({
      'cmd': 'agent_skill_upload',
      'seq': seq,
      'payload': {'agent_id': agentId, 'session_id': sessionId, 'name': name},
    }, requireAuthenticated: true);
    return sent ? seq : 0;
  }

  int _sendAgentSkillDeletePacket({
    required String agentId,
    required String sessionId,
    required String name,
  }) {
    final seq = _nextActionSeq();
    final sent = _sendPacket({
      'cmd': 'agent_skill_delete',
      'seq': seq,
      'payload': {'agent_id': agentId, 'session_id': sessionId, 'name': name},
    }, requireAuthenticated: true);
    return sent ? seq : 0;
  }

  int _sendAgentSkillEnablePacket({
    required String agentId,
    required String sessionId,
    required String name,
    required String scope,
    String? force,
  }) {
    final seq = _nextActionSeq();
    final sent = _sendPacket({
      'cmd': 'agent_skill_enable',
      'seq': seq,
      'payload': {
        'agent_id': agentId,
        'session_id': sessionId,
        'name': name,
        'scope': scope,
        if (force != null && force.isNotEmpty) 'force': force,
      },
    }, requireAuthenticated: true);
    return sent ? seq : 0;
  }

  int _sendAgentSkillDisablePacket({
    required String agentId,
    required String sessionId,
    required String name,
    required String scope,
  }) {
    final seq = _nextActionSeq();
    final sent = _sendPacket({
      'cmd': 'agent_skill_disable',
      'seq': seq,
      'payload': {
        'agent_id': agentId,
        'session_id': sessionId,
        'name': name,
        'scope': scope,
      },
    }, requireAuthenticated: true);
    return sent ? seq : 0;
  }

  int _sendAgentSkillRefreshPacket({
    required String agentId,
    required String sessionId,
  }) {
    final seq = _nextActionSeq();
    final sent = _sendPacket({
      'cmd': 'agent_skill_refresh',
      'seq': seq,
      'payload': {'agent_id': agentId, 'session_id': sessionId},
    }, requireAuthenticated: true);
    return sent ? seq : 0;
  }

  int _sendAgentCreateFolderPacket({
    required String agentId,
    required String sessionId,
    String? parentId,
    required String name,
  }) {
    final seq = _nextActionSeq();
    final sent = _sendPacket({
      'cmd': 'agent_create_folder',
      'seq': seq,
      'payload': {
        'agent_id': agentId,
        'session_id': sessionId,
        'name': name,
        if (parentId != null) 'parent_id': parentId,
      },
    }, requireAuthenticated: true);
    return sent ? seq : 0;
  }

  int _sendAgentSessionBindingsListPacket({
    required String agentId,
    required String sessionId,
  }) {
    final seq = _nextActionSeq();
    final sent = _sendPacket({
      'cmd': 'agent_session_bindings_list',
      'seq': seq,
      'payload': {'agent_id': agentId, 'session_id': sessionId},
    }, requireAuthenticated: true);
    return sent ? seq : 0;
  }

  int _sendAgentSessionBindPacket({
    required String agentId,
    required String sessionId,
    required String cwd,
    String agentSessionId = '',
    String title = '',
  }) {
    final seq = _nextActionSeq();
    final payload = <String, dynamic>{
      'agent_id': agentId,
      'session_id': sessionId,
      'cwd': cwd,
    };
    final normalizedAgentSessionId = agentSessionId.trim();
    if (normalizedAgentSessionId.isNotEmpty) {
      payload['agent_session_id'] = normalizedAgentSessionId;
    }
    final normalizedTitle = title.trim();
    if (normalizedTitle.isNotEmpty) {
      payload['title'] = normalizedTitle;
    }
    final sent = _sendPacket({
      'cmd': 'agent_session_bind',
      'seq': seq,
      'payload': payload,
    }, requireAuthenticated: true);
    return sent ? seq : 0;
  }

  int _nextActionSeq() {
    final now = DateTime.now().microsecondsSinceEpoch;
    if (_nextLocalActionSeq < now) {
      _nextLocalActionSeq = now;
    } else {
      _nextLocalActionSeq += 1;
    }
    return _nextLocalActionSeq;
  }
}
