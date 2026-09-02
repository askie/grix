part of 'account_info_controller.dart';

mixin _AccountInfoControllerActions on _AccountInfoControllerSessionContext {
  RxBool get isActionProcessing;
  RxString get lastTappedSessionId;
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
    _searchWorker = debounce<String>(searchQuery, (query) {
      final q = query.trim();
      if (q.isNotEmpty) {
        unawaited(_performDbSearch(q));
      } else {
        _dbSearchResults.clear();
      }
    }, time: const Duration(milliseconds: 200));
  }

  void _disposeDbSearch() {
    _searchWorker?.dispose();
  }

  Future<void> _performDbSearch(String query) async {
    final version = ++_dbSearchVersion;
    final groupKey = _effectiveGroupKey;
    final sid = seedSessionId.trim();

    final rows = await LocalDb.searchSessionRecords([query]);
    if (_dbSearchVersion != version) return;

    final seen = <String>{};
    final matched = <SessionModel>[];
    for (final row in rows) {
      final session = SessionModel.fromJson(row);
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

    // 服务端分页补回来的历史会话不在本地库里，本地关键词搜索扫不到；
    // 这里按同样的口径（标题 / 最后一条消息）在内存里补一遍，避免一搜索
    // 刚翻出来的老会话就整批消失。
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
      final haystack = '${session.title} ${session.lastMessage}'.toLowerCase();
      if (!haystack.contains(lowered)) continue;
      matched.add(session);
    }

    matched.sort(_compareSessionsByPinThenActivity);

    _dbSearchResults.assignAll(matched);
  }

  /// 系列页单会话级排序：置顶优先；同为置顶按 pinnedAt 新到旧，再按活跃时间。
  int _compareSessionsByPinThenActivity(SessionModel a, SessionModel b) {
    if (a.isPinned != b.isPinned) {
      return b.isPinned ? 1 : -1;
    }
    if (a.isPinned && b.isPinned) {
      final pinCompare = b.pinnedAt.compareTo(a.pinnedAt);
      if (pinCompare != 0) return pinCompare;
    }
    return b.activityAt.compareTo(a.activityAt);
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
  String sessionThreadPreview(SessionModel session) {
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
