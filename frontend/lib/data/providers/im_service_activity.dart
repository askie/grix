part of 'im_service.dart';

extension _ImServiceActivity on ImService {
  String _sessionActivityActorTypeFromSenderType(int senderType) {
    return senderType == 2 ? 'agent' : 'human';
  }

  String _sessionComposingResolutionKey(
    String sessionId, {
    required String participantId,
    required String participantType,
  }) {
    return '${sessionId.trim()}:${participantType.trim().toLowerCase()}:${participantId.trim()}';
  }

  bool _sessionActivityMatchesParticipant(
    SessionActivityModel activity, {
    required String participantId,
    required String participantType,
  }) {
    final normalizedParticipantId = participantId.trim();
    final normalizedParticipantType = participantType.trim().toLowerCase();
    if (normalizedParticipantId.isEmpty || normalizedParticipantType.isEmpty) {
      return false;
    }
    final actorMatches =
        activity.actorType.trim().toLowerCase() == normalizedParticipantType &&
        activity.actorId == normalizedParticipantId;
    if (actorMatches) {
      return true;
    }
    return activity.executorType.trim().toLowerCase() ==
            normalizedParticipantType &&
        activity.executorId == normalizedParticipantId;
  }

  void _markSessionComposingResolvedForParticipant(
    String sessionId, {
    required String participantId,
    required String participantType,
    required int resolvedAt,
  }) {
    final sid = sessionId.trim();
    final normalizedParticipantId = participantId.trim();
    final normalizedParticipantType = participantType.trim().toLowerCase();
    if (sid.isEmpty ||
        normalizedParticipantId.isEmpty ||
        normalizedParticipantType.isEmpty) {
      return;
    }

    final normalizedResolvedAt = resolvedAt > 0
        ? resolvedAt
        : DateTime.now().millisecondsSinceEpoch;
    final key = _sessionComposingResolutionKey(
      sid,
      participantId: normalizedParticipantId,
      participantType: normalizedParticipantType,
    );
    final existing = _resolvedSessionComposingAtByParticipant[key] ?? 0;
    if (normalizedResolvedAt > existing) {
      _resolvedSessionComposingAtByParticipant[key] = normalizedResolvedAt;
    }
  }

  void _markSessionComposingResolvedFromActivity(
    SessionActivityModel activity, {
    int resolvedAt = 0,
  }) {
    if (activity.kind.trim() != 'composing') {
      return;
    }

    final activityResolvedAt = resolvedAt > 0 ? resolvedAt : activity.updatedAt;
    _markSessionComposingResolvedForParticipant(
      activity.sessionId,
      participantId: activity.actorId,
      participantType: activity.actorType,
      resolvedAt: activityResolvedAt,
    );
    _markSessionComposingResolvedForParticipant(
      activity.sessionId,
      participantId: activity.executorId,
      participantType: activity.executorType,
      resolvedAt: activityResolvedAt,
    );
  }

  bool _isSessionComposingResolvedForParticipant(
    String sessionId, {
    required String participantId,
    required String participantType,
    required int activityAt,
  }) {
    final sid = sessionId.trim();
    final normalizedParticipantId = participantId.trim();
    final normalizedParticipantType = participantType.trim().toLowerCase();
    if (sid.isEmpty ||
        normalizedParticipantId.isEmpty ||
        normalizedParticipantType.isEmpty ||
        activityAt <= 0) {
      return false;
    }

    final key = _sessionComposingResolutionKey(
      sid,
      participantId: normalizedParticipantId,
      participantType: normalizedParticipantType,
    );
    final resolvedAt = _resolvedSessionComposingAtByParticipant[key] ?? 0;
    return resolvedAt > 0 && activityAt <= resolvedAt;
  }

  bool _shouldSuppressResolvedSessionActivity(SessionActivityModel activity) {
    if (activity.kind.trim() != 'composing') {
      return false;
    }

    final activityAt = activity.updatedAt;
    if (activityAt <= 0) {
      return false;
    }

    return _isSessionComposingResolvedForParticipant(
          activity.sessionId,
          participantId: activity.actorId,
          participantType: activity.actorType,
          activityAt: activityAt,
        ) ||
        _isSessionComposingResolvedForParticipant(
          activity.sessionId,
          participantId: activity.executorId,
          participantType: activity.executorType,
          activityAt: activityAt,
        );
  }

  void _clearSessionComposingActivitiesForParticipant(
    String sessionId, {
    required String participantId,
    required String participantType,
  }) {
    final sid = sessionId.trim();
    final normalizedParticipantId = participantId.trim();
    final normalizedParticipantType = participantType.trim().toLowerCase();
    if (sid.isEmpty) {
      return;
    }
    if (normalizedParticipantId.isEmpty || normalizedParticipantType.isEmpty) {
      return;
    }

    final current = sessionActivities[sid];
    if (current == null || current.isEmpty) {
      return;
    }

    final next = current
        .where((item) {
          if (item.kind.trim() != 'composing') {
            return true;
          }
          return !_sessionActivityMatchesParticipant(
            item,
            participantId: normalizedParticipantId,
            participantType: normalizedParticipantType,
          );
        })
        .toList(growable: false);

    if (next.length == current.length) {
      return;
    }
    if (next.isEmpty) {
      sessionActivities.remove(sid);
    } else {
      sessionActivities[sid] = next;
    }
    _scheduleSessionActivityCleanup();
  }

  void _clearSessionComposingActivitiesForMessage(
    String sessionId, {
    required String msgId,
    required String senderId,
    required int senderType,
  }) {
    final sid = sessionId.trim();
    final normalizedMsgId = msgId.trim();
    final normalizedSenderId = senderId.trim();
    final participantType = _sessionActivityActorTypeFromSenderType(senderType);
    if (sid.isEmpty) {
      return;
    }

    final current = sessionActivities[sid];
    if (current == null || current.isEmpty) {
      return;
    }

    final next = current
        .where((item) {
          if (item.kind.trim() != 'composing') {
            return true;
          }
          if (normalizedMsgId.isNotEmpty &&
              item.refMsgId.trim().isNotEmpty &&
              item.refMsgId.trim() == normalizedMsgId) {
            return false;
          }
          if (normalizedSenderId.isEmpty) {
            return true;
          }
          return !_sessionActivityMatchesParticipant(
            item,
            participantId: normalizedSenderId,
            participantType: participantType,
          );
        })
        .toList(growable: false);

    if (next.length == current.length) {
      return;
    }
    if (next.isEmpty) {
      sessionActivities.remove(sid);
    } else {
      sessionActivities[sid] = next;
    }
    _scheduleSessionActivityCleanup();
  }

  void _updateSessionComposingImpl(String sessionId, {required bool active}) {
    final sid = sessionId.trim();
    if (!active) {
      final targetSid = sid.isNotEmpty ? sid : _composingSessionId;
      _stopSessionComposing(targetSid, notifyRemote: true);
      return;
    }

    if (sid.isEmpty) return;
    if (_composingActive &&
        _composingSessionId.isNotEmpty &&
        _composingSessionId != sid) {
      _stopSessionComposing(_composingSessionId, notifyRemote: true);
    }

    final shouldSendImmediately =
        !_composingActive ||
        _composingSessionId != sid ||
        !(_composingRenewTimer?.isActive ?? false);
    _composingSessionId = sid;
    _composingActive = true;
    if (shouldSendImmediately) {
      _sendSessionActivitySet(
        sid,
        kind: 'composing',
        active: true,
        scheduleReconnect: true,
      );
      _composingRenewTimer?.cancel();
      _composingRenewTimer = Timer.periodic(ImService._composingRenewInterval, (
        _,
      ) {
        if (!_composingActive || _composingSessionId != sid) {
          return;
        }
        _sendSessionActivitySet(
          sid,
          kind: 'composing',
          active: true,
          scheduleReconnect: false,
        );
      });
    }
    // 空闲兜底：本方法由本地文本变化驱动（续期循环不会再回到这里），
    // 每次击键都会重置计时；超时没有被重新触达就说明人已经停止输入，
    // 主动结束 composing。注意续期中的再次触达只在上面 shouldSendImmediately
    // 为 false 时走到这里，此时绝不能重建续期定时器，否则快速连续输入会把
    // 2s 续期无限推迟、服务端 TTL 过期导致指示器闪断。
    _composingIdleTimer?.cancel();
    _composingIdleTimer = Timer(
      ImService.composingIdleTimeoutForTest ?? ImService._composingIdleTimeout,
      () {
        if (!_composingActive || _composingSessionId != sid) {
          return;
        }
        _stopSessionComposing(sid, notifyRemote: true);
      },
    );
  }

  void _stopSessionComposing(String sessionId, {required bool notifyRemote}) {
    _composingIdleTimer?.cancel();
    _composingIdleTimer = null;
    final sid = sessionId.trim();
    final currentSid = _composingSessionId.trim();
    final targetSid = sid.isNotEmpty ? sid : currentSid;
    if (targetSid.isEmpty) {
      _composingRenewTimer?.cancel();
      _composingRenewTimer = null;
      _composingActive = false;
      _composingSessionId = '';
      return;
    }

    final shouldSend =
        notifyRemote && (_composingActive || currentSid == targetSid);
    _composingRenewTimer?.cancel();
    _composingRenewTimer = null;
    _composingActive = false;
    _composingSessionId = '';
    if (shouldSend) {
      _sendSessionActivitySet(
        targetSid,
        kind: 'composing',
        active: false,
        scheduleReconnect: false,
      );
    }
  }

  void _startSessionViewing(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (_viewingActive &&
        _viewingSessionId.isNotEmpty &&
        _viewingSessionId != sid) {
      _stopSessionViewing(_viewingSessionId, notifyRemote: true);
    }

    final shouldSendImmediately =
        !_viewingActive ||
        _viewingSessionId != sid ||
        !(_sessionViewingRenewTimer?.isActive ?? false);
    _viewingSessionId = sid;
    _viewingActive = true;
    if (shouldSendImmediately) {
      _sendSessionActivitySet(
        sid,
        kind: 'viewing',
        active: true,
        scheduleReconnect: true,
      );
    }
    _sessionViewingRenewTimer?.cancel();
    _sessionViewingRenewTimer = Timer.periodic(
      ImService._sessionViewingRenewInterval,
      (_) {
        if (!_viewingActive || _viewingSessionId != sid) {
          return;
        }
        _sendSessionActivitySet(
          sid,
          kind: 'viewing',
          active: true,
          scheduleReconnect: false,
        );
      },
    );
  }

  void _stopSessionViewing(String sessionId, {required bool notifyRemote}) {
    final sid = sessionId.trim();
    final currentSid = _viewingSessionId.trim();
    final targetSid = sid.isNotEmpty ? sid : currentSid;
    if (targetSid.isEmpty) {
      _sessionViewingRenewTimer?.cancel();
      _sessionViewingRenewTimer = null;
      _viewingActive = false;
      _viewingSessionId = '';
      return;
    }

    final shouldSend =
        notifyRemote && (_viewingActive || currentSid == targetSid);
    _sessionViewingRenewTimer?.cancel();
    _sessionViewingRenewTimer = null;
    _viewingActive = false;
    _viewingSessionId = '';
    if (shouldSend) {
      _sendSessionActivitySet(
        targetSid,
        kind: 'viewing',
        active: false,
        scheduleReconnect: false,
      );
    }
  }

  void _sendSessionActivitySet(
    String sessionId, {
    required String kind,
    required bool active,
    bool scheduleReconnect = false,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final req = {
      'cmd': 'session_activity_set',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'session_id': sid, 'kind': kind, 'active': active},
    };
    final sent = _sendPacket(req, requireAuthenticated: true);
    if (!sent && scheduleReconnect && !_isConnected.value && _wsUrl != null) {
      _scheduleReconnect();
    }
  }

  void _requestSessionActivityList(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    _sendPacket({
      'cmd': 'session_activity_list',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'session_id': sid},
    }, requireAuthenticated: true);
  }

  void _applySessionActivitySnapshot(
    String sessionId,
    List<SessionActivityModel> activities,
  ) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final next =
        activities
            .where(
              (item) =>
                  item.active &&
                  !item.isExpired &&
                  !_shouldSuppressResolvedSessionActivity(item),
            )
            .toList(growable: false)
          ..sort((a, b) => a.updatedAt.compareTo(b.updatedAt));
    if (next.isEmpty) {
      sessionActivities.remove(sid);
    } else {
      sessionActivities[sid] = next;
    }
    _scheduleSessionActivityCleanup();
  }

  void _applySessionActivitySync(SessionActivityModel activity) {
    final sid = activity.sessionId.trim();
    if (sid.isEmpty) return;
    if (!activity.active) {
      _markSessionComposingResolvedFromActivity(activity);
    }

    final current = List<SessionActivityModel>.from(
      sessionActivities[sid] ?? [],
    );
    current.removeWhere((item) {
      return item.actorType == activity.actorType &&
          item.actorId == activity.actorId &&
          item.kind == activity.kind;
    });
    // Real-time sync events represent the server's current state; trust them
    // without the stale-resolution check (which is only needed for snapshots).
    if (activity.active && !activity.isExpired) {
      current.add(activity);
      current.sort((a, b) => a.updatedAt.compareTo(b.updatedAt));
    }

    if (current.isEmpty) {
      sessionActivities.remove(sid);
    } else {
      sessionActivities[sid] = current;
    }
    _scheduleSessionActivityCleanup();
  }

  void _scheduleSessionActivityCleanup() {
    _sessionActivityCleanupTimer?.cancel();

    int nextExpiryAt = 0;
    for (final items in sessionActivities.values) {
      for (final item in items) {
        if (!item.active || item.expiresAt <= 0) continue;
        if (nextExpiryAt == 0 || item.expiresAt < nextExpiryAt) {
          nextExpiryAt = item.expiresAt;
        }
      }
    }
    if (nextExpiryAt <= 0) return;

    final now = DateTime.now().millisecondsSinceEpoch;
    final delayMs = (nextExpiryAt - now).clamp(50, 60000);
    _sessionActivityCleanupTimer = Timer(
      Duration(milliseconds: delayMs),
      _pruneExpiredSessionActivities,
    );
  }

  void _pruneExpiredSessionActivities() {
    final now = DateTime.now().millisecondsSinceEpoch;
    final next = <String, List<SessionActivityModel>>{};

    for (final entry in sessionActivities.entries) {
      final items = entry.value
          .where((item) {
            return item.active && (item.expiresAt <= 0 || item.expiresAt > now);
          })
          .toList(growable: false);
      if (items.isNotEmpty) {
        next[entry.key] = items;
      }
    }

    sessionActivities.assignAll(next);
    _scheduleSessionActivityCleanup();
  }
}
