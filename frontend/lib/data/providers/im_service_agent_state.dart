part of 'im_service.dart';

extension _ImServiceAgentState on ImService {
  String _mapSendNackMessageImpl(String message) {
    switch (message.trim()) {
      case 'message too large':
        return '输入内容过长，请精简后再发送';
      case 'send too fast':
        return '发送过于频繁，请稍后再试';
      case 'duplicate content detected':
        return '请勿重复发送相同内容';
      case 'message content rejected':
        return '输入内容异常，请调整后再发送';
      case 'member is muted':
        return 'chat_send_blocked_member_muted'.tr;
      case 'group is muted':
        return 'chat_send_blocked_all_members_muted'.tr;
      default:
        return message;
    }
  }

  bool _isDelegateOriginExtra(Map<String, dynamic> extra) {
    return extra['delegate_origin'] == true;
  }

  void _markDelegateChannelHealthy(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final existing = delegateStates[sid];
    if (existing == null || existing['channel_unavailable'] != true) {
      return;
    }
    final next = Map<String, dynamic>.from(existing);
    next['channel_unavailable'] = false;
    next.remove('last_error_code');
    delegateStates[sid] = next;
  }

  void _requestAgentOutputSnapshot(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    _sendPacket({
      'cmd': 'agent_output_get',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'session_id': sid},
    }, requireAuthenticated: true);
  }

  void _applyAgentOutputSnapshot(Map<String, dynamic> payload) {
    final sid = payload['session_id']?.toString().trim() ?? '';
    if (sid.isEmpty) {
      return;
    }

    final resolvedAt = _toInt(payload['resolved_at']);
    final active = payload['active'] == true;
    final rawStatus = payload['status'];
    if (active && rawStatus is Map) {
      _handleAgentOutputStatus(Map<String, dynamic>.from(rawStatus));
      return;
    }

    final existing = agentOutputStates[sid];
    if (existing == null) {
      return;
    }
    // The server reports active=false. Preserve the local capsule only when we
    // can tell it was updated after the server-side run had already resolved;
    // otherwise the empty snapshot should clear the stale state.
    final existingUpdatedAt = _toInt(existing['updated_at']);
    if (resolvedAt > 0 &&
        existingUpdatedAt > 0 &&
        existingUpdatedAt > resolvedAt) {
      return;
    }
    final existingAgentId = existing['agent_id']?.toString().trim() ?? '';
    if (existingAgentId.isNotEmpty) {
      _markSessionComposingResolvedForParticipant(
        sid,
        participantId: existingAgentId,
        participantType: 'agent',
        resolvedAt: resolvedAt,
      );
    }
    if (!_hasActiveLocalStreamForSession(sid)) {
      _clearActiveStreamingStateForSession(sid);
    }
    agentOutputStates.remove(sid);
  }

  /// Legacy handler for agent_delivery_error (pre-Phase-3.1 backends).
  /// Kept for backward compatibility until all backend instances are updated.
  void _handleAgentDeliveryError(Map<String, dynamic> payload) {
    final sessionId = payload['session_id']?.toString().trim() ?? '';
    final scope = payload['scope']?.toString().trim() ?? '';
    final code = payload['code']?.toString().trim() ?? '';
    lastAgentDeliveryError.value = Map<String, dynamic>.from(payload);
    if (code == 'agent_api_channel_unavailable' &&
        scope == 'direct' &&
        (sessionId.isEmpty || !hasStreamingAgentOutputForSession(sessionId))) {
      CustomToast.show('chat_agent_offline_reminder'.tr);
    }
    if (code == 'agent_api_channel_unavailable' &&
        scope == 'delegate' &&
        sessionId.isNotEmpty) {
      final existing = delegateStates[sessionId];
      if (existing == null) return;
      final next = Map<String, dynamic>.from(existing);
      next['channel_unavailable'] = true;
      next['last_error_code'] = code;
      delegateStates[sessionId] = next;
    }
  }

  Future<void> _handleAgentDeliveryStatus(Map<String, dynamic> payload) async {
    final msgId = payload['trigger_msg_id']?.toString().trim() ?? '';
    final status = payload['status']?.toString().trim() ?? '';
    if (msgId.isEmpty || status.isEmpty) {
      return;
    }
    if (!_acceptAgentDeliveryOrder(msgId, payload)) {
      return;
    }

    // Grace period for timeout: the backend ack timer may expire before the
    // agent can respond, but the agent could still be processing.  Defer the
    // status update so the user doesn't see a spurious "Processing timeout"
    // flash that disappears once streaming output arrives.
    if (status == 'timeout') {
      final sessionId = payload['session_id']?.toString().trim() ?? '';
      if (sessionId.isNotEmpty &&
          hasStreamingAgentOutputForSession(sessionId)) {
        debugPrint(
          '⏭️ agent_delivery_status timeout discarded '
          '(streaming active) msg_id=$msgId session=$sessionId',
        );
        return;
      }
      if (sessionId.isNotEmpty) {
        final existing = agentOutputStates[sessionId];
        if (existing != null) {
          final existingTriggerMsgId =
              existing['trigger_msg_id']?.toString().trim() ?? '';
          if (existingTriggerMsgId.isEmpty || existingTriggerMsgId == msgId) {
            _scheduleDeliveryTimeoutGracePeriod(msgId, payload);
            return;
          }
        }
      }
    }

    await _applyAgentDeliveryStatus(msgId, status, payload);
  }

  /// Bulk replay path used on auth/reauth. Groups all eligible items into a
  /// single IndexedDB transaction so 256 stored statuses don't serialize into
  /// a 15-second downstream queue on Web.  Per-item timeout grace-period and
  /// streaming-active checks still apply; items deferred to the grace timer
  /// keep their existing single-write behavior (acceptable: rare).
  Future<void> _handleAgentDeliveryStatusBatch(
    Map<String, dynamic> payload,
  ) async {
    final rawItems = payload['items'];
    if (rawItems is! List || rawItems.isEmpty) return;

    final toApply = <MapEntry<String, String>>[];
    final byMsgId = <String, Map<String, dynamic>>{};
    for (final raw in rawItems) {
      if (raw is! Map) continue;
      final item = Map<String, dynamic>.from(raw);
      final msgId = item['trigger_msg_id']?.toString().trim() ?? '';
      final status = item['status']?.toString().trim() ?? '';
      if (msgId.isEmpty || status.isEmpty) continue;
      if (!_acceptAgentDeliveryOrder(msgId, item)) continue;

      if (status == 'timeout') {
        final sessionId = item['session_id']?.toString().trim() ?? '';
        if (sessionId.isNotEmpty &&
            hasStreamingAgentOutputForSession(sessionId)) {
          debugPrint(
            '⏭️ agent_delivery_status_batch timeout discarded '
            '(streaming active) msg_id=$msgId session=$sessionId',
          );
          continue;
        }
        if (sessionId.isNotEmpty) {
          final existing = agentOutputStates[sessionId];
          if (existing != null) {
            final existingTriggerMsgId =
                existing['trigger_msg_id']?.toString().trim() ?? '';
            if (existingTriggerMsgId.isEmpty || existingTriggerMsgId == msgId) {
              _scheduleDeliveryTimeoutGracePeriod(msgId, item);
              continue;
            }
          }
        }
      }

      // Backend stores statuses in a Redis HASH keyed by msgId, so the list
      // is already deduped.  Defensive guard below keeps behavior correct if
      // that ever changes: skip earlier occurrence when same msgId appears.
      if (byMsgId.containsKey(msgId)) {
        final int priorIdx = toApply.indexWhere((e) => e.key == msgId);
        if (priorIdx >= 0) toApply.removeAt(priorIdx);
      }
      byMsgId[msgId] = item;
      toApply.add(MapEntry(msgId, status));
    }

    if (toApply.isEmpty) return;

    await _guardDbOp(
      LocalDb.updateAgentDeliveryStatusBatch(toApply),
      op: 'updateAgentDeliveryStatusBatch',
    );

    // Apply in-memory updates after the DB transaction commits.
    // Build an index by msgId for O(1) lookup instead of O(n) per item.
    final msgIndex = <String, int>{};
    for (var i = 0; i < currentMessages.length; i++) {
      msgIndex[currentMessages[i].msgId] = i;
    }
    for (final entry in toApply) {
      final msgId = entry.key;
      final status = entry.value;
      final item = byMsgId[msgId]!;
      final idx = msgIndex[msgId];
      if (idx != null) {
        currentMessages[idx] = currentMessages[idx].copyWith(
          agentDeliveryStatus: status,
        );
      }
      if (_isAgentDeliveryStatusErrorImpl(status)) {
        final sessionId = item['session_id']?.toString().trim() ?? '';
        if (sessionId.isNotEmpty) {
          final existing = agentOutputStates[sessionId];
          if (existing != null) {
            final existingTriggerMsgId =
                existing['trigger_msg_id']?.toString().trim() ?? '';
            if (existingTriggerMsgId.isEmpty || existingTriggerMsgId == msgId) {
              agentOutputStates.remove(sessionId);
            }
          }
        }
      }
    }
    _normalizeCurrentMessageOrder();
  }

  Future<void> _applyAgentDeliveryStatus(
    String msgId,
    String status,
    Map<String, dynamic> payload,
  ) async {
    // Encode channel_unavailable into status for per-message bubble display.
    final code = payload['code']?.toString().trim() ?? '';
    final scope = payload['scope']?.toString().trim() ?? '';
    final effectiveStatus =
        (status == 'failed' && code == 'agent_api_channel_unavailable')
        ? 'failed:channel_unavailable'
        : status;

    await _guardDbOp(
      LocalDb.updateAgentDeliveryStatusByMsgId(msgId, effectiveStatus),
      op: 'updateAgentDeliveryStatusByMsgId(agent_delivery_status)',
    );

    final idx = currentMessages.indexWhere((e) => e.msgId == msgId);
    if (idx != -1) {
      currentMessages[idx] = currentMessages[idx].copyWith(
        agentDeliveryStatus: effectiveStatus,
      );
      _normalizeCurrentMessageOrder();
    }

    if (_isAgentDeliveryStatusErrorImpl(effectiveStatus)) {
      final sessionId = payload['session_id']?.toString().trim() ?? '';
      // Show offline reminder toast for direct-scope channel unavailable errors,
      // unless the agent is actively streaming a response (clearly online).
      if (status == 'failed' &&
          code == 'agent_api_channel_unavailable' &&
          scope == 'direct' &&
          (sessionId.isEmpty ||
              !hasStreamingAgentOutputForSession(sessionId))) {
        CustomToast.show('chat_agent_offline_reminder'.tr);
      }
      if (sessionId.isNotEmpty) {
        final existing = agentOutputStates[sessionId];
        if (existing != null) {
          final existingTriggerMsgId =
              existing['trigger_msg_id']?.toString().trim() ?? '';
          if (existingTriggerMsgId.isEmpty || existingTriggerMsgId == msgId) {
            agentOutputStates.remove(sessionId);
          }
        }
      }
    }
  }

  void _scheduleDeliveryTimeoutGracePeriod(
    String msgId,
    Map<String, dynamic> payload,
  ) {
    _pendingDeliveryTimeoutTimers[msgId]?.timer?.cancel();

    final sessionId = payload['session_id']?.toString().trim() ?? '';
    debugPrint(
      '⏳ agent_delivery_status timeout deferred (grace period) '
      'msg_id=$msgId session=$sessionId',
    );

    final entry = _DeliveryTimeoutGracePeriod(
      sessionId: sessionId,
      payload: payload,
    );
    entry.timer = Timer(
      const Duration(milliseconds: ImService._deliveryTimeoutGracePeriodMs),
      () {
        _pendingDeliveryTimeoutTimers.remove(msgId);
        _onDeliveryTimeoutGracePeriodExpired(msgId, payload);
      },
    );
    _pendingDeliveryTimeoutTimers[msgId] = entry;
  }

  void _onDeliveryTimeoutGracePeriodExpired(
    String msgId,
    Map<String, dynamic> payload,
  ) {
    final sessionId = payload['session_id']?.toString().trim() ?? '';

    if (sessionId.isNotEmpty && hasStreamingAgentOutputForSession(sessionId)) {
      debugPrint(
        '⏭️ agent_delivery_status timeout discarded after grace period '
        '(streaming active) msg_id=$msgId session=$sessionId',
      );
      return;
    }

    debugPrint(
      '⏰ agent_delivery_status timeout applied after grace period '
      'msg_id=$msgId session=$sessionId',
    );
    _applyAgentDeliveryStatus(msgId, 'timeout', payload);
  }

  bool _acceptAgentDeliveryOrder(String msgId, Map<String, dynamic> payload) {
    final generation = _toInt(payload['dispatch_generation']);
    final revision = _toInt(payload['revision']);
    final current = _agentDeliveryOrderByMsgId[msgId];
    if (current != null) {
      final currentGeneration = current['generation'] ?? 0;
      final currentRevision = current['revision'] ?? 0;
      if (currentGeneration > 0 &&
          (generation <= 0 || generation < currentGeneration)) {
        return false;
      }
      if (generation == currentGeneration &&
          currentRevision > 0 &&
          revision > 0 &&
          revision <= currentRevision) {
        return false;
      }
    }
    _agentDeliveryOrderByMsgId[msgId] = {
      'generation': generation,
      'revision': revision,
    };
    while (_agentDeliveryOrderByMsgId.length >
        ImService._agentDeliveryOrderCacheLimit) {
      _agentDeliveryOrderByMsgId.remove(_agentDeliveryOrderByMsgId.keys.first);
    }
    return true;
  }

  void _cancelDeliveryTimeoutGracePeriodForSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final keysToRemove = <String>[];
    for (final entry in _pendingDeliveryTimeoutTimers.entries) {
      if (entry.value.sessionId == sid) {
        entry.value.timer?.cancel();
        keysToRemove.add(entry.key);
      }
    }
    for (final key in keysToRemove) {
      _pendingDeliveryTimeoutTimers.remove(key);
      debugPrint(
        '⏭️ agent_delivery_status timeout cancelled (agent active) '
        'msg_id=$key session=$sid',
      );
    }
  }

  bool _isTerminalAgentOutputState(String state) {
    switch (state.trim()) {
      case 'stopped':
      case 'completed':
      case 'failed':
        return true;
      default:
        return false;
    }
  }

  String _describeAgentOutputStateForTrace(Map<String, dynamic>? state) {
    if (state == null) {
      return 'state=- run_id=- stream_msg_id=- can_stop=false updated_at=0';
    }
    final runId = state['run_id']?.toString().trim() ?? '';
    final streamMsgId = state['stream_msg_id']?.toString().trim() ?? '';
    final currentState = state['state']?.toString().trim() ?? '';
    return 'state=${currentState.isEmpty ? "-" : currentState} '
        'run_id=${runId.isEmpty ? "-" : runId} '
        'stream_msg_id=${streamMsgId.isEmpty ? "-" : streamMsgId} '
        'can_stop=${state['can_stop'] == true} '
        'updated_at=${_toInt(state['updated_at'])}';
  }

  String _describeStreamingUiForTrace(String sessionId, {String? focusMsgId}) {
    final sid = sessionId.trim();
    final normalizedFocusMsgId = focusMsgId?.trim() ?? '';
    if (sid.isEmpty) {
      return 'ui_current=- ui_placeholder=- focus_in_current=false '
          'focus_in_placeholder=false focus_active=false';
    }

    List<String> collectIds(Iterable<MessageModel> items) {
      final ids = <String>[];
      for (final item in items) {
        final msgId = item.msgId.trim();
        if (msgId.isEmpty || ids.contains(msgId)) {
          continue;
        }
        ids.add(msgId);
      }
      if (ids.length <= 3) {
        return ids;
      }
      return ids.sublist(ids.length - 3);
    }

    final currentIds = collectIds(
      currentMessages.where(
        (msg) =>
            msg.sessionId == sid && msg.msgType == 4 && msg.msgId.isNotEmpty,
      ),
    );
    final placeholderIds = collectIds(
      _streamingPlaceholders.values.where(
        (msg) =>
            msg.sessionId == sid && msg.msgType == 4 && msg.msgId.isNotEmpty,
      ),
    );

    final focusInCurrent =
        normalizedFocusMsgId.isNotEmpty &&
        currentIds.contains(normalizedFocusMsgId);
    final focusInPlaceholder =
        normalizedFocusMsgId.isNotEmpty &&
        placeholderIds.contains(normalizedFocusMsgId);
    final focusActive =
        normalizedFocusMsgId.isNotEmpty &&
        _activeStreamingMsgIds.contains(normalizedFocusMsgId);

    final currentSummary = currentIds.isEmpty ? '-' : currentIds.join(',');
    final placeholderSummary = placeholderIds.isEmpty
        ? '-'
        : placeholderIds.join(',');
    return 'ui_current=$currentSummary '
        'ui_placeholder=$placeholderSummary '
        'focus_in_current=$focusInCurrent '
        'focus_in_placeholder=$focusInPlaceholder '
        'focus_active=$focusActive';
  }

  void _markAgentOutputStoppingLocally(String sessionId, {String? runId}) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final existing = agentOutputStates[sid];
    if (existing == null) {
      debugPrint(
        '⚠️ agent_output_stop local mark skipped session=$sid reason=no_active_state',
      );
      return;
    }
    final normalizedRunId = runId?.trim() ?? '';
    final existingRunId = existing['run_id']?.toString().trim() ?? '';
    if (normalizedRunId.isNotEmpty &&
        existingRunId.isNotEmpty &&
        normalizedRunId != existingRunId) {
      debugPrint(
        '⚠️ agent_output_stop local mark skipped session=$sid '
        'reason=run_mismatch requested_run_id=$normalizedRunId '
        'existing=${_describeAgentOutputStateForTrace(existing)}',
      );
      return;
    }
    final streamMsgId = existing['stream_msg_id']?.toString().trim() ?? '';
    if (streamMsgId.isEmpty) {
      debugPrint(
        '⚠️ agent_output_stop local mark skipped session=$sid '
        'reason=no_stream_msg_id existing=${_describeAgentOutputStateForTrace(existing)} '
        'ui=${_describeStreamingUiForTrace(sid)}',
      );
      return;
    }
    final hiddenMessage = _messageInCurrentWindowOrPlaceholder(streamMsgId);
    if (hiddenMessage != null) {
      _hiddenAgentOutputMessages[streamMsgId] = hiddenMessage;
    }
    _pendingLocalStopStateBySession[sid] = Map<String, dynamic>.from(existing);
    _pendingLocalStopStreamMsgIdBySession[sid] = streamMsgId;
    _locallyStoppedStreamMsgIds.add(streamMsgId);
    _activeStreamingMsgIds.remove(streamMsgId);
    _discardStreamingSessionPreview(streamMsgId);
    _clearStreamChunkGapTrackingForMessage(streamMsgId);
    _removeUIMessage(streamMsgId);
    final next = Map<String, dynamic>.from(existing);
    next['state'] = 'stopping';
    next['can_stop'] = false;
    agentOutputStates[sid] = next;
    debugPrint(
      '👁️ agent_output_stop local mark applied session=$sid '
      'run_id=${existingRunId.isEmpty ? "-" : existingRunId} '
      'stream_msg_id=$streamMsgId hidden=${hiddenMessage != null}',
    );
  }

  bool _stopStreamingMessageLocallyImpl(String sessionId, String msgId) {
    final sid = sessionId.trim();
    final normalizedMsgId = msgId.trim();
    if (sid.isEmpty || normalizedMsgId.isEmpty) {
      return false;
    }
    final hiddenMessage = _messageInCurrentWindowOrPlaceholder(normalizedMsgId);
    final tracked =
        _activeStreamingMsgIds.contains(normalizedMsgId) ||
        _streamingPlaceholders.containsKey(normalizedMsgId) ||
        currentMessages.any(
          (m) => m.msgId == normalizedMsgId && m.msgType == 4,
        );
    if (!tracked && hiddenMessage == null) {
      return false;
    }
    if (hiddenMessage != null) {
      _hiddenAgentOutputMessages[normalizedMsgId] = hiddenMessage;
    }
    _locallyStoppedStreamMsgIds.add(normalizedMsgId);
    _activeStreamingMsgIds.remove(normalizedMsgId);
    _discardStreamingSessionPreview(normalizedMsgId);
    _clearStreamChunkGapTrackingForMessage(normalizedMsgId);
    MessageStreamController.discard(normalizedMsgId);
    _removeUIMessage(normalizedMsgId);

    final existing = agentOutputStates[sid];
    if (existing != null) {
      final next = Map<String, dynamic>.from(existing);
      next['state'] = 'stopping';
      next['can_stop'] = false;
      next['stream_msg_id'] = normalizedMsgId;
      agentOutputStates[sid] = next;
    }
    debugPrint(
      '👁️ agent_output_stop local fallback applied session=$sid '
      'stream_msg_id=$normalizedMsgId hidden=${hiddenMessage != null}',
    );
    return true;
  }

  void _clearAgentOutputStateForStreamMessage({
    required String sessionId,
    required String msgId,
    int finalizedCreatedAt = 0,
    bool allowDetachedStateFallback = false,
  }) {
    final sid = sessionId.trim();
    final normalizedMsgId = msgId.trim();
    if (sid.isEmpty || normalizedMsgId.isEmpty) {
      return;
    }

    final existing = agentOutputStates[sid];
    if (existing == null) {
      return;
    }

    final existingUpdatedAt = _toInt(existing['updated_at']);
    final streamMsgId = existing['stream_msg_id']?.toString().trim() ?? '';
    if (streamMsgId.isNotEmpty) {
      if (streamMsgId == normalizedMsgId) {
        agentOutputStates.remove(sid);
        return;
      }
      if (finalizedCreatedAt > 0 &&
          existingUpdatedAt > 0 &&
          existingUpdatedAt > finalizedCreatedAt) {
        return;
      }
      if (finalizedCreatedAt <= 0 ||
          _activeStreamingMsgIds.contains(streamMsgId)) {
        return;
      }
      // A different stream just finalized and the tracked stream marker is no
      // longer active, so the capsule belongs to an older run and can be
      // cleared.
      agentOutputStates.remove(sid);
      return;
    }

    final state = existing['state']?.toString().trim() ?? '';
    if (state == 'stopping') {
      agentOutputStates.remove(sid);
      return;
    }
    if (!allowDetachedStateFallback) {
      return;
    }
    if (hasStreamingAgentOutputForSession(sid)) {
      return;
    }
    if (finalizedCreatedAt > 0 &&
        existingUpdatedAt > 0 &&
        existingUpdatedAt > finalizedCreatedAt) {
      return;
    }
    agentOutputStates.remove(sid);
  }

  void _handleAgentOutputStopAck(Map<String, dynamic> payload) {
    final sessionId = payload['session_id']?.toString().trim() ?? '';
    final accepted = payload['accepted'] == true;
    final msg = payload['msg']?.toString().trim() ?? '';
    final runId = payload['run_id']?.toString().trim() ?? '';
    final updatedAt = _toInt(payload['updated_at']);
    debugPrint(
      '📥 agent_output_stop_ack session=${sessionId.isEmpty ? "-" : sessionId} '
      'run_id=${runId.isEmpty ? "-" : runId} accepted=$accepted '
      'msg=${msg.isEmpty ? "-" : msg} updated_at=$updatedAt',
    );

    if (!accepted) {
      CustomToast.show(
        msg.isNotEmpty ? msg : 'chat_agent_output_stop_failed'.tr,
      );
      if (sessionId.isEmpty) {
        return;
      }
      final previousState = _pendingLocalStopStateBySession.remove(sessionId);
      final streamMsgId =
          _pendingLocalStopStreamMsgIdBySession.remove(sessionId) ??
          previousState?['stream_msg_id']?.toString().trim() ??
          agentOutputStates[sessionId]?['stream_msg_id']?.toString().trim() ??
          '';
      if (streamMsgId.isEmpty) {
        return;
      }
      _locallyStoppedStreamMsgIds.remove(streamMsgId);
      final hiddenMessage = _hiddenAgentOutputMessages.remove(streamMsgId);
      if (hiddenMessage != null) {
        _upsertUIMessageInOrder(hiddenMessage);
        if (hiddenMessage.msgType == 4) {
          _markStreamingActivity(streamMsgId);
          _stageStreamingSessionPreview(
            msgId: streamMsgId,
            sessionId: sessionId,
            activityAt: hiddenMessage.createdAt,
            isThinking: hiddenMessage.isThinking,
          );
        }
      }
      if (previousState != null &&
          hiddenMessage != null &&
          hiddenMessage.msgType == 4) {
        agentOutputStates[sessionId] = previousState;
      } else {
        agentOutputStates.remove(sessionId);
      }
      debugPrint(
        '↩️ agent_output_stop_ack restore session=$sessionId '
        'stream_msg_id=$streamMsgId restored=${hiddenMessage != null} '
        'previous=${_describeAgentOutputStateForTrace(previousState)}',
      );
      return;
    }

    if (sessionId.isEmpty) {
      return;
    }
    _pendingLocalStopStateBySession.remove(sessionId);
    _pendingLocalStopStreamMsgIdBySession.remove(sessionId);
    if (updatedAt > 0) {
      final existing = agentOutputStates[sessionId];
      if (existing != null) {
        final next = Map<String, dynamic>.from(existing);
        next['updated_at'] = updatedAt;
        agentOutputStates[sessionId] = next;
      }
    }
    final existing = agentOutputStates[sessionId];
    if (existing == null) {
      _markAgentOutputStoppingLocally(sessionId, runId: runId);
      return;
    }
    final next = Map<String, dynamic>.from(existing);
    next['state'] = 'stopping';
    next['can_stop'] = false;
    agentOutputStates[sessionId] = next;
    debugPrint(
      '✅ agent_output_stop_ack applied session=$sessionId '
      'state=${_describeAgentOutputStateForTrace(next)}',
    );
  }

  void _handleAgentOutputStatus(Map<String, dynamic> payload) {
    final sessionId = payload['session_id']?.toString().trim() ?? '';
    final state = payload['state']?.toString().trim() ?? '';
    if (sessionId.isEmpty || state.isEmpty) {
      return;
    }

    final updatedAt = _toInt(payload['updated_at']);
    final existing = agentOutputStates[sessionId];
    final existingUpdatedAt = existing == null
        ? 0
        : _toInt(existing['updated_at']);
    final existingState = existing?['state']?.toString().trim() ?? '';
    final existingRunId = existing?['run_id']?.toString().trim() ?? '';
    final incomingRunId = payload['run_id']?.toString().trim() ?? '';
    final incomingGeneration = _toInt(payload['dispatch_generation']);
    final existingGeneration = existing == null
        ? 0
        : _toInt(existing['dispatch_generation']);
    final incomingRevision = _toInt(payload['revision']);
    final existingRevision = existing == null
        ? 0
        : _toInt(existing['revision']);
    final incomingStreamMsgId =
        payload['stream_msg_id']?.toString().trim() ?? '';
    if (state == 'stopping' ||
        existingState == 'stopping' ||
        incomingStreamMsgId.isNotEmpty) {
      debugPrint(
        '📥 agent_output_status session=$sessionId '
        'run_id=${incomingRunId.isEmpty ? "-" : incomingRunId} '
        'state=$state can_stop=${payload['can_stop'] == true} '
        'stream_msg_id=${incomingStreamMsgId.isEmpty ? "-" : incomingStreamMsgId} '
        'updated_at=$updatedAt existing_state=${existingState.isEmpty ? "-" : existingState} '
        'existing_run_id=${existingRunId.isEmpty ? "-" : existingRunId} '
        'ui=${_describeStreamingUiForTrace(sessionId, focusMsgId: incomingStreamMsgId)}',
      );
    }
    if (existingGeneration > 0 &&
        (incomingGeneration <= 0 || incomingGeneration < existingGeneration)) {
      return;
    }
    if (incomingGeneration > 0 &&
        incomingGeneration == existingGeneration &&
        existingRunId.isNotEmpty &&
        incomingRunId != existingRunId) {
      return;
    }
    if (incomingGeneration == existingGeneration &&
        incomingRunId == existingRunId &&
        incomingRevision > 0 &&
        existingRevision > 0 &&
        incomingRevision <= existingRevision) {
      return;
    }
    if (incomingGeneration <= 0 &&
        existingGeneration <= 0 &&
        updatedAt > 0 &&
        existingUpdatedAt > 0 &&
        updatedAt < existingUpdatedAt) {
      return;
    }
    if (existingState == 'stopping' &&
        state != 'stopping' &&
        !_isTerminalAgentOutputState(state) &&
        !(incomingRunId == existingRunId &&
            incomingRevision > 0 &&
            existingRevision > 0 &&
            incomingRevision > existingRevision) &&
        (incomingRunId.isEmpty ||
            existingRunId.isEmpty ||
            incomingRunId == existingRunId)) {
      debugPrint(
        '⚠️ agent_output_status ignored by stopping guard session=$sessionId '
        'run_id=${incomingRunId.isEmpty ? "-" : incomingRunId} '
        'incoming_state=$state existing_state=$existingState',
      );
      return;
    }

    if (_isTerminalAgentOutputState(state)) {
      // Terminal output is scoped to one run. A delayed retry from an older
      // run must not clear the current run's stream/composing UI.
      if (existing == null || incomingRunId != existingRunId) {
        return;
      }
      final terminalStreamMsgId = incomingStreamMsgId.isNotEmpty
          ? incomingStreamMsgId
          : existing['stream_msg_id']?.toString().trim() ?? '';
      _clearActiveStreamingStateForMessage(terminalStreamMsgId);
      final payloadAgentId = payload['agent_id']?.toString().trim() ?? '';
      final agentId = payloadAgentId.isNotEmpty
          ? payloadAgentId
          : existing['agent_id']?.toString().trim() ?? '';
      if (agentId.isNotEmpty) {
        _markSessionComposingResolvedForParticipant(
          sessionId,
          participantId: agentId,
          participantType: 'agent',
          resolvedAt: updatedAt,
        );
        _clearSessionComposingActivitiesForParticipant(
          sessionId,
          participantId: agentId,
          participantType: 'agent',
        );
      }
      agentOutputStates.remove(sessionId);
      return;
    }

    if (state == 'stopping') {
      final streamMsgId = payload['stream_msg_id']?.toString().trim() ?? '';
      if (streamMsgId.isNotEmpty) {
        if (!_activeStreamingMsgIds.contains(streamMsgId)) {
          return;
        }
        _activeStreamingMsgIds.remove(streamMsgId);
        _discardStreamingSessionPreview(streamMsgId);
        _clearStreamChunkGapTrackingForMessage(streamMsgId);
        MessageStreamController.discard(streamMsgId);
        _removeUIMessage(streamMsgId);
      }
    }

    agentOutputStates[sessionId] = {
      'run_id': payload['run_id']?.toString().trim() ?? '',
      'session_id': sessionId,
      'dispatch_generation': incomingGeneration,
      'revision': incomingRevision,
      'scope': payload['scope']?.toString().trim() ?? '',
      'owner_id': payload['owner_id']?.toString().trim() ?? '',
      'agent_id': payload['agent_id']?.toString().trim() ?? '',
      'trigger_msg_id': payload['trigger_msg_id']?.toString().trim() ?? '',
      'stream_msg_id': payload['stream_msg_id']?.toString().trim() ?? '',
      'state': state,
      'can_stop': payload['can_stop'] == true,
      'stop_reason': payload['stop_reason']?.toString().trim() ?? '',
      'updated_at': updatedAt,
    };
    _cancelDeliveryTimeoutGracePeriodForSession(sessionId);
    _scheduleStaleAgentOutputTimer();
  }

  String? _describeAgentDeliveryStatusImpl(String? status) {
    final normalized = (status ?? '').trim();
    if (normalized == 'failed:channel_unavailable') {
      return 'chat_agent_channel_offline'.tr;
    }
    switch (normalized) {
      case 'queued':
        return null;
      case 'received':
        return 'chat_agent_delivery_received'.tr;
      case 'responded':
        return null;
      case 'timeout':
        return 'chat_agent_delivery_timeout'.tr;
      case 'failed':
        return 'chat_agent_delivery_failed'.tr;
      default:
        return null;
    }
  }

  bool _isAgentDeliveryStatusErrorImpl(String? status) {
    final normalized = (status ?? '').trim();
    return normalized == 'timeout' ||
        normalized == 'failed' ||
        normalized == 'failed:channel_unavailable';
  }

  void _handleAgentStateSync(Map<String, dynamic> payload) {
    final agentId = payload['agent_id']?.toString().trim() ?? '';
    if (agentId.isEmpty) {
      return;
    }

    final extra = payload['extra'];
    final extraMap = extra is Map
        ? Map<String, dynamic>.from(extra)
        : const <String, dynamic>{};
    final leaseUntil = _toInt(extraMap['lease_until']);
    final connected = _toBool(extraMap['connected']);
    final incomingConnectionEpoch = _toInt(extraMap['connection_epoch']);
    final currentConnectionEpoch = _toInt(
      agentStates[agentId]?['connection_epoch'],
    );
    if (currentConnectionEpoch > 0 &&
        (incomingConnectionEpoch <= 0 ||
            incomingConnectionEpoch < currentConnectionEpoch)) {
      return;
    }
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final incomingState =
        payload['state']?.toString().trim().toLowerCase() ?? '';
    final isOnline =
        incomingState == 'online' && connected && leaseUntil > nowMs;

    agentStates[agentId] = <String, dynamic>{
      'state': isOnline ? 'online' : 'offline',
      'lease_until': isOnline ? leaseUntil : 0,
      'connection_epoch': incomingConnectionEpoch > 0
          ? incomingConnectionEpoch
          : 0,
    };
    agentStates.refresh();
    _agentStateExpiryTick.value++;
    _scheduleAgentStateExpiryTimer();
  }

  void _scheduleAgentStateExpiryTimer() {
    _agentStateExpiryTimer?.cancel();
    _agentStateExpiryTimer = null;

    final nowMs = DateTime.now().millisecondsSinceEpoch;
    var nextLeaseUntil = 0;
    for (final entry in agentStates.values) {
      if ((entry['state']?.toString().trim() ?? '') != 'online') {
        continue;
      }
      final leaseUntil = _toInt(entry['lease_until']);
      if (leaseUntil <= nowMs) {
        continue;
      }
      if (nextLeaseUntil == 0 || leaseUntil < nextLeaseUntil) {
        nextLeaseUntil = leaseUntil;
      }
    }

    if (nextLeaseUntil <= 0) {
      return;
    }

    final delayMs = nextLeaseUntil - nowMs;
    _agentStateExpiryTimer = Timer(Duration(milliseconds: delayMs), () {
      _normalizeExpiredAgentStates();
      _agentStateExpiryTick.value++;
      _scheduleAgentStateExpiryTimer();
    });
  }

  void _normalizeExpiredAgentStates() {
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    var changed = false;
    for (final entry in agentStates.entries.toList()) {
      final value = entry.value;
      if ((value['state']?.toString().trim() ?? '') != 'online') {
        continue;
      }
      final leaseUntil = _toInt(value['lease_until']);
      if (leaseUntil > nowMs) {
        continue;
      }
      agentStates[entry.key] = <String, dynamic>{
        'state': 'offline',
        'lease_until': 0,
        'connection_epoch': _toInt(value['connection_epoch']),
      };
      changed = true;
    }
    if (changed) {
      agentStates.refresh();
    }
  }

  int _resolveDelegateMaxConsecutiveRepliesImpl(
    dynamic raw, {
    required String sessionId,
  }) {
    final fromPayload = _toInt(raw);
    if (fromPayload > 0) return fromPayload;
    final existing = delegateStates[sessionId];
    if (existing != null) {
      final fromExisting = _toInt(existing['max_consecutive_replies']);
      if (fromExisting > 0) return fromExisting;
    }
    return ImService.defaultDelegateMaxConsecutiveReplies;
  }

  // ---------------------------------------------------------------------------
  // Stale agent-output state cleanup
  // ---------------------------------------------------------------------------

  // How long an agentOutputStates entry may remain without an update before it
  // is considered abandoned and eligible for forced removal.
  static const int _staleAgentOutputTimeoutMs = 90 * 1000;

  void _scheduleStaleAgentOutputTimer() {
    _staleAgentOutputTimer?.cancel();
    _staleAgentOutputTimer = null;

    var nextExpiryAt = 0;
    for (final entry in agentOutputStates.entries) {
      final updatedAt = _toInt(entry.value['updated_at']);
      if (updatedAt <= 0) continue;
      final expiresAt = updatedAt + _staleAgentOutputTimeoutMs;
      if (nextExpiryAt == 0 || expiresAt < nextExpiryAt) {
        nextExpiryAt = expiresAt;
      }
    }
    if (nextExpiryAt <= 0) return;

    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final delayMs = (nextExpiryAt - nowMs).clamp(
      1000,
      _staleAgentOutputTimeoutMs,
    );
    _staleAgentOutputTimer = Timer(
      Duration(milliseconds: delayMs),
      _pruneStaleAgentOutputStates,
    );
  }

  void _pruneStaleAgentOutputStates() {
    _staleAgentOutputTimer = null;
    final nowMs = DateTime.now().millisecondsSinceEpoch;

    for (final sid in agentOutputStates.keys.toList()) {
      final entry = agentOutputStates[sid];
      if (entry == null) continue;
      final updatedAt = _toInt(entry['updated_at']);
      if (updatedAt > 0 && nowMs - updatedAt < _staleAgentOutputTimeoutMs) {
        continue;
      }
      if (hasStreamingAgentOutputForSession(sid)) continue;
      agentOutputStates.remove(sid);
      debugPrint(
        '🧹 pruned stale agent output state session=$sid '
        'updated_at=$updatedAt now=$nowMs',
      );
    }

    if (agentOutputStates.isNotEmpty) {
      _scheduleStaleAgentOutputTimer();
    }
  }
}
