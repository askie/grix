part of 'chat_controller.dart';

class _ChatNavigationController {
  const _ChatNavigationController(this.owner);

  final ChatController owner;

  void retryMessage(String? clientMsgId, {String? msgId}) {
    final normalizedClientMsgId = clientMsgId?.trim() ?? '';
    final normalizedMsgId = msgId?.trim() ?? '';
    if (normalizedClientMsgId.isEmpty && normalizedMsgId.isEmpty) return;
    owner.imService.retryMessage(
      normalizedClientMsgId.isEmpty ? null : normalizedClientMsgId,
      msgId: normalizedMsgId.isEmpty ? null : normalizedMsgId,
    );
  }

  Future<void> revokeMessage(String msgId) async {
    if (msgId.isEmpty) return;
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return;

    final success = await owner.sessionService.deleteMessage(
      sessionId: sid,
      msgId: msgId,
    );
    if (success) {
      await owner.imService.applyLocalMessageRevoke(
        sessionId: sid,
        msgId: msgId,
        dbOpLabel: 'deleteMessage(chat_revoke_success)',
      );
    } else {
      CustomToast.show('chat_revoke_failed'.tr, isError: true);
    }
  }

  void onHeaderAvatarTap() {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return;

    if (owner.isGroupChat) {
      Get.toNamed(
        AppRoutes.groupInfo,
        arguments: {'session_id': sid, 'title': owner.displayChatTitle},
        parameters: {'session_id': sid},
      );
      return;
    }

    final currentSession = owner.imService.findSessionById(sid);
    final peerType = () {
      final fromSession = currentSession?.peerType ?? 0;
      if (fromSession > 0) return fromSession;
      return 1;
    }();
    final peerId = _resolvePrivateProfilePeerId(
      sessionId: sid,
      currentSession: currentSession,
      peerType: peerType,
    );
    if (peerId.isEmpty && peerType == 1) {
      unawaited(owner._probePrivatePeerUserIdFromSessionDetail());
    }

    final peerNickname = () {
      final fromLive = owner.privatePeerNickname.trim();
      if (fromLive.isNotEmpty) return fromLive;
      final fromSession = owner._resolvePrivatePeerNameFromSession().trim();
      if (fromSession.isNotEmpty) return fromSession;
      if (peerType == 2 && peerId.isNotEmpty) {
        final knownAgentName = owner._resolveKnownAgentName(peerId).trim();
        if (knownAgentName.isNotEmpty) {
          return knownAgentName;
        }
      }
      if (peerId.isNotEmpty) return peerId;
      return owner.displayChatTitle;
    }();
    final peerUsername = () {
      if (peerType == 2) {
        return '';
      }
      final fromSession = currentSession?.peerUsername.trim() ?? '';
      if (fromSession.isNotEmpty) return fromSession;
      if (peerId.isEmpty) return '';
      return owner._friendService?.getUserUsername(peerId)?.trim() ?? '';
    }();
    final avatarUrl = () {
      if (peerType == 2) {
        return _resolveAgentAvatarUrl(peerId);
      }
      if (peerId.isEmpty) return '';
      return owner._friendService?.getUserAvatarUrl(peerId)?.trim() ?? '';
    }();
    final groupKey = () {
      if (peerId.isNotEmpty) {
        return 'private:$peerType:$peerId';
      }
      return 'session:$sid';
    }();

    _navigateToAccountInfo(
      groupKey: groupKey,
      sid: sid,
      peerId: peerId,
      peerType: peerType,
      nickname: peerNickname,
      username: peerUsername,
      avatarUrl: avatarUrl,
      title: owner.displayChatTitle,
    );
  }

  void onMessageAvatarTap({
    required String senderId,
    required int senderType,
    required bool isMine,
    required String senderName,
    required String senderAvatarUrl,
  }) {
    if (senderType != 1 && senderType != 2) {
      return;
    }

    if (senderType == 2) {
      _onAgentMessageAvatarTap(
        senderId: senderId,
        senderName: senderName,
        senderAvatarUrl: senderAvatarUrl,
      );
      return;
    }

    if (!owner.isGroupChat && !isMine) {
      onHeaderAvatarTap();
      return;
    }

    final sid = owner.sessionId.trim();
    if (sid.isEmpty) {
      return;
    }

    final myId = owner.authService.userId?.trim() ?? '';
    final rawSenderId = senderId.trim();
    final resolvedSenderId = isMine
        ? (myId.isNotEmpty
              ? myId
              : (rawSenderId.isNotEmpty && rawSenderId != 'me'
                    ? rawSenderId
                    : ''))
        : rawSenderId;
    if (resolvedSenderId.isEmpty) {
      return;
    }

    final fs = owner._friendService;

    final displayName = () {
      final fromSender = senderName.trim();
      if (fromSender.isNotEmpty) {
        return fromSender;
      }
      if (isMine) {
        return owner.myDisplayName;
      }
      if (!owner.isGroupChat) {
        final peerName = owner.peerDisplayName.trim();
        if (peerName.isNotEmpty) {
          return peerName;
        }
      }
      final cached = fs?.getUserNickname(resolvedSenderId)?.trim() ?? '';
      if (cached.isNotEmpty) {
        return cached;
      }
      return resolvedSenderId;
    }();

    final username = () {
      if (isMine) {
        return owner.authService.user?.username.trim() ?? '';
      }

      if (owner.isGroupChat) {
        final member = owner._findGroupHumanMember(resolvedSenderId);
        if (member != null) {
          final account = owner.resolveGroupMemberAccount(member).trim();
          if (account.isNotEmpty) {
            return account;
          }
        }
      } else {
        final session = owner.imService.findSessionById(sid);
        final sessionUsername = session?.peerUsername.trim() ?? '';
        if (sessionUsername.isNotEmpty) {
          return sessionUsername;
        }
      }

      return fs?.getUserUsername(resolvedSenderId)?.trim() ?? '';
    }();

    final avatarUrl = () {
      final fromSender = senderAvatarUrl.trim();
      if (fromSender.isNotEmpty) {
        return fromSender;
      }
      if (isMine) {
        return owner.authService.user?.avatarUrl?.trim() ?? '';
      }
      final cached = fs?.getUserAvatarUrl(resolvedSenderId)?.trim() ?? '';
      if (cached.isNotEmpty) {
        return cached;
      }
      owner._ensureProfileLoaded(resolvedSenderId);
      return '';
    }();

    _navigateToAccountInfo(
      groupKey: owner.isGroupChat
          ? 'session:$sid'
          : 'private:1:$resolvedSenderId',
      sid: sid,
      peerId: resolvedSenderId,
      peerType: 1,
      nickname: displayName,
      username: username,
      avatarUrl: avatarUrl,
      title: displayName,
    );
  }

  void _onAgentMessageAvatarTap({
    required String senderId,
    required String senderName,
    required String senderAvatarUrl,
  }) {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) {
      return;
    }

    final currentSession = owner.imService.findSessionById(sid);
    final resolvedAgentId = () {
      final fromSender = senderId.trim();
      if (fromSender.isNotEmpty && fromSender != 'me') {
        return fromSender;
      }
      if (owner.isGroupChat) {
        return '';
      }
      return _resolvePrivateProfilePeerId(
        sessionId: sid,
        currentSession: currentSession,
        peerType: 2,
      );
    }();

    if (resolvedAgentId.isEmpty) {
      return;
    }

    final displayName = () {
      final fromSender = senderName.trim();
      if (fromSender.isNotEmpty) {
        return fromSender;
      }
      final knownAgentName = owner
          ._resolveKnownAgentName(resolvedAgentId)
          .trim();
      if (knownAgentName.isNotEmpty) {
        return knownAgentName;
      }
      if (!owner.isGroupChat) {
        final fromTitle = owner.displayChatTitle.trim();
        if (fromTitle.isNotEmpty) {
          return fromTitle;
        }
      }
      return resolvedAgentId;
    }();

    final avatarUrl = () {
      final fromSender = senderAvatarUrl.trim();
      if (fromSender.isNotEmpty) {
        return fromSender;
      }
      final fromAgent = _resolveAgentAvatarUrl(resolvedAgentId);
      if (fromAgent.isNotEmpty) {
        return fromAgent;
      }
      if (!owner.isGroupChat) {
        return owner.privatePeerAvatarUrl.trim();
      }
      return '';
    }();

    final groupKey = owner.isGroupChat
        ? 'session:$sid'
        : (resolvedAgentId.isNotEmpty
              ? 'private:2:$resolvedAgentId'
              : 'session:$sid');
    _navigateToAccountInfo(
      groupKey: groupKey,
      sid: sid,
      peerId: resolvedAgentId,
      peerType: 2,
      nickname: displayName,
      username: '',
      avatarUrl: avatarUrl,
      title: displayName,
    );
  }

  String _resolvePrivateProfilePeerId({
    required String sessionId,
    required SessionModel? currentSession,
    required int peerType,
  }) {
    if (peerType == 2) {
      final fromSession = currentSession?.peerId.trim() ?? '';
      if (fromSession.isNotEmpty) {
        return fromSession;
      }
      return _resolvePrivateAgentIdBySession(sessionId);
    }
    return owner._resolvePrivatePeerUserId();
  }

  String _resolvePrivateAgentIdBySession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return '';
    }

    final idx = owner.agentService.agents.indexWhere(
      (agent) => agent.sessionId.trim() == sid,
    );
    if (idx == -1) {
      return '';
    }
    return owner.agentService.agents[idx].id.trim();
  }

  String _resolveAgentAvatarUrl(String agentId) {
    final normalizedAgentId = agentId.trim();
    if (normalizedAgentId.isEmpty) {
      return '';
    }

    final idx = owner.agentService.agents.indexWhere(
      (agent) => agent.id.trim() == normalizedAgentId,
    );
    if (idx == -1) {
      return '';
    }
    return owner.agentService.agents[idx].avatarUrl.trim();
  }

  void onMessageCardTap(ChatMessageCardData card) {
    if (card is ChatUserProfileCardData) {
      onUserProfileCardTap(card);
      return;
    }
    if (card is ChatConversationCardData) {
      unawaited(onConversationCardTap(card));
    }
  }

  Future<ChatMessageCardActionResult> onMessageCardAction(
    ChatMessageCardAction action,
  ) async {
    final card = action.card;
    if (card is ChatExecApprovalCardData) {
      return onExecApprovalCardAction(card, action.actionId);
    }
    if (card is ChatAgentQuestionCardData) {
      return onAgentQuestionCardAction(card, action.actionId);
    }
    if (card is ChatAgentOpenSessionCardData) {
      return onAgentOpenSessionCardAction(action);
    }
    if (card is ChatCallOwnerCardData) {
      owner.startVoiceBrainCall();
      return const ChatMessageCardActionResult.submitted();
    }
    return const ChatMessageCardActionResult.ignored();
  }

  void onUserProfileCardTap(ChatUserProfileCardData card) {
    final sid = owner.sessionId.trim();
    final userId = card.userId.trim();
    final peerType = card.normalizedPeerType;
    if (sid.isEmpty || userId.isEmpty) {
      return;
    }

    final fs = owner._friendService;
    final nickname = () {
      final fromCard = card.nickname.trim();
      if (fromCard.isNotEmpty) {
        return fromCard;
      }
      if (peerType == 2) {
        final fromAgent = owner._resolveKnownAgentName(userId).trim();
        if (fromAgent.isNotEmpty) {
          return fromAgent;
        }
        return userId;
      }
      final fromCache = fs?.getUserNickname(userId)?.trim() ?? '';
      if (fromCache.isNotEmpty) {
        return fromCache;
      }
      return userId;
    }();
    final username = peerType == 2
        ? ''
        : (fs?.getUserUsername(userId)?.trim() ?? '');
    final avatarUrl = () {
      final fromCard = card.avatarUrl.trim();
      if (fromCard.isNotEmpty) {
        return fromCard;
      }
      if (peerType == 2) {
        return _resolveAgentAvatarUrl(userId);
      }
      final fromCache = fs?.getUserAvatarUrl(userId)?.trim() ?? '';
      if (fromCache.isNotEmpty) {
        return fromCache;
      }
      owner._ensureProfileLoaded(userId);
      return '';
    }();

    _navigateToAccountInfo(
      groupKey: 'private:$peerType:$userId',
      sid: sid,
      peerId: userId,
      peerType: peerType,
      nickname: nickname,
      username: username,
      avatarUrl: avatarUrl,
      title: nickname,
    );
  }

  Future<void> onConversationCardTap(ChatConversationCardData card) async {
    final sid = card.sessionId.trim();
    if (sid.isEmpty) {
      return;
    }

    final title = card.displayTitle;
    if (title.isEmpty) {
      return;
    }

    final type = card.normalizedSessionType;
    final session = owner.imService.findSessionById(sid);
    if (session == null) {
      CustomToast.show('会话不存在或已被删除');
      return;
    }
    if (session.type != type) {
      CustomToast.show('会话类型不匹配');
      return;
    }
    if (!owner.imService.hasSessionDisplayTitleById(sid)) {
      await owner.imService.bindSessionDisplayTitle(sid, title, type: type);
    }

    await ChatRouteNavigator.toChat(sessionId: sid, title: title, type: type);
  }

  Future<ChatMessageCardActionResult> onExecApprovalCardAction(
    ChatExecApprovalCardData card,
    String decision,
  ) async {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) {
      return const ChatMessageCardActionResult.failed();
    }
    final normalizedDecision = decision.trim();
    if (!card.supportsDecision(normalizedDecision)) {
      return const ChatMessageCardActionResult.failed();
    }
    if (!owner.tryLockExecApprovalAction(card)) {
      return const ChatMessageCardActionResult.ignored();
    }
    try {
      await owner.imService.sendMessage(
        card.buildSubmissionMessage(normalizedDecision),
        sid,
        updateCurrentSessionUi: false,
      );
      return const ChatMessageCardActionResult.submitted();
    } catch (_) {
      owner.rollbackExecApprovalAction(card);
      final errorMessage = 'chat_message_card_exec_approval_submit_failed'.tr;
      CustomToast.show(errorMessage);
      return ChatMessageCardActionResult.failed(errorMessage);
    }
  }

  Future<ChatMessageCardActionResult> onAgentQuestionCardAction(
    ChatAgentQuestionCardData _,
    String answer,
  ) async {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) {
      return const ChatMessageCardActionResult.failed();
    }
    final normalizedAnswer = answer.trim();
    if (normalizedAnswer.isEmpty) {
      return const ChatMessageCardActionResult.failed();
    }
    try {
      await owner.imService.sendMessage(
        normalizedAnswer,
        sid,
        updateCurrentSessionUi: false,
      );
      return const ChatMessageCardActionResult.submitted();
    } catch (_) {
      final errorMessage = 'chat_message_card_agent_question_submit_failed'.tr;
      CustomToast.show(errorMessage);
      return ChatMessageCardActionResult.failed(errorMessage);
    }
  }

  Future<ChatMessageCardActionResult> onAgentOpenSessionCardAction(
    ChatMessageCardAction action,
  ) async {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) {
      return const ChatMessageCardActionResult.failed();
    }
    final normalizedValue = action.actionId.trim();
    if (normalizedValue.isEmpty) {
      return const ChatMessageCardActionResult.failed();
    }
    final quotedMessageId = action.sourceMessageId.trim();
    try {
      await owner.imService.sendMessage(
        normalizedValue,
        sid,
        quotedMessageId: quotedMessageId.isEmpty ? null : quotedMessageId,
        updateCurrentSessionUi: false,
      );
      _recordRecentBindDirectory(
        ChatAgentCardActionEncoder.tryParseOpenSessionCwd(normalizedValue),
      );
      return const ChatMessageCardActionResult.submitted();
    } catch (_) {
      final errorMessage =
          'chat_message_card_agent_open_session_submit_failed'.tr;
      CustomToast.show(errorMessage);
      return ChatMessageCardActionResult.failed(errorMessage);
    }
  }

  /// 空白页快捷绑定组件的绑定入口：直接发无卡片的目录绑定消息。
  Future<bool> sendQuickBindDirectory(String path) async {
    final sid = owner.sessionId.trim();
    final normalizedPath = path.trim();
    if (sid.isEmpty || normalizedPath.isEmpty) return false;
    try {
      await owner.imService.sendMessage(
        ChatAgentCardActionEncoder.buildOpenSessionUri(normalizedPath),
        sid,
        updateCurrentSessionUi: false,
      );
      _recordRecentBindDirectory(normalizedPath);
      return true;
    } catch (_) {
      CustomToast.show('chat_message_card_agent_open_session_submit_failed'.tr);
      return false;
    }
  }

  /// 绑定消息发送成功后记入最近绑定目录缓存（MRU）。
  void _recordRecentBindDirectory(String cwd) {
    final normalizedCwd = cwd.trim();
    if (normalizedCwd.isEmpty) return;
    final agentId = () {
      if (owner.isGroupChat) return owner.groupToolbarTargetAgentId.trim();
      final session = owner.imService.findSessionById(owner.sessionId);
      if (session?.peerType == 2) return session!.peerId.trim();
      return '';
    }();
    if (agentId.isEmpty || agentId == '0') return;
    unawaited(
      ChatController.recentBindDirectoryStore.record(
        path: normalizedCwd,
        agentId: agentId,
        hostname: owner.agentHostnameOf(agentId),
      ),
    );
  }

  void _navigateToAccountInfo({
    required String groupKey,
    required String sid,
    required String peerId,
    required int peerType,
    required String nickname,
    required String username,
    required String avatarUrl,
    required String title,
  }) {
    final normalizedGroupKey = groupKey.trim();
    final normalizedSid = sid.trim();
    final normalizedPeerId = peerId.trim();
    final normalizedPeerType = peerType.toString();
    final normalizedNickname = nickname.trim();
    final normalizedUsername = username.trim();
    final normalizedAvatarUrl = avatarUrl.trim();
    final normalizedTitle = title.trim();

    Get.toNamed(
      AppRoutes.accountInfo,
      arguments: {
        'group_key': normalizedGroupKey,
        'session_id': normalizedSid,
        'peer_id': normalizedPeerId,
        'peer_type': normalizedPeerType,
        'nickname': normalizedNickname,
        'username': normalizedUsername,
        'avatar_url': normalizedAvatarUrl,
        'title': normalizedTitle,
      },
      parameters: {
        'group_key': normalizedGroupKey,
        'session_id': normalizedSid,
        'peer_id': normalizedPeerId,
        'peer_type': normalizedPeerType,
      },
    );
  }

  void openGroupReportPage() {
    if (!owner.canReportGroup) {
      return;
    }

    final subtitle = owner.groupMemberCount > 0
        ? '${owner.groupMemberCount} ${'chat_members'.tr}'
        : 'chat_group'.tr;
    Get.toNamed(
      AppRoutes.report,
      arguments: {
        'target_type': 'group',
        'target_user_id': '',
        'target_session_id': owner.sessionId.trim(),
        'source_session_id': owner.sessionId.trim(),
        'title': owner.displayChatTitle,
        'subtitle': subtitle,
        'avatar_url': '',
      },
    );
  }

  Future<void> deleteCurrentConversation() async {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return;
    await owner.imService.deleteConversation(sid);
  }

  bool get isCurrentSessionMuted {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return false;
    final session = owner.imService.findSessionById(sid);
    if (session == null) return false;
    return session.isMuted;
  }

  Future<bool> setCurrentSessionMuted(bool isMuted) async {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return false;
    return owner.imService.setSessionMuted(sid, isMuted: isMuted);
  }
}
