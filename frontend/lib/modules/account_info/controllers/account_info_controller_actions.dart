part of 'account_info_controller.dart';

mixin _AccountInfoControllerActions on _AccountInfoControllerSessionContext {
  RxBool get isActionProcessing;
  RxString get lastTappedSessionId;
  RxBool get searchInFlight;
  ScrollController get scrollController;

  String get displayNickname;
  String get displayAccount;

  bool get canReportUser;
  bool get canForwardProfileCard;
  bool get canStartChat;
  bool get canAddFriend;
  bool get canEditRemark;
  bool get canDeleteFriend;

  final RxList<SessionModel> _dbSearchResults = <SessionModel>[].obs;
  int _dbSearchVersion = 0;
  Worker? _searchWorker;
  Worker? _searchClearWorker;

  /// 会话段、消息段各自最近一次落地的匹配结果；每次派发新版本时重置，
  /// 两段各自到达即重新求并集发布，互不等待。
  List<SessionModel> _sessionStageMatches = const <SessionModel>[];
  List<SessionModel> _messageStageMatches = const <SessionModel>[];

  /// sessionId → 命中该会话的最新一条消息原文，用于列表项摘要行。
  /// 只在消息段命中时写入；会话段命中但没有消息命中的会话不在此列，
  /// 摘要行退回显示 lastMessage。
  final Map<String, String> _dbSearchMessageSummaries = <String, String>{};

  bool _sessionsSearchPending = false;
  bool _messagesSearchPending = false;

  /// 测试用：替换会话段搜索的底层数据源，免于在单测里拉起真实 sqlite。
  @visibleForTesting
  Future<List<Map<String, dynamic>>> Function(
    List<String> keywords, {
    LocalSearchScope? scope,
  })?
  searchSessionRecordsOverrideForTest;

  /// 测试用：替换消息段搜索的底层数据源。
  @visibleForTesting
  Future<List<MatchedMessage>> Function(
    List<String> keywords, {
    LocalSearchScope? scope,
  })?
  searchMessagesOverrideForTest;

  /// 测试用：替换「消息命中的 sessionId → SessionModel」解析逻辑。
  @visibleForTesting
  Future<SessionModel?> Function(String sessionId)?
  resolveSessionForIdOverrideForTest;

  /// 服务端分页拉回的历史会话（`/sessions/conversation_threads`）。
  ///
  /// 客户端本地只同步「最新 N 条」会话窗口，会话量大的账号里更早的会话根本
  /// 不在本地库，仅靠 imService.sessions 过滤会让资料页看不到历史。这里按
  /// group_key 向服务端分页补齐，与本地内存合并后展示。
  final RxList<SessionModel> _serverThreadSessions = <SessionModel>[].obs;

  /// 当前已建立分页的 group_key；对端身份解析完成后会变化，变化即重置分页。
  String _threadPageGroupKey = '';
  String _threadNextCursor = '';
  bool _threadHasMore = true;
  bool _threadLoadInFlight = false;

  /// 是否正在向服务端拉取历史会话分页（视图底部展示加载指示）。
  final RxBool isThreadHistoryLoading = false.obs;

  static const int _threadPageLimit = 30;

  /// 触底提前量：距列表底部不足该像素时预拉下一页。
  static const double _threadLoadMoreTriggerExtent = 320;

  /// 建立/重建服务端历史分页。group_key 未变时是空操作。
  void _ensureThreadHistoryLoaded() {
    final groupKey = _effectiveGroupKey;
    if (groupKey.isEmpty || _threadPageGroupKey == groupKey) return;
    _threadPageGroupKey = groupKey;
    _threadNextCursor = '';
    _threadHasMore = true;
    _serverThreadSessions.clear();
    unawaited(_loadMoreThreadHistory());
  }

  Future<void> _loadMoreThreadHistory() async {
    final sessionService = _sessionService;
    final groupKey = _threadPageGroupKey;
    if (sessionService == null || !sessionService.isInitialized) return;
    if (groupKey.isEmpty) return;
    if (_threadLoadInFlight || !_threadHasMore) return;

    _threadLoadInFlight = true;
    isThreadHistoryLoading.value = true;
    final cursor = _threadNextCursor;
    try {
      final result = await sessionService.fetchConversationThreads(
        groupKey: groupKey,
        limit: _threadPageLimit,
        cursor: cursor,
      );
      // 目标已切换（对端身份解析完成）→ 丢弃这一页，finally 里改拉新目标。
      if (_threadPageGroupKey != groupKey) return;
      // 失败不推进游标：下次触底可原地重试。
      if (!result.success) return;
      _threadNextCursor = result.nextCursor.trim();
      _threadHasMore = result.hasMore && _threadNextCursor.isNotEmpty;
      if (result.sessions.isNotEmpty) {
        _serverThreadSessions.addAll(result.sessions);
      }
    } finally {
      _threadLoadInFlight = false;
      isThreadHistoryLoading.value = false;
      if (_threadPageGroupKey != groupKey && _threadPageGroupKey.isNotEmpty) {
        unawaited(_loadMoreThreadHistory());
      }
    }
  }

  /// 列表滚动接近底部时预拉下一页；搜索态下列表来源是本地库，不参与分页。
  void _maybeLoadMoreThreadHistoryOnScroll() {
    if (!_threadHasMore || _threadLoadInFlight) return;
    if (searchQuery.value.trim().isNotEmpty) return;
    if (!scrollController.hasClients) return;
    final position = scrollController.position;
    if (position.maxScrollExtent <= 0) return;
    if (position.pixels <
        position.maxScrollExtent - _threadLoadMoreTriggerExtent) {
      return;
    }
    unawaited(_loadMoreThreadHistory());
  }

  void _initDbSearch() {
    // 清空关键词要立即收起 in-flight、清空结果，不等 200ms 去抖——否则用户
    // 删空关键词后还会看到上一轮搜索结果或加载态残留一瞬。用不带去抖的
    // ever 单独兜这一支路；非空关键词仍然只走下面的 debounce 派发查询。
    _searchClearWorker = ever<String>(searchQuery, (query) {
      if (query.trim().isEmpty) {
        _clearDbSearchImmediately();
      }
    });
    _searchWorker = debounce<String>(searchQuery, (query) {
      final q = query.trim();
      if (q.isNotEmpty) {
        unawaited(_performDbSearch(q));
      }
    }, time: const Duration(milliseconds: 200));
  }

  void _disposeDbSearch() {
    _searchWorker?.dispose();
    _searchClearWorker?.dispose();
  }

  void _clearDbSearchImmediately() {
    _dbSearchVersion++;
    _sessionStageMatches = const <SessionModel>[];
    _messageStageMatches = const <SessionModel>[];
    _dbSearchMessageSummaries.clear();
    _sessionsSearchPending = false;
    _messagesSearchPending = false;
    searchInFlight.value = false;
    if (_dbSearchResults.isNotEmpty) _dbSearchResults.clear();
  }

  void _maybeClearSearchInFlight(int version) {
    if (_dbSearchVersion != version) return;
    if (_sessionsSearchPending || _messagesSearchPending) return;
    searchInFlight.value = false;
  }

  /// 把当前对话范围（`_effectiveGroupKey` / `seedSessionId`）转成 SQL 范围。
  /// 两者都取不到时返回 null——语义与旧版 `_matchesConversationSession`
  /// 在 groupKey、seedSessionId 都为空时恒返回 false 一致：不搜索任何会话。
  LocalSearchScope? _buildSearchScope({
    required String groupKey,
    required String seedSessionId,
  }) {
    if (groupKey.startsWith('private:')) {
      final parts = groupKey.split(':');
      final peerType = parts.length >= 2 ? int.tryParse(parts[1]) : null;
      final peerId = _extractPeerIdFromGroupKey(groupKey);
      if (peerType != null && peerId.isNotEmpty) {
        return LocalSearchScope.peer(peerType: peerType, peerId: peerId);
      }
    } else if (groupKey.startsWith('session:')) {
      final sid = groupKey.substring('session:'.length).trim();
      if (sid.isNotEmpty) {
        return LocalSearchScope.session(sid);
      }
    }
    if (seedSessionId.isNotEmpty) {
      return LocalSearchScope.session(seedSessionId);
    }
    return null;
  }

  /// 解析消息命中的 sessionId 对应的会话：优先取内存里已有的（本地实时态 /
  /// 服务端分页补齐的），都没有再查一次本地库——消息行能命中说明本地库里
  /// 一定有对应的 session 行,只是它自己的 title/last_message 没匹配关键词。
  Future<SessionModel?> _resolveSessionForId(String sessionId) async {
    final override = resolveSessionForIdOverrideForTest;
    if (override != null) return override(sessionId);
    final fromMemory = imService.findSessionById(sessionId);
    if (fromMemory != null) return fromMemory;
    for (final session in _serverThreadSessions) {
      if (session.sessionId == sessionId) return session;
    }
    final row = await LocalDb.getSessionRecord(sessionId);
    if (row == null) return null;
    return SessionModel.fromJson(row);
  }

  void _publishMergedSearchResults(int version) {
    if (_dbSearchVersion != version) return;
    final seen = <String>{};
    final merged = <SessionModel>[];
    for (final session in _sessionStageMatches) {
      if (seen.add(session.sessionId)) merged.add(session);
    }
    for (final session in _messageStageMatches) {
      if (seen.add(session.sessionId)) merged.add(session);
    }
    merged.sort(_compareSessionsByPinThenActivity);
    _dbSearchResults.assignAll(merged);
  }

  Future<void> _performDbSearch(String query) async {
    final version = ++_dbSearchVersion;
    final keywords = LocalDbSearchRepository.tokenize(query);
    if (keywords.isEmpty) return;

    final groupKey = _effectiveGroupKey;
    final sid = seedSessionId.trim();
    final scope = _buildSearchScope(groupKey: groupKey, seedSessionId: sid);
    if (scope == null) {
      _sessionStageMatches = const <SessionModel>[];
      _messageStageMatches = const <SessionModel>[];
      _dbSearchMessageSummaries.clear();
      _dbSearchResults.clear();
      searchInFlight.value = false;
      return;
    }

    searchInFlight.value = true;
    _sessionsSearchPending = true;
    _messagesSearchPending = true;
    _dbSearchMessageSummaries.clear();

    final searchSessionRecords =
        searchSessionRecordsOverrideForTest ??
        (List<String> kws, {LocalSearchScope? scope}) =>
            LocalDb.searchSessionRecords(
              kws,
              scope: scope,
              isCancelled: () => _dbSearchVersion != version,
            );
    final searchMessages =
        searchMessagesOverrideForTest ??
        (List<String> kws, {LocalSearchScope? scope}) =>
            LocalDb.searchMessages(
              kws,
              scope: scope,
              limit: 100,
              isCancelled: () => _dbSearchVersion != version,
            );

    final sessionsFuture = searchSessionRecords(keywords, scope: scope)
        .then((rows) {
          if (_dbSearchVersion != version) return;
          final seen = <String>{};
          final matched = <SessionModel>[];
          for (final row in rows) {
            final session = SessionModel.fromJson(row);
            if (seen.add(session.sessionId)) matched.add(session);
          }

          // 服务端分页补回来的历史会话不在本地库里，本地关键词搜索扫不到；
          // 这里按同样的口径（标题 / 最后一条消息）在内存里补一遍，避免一
          // 搜索刚翻出来的老会话就整批消失。口径不变：仍是原始 query 的
          // 整串小写包含匹配，不按 tokenize 后的分词分别匹配。
          final lowered = query.toLowerCase();
          for (final session in _serverThreadSessions) {
            final threadSid = session.sessionId.trim();
            if (threadSid.isEmpty || !seen.add(threadSid)) continue;
            if (imService.isSessionLocallyDeleted(threadSid) ||
                imService.isSessionLocallyRevoked(threadSid)) {
              continue;
            }
            if (!_matchesConversationSession(
              session,
              groupKey: groupKey,
              seedSessionId: sid,
            )) {
              continue;
            }
            final haystack = '${session.title} ${session.lastMessage}'
                .toLowerCase();
            if (!haystack.contains(lowered)) continue;
            matched.add(session);
          }

          _sessionStageMatches = matched;
          _publishMergedSearchResults(version);
        })
        .whenComplete(() {
          if (_dbSearchVersion == version) _sessionsSearchPending = false;
          _maybeClearSearchInFlight(version);
        });

    final messagesFuture = searchMessages(keywords, scope: scope)
        .then((messages) async {
          if (_dbSearchVersion != version) return;
          final latestBySession = <String, MatchedMessage>{};
          for (final message in messages) {
            final msid = message.sessionId.trim();
            if (msid.isEmpty) continue;
            final existing = latestBySession[msid];
            if (existing == null || message.createdAt > existing.createdAt) {
              latestBySession[msid] = message;
            }
          }

          final resolved = <SessionModel>[];
          for (final entry in latestBySession.entries) {
            final session = await _resolveSessionForId(entry.key);
            if (_dbSearchVersion != version) return;
            if (session == null) continue;
            resolved.add(session);
            _dbSearchMessageSummaries[entry.key] = entry.value.content;
          }

          if (_dbSearchVersion != version) return;
          _messageStageMatches = resolved;
          _publishMergedSearchResults(version);
        })
        .whenComplete(() {
          if (_dbSearchVersion == version) _messagesSearchPending = false;
          _maybeClearSearchInFlight(version);
        });

    await Future.wait([sessionsFuture, messagesFuture]);
  }

  /// 系列页单会话级排序：置顶优先；再按活跃时间新到旧；
  /// 同为置顶且活跃时间相同时才按 pinnedAt 新到旧。与首页会话列表口径一致。
  int _compareSessionsByPinThenActivity(SessionModel a, SessionModel b) {
    if (a.isPinned != b.isPinned) {
      return b.isPinned ? 1 : -1;
    }
    final activityCompare = b.activityAt.compareTo(a.activityAt);
    if (activityCompare != 0) return activityCompare;
    if (a.isPinned && b.isPinned) {
      final pinCompare = b.pinnedAt.compareTo(a.pinnedAt);
      if (pinCompare != 0) return pinCompare;
    }
    return 0;
  }

  void openReportPage() {
    if (!canReportUser) {
      return;
    }

    Get.toNamed(
      AppRoutes.report,
      arguments: {
        'target_type': 'user',
        'target_user_id': peerId.value.trim(),
        'target_session_id': '',
        'source_session_id': seedSessionId.trim(),
        'title': displayNickname,
        'subtitle': displayAccount,
        'avatar_url': avatarUrl.value.trim(),
      },
    );
  }

  Future<int> forwardProfileCard({required String targetSessionId}) async {
    if (!canForwardProfileCard) {
      return 0;
    }

    final sid = targetSessionId.trim();
    if (sid.isEmpty) {
      return 0;
    }

    final cardEnvelope = ChatMessageCardCodec.buildUserProfileCard(
      userId: peerId.value,
      nickname: displayNickname,
      avatarUrl: avatarUrl.value,
      peerType: peerTypeHint,
    );
    await imService.sendMessage(
      cardEnvelope.content,
      sid,
      extra: cardEnvelope.extra,
    );
    return 1;
  }

  /// 转发名片 sheet 上 "+" 入口使用：把名片信息整理成
  /// "发给 Agent"对话框的预填文本（卡片 JSON 不适合直接给 Agent 读）。
  String buildProfileCardAgentDraft() {
    final lines = <String>[
      'chat_profile_card_draft_heading'.tr,
      'chat_profile_card_draft_name'.trParams({'name': displayNickname}),
      'chat_profile_card_draft_account'.trParams({'account': displayAccount}),
      'chat_profile_card_draft_user_id'.trParams({'id': peerId.value.trim()}),
    ];
    return lines.join('\n');
  }

  String get currentRemarkName {
    final fs = _friendService;
    final pid = peerId.value.trim();
    if (fs == null || pid.isEmpty) {
      return '';
    }
    return fs.getFriendRemarkName(pid)?.trim() ?? '';
  }

  /// 本地内存 + 服务端分页的并集。
  ///
  /// 本地优先：未读、置顶、最后一条消息等实时状态以本地为准，服务端分页只负责
  /// 补齐本地窗口之外的历史会话。已本地删除 / 权限已回收的会话不参与补齐，
  /// 否则服务端分页会把它们重新显示出来。
  List<SessionModel> _collectConversationSessions() {
    imService.sessions.length;
    _serverThreadSessions.length;
    final groupKey = _effectiveGroupKey;
    final sid = seedSessionId.trim();

    final seen = <String>{};
    final matched = <SessionModel>[];
    for (final session in imService.sessions) {
      if (!seen.add(session.sessionId)) continue;
      if (!_matchesConversationSession(
        session,
        groupKey: groupKey,
        seedSessionId: sid,
      )) {
        continue;
      }
      matched.add(session);
    }
    for (final session in _serverThreadSessions) {
      final threadSid = session.sessionId.trim();
      if (threadSid.isEmpty) continue;
      if (!seen.add(threadSid)) continue;
      if (imService.isSessionLocallyDeleted(threadSid) ||
          imService.isSessionLocallyRevoked(threadSid)) {
        continue;
      }
      if (!_matchesConversationSession(
        session,
        groupKey: groupKey,
        seedSessionId: sid,
      )) {
        continue;
      }
      matched.add(session);
    }
    return matched;
  }

  List<SessionModel> get allConversationSessions {
    final query = searchQuery.value.trim();
    if (query.isNotEmpty) {
      return List<SessionModel>.unmodifiable(_dbSearchResults);
    }
    return _collectConversationSessions();
  }

  List<SessionModel> get conversationSessions {
    final query = searchQuery.value.trim();
    if (query.isNotEmpty) {
      return List<SessionModel>.unmodifiable(_dbSearchResults);
    }
    final matched = _collectConversationSessions()
      ..sort(_compareSessionsByPinThenActivity);
    return matched;
  }

  String formatSessionTime(SessionModel session) {
    return TimeFormatter.formatChatTime(session.displayTime);
  }

  String sessionThreadTitle(SessionModel session) {
    final sid = session.sessionId.trim();
    final explicitTitle = _normalizeThreadTitle(session.title);
    if (explicitTitle.isNotEmpty && explicitTitle != sid) {
      return explicitTitle;
    }

    return '';
  }

  /// 无可展示摘要时返回空串，由视图隐藏摘要行（不再用 "..." 占位）。
  ///
  /// 搜索命中来自消息内容而非 lastMessage 时，优先显示命中的那条消息，
  /// 让用户看懂这条会话为什么会出现在结果里。
  String sessionThreadPreview(SessionModel session) {
    final hitSummary = _dbSearchMessageSummaries[session.sessionId];
    if (hitSummary != null && hitSummary.trim().isNotEmpty) {
      return _normalizeThreadText(hitSummary);
    }
    return _normalizeThreadText(session.lastMessage);
  }

  Future<void> startChatFromProfile() async {
    if (!canStartChat) {
      return;
    }

    final pid = peerId.value.trim();
    final peerType = peerTypeHint;
    if (_sessionService == null || pid.isEmpty || peerType <= 0) {
      return;
    }
    if (isActionProcessing.value) {
      return;
    }

    isActionProcessing.value = true;
    try {
      await _openPrivateChatForPeer(pid, peerType: peerType);
    } finally {
      isActionProcessing.value = false;
    }
  }

  Future<void> _openPrivateChatForPeer(
    String pid, {
    required int peerType,
  }) async {
    if (_sessionService == null) {
      return;
    }
    final realSessionId = await ChatRouteNavigator.createAndOpenPrivateChat(
      peerId: pid,
      peerType: peerType,
      fallbackTitle: displayNickname,
    );
    if (realSessionId == null) {
      CustomToast.show('contacts_create_session_failed'.tr);
    }
  }

  Future<void> sendFriendRequest() async {
    if (!canAddFriend) {
      return;
    }
    final fs = _friendService;
    final pid = peerId.value.trim();
    if (fs == null || pid.isEmpty) {
      return;
    }
    if (isActionProcessing.value) {
      return;
    }

    isActionProcessing.value = true;
    try {
      final result = await fs.sendFriendRequest(toUserId: pid);
      if (!result.success) {
        return;
      }

      if (result.autoApproved) {
        _syncProfileFromFriendService();
        await _openPrivateChatForPeer(pid, peerType: 1);
        return;
      }

      friendRequestSent.value = true;
      CustomToast.show('account_info_friend_request_sent'.tr, isError: false);
    } finally {
      isActionProcessing.value = false;
    }
  }

  Future<bool> updateFriendRemark(String rawRemarkName) async {
    if (!canEditRemark) {
      return false;
    }

    final fs = _friendService;
    final pid = peerId.value.trim();
    if (fs == null || pid.isEmpty) {
      return false;
    }
    if (isActionProcessing.value) {
      return false;
    }

    isActionProcessing.value = true;
    try {
      final success = await fs.updateFriendRemark(
        friendUserId: pid,
        remarkName: rawRemarkName,
      );
      if (!success) {
        return false;
      }
      _syncProfileFromFriendService();
      await imService.refreshSessionsNow();
      return true;
    } finally {
      isActionProcessing.value = false;
    }
  }

  Future<bool> deleteFriend() async {
    if (!canDeleteFriend) {
      return false;
    }

    final fs = _friendService;
    final pid = peerId.value.trim();
    if (fs == null || pid.isEmpty) {
      return false;
    }
    if (isActionProcessing.value) {
      return false;
    }

    isActionProcessing.value = true;
    try {
      final success = await fs.deleteFriend(pid);
      if (!success) {
        return false;
      }
      friendRequestSent.value = false;
      _syncProfileFromFriendService();
      await imService.refreshSessionsNow();
      return true;
    } finally {
      isActionProcessing.value = false;
    }
  }

  void openSession(SessionModel session) {
    // 记录最近点击的会话，便于本次资料页停留期间在列表上显示高亮背景。
    lastTappedSessionId.value = session.sessionId;
    final routeTitle = _resolveSessionRouteTitle(session);
    ChatRouteNavigator.toChat(
      sessionId: session.sessionId,
      title: routeTitle,
      type: session.type,
    );
  }

  Future<void> setSessionPinned(
    SessionModel session, {
    required bool isPinned,
  }) async {
    final success = await imService.setSessionPinned(
      session.sessionId,
      isPinned: isPinned,
    );
    if (success) {
      await imService.refreshSessionsNow();
    }
  }

  Future<void> setSessionMuted(
    SessionModel session, {
    required bool isMuted,
  }) async {
    final success = await imService.setSessionMuted(
      session.sessionId,
      isMuted: isMuted,
    );
    if (success) {
      await imService.refreshSessionsNow();
    }
  }
}
