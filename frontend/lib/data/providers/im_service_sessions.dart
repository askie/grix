part of 'im_service.dart';

extension _ImServiceSessions on ImService {
  Future<void> loadSessions({bool refreshFromServer = true}) async {
    try {
      await _ensureDeletedSessionsLoaded();
      await _ensureRevokedSessionsLoaded();
      final dbSessions = await LocalDb.getSessions();
      final lastMsgs = await LocalDb.getLastMessages();

      final sessionMap = <String, Map<String, dynamic>>{};
      final suppressedDeletedSessionIDs = <String>[];
      final suppressedRevokedSessionIDs = <String>[];
      for (final s in dbSessions) {
        final sid = (s['session_id'] ?? '').toString().trim();
        if (sid.isEmpty) continue;
        final row = Map<String, dynamic>.from(s);
        final updatedAt = _requireIntLike(
          row['updated_at'] ?? 0,
          fieldName: 'sessions.updated_at',
        );
        if (_shouldSuppressDeletedSession(sid, updatedAt)) {
          suppressedDeletedSessionIDs.add(sid);
          continue;
        }
        if (_shouldSuppressAccessRevokedSession(sid)) {
          suppressedRevokedSessionIDs.add(sid);
          continue;
        }
        row['type'] = _normalizeSessionType(
          row['type']?.toString() ?? '',
          fallback: _sessionTypeHints[sid] ?? 'private',
        );
        row['is_pinned'] = _toBool(row['is_pinned']);
        row['is_muted'] = _toBool(row['is_muted']);
        row['friend_is_muted'] = _toBool(row['friend_is_muted']);
        row['pinned_at'] = _requireIntLike(
          row['pinned_at'] ?? 0,
          fieldName: 'sessions.pinned_at',
        );
        row['unread_count'] = _normalizeUnreadForSession(
          sid,
          _requireIntLike(
            row['unread_count'] ?? 0,
            fieldName: 'sessions.unread_count',
          ),
        );
        row['title'] = _normalizeStoredTitle(
          sid,
          row['title']?.toString() ?? '',
        );
        final lastMessageTime = _requireIntLike(
          row['last_message_time'] ?? 0,
          fieldName: 'sessions.last_message_time',
        );
        // 服务端会话快照落本地时 last_message_time 恒为 0（会话更新时间不等于最后
        // 一条消息时间），但其摘要已在服务端按聊天历史同口径过滤（per-user cutoff +
        // visible_to），是用户在聊天页能打开的消息，可直接展示。保留它作为预览兜底：
        // 新设备本地尚无消息时直接显示服务端摘要，本地一旦拉到更新消息再由下面的
        // lastMsgs 循环覆盖，避免首页全是占位"..."。
        row['last_message'] = row['last_message']?.toString() ?? '';
        row['last_message_time'] = lastMessageTime;
        sessionMap[sid] = row;
      }

      for (final entry in lastMsgs.entries) {
        final sid = entry.key.trim();
        if (sid.isEmpty) continue;
        final msgCreatedAt = _requireIntLike(
          entry.value['created_at'] ?? 0,
          fieldName: 'messages.created_at',
        );
        if (_shouldSuppressDeletedSession(sid, msgCreatedAt)) {
          suppressedDeletedSessionIDs.add(sid);
          continue;
        }
        if (_shouldSuppressAccessRevokedSession(sid)) {
          suppressedRevokedSessionIDs.add(sid);
          continue;
        }
        if (!sessionMap.containsKey(sid)) {
          sessionMap[sid] = {
            'session_id': sid,
            'title': '',
            'type': _sessionTypeHints[sid] ?? 'private',
            'peer_id': '',
            'peer_type': 0,
            'peer_nickname': '',
            'peer_username': '',
            'updated_at': entry.value['created_at'],
            'is_pinned': false,
            'is_muted': false,
            'pinned_at': 0,
            'unread_count': 0,
            'last_message': '',
            'last_message_time': 0,
          };
        }
        final currentUpdatedAt = _requireIntLike(
          sessionMap[sid]!['updated_at'] ?? 0,
          fieldName: 'sessions.updated_at',
        );
        if (msgCreatedAt > currentUpdatedAt) {
          sessionMap[sid]!['updated_at'] = msgCreatedAt;
        }
        if (_shouldUseLatestLocalMessagePreview(
          sessionMap[sid]!,
          localMessageCreatedAt: msgCreatedAt,
        )) {
          sessionMap[sid]!['last_message'] = entry.value['content'] ?? '';
          sessionMap[sid]!['last_message_time'] = entry.value['created_at'];
        }
      }

      for (final sid in suppressedDeletedSessionIDs) {
        await LocalDb.deleteConversation(sid);
      }
      for (final sid in suppressedRevokedSessionIDs) {
        await LocalDb.deleteSessionRecord(sid);
      }

      final nextSessions = sessionMap.values.map((e) {
        final model = SessionModel.fromJson(e);
        return model.copyWith(
          isVisitor: _visitorSessionIds.contains(model.sessionId.trim()),
        );
      }).toList()..sort(SessionModel.compareByPriority);
      _applyLoadedSessionsSnapshot(nextSessions);
      sessionsLoadTick.value++;
    } catch (e) {
      debugPrint('Load sessions error: $e');
    }

    unawaited(_backfillMissingPrivatePeerIdentities());

    if (refreshFromServer) {
      unawaited(
        _syncSessionsFromServerIfNeeded(
          force: true,
          limit: ImService._coldStartSessionSnapshotLimit,
          maxPages: ImService._coldStartSessionSnapshotMaxPages,
          fullSync: false,
        ),
      );
    }
  }

  SessionService? _sessionServiceOrNull() {
    if (!Get.isRegistered<SessionService>()) return null;
    return Get.find<SessionService>();
  }

  /// 由消息/未读快照创建的本地私聊会话记录缺对端身份（peer_id 为空），
  /// 会话列表按对端归组时会失配，未读对账随之错位。这里按需补拉会话详情，
  /// 把对端身份回填到本地库与内存，使归组口径收敛。
  ///
  /// [triedInChain] 是本条续跑链上已经尝试过的 sid 集合（跨轮穿透）：
  /// pending 选取与续跑判断都要排除它。否则"前 6 个占位全部瞬时失败且
  /// 还有第 7 个"时，每轮都取到同样的失败 6 个、第 7 个永远排不进批次，
  /// 却又每轮都满足续跑条件——退化成饿死事件循环的自旋/请求风暴。
  /// 链外（下一次 loadSessions 新开一条链）瞬时失败仍可重试，语义不变。
  Future<void> _backfillMissingPrivatePeerIdentities({
    Set<String>? triedInChain,
  }) async {
    if (_peerIdentityBackfillInFlight) return;
    final sessionService = _sessionServiceOrNull();
    if (sessionService == null || !sessionService.isInitialized) return;
    final tried = triedInChain ?? <String>{};
    final pending = sessions
        .where(
          (s) =>
              s.type == 'private' &&
              !s.isVisitor &&
              s.peerId.trim().isEmpty &&
              s.unreadCount > 0 &&
              !tried.contains(s.sessionId.trim()) &&
              !_peerIdentityBackfillAttempted.contains(s.sessionId.trim()),
        )
        .take(ImService._peerIdentityBackfillBatchLimit)
        .toList();
    if (pending.isEmpty) return;
    // 本轮处理过的 sid：链内每轮并入 tried，保证链上每个 sid 至多尝试一次。
    final attemptedThisRun = pending
        .map((s) => s.sessionId.trim())
        .where((sid) => sid.isNotEmpty)
        .toSet();
    _peerIdentityBackfillInFlight = true;
    try {
      final myUserId = Get.isRegistered<AuthService>()
          ? (Get.find<AuthService>().userId?.trim() ?? '')
          : '';
      var changed = false;
      for (final session in pending) {
        final sid = session.sessionId.trim();
        if (sid.isEmpty) continue;
        final detailResult = await sessionService.fetchSessionDetailResult(sid);
        final data = detailResult.data;
        if (data == null) {
          // 只有拿到明确的业务性失败（无权限/会话不存在）才放弃；网络抖动、
          // 服务端临时错误等留给下一轮 loadSessions 重试。API 层自带按会话
          // 的失败退避与请求排队，重试不会放大请求量。
          if (detailResult.code == 4003 || detailResult.code == 4004) {
            _peerIdentityBackfillAttempted.add(sid);
          }
          continue;
        }
        _peerIdentityBackfillAttempted.add(sid);
        if (_toInt(data['session_type']) != 1) continue;
        final members = data['members'];
        if (members is! List) continue;
        var peerId = '';
        var peerType = 0;
        var peerNickname = '';
        for (final member in members) {
          if (member is! Map) continue;
          final memberId = (member['member_id'] ?? '').toString().trim();
          if (memberId.isEmpty) continue;
          final memberType = _toInt(member['member_type']);
          if (memberType == 1 && memberId == myUserId) continue;
          peerId = memberId;
          peerType = memberType;
          peerNickname = (member['nickname'] ?? '').toString().trim();
          break;
        }
        if (peerId.isEmpty) continue;
        await _guardDbOp(
          LocalDb.updateSessionPeerIdentity(
            sid,
            peerId: peerId,
            peerType: peerType,
            peerNickname: peerNickname,
          ),
          op: 'updateSessionPeerIdentity(backfill)',
        );
        final idx = sessions.indexWhere((s) => s.sessionId == sid);
        if (idx >= 0) {
          final prev = sessions[idx];
          sessions[idx] = prev.copyWith(
            peerId: peerId,
            peerType: peerType,
            peerNickname: peerNickname.isNotEmpty
                ? peerNickname
                : prev.peerNickname,
          );
          changed = true;
        }
      }
      if (changed) {
        sessions.refresh();
      }
    } finally {
      _peerIdentityBackfillInFlight = false;
    }
    // 续跑：本轮进行期间新落库的无 peer 未读占位、或超出批量的剩余占位，
    // 不再干等下一次 loadSessions（安静期可能很久才来），立即再跑一轮，
    // 把"底部有未读数、列表无角标"的窗口收敛到一次网络往返。
    // 本链已尝试过的 sid（含瞬时失败）不再参与本轮续跑，仍走链外
    // loadSessions 的重试节奏，链长因此有界。
    tried.addAll(attemptedThisRun);
    final hasMorePending = sessions.any(
      (s) =>
          s.type == 'private' &&
          !s.isVisitor &&
          s.peerId.trim().isEmpty &&
          s.unreadCount > 0 &&
          !tried.contains(s.sessionId.trim()) &&
          !_peerIdentityBackfillAttempted.contains(s.sessionId.trim()),
    );
    if (hasMorePending) {
      unawaited(_backfillMissingPrivatePeerIdentities(triedInChain: tried));
    }
  }

  String _normalizeStoredTitle(String sessionId, String rawTitle) {
    final sid = sessionId.trim();
    final title = rawTitle.trim();
    if (title.isEmpty) return '';
    if (sid.isNotEmpty && title == sid) return '';
    return title;
  }

  String _normalizeSessionType(String type, {String fallback = 'private'}) {
    final normalized = type.trim().toLowerCase();
    if (normalized == 'group') return 'group';
    if (normalized == 'private') return 'private';
    return fallback;
  }

  String _normalizeSessionTypeFromWire(
    dynamic raw, {
    String fallback = 'private',
  }) {
    final iv = _toInt(raw);
    if (iv == 2) return 'group';
    if (iv == 1) return 'private';
    final text = raw?.toString().trim().toLowerCase() ?? '';
    if (text == 'group' || text == '2') return 'group';
    if (text == 'private' || text == '1') return 'private';
    return _normalizeSessionType(fallback);
  }

  bool _shouldUseLatestLocalMessagePreview(
    Map<String, dynamic> sessionRow, {
    required int localMessageCreatedAt,
  }) {
    final storedPreview = sessionRow['last_message']?.toString().trim() ?? '';
    if (storedPreview.isEmpty) {
      return true;
    }
    final storedPreviewTime = _requireIntLike(
      sessionRow['last_message_time'] ?? 0,
      fieldName: 'sessions.last_message_time',
    );
    if (storedPreviewTime <= 0) {
      // 无真实时间戳的预览来自服务端会话快照（snapshot 落本地时 last_message_time
      // 恒为 0）。该摘要可作为预览兜底直接展示，但本地最新可见消息带有准确时间戳、
      // 且一定是用户拉到的最新内容，存在时以本地为准更精确。
      return true;
    }
    return localMessageCreatedAt >= storedPreviewTime;
  }

  Future<void> _syncSessionsFromServer({
    int limit = 200,
    int maxPages = 5,
    bool fullSync = true,
  }) async {
    final sessionService = _sessionServiceOrNull();
    if (sessionService == null) return;
    await _ensureSessionSyncCursorLoaded();
    await _ensureDeletedSessionsLoaded();
    await _ensureRevokedSessionsLoaded();

    final result = await sessionService.fetchSessionSnapshotsResult(
      limit: limit,
      maxPages: maxPages,
    );
    final snapshots = result.snapshots;
    if (snapshots.isEmpty && !result.success) return;

    await _upsertSessionsFromServerSnapshots(snapshots);
    if (!fullSync) {
      _observeSessionWindowPaginationResult(result);
    }
    if (result.success && fullSync) {
      await _removeSessionsMissingFromServerSnapshots(snapshots);
    }
    await loadSessions(refreshFromServer: false);
    if (result.success) {
      _lastAuthoritativeSessionRefreshAtMs =
          DateTime.now().millisecondsSinceEpoch;
      // 完整拉取成功（已无更多页）→ 用服务端 cursor 作为后续增量 sync 的基线游标。
      if (result.cursor > 0) {
        _observeSessionSyncCursor(result.cursor);
      }
    }
    if (result.success && fullSync) {
      await _syncDeferredSystemUnreadBadgeAfterAuthoritativeRefresh();
    }
  }

  /// 增量同步：基于服务端 cursor 仅拉取变化的会话与被移除的会话，替代日常的全量
  /// /sessions/list 比对。无基线游标（首次 / 重置 / 会话量超全量上限未拉完）时
  /// 回退到全量拉取建立基线。删除对账由服务端 deleted_session_ids 驱动，无需整表比对。
  Future<void> _syncSessionsIncremental() async {
    if (_sessionSyncIncrementalInFlight) return;
    _sessionSyncIncrementalInFlight = true;
    try {
      final sessionService = _sessionServiceOrNull();
      if (sessionService == null) return;
      await _ensureSessionSyncCursorLoaded();
      await _ensureDeletedSessionsLoaded();
      await _ensureRevokedSessionsLoaded();

      if (_lastSessionSyncCursor <= 0) {
        await _syncSessionsFromServer(limit: 200, maxPages: 5, fullSync: true);
        return;
      }

      final result = await sessionService.fetchSessionSyncResult(
        since: _lastSessionSyncCursor,
        limit: 200,
      );
      if (!result.success) return;

      await _upsertSessionsFromServerSnapshots(result.snapshots);
      var changed = result.snapshots.isNotEmpty;
      for (final sid in result.deletedSessionIds) {
        final s = sid.trim();
        if (s.isEmpty) continue;
        _visitorSessionIds.remove(s);
        await LocalDb.deleteSessionRecord(s);
        changed = true;
      }
      if (result.cursor > 0) {
        _observeSessionSyncCursor(result.cursor);
      }
      if (changed) {
        await loadSessions(refreshFromServer: false);
      }
      _lastAuthoritativeSessionRefreshAtMs =
          DateTime.now().millisecondsSinceEpoch;
    } finally {
      _sessionSyncIncrementalInFlight = false;
    }
  }

  Future<void> refreshSessionsNow() async {
    await _syncSessionsFromServerIfNeeded(
      force: true,
      limit: 200,
      maxPages: 5,
      fullSync: true,
    );
  }

  Future<void> refreshSessionsWindowNow() async {
    final inFlight = _sessionsAuthoritativeRefreshFuture;
    if (inFlight != null) {
      return inFlight;
    }
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    if (_lastSessionWindowRefreshAttemptAtMs > 0 &&
        nowMs - _lastSessionWindowRefreshAttemptAtMs <
            ImService._sessionWindowRefreshMinInterval.inMilliseconds) {
      return;
    }
    _lastSessionWindowRefreshAttemptAtMs = nowMs;
    await _syncSessionsFromServerIfNeeded(
      force: true,
      limit: ImService._coldStartSessionSnapshotLimit,
      maxPages: ImService._coldStartSessionSnapshotMaxPages,
      fullSync: false,
    );
  }

  Future<void> refreshSessionsIfStale({
    Duration maxAge = const Duration(seconds: 45),
  }) async {
    if (sessions.isEmpty) {
      await refreshSessionsWindowNow();
      return;
    }
    if (_hasFreshAuthoritativeSessionSnapshot(maxAge)) {
      return;
    }
    // 已有增量基线游标 → 走轻量增量 sync（仅变化会话 + 删除列表），替代全量比对。
    await _ensureSessionSyncCursorLoaded();
    if (_lastSessionSyncCursor > 0) {
      await _syncSessionsIncremental();
      return;
    }
    // 尚无基线（首次或会话量超全量上限未拉完）→ 沿用窗口全量刷新，其成功路径会顺带
    // 建立基线游标，后续即可切到增量。
    await _syncSessionsFromServerIfNeeded(
      force: false,
      maxAge: maxAge,
      limit: ImService._coldStartSessionSnapshotLimit,
      maxPages: ImService._coldStartSessionSnapshotMaxPages,
      fullSync: false,
    );
  }

  bool get canLoadMoreSessionWindow {
    if (!_sessionWindowPaginationHasMore) return false;
    if (_sessionWindowPaginationInFlight) return false;
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    return nowMs >= _sessionWindowPaginationNextAllowedAtMs;
  }

  Future<bool> loadMoreSessionWindowIfNeeded({bool force = false}) async {
    final sessionService = _sessionServiceOrNull();
    if (sessionService == null) return false;
    if (!_sessionWindowPaginationHasMore) return false;
    if (_sessionWindowPaginationInFlight) return false;

    final nowMs = DateTime.now().millisecondsSinceEpoch;
    if (!force && nowMs < _sessionWindowPaginationNextAllowedAtMs) {
      return false;
    }

    _sessionWindowPaginationInFlight = true;
    try {
      final result = await sessionService.fetchSessionSnapshotPageResult(
        limit: ImService._sessionWindowPaginationLimit,
        offset: _sessionWindowPaginationNextOffset,
      );
      if (!result.success) {
        _sessionWindowPaginationNextAllowedAtMs =
            DateTime.now().millisecondsSinceEpoch +
            (result.rateLimited
                ? ImService
                      ._sessionWindowPaginationRateLimitBackoff
                      .inMilliseconds
                : ImService
                      ._sessionWindowPaginationNetworkBackoff
                      .inMilliseconds);
        return false;
      }

      await _upsertSessionsFromServerSnapshots(result.snapshots);
      _observeSessionWindowPaginationResult(result);
      await loadSessions(refreshFromServer: false);
      _sessionWindowPaginationNextAllowedAtMs =
          DateTime.now().millisecondsSinceEpoch +
          ImService._sessionWindowPaginationInterval.inMilliseconds;
      return result.snapshots.isNotEmpty;
    } finally {
      _sessionWindowPaginationInFlight = false;
    }
  }

  void _observeSessionWindowPaginationResult(
    SessionSnapshotFetchResult result,
  ) {
    final nextOffset = result.nextOffset > 0
        ? result.nextOffset
        : result.snapshots.length;
    if (nextOffset > _sessionWindowPaginationNextOffset) {
      _sessionWindowPaginationNextOffset = nextOffset;
    }
    _sessionWindowPaginationHasMore = result.hasMore;
    if (!result.hasMore) {
      _sessionWindowPaginationNextAllowedAtMs = 0;
    }
  }

  Future<void> _upsertSessionsFromServerSnapshots(
    List<SessionSnapshot> snapshots,
  ) async {
    if (snapshots.isEmpty) return;
    await _ensureDeletedSessionsLoaded();
    await _ensureRevokedSessionsLoaded();

    final nowMs = DateTime.now().millisecondsSinceEpoch;
    for (final snapshot in snapshots) {
      final sid = snapshot.sessionId.trim();
      if (sid.isEmpty) continue;
      _clearSessionLocalRevokedMark(sid);
      _clearSessionAccessRevokedReason(sid);
      final type = _normalizeSessionType(snapshot.type);
      final normalizedTitle = _normalizeStoredTitle(sid, snapshot.title);
      var unread = snapshot.unreadCount < 0 ? 0 : snapshot.unreadCount;
      // If the user recently cleared/marked unread locally but the server
      // hasn't processed the read receipt yet, the server snapshot still
      // carries the stale count.  Use the local override instead.
      final localOverride = _localUnreadOverrides[sid];
      if (localOverride != null) {
        unread = localOverride.unreadCount;
      }
      // Current session always wins — the user is actively viewing it.
      if (_isCurrentSession(sid)) {
        unread = 0;
      }
      final peerId = snapshot.peerId.trim();
      final peerType = snapshot.peerType;
      final peerNickname = snapshot.peerNickname.trim();
      final peerUsername = snapshot.peerUsername.trim();
      var sessionPinnedAt = snapshot.isPinned ? snapshot.pinnedAt : 0;
      var sessionIsPinned = snapshot.isPinned;
      var friendIsPinned = snapshot.friendIsPinned;
      var friendPinnedAt = snapshot.friendPinnedAt;
      var friendIsMuted = snapshot.friendIsMuted;
      if (_shouldSuppressDeletedSession(sid, snapshot.updatedAt)) {
        continue;
      }
      final updatedAt = snapshot.updatedAt > 0 ? snapshot.updatedAt : nowMs;

      // If the user recently changed pin state locally but the server
      // snapshot hasn't caught up yet, preserve the local override.
      // Overrides stamp `pinnedAt` with the device millisecond clock while
      // the API returns server second-level timestamps (x1000), so the
      // timestamps never compare equal; matching `isPinned` alone is enough
      // to treat the server as caught up and clear the override.
      final localPin = _localPinOverrides[sid];
      if (localPin != null) {
        if (localPin.isFriendPin) {
          if (friendIsPinned == localPin.isPinned) {
            // Server caught up — clear the override.
            _localPinOverrides.remove(sid);
          } else {
            friendIsPinned = localPin.isPinned;
            friendPinnedAt = localPin.pinnedAt;
          }
        } else {
          if (sessionIsPinned == localPin.isPinned) {
            _localPinOverrides.remove(sid);
          } else {
            sessionIsPinned = localPin.isPinned;
            sessionPinnedAt = localPin.pinnedAt;
          }
        }
      }

      if (peerId.isNotEmpty) {
        reconcilePeerMuteFromServer(peerId, friendIsMuted);
        friendIsMuted = isPeerMuted(peerId);
      }

      _sessionTypeHints[sid] = type;
      if (snapshot.isVisitor) {
        _visitorSessionIds.add(sid);
      } else {
        _visitorSessionIds.remove(sid);
      }

      await LocalDb.upsertSession({
        'session_id': sid,
        'title': normalizedTitle,
        'type': type,
        'peer_id': peerId,
        'peer_type': peerType,
        'peer_nickname': peerNickname,
        'peer_username': peerUsername,
        'updated_at': updatedAt,
        'is_pinned': sessionIsPinned,
        'is_muted': snapshot.isMuted,
        'pinned_at': sessionPinnedAt,
        'friend_is_pinned': friendIsPinned,
        'friend_pinned_at': friendPinnedAt,
        'friend_is_muted': friendIsMuted,
        'unread_count': unread,
        // 快照摘要是权威值：服务端已按与聊天页一致的口径（排除卡片与流式占位）取
        // 最后一条可预览消息，为空即该会话已无可预览消息（如被撤回/清历史），
        // 必须原样落库，否则撤回后的旧摘要会一直挂在会话列表上。
        'last_message': snapshot.lastMessage,
        // Session snapshot updated_at is not guaranteed to be the last message
        // timestamp because membership/role changes can bump it too.
        'last_message_time': 0,
      });
    }
  }

  Future<void> _removeSessionsMissingFromServerSnapshots(
    List<SessionSnapshot> snapshots,
  ) async {
    final expected = <String>{};
    for (final snapshot in snapshots) {
      final sid = snapshot.sessionId.trim();
      if (sid.isNotEmpty) {
        expected.add(sid);
      }
    }

    final rows = await LocalDb.getSessions();
    for (final row in rows) {
      final sid = (row['session_id'] ?? '').toString().trim();
      if (sid.isEmpty || expected.contains(sid)) {
        continue;
      }
      _visitorSessionIds.remove(sid);
      await LocalDb.deleteSessionRecord(sid);
    }
  }

  Future<void> bindSessionDisplayTitle(
    String sessionId,
    String title, {
    String type = 'private',
    String peerId = '',
    int peerType = 0,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final normalized = _normalizeStoredTitle(sid, title);
    if (normalized.isEmpty) return;
    await setSessionDisplayTitle(
      sid,
      normalized,
      type: type,
      peerId: peerId,
      peerType: peerType,
    );
  }

  Future<void> setSessionDisplayTitle(
    String sessionId,
    String title, {
    String type = 'private',
    String peerId = '',
    int peerType = 0,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final normalized = _normalizeStoredTitle(sid, title);
    final normalizedType = _normalizeSessionType(type);
    _sessionTypeHints[sid] = normalizedType;
    _clearSessionLocalDeleteMark(sid);

    await LocalDb.upsertSessionTitle(sid, normalized, type: normalizedType);

    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx == -1) {
      if (normalized.isEmpty) return;
      _upsertSessionAndResortInMemory(
        SessionModel(
          sessionId: sid,
          title: normalized,
          type: normalizedType,
          peerId: peerId,
          peerType: peerType,
          updatedAt: DateTime.now().millisecondsSinceEpoch,
          isPinned: false,
          pinnedAt: 0,
          unreadCount: 0,
          lastMessageTime: 0,
        ),
      );
      return;
    }

    final prev = sessions[idx];
    if (prev.title == normalized) return;
    sessions[idx] = prev.copyWith(title: normalized);
  }

  String resolveSessionDisplayTitle(SessionModel session) {
    return resolveSessionDisplayTitleById(
      session.sessionId,
      fallbackTitle: session.title,
      type: session.type,
    );
  }

  String resolveSessionTypeById(
    String sessionId, {
    String fallback = 'private',
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return _normalizeSessionType(fallback);
    }

    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx != -1) {
      return _normalizeSessionType(sessions[idx].type, fallback: fallback);
    }

    final hinted = _sessionTypeHints[sid];
    if (hinted != null && hinted.trim().isNotEmpty) {
      return _normalizeSessionType(hinted, fallback: fallback);
    }

    return _normalizeSessionType(fallback);
  }

  SessionModel? findSessionById(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return null;
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx < 0) return null;
    return sessions[idx];
  }

  Future<void> syncPrivateAgentSessions(
    Map<String, String> currentAgentNames, {
    Map<String, String> previousAgentNames = const <String, String>{},
  }) async {
    if (currentAgentNames.isEmpty || sessions.isEmpty) {
      return;
    }

    final normalizedCurrent = <String, String>{};
    for (final entry in currentAgentNames.entries) {
      final agentId = entry.key.trim();
      final agentName = entry.value.trim();
      if (agentId.isEmpty || agentName.isEmpty) {
        continue;
      }
      normalizedCurrent[agentId] = agentName;
    }
    if (normalizedCurrent.isEmpty) {
      return;
    }

    final normalizedPrevious = <String, String>{};
    for (final entry in previousAgentNames.entries) {
      final agentId = entry.key.trim();
      final agentName = entry.value.trim();
      if (agentId.isEmpty || agentName.isEmpty) {
        continue;
      }
      normalizedPrevious[agentId] = agentName;
    }

    final updatedSessions = <SessionModel>[];
    for (var i = 0; i < sessions.length; i++) {
      final session = sessions[i];
      if (session.type != 'private' || session.peerType != 2) {
        continue;
      }

      final agentId = session.peerId.trim();
      final currentAgentName = normalizedCurrent[agentId];
      if (currentAgentName == null) {
        continue;
      }

      final previousAgentName = normalizedPrevious[agentId] ?? '';
      final nextSession = _syncPrivateAgentSessionSnapshot(
        session,
        currentAgentName: currentAgentName,
        previousAgentName: previousAgentName,
      );
      if (nextSession == null) {
        continue;
      }

      sessions[i] = nextSession;
      updatedSessions.add(nextSession);
    }

    for (final session in updatedSessions) {
      await LocalDb.upsertSession(session.toJson());
    }
  }

  SessionModel? _syncPrivateAgentSessionSnapshot(
    SessionModel session, {
    required String currentAgentName,
    required String previousAgentName,
  }) {
    final currentPeerNickname = session.peerNickname.trim();
    final nextPeerNickname = _syncPrivateAgentPeerNickname(
      currentPeerNickname: currentPeerNickname,
      currentAgentName: currentAgentName,
      previousAgentName: previousAgentName,
    );
    final nextTitle = _syncPrivateAgentSessionTitle(
      session,
      currentTitle: session.title.trim(),
      currentPeerNickname: currentPeerNickname,
      nextPeerNickname: nextPeerNickname,
      previousAgentName: previousAgentName,
    );

    if (nextPeerNickname == currentPeerNickname &&
        nextTitle == session.title.trim()) {
      return null;
    }

    return session.copyWith(title: nextTitle, peerNickname: nextPeerNickname);
  }

  String _syncPrivateAgentPeerNickname({
    required String currentPeerNickname,
    required String currentAgentName,
    required String previousAgentName,
  }) {
    if (currentPeerNickname.isEmpty) {
      return currentAgentName;
    }
    if (previousAgentName.isNotEmpty &&
        currentPeerNickname == previousAgentName) {
      return currentAgentName;
    }
    return currentPeerNickname;
  }

  String _syncPrivateAgentSessionTitle(
    SessionModel session, {
    required String currentTitle,
    required String currentPeerNickname,
    required String nextPeerNickname,
    required String previousAgentName,
  }) {
    final sid = session.sessionId.trim();
    if (currentTitle.isEmpty || currentTitle == sid) {
      return nextPeerNickname;
    }
    if (currentPeerNickname.isNotEmpty && currentTitle == currentPeerNickname) {
      return nextPeerNickname;
    }
    if (previousAgentName.isNotEmpty && currentTitle == previousAgentName) {
      return nextPeerNickname;
    }
    return currentTitle;
  }

  PeerPresenceState resolveSessionPeerPresence(String sessionId) {
    final session = findSessionById(sessionId);
    if (session == null || session.type != 'private') {
      return PeerPresenceState.unknown;
    }
    if (session.peerType != 2) {
      return PeerPresenceState.unknown;
    }
    return isAgentChannelOnline(session.peerId)
        ? PeerPresenceState.online
        : PeerPresenceState.offline;
  }

  bool isAgentChannelOnline(String agentId) {
    _agentStateExpiryTick.value;
    if (!_isConnected.value || !_isAuthenticated.value) {
      return false;
    }
    final normalizedAgentId = agentId.trim();
    if (normalizedAgentId.isEmpty) {
      return false;
    }
    final state = agentStates[normalizedAgentId];
    if (state == null) {
      return false;
    }
    if ((state['state']?.toString().trim() ?? '') != 'online') {
      return false;
    }
    final leaseUntil = _toInt(state['lease_until']);
    if (leaseUntil <= 0) {
      return false;
    }
    return DateTime.now().millisecondsSinceEpoch < leaseUntil;
  }

  bool hasAgentChannelState(String agentId) {
    _agentStateExpiryTick.value;
    final normalizedAgentId = agentId.trim();
    if (normalizedAgentId.isEmpty) {
      return false;
    }
    return agentStates.containsKey(normalizedAgentId);
  }

  bool hasSessionTypeHint(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    if (_sessionTypeHints.containsKey(sid)) return true;
    return sessions.any((s) => s.sessionId == sid);
  }

  bool _hasSessionDisplayTitle(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx == -1) return false;
    final normalized = _normalizeStoredTitle(sid, sessions[idx].title);
    return normalized.isNotEmpty;
  }

  bool hasSessionDisplayTitleById(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    return _hasSessionDisplayTitle(sid);
  }

  int getSessionMemberEventVersion(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return 0;
    return sessionMemberEventVersions[sid] ?? 0;
  }

  void _bumpSessionMemberEventVersion(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    sessionMemberEventVersions[sid] =
        (sessionMemberEventVersions[sid] ?? 0) + 1;
    _invalidateSessionDetailCache(sid);
  }

  void _invalidateSessionDetailCache(String sessionId) {
    if (!Get.isRegistered<SessionService>()) return;
    Get.find<SessionService>().invalidateSessionDetailCache(sessionId);
  }

  int getSessionAccessRevokedVersion(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return 0;
    return sessionAccessRevokedVersions[sid] ?? 0;
  }

  String getSessionAccessRevokedReason(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return '';
    return _sessionAccessRevokedReasons[sid] ?? '';
  }

  int getSessionReadVersion(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return 0;
    return sessionReadVersions[sid] ?? 0;
  }

  String getSessionReadCursor(String sessionId, String memberId) {
    final sid = sessionId.trim();
    final mid = memberId.trim();
    if (sid.isEmpty || mid.isEmpty) return '';
    final sessionState = _sessionReadCursorBySession[sid];
    if (sessionState == null) return '';
    return sessionState[mid] ?? '';
  }

  // 雪花消息号在 Web 端不能转 int（精度丢失），统一以字符串归一化：有效正值原样
  // 返回，非法/空值返回空串。
  String _parseServerMessageId(String rawMsgId) {
    final normalized = rawMsgId.trim();
    return _isValidMsgId(normalized) ? normalized : '';
  }

  String _resolveCurrentSessionReadBoundaryFromMemory(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty || sid != currentSessionId) {
      return '';
    }
    var maxMsgId = '';
    for (final message in currentMessages) {
      final parsed = _parseServerMessageId(message.msgId);
      if (_isValidMsgId(parsed) && _compareMsgId(parsed, maxMsgId) > 0) {
        maxMsgId = parsed;
      }
    }
    return maxMsgId;
  }

  Future<String> _resolveSessionReadBoundary(String sessionId) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return '';

    final currentBoundary = _resolveCurrentSessionReadBoundaryFromMemory(sid);
    if (_isValidMsgId(currentBoundary)) {
      return currentBoundary;
    }

    return LocalDb.getLatestServerMessageId(sid);
  }

  Future<void> _queueSessionReadByKnownBoundary(String sessionId) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final lastReadMsgId = await _resolveSessionReadBoundary(sid);
    if (!_isValidMsgId(lastReadMsgId)) return;
    _markSessionReadRemote(sid, lastReadMsgId: lastReadMsgId);
  }

  String resolveSessionDisplayTitleById(
    String sessionId, {
    String fallbackTitle = '',
    String type = 'private',
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return '';

    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx != -1) {
      final session = sessions[idx];
      // 私聊会话优先返回 peer 显示名（peerNickname/peerUsername），
      // 与左侧会话列表 _getConversationPrimaryTitle 保持一致；
      // session.title 仅作为最终回退。
      if (session.type == 'private') {
        final peerNickname = session.peerNickname.trim();
        if (peerNickname.isNotEmpty) return peerNickname;
        final peerUsername = session.peerUsername.trim();
        if (peerUsername.isNotEmpty) return peerUsername;
      }
      final normalizedFromSession = _normalizeStoredTitle(sid, session.title);
      if (normalizedFromSession.isNotEmpty) {
        return normalizedFromSession;
      }
      // 私聊会话：session.title 为空时回退到 peerNickname/peerUsername，
      // 与聊天页 AppBar 标题解析保持一致。
      if (session.type == 'private') {
        final peerNickname = session.peerNickname.trim();
        if (peerNickname.isNotEmpty) return peerNickname;
        final peerUsername = session.peerUsername.trim();
        if (peerUsername.isNotEmpty) return peerUsername;
      }
      type = session.type;
    }

    final normalizedFallback = _normalizeStoredTitle(sid, fallbackTitle);
    if (normalizedFallback.isNotEmpty) {
      return normalizedFallback;
    }

    return sid;
  }

  void clearUnread(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    LocalDb.clearUnread(sid);
    _localUnreadOverrides[sid] = _LocalUnreadOverride(
      unreadCount: 0,
      setAtMs: DateTime.now().millisecondsSinceEpoch,
    );
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx != -1 && sessions[idx].unreadCount != 0) {
      sessions[idx] = sessions[idx].copyWith(unreadCount: 0);
    }
    unawaited(_queueSessionReadByKnownBoundary(sid));
  }

  void markUnread(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    LocalDb.setUnreadCount(sid, 1);
    _localUnreadOverrides[sid] = _LocalUnreadOverride(
      unreadCount: 1,
      setAtMs: DateTime.now().millisecondsSinceEpoch,
    );
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx != -1) {
      sessions[idx] = sessions[idx].copyWith(unreadCount: 1);
    }
  }

  Future<void> _setSessionUnreadCountLocal(
    String sessionId,
    int unreadCount,
  ) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    // If there is a local override (clearUnread/markUnread), the server's
    // authoritative count may be stale — honour the local override instead.
    final override = _localUnreadOverrides[sid];
    final effectiveUnread = override != null
        ? override.unreadCount
        : unreadCount;
    final normalizedUnread = _normalizeUnreadForSession(sid, effectiveUnread);
    await _guardDbOp(
      LocalDb.setUnreadCount(sid, normalizedUnread),
      op: 'setUnreadCount(local)',
    );
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx != -1) {
      sessions[idx] = sessions[idx].copyWith(unreadCount: normalizedUnread);
      _resortSessionsInMemory();
    }
  }

  /// 批量同步服务端未读：单次内存发布 + 单事务 DB。
  Future<void> syncSessionUnreadCountsFromServerBatch(
    Iterable<SessionModel> serverSessions,
  ) async {
    final updates = <String, int>{};
    for (final session in serverSessions) {
      final sid = session.sessionId.trim();
      if (sid.isEmpty) continue;
      final override = _localUnreadOverrides[sid];
      final effectiveUnread = override != null
          ? override.unreadCount
          : session.unreadCount;
      updates[sid] = _normalizeUnreadForSession(sid, effectiveUnread);
    }
    if (updates.isEmpty) return;

    final next = List<SessionModel>.of(sessions);
    var changed = false;
    for (var i = 0; i < next.length; i++) {
      final unread = updates[next[i].sessionId];
      if (unread == null || next[i].unreadCount == unread) continue;
      next[i] = next[i].copyWith(unreadCount: unread);
      changed = true;
    }
    if (changed) {
      if (next.length > 1) {
        next.sort(SessionModel.compareByPriority);
      }
      // 一次赋值，只触发一次 sessions Rx（随后列表 debounce 对齐至多一次）。
      sessions.value = next;
    }

    await _guardDbOp(
      LocalDb.setUnreadCounts(updates),
      op: 'setUnreadCounts(batch)',
    );
  }

  Future<bool> setSessionPinned(
    String sessionId, {
    required bool isPinned,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    final sessionService = _sessionServiceOrNull();
    if (sessionService == null) return false;

    final result = await sessionService.setSessionPinnedResult(
      sid,
      isPinned: isPinned,
    );
    if (result.code != 0) {
      return false;
    }

    final effectivePinnedAt = isPinned
        ? (result.pinnedAt > 0
              ? result.pinnedAt
              : DateTime.now().millisecondsSinceEpoch)
        : 0;

    await LocalDb.setSessionPinned(
      sid,
      isPinned: isPinned,
      pinnedAt: effectivePinnedAt,
    );

    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx >= 0) {
      sessions[idx] = sessions[idx].copyWith(
        isPinned: isPinned,
        pinnedAt: effectivePinnedAt,
      );
      _resortSessionsInMemory();
    }
    // Register local pin override so that subsequent server syncs
    // don't overwrite this pin state until the server catches up.
    _localPinOverrides[sid] = _LocalPinOverride(
      isPinned: isPinned,
      isFriendPin: false,
      pinnedAt: effectivePinnedAt,
      setAtMs: DateTime.now().millisecondsSinceEpoch,
    );
    return true;
  }

  Future<void> applyLocalFriendPin({
    required List<String> sessionIds,
    required bool isPinned,
    required int pinnedAt,
  }) async {
    final effectivePinnedAt = isPinned
        ? (pinnedAt > 0 ? pinnedAt : DateTime.now().millisecondsSinceEpoch)
        : 0;
    final result = await _writeFriendPinLocal(
      sessionIds: sessionIds,
      isPinned: isPinned,
      pinnedAt: effectivePinnedAt,
    );
    if (result.ids.isEmpty) return;
    registerFriendPinOverrides(
      result.ids,
      isPinned: isPinned,
      pinnedAt: effectivePinnedAt,
    );
    if (result.wrote) {
      _resortSessionsInMemory();
    }
  }

  Future<void> applyLocalFriendMute({
    required String peerId,
    required List<String> sessionIds,
    required bool isMuted,
  }) async {
    final normalizedPeer = peerId.trim();
    if (normalizedPeer.isNotEmpty) {
      _peerMuteState[normalizedPeer] = isMuted;
      _peerMuteOverrides[normalizedPeer] = isMuted;
    }

    final ids = <String>{};
    for (final raw in sessionIds) {
      final sid = raw.trim();
      if (sid.isNotEmpty) ids.add(sid);
    }
    if (normalizedPeer.isNotEmpty) {
      for (final session in sessions) {
        if (session.type == 'private' &&
            session.peerId.trim() == normalizedPeer) {
          ids.add(session.sessionId);
        }
      }
    }
    if (ids.isEmpty) return;

    var wrote = false;
    for (final sid in ids) {
      final idx = sessions.indexWhere((s) => s.sessionId == sid);
      if (idx >= 0 && sessions[idx].friendIsMuted == isMuted) {
        continue;
      }
      await LocalDb.setFriendMuted(sid, isMuted: isMuted);
      wrote = true;
      if (idx >= 0) {
        sessions[idx] = sessions[idx].copyWith(friendIsMuted: isMuted);
      }
    }
    if (wrote) {
      _resortSessionsInMemory();
    }
  }

  Future<({List<String> ids, bool wrote})> _writeFriendPinLocal({
    required List<String> sessionIds,
    required bool isPinned,
    required int pinnedAt,
  }) async {
    final normalizedIds = <String>[];
    for (final raw in sessionIds) {
      final sid = raw.trim();
      if (sid.isEmpty || normalizedIds.contains(sid)) continue;
      normalizedIds.add(sid);
    }
    var wrote = false;
    for (final sid in normalizedIds) {
      final idx = sessions.indexWhere((s) => s.sessionId == sid);
      if (idx >= 0) {
        final prev = sessions[idx];
        final alreadyMatches =
            prev.friendIsPinned == isPinned &&
            (isPinned ? prev.friendPinnedAt == pinnedAt : true);
        if (alreadyMatches) {
          continue;
        }
      }
      await LocalDb.setFriendPinned(
        sid,
        isPinned: isPinned,
        pinnedAt: pinnedAt,
      );
      wrote = true;
      if (idx >= 0) {
        sessions[idx] = sessions[idx].copyWith(
          friendIsPinned: isPinned,
          friendPinnedAt: pinnedAt,
        );
      }
    }
    return (ids: normalizedIds, wrote: wrote);
  }

  /// Persist `/sessions/conversations` pin truth into LocalDb/memory.
  ///
  /// Conversation list API previously only refreshed UI memory; LocalDb kept
  /// stale `is_pinned` / `friend_is_pinned`. Weak-network fallback then rebuilt
  /// from LocalDb and resurrected old pins. When the loaded pages already cover
  /// the full pinned set (no more pages, or an unpinned row appears — server
  /// sorts pins first), clear any other local pins not in that set.
  ///
  /// Recent local pin overrides win over a still-stale conversations page, same
  /// as snapshot upsert: only clear an override once API pin state matches it.
  ///
  /// Pin dimensions coexist: the friend-level pin (`friend_is_pinned`) drives
  /// the main conversation list, while the session-level pin (`is_pinned`) is
  /// owned by the profile-page series list and the thread popup. Reconcile
  /// only maintains the friend-level dimension for private chats and never
  /// clears session-level pins.
  Future<void> reconcilePinsFromConversationSummaries(
    List<ConversationSummaryModel> items, {
    required bool hasMore,
  }) async {
    final authoritativePinnedSessionIds = <String>{};
    final authoritativePinnedPeerIds = <String>{};
    var mutated = false;

    for (final summary in items) {
      final latest = summary.toLatestSessionModel();
      final sid = latest.sessionId.trim();
      if (sid.isEmpty) continue;

      if (latest.type == 'private') {
        final peerId = latest.peerId.trim();
        final targetIds = <String>{sid};
        if (peerId.isNotEmpty) {
          for (final session in sessions) {
            if (session.type == 'private' && session.peerId.trim() == peerId) {
              targetIds.add(session.sessionId);
            }
          }
        }
        final effectivePinnedAt = latest.friendIsPinned
            ? (latest.friendPinnedAt > 0
                  ? latest.friendPinnedAt
                  : DateTime.now().millisecondsSinceEpoch)
            : 0;

        final overrideDecision = _resolvePinOverrideForTargets(
          targetIds,
          isFriendPin: true,
          apiIsPinned: latest.friendIsPinned,
        );
        if (overrideDecision.keepLocalOverride) {
          if (overrideDecision.effectiveIsPinned && peerId.isNotEmpty) {
            authoritativePinnedPeerIds.add(peerId);
          }
          continue;
        }

        final result = await _writeFriendPinLocal(
          sessionIds: targetIds.toList(growable: false),
          isPinned: latest.friendIsPinned,
          pinnedAt: effectivePinnedAt,
        );
        mutated = mutated || result.wrote;
        // Friend-level pin drives the main conversation list only. The
        // session-level `is_pinned` is owned by the profile-page series list
        // and the thread popup, so reconcile must not clear it here.
        if (latest.friendIsPinned && peerId.isNotEmpty) {
          authoritativePinnedPeerIds.add(peerId);
        }
      } else {
        final effectivePinnedAt = latest.isPinned
            ? (latest.pinnedAt > 0
                  ? latest.pinnedAt
                  : DateTime.now().millisecondsSinceEpoch)
            : 0;
        final overrideDecision = _resolvePinOverrideForTargets(
          <String>{sid},
          isFriendPin: false,
          apiIsPinned: latest.isPinned,
        );
        if (overrideDecision.keepLocalOverride) {
          if (overrideDecision.effectiveIsPinned) {
            authoritativePinnedSessionIds.add(sid);
          }
          continue;
        }

        final idx = sessions.indexWhere((s) => s.sessionId == sid);
        final alreadyMatches =
            idx >= 0 &&
            sessions[idx].isPinned == latest.isPinned &&
            (latest.isPinned
                ? sessions[idx].pinnedAt == effectivePinnedAt
                : true);
        if (!alreadyMatches) {
          await LocalDb.setSessionPinned(
            sid,
            isPinned: latest.isPinned,
            pinnedAt: effectivePinnedAt,
          );
          if (idx >= 0) {
            sessions[idx] = sessions[idx].copyWith(
              isPinned: latest.isPinned,
              pinnedAt: effectivePinnedAt,
            );
          }
          mutated = true;
        }
        if (latest.isPinned) {
          authoritativePinnedSessionIds.add(sid);
        }
      }
    }

    // Pins sort ahead of unpinned rows server-side. Once an unpinned row is
    // visible (or the list is exhausted), loaded pages hold every pin.
    final pinSetComplete = !hasMore || items.any((item) => !item.isPinned);
    if (!pinSetComplete) {
      if (mutated) {
        _resortSessionsInMemory();
      }
      return;
    }

    final rows = await LocalDb.getSessions();
    for (final row in rows) {
      final sid = (row['session_id'] ?? '').toString().trim();
      if (sid.isEmpty) continue;
      final override = _localPinOverrides[sid];
      if (override != null && override.isPinned) {
        // Preserve a newer local pin that has not reached the server yet.
        continue;
      }

      final type = _normalizeSessionType(
        row['type']?.toString() ?? '',
        fallback: _sessionTypeHints[sid] ?? 'private',
      );
      if (type == 'private') {
        final peerId = (row['peer_id'] ?? '').toString().trim();
        final shouldPin =
            peerId.isNotEmpty && authoritativePinnedPeerIds.contains(peerId);
        final currentFriendPinned = _toBool(row['friend_is_pinned']);
        if (currentFriendPinned != shouldPin) {
          await LocalDb.setFriendPinned(
            sid,
            isPinned: shouldPin,
            pinnedAt: shouldPin ? _toInt(row['friend_pinned_at']) : 0,
          );
          final idx = sessions.indexWhere((s) => s.sessionId == sid);
          if (idx >= 0) {
            sessions[idx] = sessions[idx].copyWith(
              friendIsPinned: shouldPin,
              friendPinnedAt: shouldPin ? sessions[idx].friendPinnedAt : 0,
            );
          }
          mutated = true;
        }
        // Session-level `is_pinned` is independent from the friend-level pin
        // and is preserved here on purpose (series list / thread popup).
      } else {
        final shouldPin = authoritativePinnedSessionIds.contains(sid);
        final currentPinned = _toBool(row['is_pinned']);
        if (currentPinned == shouldPin) continue;
        await LocalDb.setSessionPinned(
          sid,
          isPinned: shouldPin,
          pinnedAt: shouldPin ? _toInt(row['pinned_at']) : 0,
        );
        final idx = sessions.indexWhere((s) => s.sessionId == sid);
        if (idx >= 0) {
          sessions[idx] = sessions[idx].copyWith(
            isPinned: shouldPin,
            pinnedAt: shouldPin ? sessions[idx].pinnedAt : 0,
          );
        }
        mutated = true;
      }
    }

    if (mutated) {
      _resortSessionsInMemory();
    }
  }

  /// Align conversations-page pin writes with snapshot upsert override rules.
  _PinOverrideDecision _resolvePinOverrideForTargets(
    Set<String> targetIds, {
    required bool isFriendPin,
    required bool apiIsPinned,
  }) {
    _LocalPinOverride? matched;
    for (final sid in targetIds) {
      final override = _localPinOverrides[sid];
      if (override == null || override.isFriendPin != isFriendPin) {
        continue;
      }
      matched = override;
      break;
    }
    if (matched == null) {
      return const _PinOverrideDecision(
        keepLocalOverride: false,
        effectiveIsPinned: false,
      );
    }
    // Local overrides stamp `pinnedAt` with the device millisecond clock
    // while the API returns server second-level timestamps (x1000), so an
    // exact timestamp match almost never holds. Matching `isPinned` alone is
    // enough to consider the server caught up and drop the override.
    if (matched.isPinned == apiIsPinned) {
      for (final sid in targetIds) {
        final override = _localPinOverrides[sid];
        if (override != null && override.isFriendPin == isFriendPin) {
          _localPinOverrides.remove(sid);
        }
      }
      return _PinOverrideDecision(
        keepLocalOverride: false,
        effectiveIsPinned: apiIsPinned,
      );
    }
    return _PinOverrideDecision(
      keepLocalOverride: true,
      effectiveIsPinned: matched.isPinned,
    );
  }

  Future<bool> setSessionMuted(
    String sessionId, {
    required bool isMuted,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    final sessionService = _sessionServiceOrNull();
    if (sessionService == null) return false;

    final result = await sessionService.setSessionMutedResult(
      sid,
      isMuted: isMuted,
    );
    if (result.code != 0) {
      return false;
    }

    await LocalDb.setSessionMuted(sid, isMuted: isMuted);

    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx >= 0) {
      sessions[idx] = sessions[idx].copyWith(isMuted: isMuted);
    }
    return true;
  }

  Future<void> _deleteConversationImpl(String sessionId) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    await _ensureDeletedSessionsLoaded();
    final deletedAt = DateTime.now().millisecondsSinceEpoch;
    _locallyDeletedSessions[sid] = deletedAt;
    _persistDeletedSessions();
    _sendSessionHistoryReset(sid, deletedAt);
    unawaited(_queueSessionReadByKnownBoundary(sid));
    await _removeSessionLocally(sid, preserveMessages: false);
  }

  Future<void> revokeSessionAccess(String sessionId) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    await _ensureRevokedSessionsLoaded();
    _locallyRevokedSessions[sid] = DateTime.now().millisecondsSinceEpoch;
    _persistRevokedSessions();
    await _removeSessionLocally(sid, preserveMessages: true);
  }

  Future<void> _handleSessionMemberChangedEvent(
    String sessionId,
    Map<String, dynamic> payload,
  ) async {
    final action = payload['action']?.toString().trim() ?? '';
    if (!_isSessionMemberChangeActionSupported(action)) {
      return;
    }

    final myUserId = Get.isRegistered<AuthService>()
        ? (Get.find<AuthService>().userId?.trim() ?? '')
        : '';
    if (myUserId.isEmpty) {
      return;
    }

    final removedUserIDs = _parseIdSet(payload['removed_user_ids']);
    if (action == 'dissolve') {
      final appliesToMe =
          removedUserIDs.isEmpty || removedUserIDs.contains(myUserId);
      if (!appliesToMe) {
        return;
      }
      _markSessionAccessRevoked(sessionId, reason: 'dissolve');
      final service = this;
      await service.deleteConversation(sessionId);
      return;
    }

    if (action == 'rename') {
      if (!_hasSessionLocal(sessionId)) {
        return;
      }
      final incomingTitle = payload['title']?.toString() ?? '';
      final normalizedType = resolveSessionTypeById(sessionId);
      if (incomingTitle.trim().isNotEmpty) {
        await setSessionDisplayTitle(
          sessionId,
          incomingTitle,
          type: normalizedType,
        );
        return;
      }

      await setSessionDisplayTitle(sessionId, '', type: normalizedType);
      await refreshSessionsNow();
      return;
    }

    if (action == 'nickname') {
      return;
    }

    if (action == 'convert') {
      // 本地若还没同步到这条私聊，先兜底拉取，避免转换通知被静默丢弃（比照 add 分支）。
      final ready = await _ensureSessionPresentForAddEvent(sessionId);
      if (!ready) {
        return;
      }
      final incomingTitle = payload['title']?.toString() ?? '';
      // 本地标记为群聊并从服务端拉取最新会话类型/成员。
      await setSessionDisplayTitle(sessionId, incomingTitle, type: 'group');
      await refreshSessionsNow();
      await _appendSessionMemberChangedSystemMessage(
        sessionId,
        action: action,
        payload: payload,
      );
      return;
    }

    if (action == 'add') {
      final ready = await _ensureSessionPresentForAddEvent(sessionId);
      if (!ready) {
        return;
      }
      await _appendSessionMemberChangedSystemMessage(
        sessionId,
        action: action,
        payload: payload,
      );
      return;
    }

    if (action == 'role' || action == 'transfer_owner') {
      await _appendSessionMemberChangedSystemMessage(
        sessionId,
        action: action,
        payload: payload,
      );
      return;
    }

    if (removedUserIDs.isNotEmpty) {
      if (!removedUserIDs.contains(myUserId)) {
        await _appendSessionMemberChangedSystemMessage(
          sessionId,
          action: action,
          payload: payload,
        );
        return;
      }
      _markSessionAccessRevoked(sessionId, reason: 'removed');
      final service = this;
      await service.deleteConversation(sessionId);
      return;
    }

    if (!_hasSessionLocal(sessionId)) {
      return;
    }
    await _probeSessionAccessAfterMemberChange(sessionId);
    if (!_hasSessionLocal(sessionId)) {
      return;
    }
    await _appendSessionMemberChangedSystemMessage(
      sessionId,
      action: action,
      payload: payload,
    );
  }

  bool _isSessionMemberChangeActionSupported(String action) {
    return action == 'add' ||
        action == 'remove' ||
        action == 'role' ||
        action == 'transfer_owner' ||
        action == 'rename' ||
        action == 'nickname' ||
        action == 'convert' ||
        action == 'dissolve';
  }

  bool _hasSessionLocal(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    if (_currentSessionId.value == sid) return true;
    return sessions.any((s) => s.sessionId == sid);
  }

  Future<bool> _ensureSessionPresentForAddEvent(String sessionId) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    if (_hasSessionLocal(sid)) return true;
    await refreshSessionsNow();
    if (_hasSessionLocal(sid)) return true;
    return _ensureSessionInMemoryFromLocalStore(sid);
  }

  Future<bool> _ensureSessionInMemoryFromLocalStore(String sessionId) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    if (_hasSessionLocal(sid)) return true;

    final rows = await LocalDb.getSessions();
    Map<String, dynamic>? matched;
    for (final row in rows) {
      final rowSid = (row['session_id'] ?? '').toString().trim();
      if (rowSid != sid) {
        continue;
      }
      matched = Map<String, dynamic>.from(row);
      break;
    }
    if (matched == null) {
      return false;
    }

    final type = _normalizeSessionType(
      matched['type']?.toString() ?? '',
      fallback: _sessionTypeHints[sid] ?? 'private',
    );
    final model = SessionModel.fromJson({
      'session_id': sid,
      'title': _normalizeStoredTitle(sid, matched['title']?.toString() ?? ''),
      'type': type,
      'peer_id': matched['peer_id']?.toString() ?? '',
      'peer_type': _toInt(matched['peer_type']),
      'peer_nickname': matched['peer_nickname']?.toString() ?? '',
      'peer_username': matched['peer_username']?.toString() ?? '',
      'updated_at': _toInt(matched['updated_at']),
      'is_pinned': _toBool(matched['is_pinned']),
      'is_muted': _toBool(matched['is_muted']),
      'pinned_at': _toInt(matched['pinned_at']),
      'friend_is_muted': _toBool(matched['friend_is_muted']),
      'unread_count': _normalizeUnreadForSession(
        sid,
        _toInt(matched['unread_count']),
      ),
      'last_message': matched['last_message']?.toString() ?? '',
      'last_message_time': _toInt(matched['last_message_time']),
    });
    _sessionTypeHints[sid] = type;

    _upsertSessionAndResortInMemory(model);
    return true;
  }

  Set<String> _parseIdSet(dynamic raw) {
    if (raw is! List) return const <String>{};
    final ids = <String>{};
    for (final item in raw) {
      final id = _toId(item);
      if (id.isEmpty) continue;
      ids.add(id);
    }
    return ids;
  }

  Future<void> _appendSessionMemberChangedSystemMessage(
    String sessionId, {
    required String action,
    required Map<String, dynamic> payload,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (!_hasSessionLocal(sid)) {
      final hydrated = await _ensureSessionInMemoryFromLocalStore(sid);
      if (!hydrated) {
        return;
      }
    }

    final content = _resolveSessionMemberChangedSystemText(action);
    if (content.isEmpty) return;

    final ts = _toInt(payload['updated_at']);
    final createdAt = (ts > 0 && ts < 10000000000)
        ? ts * 1000
        : (ts > 0 ? ts : DateTime.now().millisecondsSinceEpoch);
    final msgId = 'sys_evt_${const Uuid().v4()}';
    final dict = <String, dynamic>{
      'msg_id': msgId,
      'session_id': sid,
      'sender_id': '0',
      'msg_type': 3,
      'content': content,
      'created_at': createdAt,
    };

    await LocalDb.upsertMessage(dict);
    final msgModel = MessageModel.fromJson(dict);
    LocalDbChangeBus.instance.emitMessageChange(
      LocalMessagesInserted(
        sessionId: sid,
        msgIds: [msgId],
        maxCreatedAt: createdAt,
        rows: [dict],
      ),
    );
    await _touchSessionByMessage(
      msgModel,
      increaseUnread: !_isCurrentSession(sid),
    );
  }

  String _resolveSessionMemberChangedSystemText(String action) {
    switch (action) {
      case 'add':
        return 'chat_system_member_added'.tr;
      case 'remove':
        return 'chat_system_member_removed'.tr;
      case 'role':
        return 'chat_system_member_role_changed'.tr;
      case 'transfer_owner':
        return 'chat_system_owner_transferred'.tr;
      case 'dissolve':
        return 'chat_system_group_dissolved'.tr;
      case 'convert':
        return 'chat_system_converted_to_group'.tr;
      default:
        return '';
    }
  }

  Future<void> _probeSessionAccessAfterMemberChange(String sessionId) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (!_inflightSessionAccessProbe.add(sid)) {
      return;
    }
    try {
      final sessionService = _sessionServiceOrNull();
      if (sessionService == null) {
        return;
      }
      final detailResult = await sessionService.fetchSessionDetailResult(sid);
      if (detailResult.code == 4003 || detailResult.code == 4004) {
        _markSessionAccessRevoked(sid, reason: 'revoked');
        final service = this;
        await service.deleteConversation(sid);
      }
    } catch (e) {
      debugPrint('Probe session access failed sid=$sid err=$e');
    } finally {
      _inflightSessionAccessProbe.remove(sid);
    }
  }

  void _markSessionAccessRevoked(
    String sessionId, {
    String reason = 'removed',
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    _sessionAccessRevokedReasons[sid] = reason.trim();
    sessionAccessRevokedVersions[sid] =
        (sessionAccessRevokedVersions[sid] ?? 0) + 1;
  }

  void _applySessionReadSync(Map<String, dynamic> payload) {
    final sid = payload['session_id']?.toString().trim() ?? '';
    final readerId = payload['reader_id']?.toString().trim() ?? '';
    final lastReadMsgId = payload['last_read_msg_id']?.toString().trim() ?? '';
    if (sid.isEmpty || readerId.isEmpty || !_isValidMsgId(lastReadMsgId)) {
      return;
    }

    final sessionState = _sessionReadCursorBySession.putIfAbsent(
      sid,
      () => <String, String>{},
    );
    final existing = sessionState[readerId] ?? '';
    if (_compareMsgId(lastReadMsgId, existing) <= 0) {
      return;
    }

    sessionState[readerId] = lastReadMsgId;
    sessionReadVersions[sid] = (sessionReadVersions[sid] ?? 0) + 1;

    // Sync unread count when the server broadcasts this to the reader's
    // other devices.  UnreadCount is non-nil only for the reader herself;
    // group-chat peer broadcasts omit it.
    final unreadCountVal = payload['unread_count'];
    if (unreadCountVal == null) return;
    final myUserId = Get.find<AuthService>().userId?.trim() ?? '';
    if (myUserId.isEmpty || readerId != myUserId) return;

    final serverUnread = _toInt(unreadCountVal);
    final normalized = serverUnread < 0 ? 0 : serverUnread;
    if (normalized == 0) {
      _localUnreadOverrides.remove(sid);
      _pendingReadStatesBySession.remove(sid);
      _persistPendingReadStates();
    }

    final override = _localUnreadOverrides[sid];
    final effectiveUnread = override != null
        ? override.unreadCount
        : normalized;
    final normalizedUnread = _normalizeUnreadForSession(sid, effectiveUnread);

    LocalDb.setUnreadCount(sid, normalizedUnread);
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx != -1 && sessions[idx].unreadCount != normalizedUnread) {
      sessions[idx] = sessions[idx].copyWith(unreadCount: normalizedUnread);
      _resortSessionsInMemory();
    }

    unawaited(syncSystemUnreadBadgeNow());
  }

  void _applyUnreadSync(Map<String, dynamic> payload) {
    final sid = payload['session_id']?.toString().trim() ?? '';
    final unreadCount = _toInt(payload['unread_count']);
    if (sid.isEmpty) return;

    final normalized = unreadCount < 0 ? 0 : unreadCount;

    // When server confirms read (normalized == 0), clear the local override
    // and pending read state — the server has acknowledged the read receipt.
    // For non-zero values, keep the override if present — the server value
    // may be stale (read receipt still in flight). The override will be
    // cleared later by _handleSessionReadAck or a subsequent unread_sync(0).
    if (normalized == 0) {
      _localUnreadOverrides.remove(sid);
      _pendingReadStatesBySession.remove(sid);
      _persistPendingReadStates();
    }

    final override = _localUnreadOverrides[sid];
    final effectiveUnread = override != null
        ? override.unreadCount
        : normalized;
    final normalizedUnread = _normalizeUnreadForSession(sid, effectiveUnread);

    LocalDb.setUnreadCount(sid, normalizedUnread);
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx != -1 && sessions[idx].unreadCount != normalizedUnread) {
      sessions[idx] = sessions[idx].copyWith(unreadCount: normalizedUnread);
      _resortSessionsInMemory();
    }

    unawaited(syncSystemUnreadBadgeNow());
  }

  Future<void> _touchSessionByMessage(
    MessageModel msg, {
    required bool increaseUnread,
  }) async {
    final type = _sessionTypeHints[msg.sessionId] ?? 'private';
    if (msg.msgType == 4) {
      // Tool/streaming-placeholder messages (tool card JSON or empty streaming
      // capsules) are not human-readable previews and must never overwrite the
      // session preview text or bump the unread counter. They MUST, however,
      // advance the session activity timestamp so the session row floats to
      // the top of the list while the agent is actively producing output —
      // matching the pull_sync_resp batch path, which already lets tool
      // messages move the timestamp without touching the preview text.
      final sid = msg.sessionId.trim();
      if (sid.isEmpty) return;
      final existingRow = await _guardDbOp<Map<String, dynamic>?>(
        LocalDb.getSessionRecord(sid),
        op: 'getSessionRecord(touchSession_tool)',
      );
      if (existingRow != null) {
        _clearSessionLocalDeleteMark(sid);
        await _guardDbOp(
          LocalDb.bumpSessionActivity(sid, msg.createdAt),
          op: 'bumpSessionActivity(tool)',
        );
        _bumpSessionActivityInMemory(sid, msg.createdAt);
        return;
      }
      // Brand-new session with only a placeholder so far: create the record so
      // it surfaces in the list, with an empty preview and the placeholder time.
      await _touchSession(
        msg.sessionId,
        '',
        msg.createdAt,
        type: type,
        increaseUnread: false,
      );
      return;
    }
    // 纯卡片消息（grix://card 链接）不适合做会话摘要——fallback 文本是
    // "[工具执行] Read file" 等内部标记。与 msg_type=4 口径统一：只推活跃
    // 时间戳，不覆盖摘要文本；但保留未读计数，因为卡片仍然是一条实际消息。
    final isCard = ChatMessagePreview.isStandaloneCardMessage(msg.content);
    // 私聊对端消息自带对端身份：发送者非本人时，sender 即会话对端。
    // 在创建/更新会话记录时同步填入 peer_id/peer_type，使归组键 groupKey
    // 从源头就与服务端一致，杜绝未读角标因 peer 缺失而与会话列表对不上。
    var peerId = '';
    var peerType = 0;
    if (type == 'private' &&
        (msg.senderType == 1 || msg.senderType == 2) &&
        msg.senderId.trim().isNotEmpty &&
        !_isMessageFromCurrentUser(msg.senderId)) {
      peerId = msg.senderId.trim();
      peerType = msg.senderType;
    }
    await _touchSession(
      msg.sessionId,
      isCard ? '' : msg.content,
      msg.createdAt,
      type: type,
      increaseUnread: increaseUnread,
      peerId: peerId,
      peerType: peerType,
    );
  }

  Future<void> _hydratePrivateSessionTitle({
    required String sessionId,
    required String sessionType,
    required String senderId,
    required int senderType,
  }) async {
    // Keep session title sourced from backend payload/snapshot fields only.
  }

  Future<void> _touchSession(
    String sessionId,
    String lastMessage,
    int updatedAt, {
    required String type,
    required bool increaseUnread,
    String peerId = '',
    int peerType = 0,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final shouldIncreaseUnread = increaseUnread && !_isCurrentSession(sid);
    _clearSessionLocalDeleteMark(sid);
    await LocalDb.updateSessionLastMsg(
      sid,
      lastMessage,
      updatedAt,
      type: type,
      peerId: peerId,
      peerType: peerType,
    );
    if (shouldIncreaseUnread) {
      await LocalDb.incrementUnread(sid);
    }
    _upsertSessionInMemory(
      sid,
      lastMessage,
      updatedAt,
      increaseUnread: shouldIncreaseUnread,
      peerIdHint: peerId,
      peerTypeHint: peerType,
    );
  }

  /// [allowClearPreview] 仅供撤回/删除路径使用：本地已无任何可预览消息时把摘要清空，
  /// 避免撤回后的内容继续留在会话列表上。其余刷新（如消息编辑）拿不到本地可预览消息
  /// 时保留已有摘要——本地只有卡片、摘要来自服务端快照就是这种情况。
  Future<void> _refreshSessionPreviewFromLocal(
    String sessionId, {
    int activityAt = 0,
    bool allowClearPreview = false,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (_shouldSuppressDeletedSession(sid, 0)) return;

    final existingIndex = sessions.indexWhere((s) => s.sessionId == sid);
    final existing = existingIndex >= 0 ? sessions[existingIndex] : null;
    final sessionRow = await _guardDbOp<Map<String, dynamic>?>(
      LocalDb.getSessionRecord(sid),
      op: 'getSessionRecord(refreshSessionPreview)',
    );
    final latestRows =
        await _guardDbOp<List<Map<String, dynamic>>>(
          LocalDb.getLatestMessages(sid, limit: 1),
          op: 'getLatestMessages(refreshSessionPreview)',
          fallback: const <Map<String, dynamic>>[],
        ) ??
        const <Map<String, dynamic>>[];
    final latestRow = latestRows.isNotEmpty ? latestRows.last : null;
    // 摘要文本另取"最近一条可预览消息"：纯卡片消息只推进时间，不当摘要。
    final previewRow = await _guardDbOp<Map<String, dynamic>?>(
      LocalDb.getLatestPreviewableMessage(sid),
      op: 'getLatestPreviewableMessage(refreshSessionPreview)',
    );

    if (sessionRow == null && latestRow == null) {
      if (existingIndex >= 0) {
        sessions.removeAt(existingIndex);
        _resortSessionsInMemory();
      }
      return;
    }

    final row = <String, dynamic>{
      'session_id': sid,
      'title': existing?.title ?? '',
      'type': existing?.type ?? (_sessionTypeHints[sid] ?? 'private'),
      'peer_id': existing?.peerId ?? '',
      'peer_type': existing?.peerType ?? 0,
      'peer_nickname': existing?.peerNickname ?? '',
      'peer_username': existing?.peerUsername ?? '',
      'updated_at': existing?.updatedAt ?? 0,
      'is_pinned': existing?.isPinned ?? false,
      'is_muted': existing?.isMuted ?? false,
      'pinned_at': existing?.pinnedAt ?? 0,
      'unread_count': existing?.unreadCount ?? 0,
      'last_message': existing?.lastMessage ?? '',
      'last_message_time': existing?.lastMessageTime ?? 0,
    };
    if (sessionRow != null) {
      row.addAll(sessionRow);
    }

    row['type'] = _normalizeSessionType(
      row['type']?.toString() ?? '',
      fallback: _sessionTypeHints[sid] ?? (existing?.type ?? 'private'),
    );
    row['title'] = _normalizeStoredTitle(sid, row['title']?.toString() ?? '');
    row['unread_count'] = _normalizeUnreadForSession(
      sid,
      _toInt(row['unread_count']),
    );

    final previewText = previewRow?['content']?.toString() ?? '';
    if (latestRow != null) {
      final latestCreatedAt = _requireIntLike(
        latestRow['created_at'] ?? 0,
        fieldName: 'messages.created_at',
      );
      // 时间跟随最后一条消息（含卡片），摘要跟随最近一条可预览消息：
      // 会话最后活跃时间照常前移，摘要保持在上一条可读文本上。
      if (previewText.isNotEmpty || allowClearPreview) {
        row['last_message'] = previewText;
      }
      row['last_message_time'] = latestCreatedAt;
      final currentUpdatedAt = _requireIntLike(
        row['updated_at'] ?? 0,
        fieldName: 'sessions.updated_at',
      );
      if (latestCreatedAt > currentUpdatedAt) {
        row['updated_at'] = latestCreatedAt;
      }
    } else if (allowClearPreview) {
      row['last_message'] = '';
      row['last_message_time'] = 0;
    }

    if (activityAt > 0) {
      final curUpdatedAt = _requireIntLike(
        row['updated_at'] ?? 0,
        fieldName: 'sessions.updated_at(activityAt)',
      );
      if (activityAt > curUpdatedAt) {
        row['updated_at'] = activityAt;
      }
    }

    final nextSession = SessionModel.fromJson(row);
    await LocalDb.upsertSession(nextSession.toJson());
    if (existingIndex >= 0) {
      sessions[existingIndex] = nextSession;
    } else {
      sessions.add(nextSession);
    }
    _resortSessionsInMemory();
  }

  Future<void> _queueSessionPreviewFromEditedMessage(
    Map<String, dynamic> row,
  ) async {
    final sid = row['session_id']?.toString().trim() ?? '';
    if (sid.isEmpty) return;
    _pendingEditPreviewBySessionId[sid] = Map<String, dynamic>.from(row);
    final inFlight = _editPreviewFlushCompleter;
    if (inFlight != null) {
      await inFlight.future;
      return;
    }
    final done = Completer<void>();
    _editPreviewFlushCompleter = done;
    _editPreviewFlushTimer?.cancel();
    _editPreviewFlushTimer = Timer(Duration.zero, () {
      unawaited(() async {
        try {
          await _flushPendingEditPreviews();
          if (!done.isCompleted) done.complete();
        } catch (e, st) {
          if (!done.isCompleted) done.completeError(e, st);
        } finally {
          if (identical(_editPreviewFlushCompleter, done)) {
            _editPreviewFlushCompleter = null;
          }
        }
      }());
    });
    await done.future;
  }

  Future<void> _flushPendingEditPreviews() async {
    _editPreviewFlushTimer?.cancel();
    _editPreviewFlushTimer = null;
    if (_pendingEditPreviewBySessionId.isEmpty) return;
    final rows = Map<String, Map<String, dynamic>>.of(
      _pendingEditPreviewBySessionId,
    );
    _pendingEditPreviewBySessionId.clear();
    var mutated = false;
    for (final row in rows.values) {
      final changed = await _applySessionPreviewFromEditedMessage(
        row,
        resort: false,
      );
      mutated = mutated || changed;
    }
    if (mutated) {
      _resortSessionsInMemory();
    }
  }

  /// Returns true when in-memory sessions were mutated.
  Future<bool> _applySessionPreviewFromEditedMessage(
    Map<String, dynamic> row, {
    bool resort = true,
  }) async {
    final sid = row['session_id']?.toString().trim() ?? '';
    if (sid.isEmpty) return false;
    final createdAt = _toInt(row['created_at']);
    if (createdAt <= 0 || _shouldSuppressDeletedSession(sid, createdAt)) {
      return false;
    }

    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    final existing = idx >= 0 ? sessions[idx] : null;
    final existingActivity = existing == null
        ? 0
        : (existing.updatedAt >= existing.lastMessageTime
              ? existing.updatedAt
              : existing.lastMessageTime);

    final msgType = _toInt(row['msg_type']);
    final content = row['content']?.toString() ?? '';
    final previewText =
        msgType == 4 || ChatMessagePreview.isStandaloneCardMessage(content)
        ? ''
        : content;
    final sessionType = _normalizeSessionTypeFromWire(
      row['session_type'],
      fallback: _sessionTypeHints[sid] ?? existing?.type ?? 'private',
    );
    final editedMsgId = row['msg_id']?.toString().trim() ?? '';

    // Memory-first: only hit SQLite for activity when the session is unknown.
    var effectiveActivity = existingActivity;
    if (existing == null) {
      final sessionRow = await _guardDbOp<Map<String, dynamic>?>(
        LocalDb.getSessionRecord(sid),
        op: 'getSessionRecord(push_edit_preview)',
      );
      final dbActivity = sessionRow == null
          ? 0
          : (_toInt(sessionRow['updated_at']) >=
                    _toInt(sessionRow['last_message_time'])
                ? _toInt(sessionRow['updated_at'])
                : _toInt(sessionRow['last_message_time']));
      effectiveActivity = dbActivity;
    }

    if (previewText.isEmpty) {
      if (createdAt <= effectiveActivity) {
        return false;
      }
      await _guardDbOp(
        LocalDb.bumpSessionActivity(sid, createdAt),
        op: 'bumpSessionActivity(push_edit_preview)',
      );
      _bumpSessionActivityInMemory(sid, createdAt, resort: resort);
      return true;
    }

    // Newer than known activity: this edit becomes the list preview. Skip the
    // latest-previewable lookup that dominated CPU after the sync-cursor fix.
    if (createdAt > effectiveActivity) {
      await _guardDbOp(
        LocalDb.updateSessionLastMsg(
          sid,
          previewText,
          createdAt,
          type: sessionType,
        ),
        op: 'updateSessionLastMsg(push_edit_preview)',
      );
      _upsertSessionInMemory(
        sid,
        previewText,
        createdAt,
        increaseUnread: false,
        resort: resort,
      );
      return true;
    }

    // Older/equal activity: only rewrite preview text when this message is
    // still the latest previewable row.
    final latestPreview = await _guardDbOp<Map<String, dynamic>?>(
      LocalDb.getLatestPreviewableMessage(sid),
      op: 'getLatestPreviewableMessage(push_edit_preview)',
    );
    final latestPreviewMsgId =
        latestPreview?['msg_id']?.toString().trim() ?? '';
    if (latestPreview != null && latestPreviewMsgId != editedMsgId) {
      final latestCreatedAt = _toInt(latestPreview['created_at']);
      if (latestCreatedAt > createdAt ||
          (latestCreatedAt == createdAt &&
              _compareMsgId(latestPreviewMsgId, editedMsgId) > 0)) {
        return false;
      }
    }
    if (createdAt < effectiveActivity) {
      if (latestPreview != null && latestPreviewMsgId == editedMsgId) {
        if (existing != null && existing.lastMessage == previewText) {
          return false;
        }
        await _guardDbOp(
          LocalDb.upsertSession({
            'session_id': sid,
            'last_message': previewText,
          }),
          op: 'upsertSession(push_edit_preview_text)',
        );
        if (existing != null) {
          sessions[idx] = existing.copyWith(lastMessage: previewText);
          if (resort) {
            _resortSessionsInMemory();
          }
          return true;
        }
      }
      return false;
    }

    await _guardDbOp(
      LocalDb.updateSessionLastMsg(
        sid,
        previewText,
        createdAt,
        type: sessionType,
      ),
      op: 'updateSessionLastMsg(push_edit_preview)',
    );
    _upsertSessionInMemory(
      sid,
      previewText,
      createdAt,
      increaseUnread: false,
      resort: resort,
    );
    return true;
  }

  void _upsertSessionInMemory(
    String sessionId,
    String lastMessage,
    int updatedAt, {
    required bool increaseUnread,
    String peerIdHint = '',
    int peerTypeHint = 0,
    bool resort = true,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    int unreadCount = increaseUnread ? 1 : 0;
    String title = '';
    String type = _sessionTypeHints[sid] ?? 'private';
    String peerId = '';
    int peerType = 0;
    String peerNickname = '';
    String peerUsername = '';
    bool isPinned = false;
    bool isMuted = false;
    int pinnedAt = 0;
    if (idx >= 0) {
      final prev = sessions[idx];
      unreadCount += prev.unreadCount;
      // Keep the local override in sync when new messages increment unread
      if (increaseUnread && _localUnreadOverrides.containsKey(sid)) {
        _localUnreadOverrides[sid] = _LocalUnreadOverride(
          unreadCount: unreadCount,
          setAtMs: DateTime.now().millisecondsSinceEpoch,
        );
      }
      title = _normalizeStoredTitle(sid, prev.title);
      type = _normalizeSessionType(prev.type, fallback: type);
      peerId = prev.peerId;
      peerType = prev.peerType;
      // 现有记录对端身份缺失时，用消息携带的对端身份补齐
      if (peerId.trim().isEmpty && peerIdHint.trim().isNotEmpty) {
        peerId = peerIdHint;
        peerType = peerTypeHint;
      }
      peerNickname = prev.peerNickname;
      peerUsername = prev.peerUsername;
      isPinned = prev.isPinned;
      isMuted = prev.isMuted;
      pinnedAt = prev.pinnedAt;
      sessions[idx] = prev.copyWith(
        title: title,
        type: type,
        peerId: peerId,
        peerType: peerType,
        peerNickname: peerNickname,
        peerUsername: peerUsername,
        isVisitor: prev.isVisitor,
        updatedAt: updatedAt,
        isPinned: isPinned,
        isMuted: isMuted,
        pinnedAt: pinnedAt,
        friendIsMuted: prev.friendIsMuted || isPeerMuted(peerId),
        unreadCount: _normalizeUnreadForSession(sid, unreadCount),
        lastMessage: lastMessage.isNotEmpty ? lastMessage : prev.lastMessage,
        lastMessageTime: updatedAt,
      );
      if (resort) {
        _resortSessionsInMemory();
      }
      return;
    }
    // New session from message — clear any stale override
    _localUnreadOverrides.remove(sid);
    // 新建会话时直接落入消息携带的对端身份，使 groupKey 一开始就正确
    if (peerId.trim().isEmpty && peerIdHint.trim().isNotEmpty) {
      peerId = peerIdHint;
      peerType = peerTypeHint;
    }
    unreadCount = _normalizeUnreadForSession(sid, unreadCount);
    sessions.add(
      SessionModel(
        sessionId: sid,
        title: title,
        type: type,
        peerId: peerId,
        peerType: peerType,
        peerNickname: peerNickname,
        peerUsername: peerUsername,
        isVisitor: _visitorSessionIds.contains(sid),
        updatedAt: updatedAt,
        isPinned: isPinned,
        isMuted: isMuted,
        pinnedAt: pinnedAt,
        friendIsMuted: isPeerMuted(peerId),
        unreadCount: unreadCount,
        lastMessage: lastMessage,
        lastMessageTime: updatedAt,
      ),
    );
    if (resort) {
      _resortSessionsInMemory();
    }
  }

  /// 仅前移内存中会话的活跃时间（updatedAt / lastMessageTime），
  /// 保留 lastMessage 文本与未读数不变。
  /// 时间倒退或会话不在内存中时直接忽略。
  void _bumpSessionActivityInMemory(
    String sessionId,
    int updatedAt, {
    bool resort = true,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty || updatedAt <= 0) return;
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx < 0) return;
    final prev = sessions[idx];
    final prevActivity = prev.updatedAt >= prev.lastMessageTime
        ? prev.updatedAt
        : prev.lastMessageTime;
    if (updatedAt <= prevActivity) return;
    sessions[idx] = prev.copyWith(
      updatedAt: updatedAt,
      lastMessageTime: updatedAt,
    );
    if (resort) {
      _resortSessionsInMemory();
    }
  }

  void _resortSessionsInMemory() {
    if (sessions.length <= 1) return;
    sessions.sort(SessionModel.compareByPriority);
  }

  void _upsertSessionAndResortInMemory(SessionModel session) {
    // RxList.insert(0, ...) shifts items through operator[]=, and every shift
    // emits refresh(). Build the final snapshot off-list and publish it once.
    final next = List<SessionModel>.of(sessions);
    final idx = next.indexWhere((item) => item.sessionId == session.sessionId);
    if (idx >= 0) {
      next[idx] = session;
    } else {
      next.add(session);
    }
    next.sort(SessionModel.compareByPriority);
    sessions.value = next;
  }

  void _applyLoadedSessionsSnapshot(List<SessionModel> nextSessions) {
    // 去重：DB 层极端情况或并发 upsert 可能产生重复 sessionId，只保留首个。
    final seen = <String>{};
    final deduped = <SessionModel>[];
    for (final s in nextSessions) {
      if (seen.add(s.sessionId)) deduped.add(s);
    }
    _hydratePeerMuteStateFromSessions(deduped);
    if (listEquals(sessions, deduped)) {
      return;
    }
    sessions.value = deduped;
  }

  void _hydratePeerMuteStateFromSessions(List<SessionModel> items) {
    for (final session in items) {
      if (!session.friendIsMuted) continue;
      final peerId = session.peerId.trim();
      if (peerId.isEmpty) continue;
      _peerMuteState.putIfAbsent(peerId, () => true);
    }
  }

  Future<void> _syncSessionsFromServerIfNeeded({
    required bool force,
    Duration maxAge = Duration.zero,
    int limit = 200,
    int maxPages = 5,
    bool fullSync = true,
  }) {
    final inflight = _sessionsAuthoritativeRefreshFuture;
    if (inflight != null) {
      if (fullSync && !_sessionsAuthoritativeRefreshIsFull) {
        return inflight.whenComplete(() {
          return _syncSessionsFromServerIfNeeded(
            force: true,
            limit: limit,
            maxPages: maxPages,
            fullSync: true,
          );
        });
      }
      return inflight;
    }

    if (!force && _hasFreshAuthoritativeSessionSnapshot(maxAge)) {
      return Future<void>.value();
    }

    late final Future<void> refreshFuture;
    _sessionsAuthoritativeRefreshIsFull = fullSync;
    refreshFuture =
        _syncSessionsFromServer(
          limit: limit,
          maxPages: maxPages,
          fullSync: fullSync,
        ).whenComplete(() {
          if (identical(_sessionsAuthoritativeRefreshFuture, refreshFuture)) {
            _sessionsAuthoritativeRefreshFuture = null;
            _sessionsAuthoritativeRefreshIsFull = false;
          }
        });
    _sessionsAuthoritativeRefreshFuture = refreshFuture;
    return refreshFuture;
  }

  bool _hasFreshAuthoritativeSessionSnapshot(Duration maxAge) {
    final lastAt = _lastAuthoritativeSessionRefreshAtMs;
    if (lastAt <= 0) {
      return false;
    }
    return DateTime.now().millisecondsSinceEpoch - lastAt <
        maxAge.inMilliseconds;
  }
}
