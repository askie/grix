part of 'im_service.dart';

extension _ImServiceEventLifecycle on ImService {
  List<EventLifecycleQueueItem> _queueItemsForSessionImpl(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return const <EventLifecycleQueueItem>[];
    }
    final items = eventLifecycleQueues[sid];
    if (items == null || items.isEmpty) {
      return const <EventLifecycleQueueItem>[];
    }
    return List<EventLifecycleQueueItem>.from(items, growable: false);
  }

  int _queueCountForSessionImpl(String sessionId) {
    return _queueItemsForSessionImpl(sessionId).length;
  }

  void _sendEventCancelImpl({
    required String sessionId,
    required EventLifecycleQueueItem item,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty || item.eventId.isEmpty) {
      return;
    }
    _sendPacket({
      'cmd': 'event_cancel',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'session_id': sid, 'event_id': item.eventId},
    }, requireAuthenticated: true);
  }

  /// 发送 event_hold（暂停/恢复排队任务）并等待回执。
  ///
  /// - 本地先乐观翻转 held，随后由回执/权威 queue_snapshot 收敛；
  ///   回执失败时主动 pull 一次快照兜底还原。
  /// - 老 backend/connector 无此命令时收不到任何回执，[timeout] 后
  ///   以 timedOut=true 收口，调用方据此降级提示。
  Future<EventLifecycleCmdResult> _sendEventHoldImpl({
    required String sessionId,
    required String eventId,
    required bool hold,
    required String reason,
    int? ttlMs,
    Duration timeout = const Duration(seconds: 5),
  }) {
    final sid = sessionId.trim();
    final eid = eventId.trim();
    if (sid.isEmpty || eid.isEmpty) {
      return Future.value(
        const EventLifecycleCmdResult(ok: false, error: 'bad_request'),
      );
    }
    _applyQueueItemHeldImpl(sid, eid, held: hold, heldReason: reason);
    final payload = <String, dynamic>{
      'session_id': sid,
      'event_id': eid,
      'hold': hold,
      'reason': reason,
    };
    if (ttlMs != null && ttlMs > 0) {
      payload['ttl_ms'] = ttlMs;
    }
    return _awaitLifecycleCmdResult(
      pending: _eventHoldPending,
      key: '$sid|$eid',
      timeout: timeout,
      onFailure: () => pullQueueSnapshot(sessionId: sid),
      send: () {
        _sendPacket({
          'cmd': 'event_hold',
          'seq': DateTime.now().millisecondsSinceEpoch,
          'payload': payload,
        }, requireAuthenticated: true);
      },
    );
  }

  /// 发送 queue_edit（改写排队任务全文）并等待回执。
  /// 成功后 connector 会紧跟推权威 queue_snapshot 收敛本地。
  Future<EventLifecycleCmdResult> _sendQueueEditImpl({
    required String sessionId,
    required String eventId,
    required String content,
    Duration timeout = const Duration(seconds: 5),
  }) {
    final sid = sessionId.trim();
    final eid = eventId.trim();
    if (sid.isEmpty || eid.isEmpty) {
      return Future.value(
        const EventLifecycleCmdResult(ok: false, error: 'bad_request'),
      );
    }
    if (content.trim().isEmpty) {
      return Future.value(
        const EventLifecycleCmdResult(ok: false, error: 'empty_content'),
      );
    }
    return _awaitLifecycleCmdResult(
      pending: _queueEditPending,
      key: '$sid|$eid',
      timeout: timeout,
      send: () {
        _sendPacket({
          'cmd': 'queue_edit',
          'seq': DateTime.now().millisecondsSinceEpoch,
          'payload': {'session_id': sid, 'event_id': eid, 'content': content},
        }, requireAuthenticated: true);
      },
    );
  }

  /// 挂起等待某条 *_result 回执：按 session|event 关联（新命令回执不带
  /// 请求 seq），超时以 timedOut 收口。同 key 的旧等待会先被超时收口，
  /// 避免续期场景下 completer 泄漏。
  Future<EventLifecycleCmdResult> _awaitLifecycleCmdResult({
    required Map<String, Completer<EventLifecycleCmdResult>> pending,
    required String key,
    required Duration timeout,
    required void Function() send,
    void Function()? onFailure,
  }) {
    final stale = pending.remove(key);
    if (stale != null && !stale.isCompleted) {
      stale.complete(const EventLifecycleCmdResult(ok: false, timedOut: true));
    }
    final completer = Completer<EventLifecycleCmdResult>();
    pending[key] = completer;
    send();
    Timer(timeout, () {
      if (identical(pending[key], completer)) {
        pending.remove(key);
      }
      if (!completer.isCompleted) {
        completer.complete(
          const EventLifecycleCmdResult(ok: false, timedOut: true),
        );
      }
    });
    return completer.future.then((result) {
      if (!result.ok && onFailure != null) {
        onFailure();
      }
      return result;
    });
  }

  void _handleEventHoldResult(Map<String, dynamic> payload) {
    final sid = payload['session_id']?.toString().trim() ?? '';
    final eventId = payload['event_id']?.toString().trim() ?? '';
    final ok = payload['ok'] == true;
    final held = payload['held'] == true;
    final error = payload['error']?.toString().trim() ?? '';
    if (ok && sid.isNotEmpty && eventId.isNotEmpty) {
      // 以回执为准修正本地 held（乐观翻转与回执竞态时向权威收敛）
      _applyQueueItemHeldImpl(sid, eventId, held: held);
    }
    final completer = _eventHoldPending.remove('$sid|$eventId');
    if (completer != null && !completer.isCompleted) {
      completer.complete(
        EventLifecycleCmdResult(ok: ok, held: held, error: error),
      );
    }
  }

  void _handleQueueEditResult(Map<String, dynamic> payload) {
    final sid = payload['session_id']?.toString().trim() ?? '';
    final eventId = payload['event_id']?.toString().trim() ?? '';
    final ok = payload['ok'] == true;
    final error = payload['error']?.toString().trim() ?? '';
    final completer = _queueEditPending.remove('$sid|$eventId');
    if (completer != null && !completer.isCompleted) {
      completer.complete(EventLifecycleCmdResult(ok: ok, error: error));
    }
  }

  /// 就地更新本地缓存中某排队项的 held 状态（乐观更新/回执收敛共用）。
  void _applyQueueItemHeldImpl(
    String sessionId,
    String eventId, {
    required bool held,
    String? heldReason,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty || eventId.isEmpty) {
      return;
    }
    final current = List<EventLifecycleQueueItem>.from(
      eventLifecycleQueues[sid] ?? const <EventLifecycleQueueItem>[],
    );
    final idx = current.indexWhere((e) => e.eventId == eventId);
    if (idx == -1) {
      return;
    }
    final item = current[idx];
    final reason = held ? (heldReason ?? item.heldReason) : '';
    if (item.held == held && item.heldReason == reason) {
      return;
    }
    current[idx] = item.copyWith(
      held: held,
      heldReason: reason,
      updatedAt: DateTime.now().millisecondsSinceEpoch,
    );
    eventLifecycleQueues[sid] = current;
  }

  void _sendQueueClearImpl({required String sessionId}) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    _sendPacket({
      'cmd': 'queue_clear',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'session_id': sid},
    }, requireAuthenticated: true);
  }

  void _sendQueueReorderImpl({
    required String sessionId,
    required List<String> orderedEventIds,
  }) {
    final sid = sessionId.trim();
    final ids = orderedEventIds
        .map((e) => e.trim())
        .where((e) => e.isNotEmpty)
        .toList(growable: false);
    if (sid.isEmpty || ids.isEmpty) {
      return;
    }
    // 先本地乐观重排让 UI 立即生效；随后被 queue_reorder_result /
    // 权威 queue_snapshot 覆盖收敛（竞态时最坏表现是弹回真实顺序）。
    _applyQueueOrderImpl(sid, ids);
    _sendPacket({
      'cmd': 'queue_reorder',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'session_id': sid, 'ordered_event_ids': ids},
    }, requireAuthenticated: true);
  }

  /// 按给定顺序（队头在前）重赋本地缓存中该 session 排队项的 position。
  /// 清单外的排队项按原相对顺序排在清单之后；running（position=0）不动。
  void _applyQueueOrderImpl(String sessionId, List<String> orderedEventIds) {
    final sid = sessionId.trim();
    if (sid.isEmpty || orderedEventIds.isEmpty) {
      return;
    }
    final current = List<EventLifecycleQueueItem>.from(
      eventLifecycleQueues[sid] ?? const <EventLifecycleQueueItem>[],
    );
    if (current.isEmpty) {
      return;
    }
    final order = <String, int>{};
    for (var i = 0; i < orderedEventIds.length; i++) {
      order.putIfAbsent(orderedEventIds[i], () => i + 1);
    }
    var tailPosition = orderedEventIds.length;
    final outside =
        current
            .where((e) => e.queuePosition > 0 && !order.containsKey(e.eventId))
            .toList()
          ..sort((a, b) => a.queuePosition.compareTo(b.queuePosition));
    for (final item in outside) {
      tailPosition += 1;
      order[item.eventId] = tailPosition;
    }
    var changed = false;
    for (var i = 0; i < current.length; i++) {
      final item = current[i];
      if (item.queuePosition <= 0) {
        continue;
      }
      final position = order[item.eventId];
      if (position == null || position == item.queuePosition) {
        continue;
      }
      current[i] = item.copyWith(
        queuePosition: position,
        updatedAt: DateTime.now().millisecondsSinceEpoch,
      );
      changed = true;
    }
    if (!changed) {
      return;
    }
    _sortQueueItems(current);
    eventLifecycleQueues[sid] = current;
  }

  void _handleQueueReorderResult(Map<String, dynamic> payload) {
    final applied = payload['applied_event_ids'];
    final ok =
        payload['ok'] == true || payload['success'] == true || applied is List;
    final sid = payload['session_id']?.toString().trim() ?? '';
    if (!ok) {
      final msg = payload['msg']?.toString().trim() ?? '';
      if (msg.isNotEmpty) {
        CustomToast.show(msg);
      }
      return;
    }
    if (sid.isEmpty || applied is! List) {
      return;
    }
    final ids = applied
        .map((e) => e?.toString().trim() ?? '')
        .where((e) => e.isNotEmpty)
        .toList(growable: false);
    if (ids.isEmpty) {
      return;
    }
    // 以 agent 应用后的实际顺序收敛本地（权威 queue_snapshot 随后还会再覆盖一次）
    _applyQueueOrderImpl(sid, ids);
  }

  /// 主动向服务端拉取一次某个 session 的队列快照。
  ///
  /// 用途：在 WS 重连成功 / 进入会话视图 / app 从后台回到前台等时机，
  /// 主动 pull 一次最新队列状态，覆盖本地缓存。
  /// 这是为了兜底服务端 push 通道的丢消息场景（connector 进程重启、
  /// idle evict、客户端短暂离线），让前端永远不会卡在 stale 的队列状态上。
  void _sendQueueSnapshotQueryImpl({required String sessionId}) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    _sendPacket({
      'cmd': 'queue_snapshot_query',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'session_id': sid},
    }, requireAuthenticated: true);
  }

  void _handleEventState(Map<String, dynamic> payload) {
    final item = _parseQueueItem(payload);
    if (item == null) {
      debugPrint(
        '[queue-debug] front event_state parse_failed payload=$payload',
      );
      return;
    }
    final sid = item.sessionId;
    final current = List<EventLifecycleQueueItem>.from(
      eventLifecycleQueues[sid] ?? const <EventLifecycleQueueItem>[],
    );
    final idx = current.indexWhere((e) => e.eventId == item.eventId);
    final terminal = _isTerminalEventLifecycleState(item.state);
    if (terminal) {
      if (idx != -1) {
        current.removeAt(idx);
      } else {
        debugPrint(
          '[queue-debug] front event_state session=$sid event=${item.eventId} '
          'state=${item.state} terminal=true not_in_queue ignored',
        );
        return;
      }
    } else if (idx == -1) {
      current.add(item);
    } else {
      current[idx] = item;
    }
    _sortQueueItems(current);
    if (current.isEmpty) {
      eventLifecycleQueues.remove(sid);
      // event_state 把最后一项收成终态时，队列 UI 已是 0，但未必再来一次
      // 空 queue_snapshot；这里同样清掉 agent composing，避免指示器空转。
      _clearAgentActivityForDrainedQueue(sid);
    } else {
      eventLifecycleQueues[sid] = current;
    }
    debugPrint(
      '[queue-debug] front event_state session=$sid event=${item.eventId} '
      'state=${item.state} terminal=$terminal queue_size=${current.length}',
    );
  }

  void _handleQueueSnapshot(Map<String, dynamic> payload) {
    final sid = payload['session_id']?.toString().trim() ?? '';
    if (sid.isEmpty) {
      return;
    }
    final next = <EventLifecycleQueueItem>[];
    final rawItems = payload['items'] ?? payload['events'] ?? payload['queue'];
    if (rawItems is List) {
      for (final raw in rawItems) {
        if (raw is! Map) {
          continue;
        }
        final item = _parseQueueItem(
          Map<String, dynamic>.from(raw),
          sessionId: sid,
        );
        if (item == null || _isTerminalEventLifecycleState(item.state)) {
          continue;
        }
        next.add(item);
      }
    }

    // grix-connector snapshot shape:
    // { running: string[], running_items?: [{event_id,content_preview,title,summary,actions}], queued: [{event_id, position, actions, ...}] }
    final runningDetails = <String, String>{};
    final runningActions = <String, List<String>>{};
    final runningItemsRaw = payload['running_items'];
    if (runningItemsRaw is List) {
      for (final raw in runningItemsRaw) {
        if (raw is! Map) {
          continue;
        }
        final map = Map<String, dynamic>.from(raw);
        final eventId = map['event_id']?.toString().trim() ?? '';
        if (eventId.isEmpty) {
          continue;
        }
        final preview =
            map['content_preview']?.toString().trim() ??
            map['title']?.toString().trim() ??
            map['summary']?.toString().trim() ??
            '';
        runningDetails[eventId] = preview;
        runningActions[eventId] = _normalizeActions(map['actions']);
      }
    }

    final runningRaw = payload['running'];
    if (runningRaw is List) {
      for (final eventIdRaw in runningRaw) {
        final eventId = eventIdRaw?.toString().trim() ?? '';
        if (eventId.isEmpty) {
          continue;
        }
        final preview = runningDetails[eventId] ?? '';
        final actions = runningActions[eventId] ?? const <String>['stop'];
        next.add(
          EventLifecycleQueueItem(
            eventId: eventId,
            sessionId: sid,
            messageId: '',
            clientMsgId: '',
            contentPreview: preview,
            state: 'running',
            queuePosition: 0,
            actions: actions,
            updatedAt: DateTime.now().millisecondsSinceEpoch,
          ),
        );
      }
    }

    final queuedRaw = payload['queued'];
    if (queuedRaw is List) {
      for (final raw in queuedRaw) {
        if (raw is! Map) {
          continue;
        }
        final map = Map<String, dynamic>.from(raw);
        final eventId = map['event_id']?.toString().trim() ?? '';
        if (eventId.isEmpty) {
          continue;
        }
        final actions = _normalizeActions(map['actions']);
        final position = _toInt(map['position']);
        final preview =
            map['content_preview']?.toString().trim() ??
            map['title']?.toString().trim() ??
            map['summary']?.toString().trim() ??
            '';
        final content = map['content']?.toString() ?? '';
        next.add(
          EventLifecycleQueueItem(
            eventId: eventId,
            sessionId: sid,
            messageId: '',
            clientMsgId: '',
            contentPreview: preview,
            state: 'queued',
            queuePosition: position,
            actions: actions.isEmpty ? const <String>['cancel'] : actions,
            updatedAt: DateTime.now().millisecondsSinceEpoch,
            content: content.trim().isNotEmpty ? content : preview,
            held: map['held'] == true,
            heldReason: map['held_reason']?.toString().trim() ?? '',
          ),
        );
      }
    }
    _sortQueueItems(next);
    if (next.isEmpty) {
      eventLifecycleQueues.remove(sid);
      _clearAgentActivityForDrainedQueue(sid);
    } else {
      eventLifecycleQueues[sid] = next;
    }
    debugPrint(
      '[queue-debug] front queue_snapshot session=$sid '
      'running=${(payload['running'] is List) ? (payload['running'] as List).length : 0} '
      'queued=${(payload['queued'] is List) ? (payload['queued'] as List).length : 0} '
      'final_queue_size=${next.length}',
    );
  }

  void _clearAgentActivityForDrainedQueue(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    final existingOutput = agentOutputStates[sid];
    if (existingOutput != null && !hasStreamingAgentOutputForSession(sid)) {
      final agentId = existingOutput['agent_id']?.toString().trim() ?? '';
      if (agentId.isNotEmpty) {
        _markSessionComposingResolvedForParticipant(
          sid,
          participantId: agentId,
          participantType: 'agent',
          resolvedAt: DateTime.now().millisecondsSinceEpoch,
        );
        _clearSessionComposingActivitiesForParticipant(
          sid,
          participantId: agentId,
          participantType: 'agent',
        );
      }
      agentOutputStates.remove(sid);
      debugPrint(
        '[queue-debug] front queue_drained clear_agent_output session=$sid '
        'agent=${agentId.isEmpty ? "-" : agentId} '
        'state=${existingOutput['state']?.toString().trim() ?? "-"}',
      );
    }

    final activities = sessionActivities[sid];
    if (activities == null || activities.isEmpty) {
      return;
    }
    final nextActivities = activities
        .where((activity) {
          if (activity.kind.trim() != 'composing') {
            return true;
          }
          final actorType = activity.actorType.trim().toLowerCase();
          final executorType = activity.executorType.trim().toLowerCase();
          return actorType != 'agent' &&
              actorType != 'agent_api' &&
              executorType != 'agent' &&
              executorType != 'agent_api';
        })
        .toList(growable: false);
    if (nextActivities.length == activities.length) {
      return;
    }
    if (nextActivities.isEmpty) {
      sessionActivities.remove(sid);
    } else {
      sessionActivities[sid] = nextActivities;
    }
    debugPrint(
      '[queue-debug] front queue_drained clear_agent_composing session=$sid '
      'removed=${activities.length - nextActivities.length}',
    );
  }

  void _handleEventCancelResult(Map<String, dynamic> payload) {
    final ok =
        payload['ok'] == true ||
        payload['success'] == true ||
        payload['canceled'] == true ||
        payload['accepted'] == true;
    final sid = payload['session_id']?.toString().trim() ?? '';
    final eventId = payload['event_id']?.toString().trim() ?? '';
    if (!ok) {
      final msg = payload['msg']?.toString().trim() ?? '';
      if (msg.isNotEmpty) {
        CustomToast.show(msg);
      }
      return;
    }
    if (sid.isEmpty || eventId.isEmpty) {
      return;
    }
    final current = List<EventLifecycleQueueItem>.from(
      eventLifecycleQueues[sid] ?? const <EventLifecycleQueueItem>[],
    );
    current.removeWhere((e) => e.eventId == eventId);
    _reindexQueuePositions(current);
    if (current.isEmpty) {
      eventLifecycleQueues.remove(sid);
      _clearAgentActivityForDrainedQueue(sid);
    } else {
      eventLifecycleQueues[sid] = current;
    }
  }

  void _handleQueueClearResult(Map<String, dynamic> payload) {
    final hasCanceledIDs = payload['canceled_event_ids'] is List;
    final ok =
        payload['ok'] == true || payload['success'] == true || hasCanceledIDs;
    final sid = payload['session_id']?.toString().trim() ?? '';
    if (!ok) {
      final msg = payload['msg']?.toString().trim() ?? '';
      if (msg.isNotEmpty) {
        CustomToast.show(msg);
      }
      return;
    }
    if (sid.isNotEmpty) {
      eventLifecycleQueues.remove(sid);
      _clearAgentActivityForDrainedQueue(sid);
    }
  }

  EventLifecycleQueueItem? _parseQueueItem(
    Map<String, dynamic> payload, {
    String? sessionId,
  }) {
    final sid = (sessionId ?? payload['session_id'])?.toString().trim() ?? '';
    if (sid.isEmpty) {
      return null;
    }
    final eventId = payload['event_id']?.toString().trim() ?? '';
    final messageId = payload['trigger_msg_id']?.toString().trim() ?? '';
    final clientMsgId = payload['client_msg_id']?.toString().trim() ?? '';
    final contentPreview = payload['content_preview']?.toString().trim() ?? '';
    final state = payload['state']?.toString().trim() ?? '';
    if (eventId.isEmpty || state.isEmpty) {
      return null;
    }
    final queuePosition = _toInt(payload['queue_position']);
    final updatedAt = _toInt(payload['updated_at']);
    final actions = _normalizeActions(payload['actions']);
    // content/held/held_reason 为快照/状态载荷新增字段，老服务端缺失时
    // content 回退 contentPreview、held 视为 false。
    final content = payload['content']?.toString() ?? '';
    return EventLifecycleQueueItem(
      eventId: eventId,
      sessionId: sid,
      messageId: messageId,
      clientMsgId: clientMsgId,
      contentPreview: contentPreview,
      state: state,
      queuePosition: queuePosition,
      actions: actions,
      updatedAt: updatedAt,
      content: content.trim().isNotEmpty ? content : contentPreview,
      held: payload['held'] == true,
      heldReason: payload['held_reason']?.toString().trim() ?? '',
    );
  }

  List<String> _normalizeActions(dynamic rawActions) {
    final actions = <String>[];
    if (rawActions is! List) {
      return actions;
    }
    for (final action in rawActions) {
      String normalized = '';
      if (action is Map) {
        normalized = action['type']?.toString().trim() ?? '';
      } else {
        normalized = action?.toString().trim() ?? '';
      }
      if (normalized.isEmpty || actions.contains(normalized)) {
        continue;
      }
      actions.add(normalized);
    }
    return actions;
  }

  bool _isTerminalEventLifecycleState(String state) {
    switch (state.trim()) {
      case 'responded':
      case 'completed':
      case 'failed':
      case 'timeout':
      case 'canceled':
        return true;
      default:
        return false;
    }
  }

  void _sortQueueItems(List<EventLifecycleQueueItem> items) {
    items.sort((a, b) {
      final leftPos = a.queuePosition > 0 ? a.queuePosition : 1 << 20;
      final rightPos = b.queuePosition > 0 ? b.queuePosition : 1 << 20;
      if (leftPos != rightPos) {
        return leftPos.compareTo(rightPos);
      }
      if (a.updatedAt != b.updatedAt) {
        return a.updatedAt.compareTo(b.updatedAt);
      }
      return a.eventId.compareTo(b.eventId);
    });
  }

  void _reindexQueuePositions(List<EventLifecycleQueueItem> items) {
    if (items.isEmpty) {
      return;
    }
    _sortQueueItems(items);
    for (var i = 0; i < items.length; i++) {
      items[i] = items[i].copyWith(queuePosition: i + 1);
    }
  }
}
