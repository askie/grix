part of 'account_info_controller.dart';

mixin _AccountInfoControllerSessionContext {
  ImService get imService;
  FriendService? get _friendService;
  AgentService? get _agentService;
  SessionService? get _sessionService;
  AuthService? get _authService;

  RxString get peerId;
  RxString get nickname;
  RxString get username;
  RxString get introduction;
  RxString get avatarUrl;
  RxBool get isProfileLoading;
  RxBool get friendRequestSent;
  RxString get searchQuery;

  bool get _hasExplicitPeerTarget;

  String get conversationGroupKey;
  set conversationGroupKey(String value);
  String get seedSessionId;
  set seedSessionId(String value);
  String get routeFallbackTitle;
  set routeFallbackTitle(String value);
  int get peerTypeHint;
  set peerTypeHint(int value);

  String get _effectiveGroupKey {
    final routedGroupKey = conversationGroupKey.trim();
    final pid = peerId.value.trim();
    if (pid.isNotEmpty) {
      if (routedGroupKey.startsWith('private:')) {
        return routedGroupKey;
      }
      return 'private:$peerTypeHint:$pid';
    }

    if (routedGroupKey.isNotEmpty) {
      return routedGroupKey;
    }

    final sid = seedSessionId.trim();
    if (sid.isEmpty) {
      return '';
    }

    final seedSession = imService.findSessionById(sid);
    if (seedSession != null) {
      return _buildConversationGroupKey(seedSession);
    }

    return 'session:$sid';
  }

  bool _matchesConversationSession(
    SessionModel session, {
    required String groupKey,
    required String seedSessionId,
  }) {
    if (groupKey.isNotEmpty) {
      return _buildConversationGroupKey(session) == groupKey;
    }
    if (seedSessionId.isNotEmpty) {
      return session.sessionId.trim() == seedSessionId;
    }
    return false;
  }

  void _applyFromSeedSession() {
    final session = _resolveSeedSession();
    if (session == null) {
      return;
    }

    seedSessionId = session.sessionId;
    if (!_hasExplicitPeerTarget && conversationGroupKey.trim().isEmpty) {
      conversationGroupKey = _buildConversationGroupKey(session);
    }

    final sessionPeerId = session.peerId.trim();
    if (!_hasExplicitPeerTarget &&
        peerId.value.trim().isEmpty &&
        sessionPeerId.isNotEmpty) {
      peerId.value = sessionPeerId;
    }

    if (!_hasExplicitPeerTarget && session.peerType > 0) {
      peerTypeHint = session.peerType;
    }

    final peerNickname = session.peerNickname.trim();
    if (!_hasExplicitPeerTarget &&
        nickname.value.trim().isEmpty &&
        peerNickname.isNotEmpty) {
      nickname.value = peerNickname;
    }

    final peerUsername = session.peerUsername.trim();
    if (!_hasExplicitPeerTarget &&
        username.value.trim().isEmpty &&
        peerUsername.isNotEmpty) {
      username.value = peerUsername;
    }

    if (routeFallbackTitle.trim().isEmpty) {
      routeFallbackTitle = imService.resolveSessionDisplayTitle(session);
    }
  }

  SessionModel? _resolveSeedSession() {
    final sid = seedSessionId.trim();
    if (sid.isNotEmpty) {
      final byId = imService.findSessionById(sid);
      if (byId != null) {
        return byId;
      }
    }

    final routedGroupKey = conversationGroupKey.trim();
    if (routedGroupKey.isNotEmpty) {
      return _findLatestSession(
        (session) => _buildConversationGroupKey(session) == routedGroupKey,
      );
    }

    final pid = peerId.value.trim();
    if (pid.isNotEmpty) {
      return _findLatestSession(
        (session) =>
            session.type.trim().toLowerCase() == 'private' &&
            session.peerId.trim() == pid,
      );
    }

    return null;
  }

  SessionModel? _findLatestSession(
    bool Function(SessionModel session) matcher,
  ) {
    SessionModel? latest;
    for (final session in imService.sessions) {
      if (!matcher(session)) {
        continue;
      }
      if (latest == null ||
          SessionModel.compareByPriority(session, latest) < 0) {
        latest = session;
      }
    }
    return latest;
  }

  String _resolveSeedSessionDisplayTitle() {
    final session = _resolveSeedSession();
    if (session == null) {
      return '';
    }
    return imService.resolveSessionDisplayTitle(session);
  }

  String _resolveSessionRouteTitle(SessionModel session) {
    final normalizedDisplayTitle = imService
        .resolveSessionDisplayTitle(session)
        .trim();
    if (session.type.trim().toLowerCase() != 'private') {
      return normalizedDisplayTitle;
    }

    final candidates = <String>[
      session.peerNickname,
      session.peerUsername,
      nickname.value,
      username.value,
      routeFallbackTitle,
      normalizedDisplayTitle,
    ];
    for (final candidate in candidates) {
      final normalized = candidate.trim();
      if (normalized.isEmpty || normalized == session.sessionId) {
        continue;
      }
      return normalized;
    }
    return normalizedDisplayTitle;
  }

  Future<void> _ensurePeerIdentityAndProfile() async {
    final shouldResolvePrivatePeer =
        peerId.value.trim().isEmpty || peerTypeHint <= 0;
    if (shouldResolvePrivatePeer) {
      final resolvedPeerTarget =
          await _resolvePrivatePeerTargetFromSessionDetail();
      if (resolvedPeerTarget != null) {
        peerId.value = resolvedPeerTarget.peerId;
        if (resolvedPeerTarget.peerType > 0) {
          peerTypeHint = resolvedPeerTarget.peerType;
        }
        if (nickname.value.trim().isEmpty &&
            resolvedPeerTarget.nickname.isNotEmpty) {
          nickname.value = resolvedPeerTarget.nickname;
        }
        if (username.value.trim().isEmpty &&
            resolvedPeerTarget.username.isNotEmpty) {
          username.value = resolvedPeerTarget.username;
        }
      }
    }

    _syncProfileFromFriendService();
    _syncProfileFromAgentService();

    if (peerTypeHint == 2) {
      final agentService = _agentService;
      if (agentService == null) {
        return;
      }
      final shouldLoadAgents =
          !agentService.hasLoaded.value && agentService.agents.isEmpty;
      if (!shouldLoadAgents) {
        return;
      }

      isProfileLoading.value = true;
      try {
        await agentService.loadAgents();
        _syncProfileFromAgentService();
      } finally {
        isProfileLoading.value = false;
      }
      return;
    }

    final fs = _friendService;
    final pid = peerId.value.trim();
    if (fs == null || pid.isEmpty) {
      return;
    }

    final shouldFetchRemote =
        nickname.value.trim().isEmpty ||
        username.value.trim().isEmpty ||
        introduction.value.trim().isEmpty;
    if (!shouldFetchRemote) {
      return;
    }

    isProfileLoading.value = true;
    try {
      await fs.fetchUserProfile(pid);
      _syncProfileFromFriendService();
    } finally {
      isProfileLoading.value = false;
    }
  }

  Future<_PrivateSessionPeerTarget?>
  _resolvePrivatePeerTargetFromSessionDetail() async {
    final sessionService = _sessionService;
    if (sessionService == null) {
      return null;
    }

    final session = _resolveSeedSession();
    final sid = session?.sessionId.trim() ?? seedSessionId.trim();
    if (sid.isEmpty) {
      return null;
    }

    final detailResult = await sessionService.fetchSessionDetailResult(sid);
    final detail = detailResult.data;
    if (detail == null) {
      return null;
    }

    final sessionType = _parseInt(detail['session_type']);
    if (sessionType != 1) {
      return null;
    }

    final myUserId = _authService?.userId?.trim() ?? '';
    return _pickPrivatePeerTarget(
      membersRaw: detail['members'],
      myUserId: myUserId,
      preferredPeerType: peerTypeHint,
    );
  }

  void _syncProfileFromFriendService() {
    if (peerTypeHint != 1) {
      return;
    }

    final fs = _friendService;
    final pid = peerId.value.trim();
    if (fs == null || pid.isEmpty) {
      return;
    }

    final cachedNickname = fs.getUserNickname(pid)?.trim() ?? '';
    if (cachedNickname.isNotEmpty) {
      nickname.value = cachedNickname;
    }

    final cachedUsername = fs.getUserUsername(pid)?.trim() ?? '';
    if (cachedUsername.isNotEmpty) {
      username.value = cachedUsername;
    }

    final cachedAvatarUrl = fs.getUserAvatarUrl(pid)?.trim() ?? '';
    if (cachedAvatarUrl.isNotEmpty) {
      avatarUrl.value = cachedAvatarUrl;
    }

    final cachedIntroduction = fs.getUserIntroduction(pid)?.trim() ?? '';
    if (cachedIntroduction.isNotEmpty) {
      introduction.value = cachedIntroduction;
    }

    if (fs.isFriend(pid)) {
      friendRequestSent.value = false;
    }
  }

  void _syncProfileFromAgentService() {
    if (peerTypeHint != 2) {
      return;
    }

    final agentService = _agentService;
    final pid = peerId.value.trim();
    if (agentService == null || pid.isEmpty) {
      return;
    }

    final idx = agentService.agents.indexWhere(
      (agent) => agent.id.trim() == pid,
    );
    if (idx == -1) {
      return;
    }

    final agent = agentService.agents[idx];
    final agentName = agent.agentName.trim();
    if (agentName.isNotEmpty) {
      nickname.value = agentName;
    }

    final profileAvatarUrl = agent.avatarUrl.trim();
    if (profileAvatarUrl.isNotEmpty) {
      avatarUrl.value = profileAvatarUrl;
    }

    final agentIntroduction = agent.introduction.trim();
    if (agentIntroduction.isNotEmpty) {
      introduction.value = agentIntroduction;
    }
  }

  String _buildConversationGroupKey(SessionModel session) {
    final type = session.type.trim().toLowerCase();
    if (type == 'private') {
      final sid = session.peerId.trim();
      if (sid.isNotEmpty) {
        return 'private:${session.peerType}:$sid';
      }
    }
    return 'session:${session.sessionId}';
  }

  String _extractPeerIdFromGroupKey(String groupKey) {
    final normalized = groupKey.trim();
    if (!normalized.startsWith('private:')) {
      return '';
    }
    final parts = normalized.split(':');
    if (parts.length < 3) {
      return '';
    }
    return parts.sublist(2).join(':').trim();
  }

  int _parseInt(dynamic raw, {int fallback = 0}) {
    if (raw is int) return raw;
    if (raw is num) return raw.toInt();
    final parsed = int.tryParse(raw?.toString() ?? '');
    return parsed ?? fallback;
  }

  _PrivateSessionPeerTarget? _pickPrivatePeerTarget({
    required dynamic membersRaw,
    required String myUserId,
    required int preferredPeerType,
  }) {
    if (membersRaw is! List) {
      return null;
    }

    _PrivateSessionPeerTarget? firstCandidate;
    for (final item in membersRaw) {
      if (item is! Map) {
        continue;
      }

      final memberId = (item['member_id'] ?? '').toString().trim();
      if (memberId.isEmpty || memberId == myUserId) {
        continue;
      }

      final candidate = _PrivateSessionPeerTarget(
        peerId: memberId,
        peerType: _parseInt(item['member_type']),
        nickname: (item['nickname'] ?? '').toString().trim(),
        username: (item['username'] ?? '').toString().trim(),
      );
      firstCandidate ??= candidate;

      if (preferredPeerType > 0 && candidate.peerType == preferredPeerType) {
        return candidate;
      }
    }

    return firstCandidate;
  }

  String _normalizeThreadText(String raw) {
    return ChatMessagePreview.summarize(raw);
  }

  // 标题清洗保留下划线等合法标记，不当作消息正文去 Markdown 化。
  String _normalizeThreadTitle(String raw) {
    return ChatMessagePreview.summarizeTitle(raw);
  }

  Map<String, dynamic> _readRouteArguments() {
    final rawArgs = Get.arguments;
    if (rawArgs is Map<String, dynamic>) {
      return rawArgs;
    }
    if (rawArgs is Map) {
      return rawArgs.map((key, value) => MapEntry(key.toString(), value));
    }
    return const <String, dynamic>{};
  }

  String _readRoutingValue({
    required Map<String, dynamic> args,
    required Map<String, String?> params,
    required String key,
    String fallback = '',
  }) {
    if (args.containsKey(key)) {
      final value = args[key]?.toString();
      if (value != null && value.trim().isNotEmpty) {
        return value;
      }
    }

    final parameterValue = params[key];
    if (parameterValue != null && parameterValue.trim().isNotEmpty) {
      return parameterValue;
    }

    return fallback;
  }
}

class _PrivateSessionPeerTarget {
  const _PrivateSessionPeerTarget({
    required this.peerId,
    required this.peerType,
    this.nickname = '',
    this.username = '',
  });

  final String peerId;
  final int peerType;
  final String nickname;
  final String username;
}
