part of 'account_info_controller.dart';

mixin _AccountInfoControllerActions on _AccountInfoControllerSessionContext {
  RxBool get isActionProcessing;
  RxString get lastTappedSessionId;

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

  void _initDbSearch() {
    _searchWorker = debounce<String>(
      searchQuery,
      (query) {
        final q = query.trim();
        if (q.isNotEmpty) {
          unawaited(_performDbSearch(q));
        } else {
          _dbSearchResults.clear();
        }
      },
      time: const Duration(milliseconds: 200),
    );
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
      '联系人名片：',
      '昵称：$displayNickname',
      '账号：$displayAccount',
      '用户 ID：${peerId.value.trim()}',
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

  List<SessionModel> get allConversationSessions {
    final query = searchQuery.value.trim();
    if (query.isNotEmpty) {
      return List<SessionModel>.unmodifiable(_dbSearchResults);
    }

    imService.sessions.length;
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
    return matched;
  }

  List<SessionModel> get conversationSessions {
    final query = searchQuery.value.trim();
    if (query.isNotEmpty) {
      return List<SessionModel>.unmodifiable(_dbSearchResults);
    }

    imService.sessions.length;
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
    matched.sort(_compareSessionsByPinThenActivity);
    return matched;
  }

  String formatSessionTime(SessionModel session) {
    return TimeFormatter.formatChatTime(session.activityAt);
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
