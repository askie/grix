part of 'chat_controller.dart';

class _ChatIdentityController {
  const _ChatIdentityController(this.owner);

  final ChatController owner;

  String get myDisplayName {
    final user = owner.authService.user;
    if (user == null) return 'Me';
    final nickname = user.nickname.trim();
    if (nickname.isNotEmpty) return nickname;
    final username = user.username.trim();
    if (username.isNotEmpty) return username;
    return 'Me';
  }

  String get peerDisplayName {
    final title = _resolvePrivateChatTitle().trim();
    if (title.isNotEmpty) return title;
    return 'Peer';
  }

  String get privatePeerNickname => owner._privatePeerNickname.value;

  String get privatePeerAvatarUrl {
    return owner._privatePeerAvatarUrl.value;
  }

  bool get shouldShowHeaderAvatar {
    owner.imService.sessions.length;
    return true;
  }

  String get chatSubtitle {
    if (owner.isGroupChat) {
      if (owner.groupMemberCount > 0) {
        return '${owner.groupMemberCount} ${'chat_members'.tr}';
      }
      return 'chat_group'.tr;
    }
    if (owner.isVisitorSession) {
      return _resolveVisitorChatSubtitle();
    }
    if (_resolveRenamedPrivateSessionTitle().isNotEmpty) {
      return '';
    }
    return _resolvePrivateChatSubtitle();
  }

  String _resolveVisitorChatSubtitle() {
    final info = owner.visitorInfo;
    final siteName = (info['site_name'] ?? '').toString().trim();
    final visitorName = (info['visitor_name'] ?? '').toString().trim();
    final visitorEmail = (info['visitor_email'] ?? '').toString().trim();
    final parts = <String>['chat_visitor_session_subtitle'.tr];
    if (siteName.isNotEmpty) {
      parts.add(siteName);
    }
    if (visitorName.isNotEmpty) {
      parts.add(visitorName);
    } else if (visitorEmail.isNotEmpty) {
      parts.add(visitorEmail);
    }
    return parts.join(' · ');
  }

  String _resolvePrivateChatSubtitle() {
    final subtitle = privatePeerNickname.trim();
    if (subtitle.isEmpty) return '';
    final title = owner.displayChatTitle.trim();
    if (title == subtitle) return '';
    return subtitle;
  }

  bool get isChatSubtitleOnline {
    if (owner.isGroupChat) {
      return owner.imService.isConnected;
    }
    return false;
  }

  bool get isChatSubtitleOffline {
    if (owner.isGroupChat) {
      return !owner.imService.isConnected;
    }
    return false;
  }

  String get displayChatTitle {
    owner.imService.sessions.length;
    if (!owner.isGroupChat) {
      final renamedSessionTitle = _resolveRenamedPrivateSessionTitle();
      if (renamedSessionTitle.isNotEmpty) {
        return renamedSessionTitle;
      }
      final privateTitle = _resolvePrivateChatTitle();
      if (privateTitle.isNotEmpty) {
        return privateTitle;
      }
    }
    return owner.imService.resolveSessionDisplayTitleById(
      owner.sessionId,
      fallbackTitle: owner.chatTitle,
      type: owner.chatType,
    );
  }

  String get headerAvatarTitle {
    if (!owner.isGroupChat) {
      final privateTitle = _resolvePrivateChatTitle();
      if (privateTitle.isNotEmpty) {
        return privateTitle;
      }
    }
    return displayChatTitle;
  }

  String get headerAvatarColorSeed {
    if (owner.isGroupChat) {
      final sid = owner.sessionId.trim();
      if (sid.isNotEmpty) {
        return sid;
      }
      return headerAvatarTitle;
    }

    if (owner.isAgentPrivateChat) {
      final session = owner.imService.findSessionById(owner.sessionId);
      final agentId = session?.peerId.trim() ?? '';
      if (agentId.isNotEmpty) {
        return 'agent:$agentId';
      }
    } else {
      final peerUserId = owner._resolvePrivatePeerUserId();
      if (peerUserId.isNotEmpty) {
        return peerUserId;
      }
    }

    final sid = owner.sessionId.trim();
    if (sid.isNotEmpty) {
      return sid;
    }
    return headerAvatarTitle;
  }

  String _resolveRenamedPrivateSessionTitle() {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) {
      return '';
    }

    final session = owner.imService.findSessionById(sid);
    if (session == null || session.type != 'private') {
      return '';
    }

    final explicitTitle = session.title.trim();
    if (explicitTitle.isEmpty || explicitTitle == sid) {
      return '';
    }

    return explicitTitle;
  }

  String _resolvePrivateChatTitle() {
    final fromLiveNickname = privatePeerNickname.trim();
    if (fromLiveNickname.isNotEmpty) {
      return fromLiveNickname;
    }

    final fromSession = owner._resolvePrivatePeerNameFromSession();
    if (fromSession.isNotEmpty) {
      return fromSession;
    }

    final peerId = owner._resolvePrivatePeerUserId();
    if (peerId.isNotEmpty) {
      final cached =
          owner._friendService?.getUserNickname(peerId)?.trim() ?? '';
      if (cached.isNotEmpty) {
        return cached;
      }
      return peerId;
    }

    return '';
  }

  List<SessionAvatarMember> get groupAvatarMembers {
    if (!owner.isGroupChat) return const <SessionAvatarMember>[];
    if (owner._groupMembers.isEmpty &&
        owner._initialGroupAvatarMembers.isNotEmpty) {
      return owner._initialGroupAvatarMembers;
    }
    return owner._groupMembers
        .map(
          (member) => SessionAvatarMember(
            memberId: (member['member_id'] ?? '').toString().trim(),
            memberType: owner._parseInt(member['member_type']),
            displayName: resolveGroupMemberDisplayName(member),
            avatarUrl: owner._resolveGroupMemberAvatarUrl(member),
          ),
        )
        .where((member) => member.memberId.isNotEmpty)
        .take(9)
        .toList(growable: false);
  }

  String get myGroupNickname {
    final myId = owner.authService.userId?.trim() ?? '';
    if (myId.isEmpty) return '';
    final me = owner._findGroupHumanMember(myId);
    if (me == null) return '';
    return (me['group_nickname'] ?? '').toString().trim();
  }

  String resolveSenderName({
    required String senderId,
    required bool isMine,
    required bool isGroup,
    int senderType = 1,
  }) {
    if (isMine) {
      if (isGroup) {
        final myId = owner.authService.userId?.trim() ?? '';
        if (myId.isNotEmpty) {
          final me = owner._findGroupHumanMember(myId);
          if (me != null) {
            final displayName = resolveGroupMemberDisplayName(me).trim();
            if (displayName.isNotEmpty) {
              return displayName;
            }
          }
        }
      }
      return myDisplayName;
    }

    if (senderType == 2) {
      final resolved = owner._resolveKnownAgentName(senderId);
      if (resolved.isNotEmpty) {
        return resolved;
      }
      return 'Agent $senderId';
    }

    if (!isGroup) {
      return peerDisplayName;
    }

    final raw = senderId.trim();
    if (raw.isEmpty) return 'Unknown';

    final userId = owner._parseUserId(raw);
    final fs = owner._friendService;
    if (userId == null || fs == null) {
      return raw;
    }

    final cachedNickname = fs.getUserNickname(userId);
    if (cachedNickname != null && cachedNickname.trim().isNotEmpty) {
      return cachedNickname.trim();
    }

    return raw;
  }

  String resolveSenderAvatarUrl({
    required String senderId,
    required bool isMine,
    required bool isGroup,
    int senderType = 1,
  }) {
    if (isMine) {
      return owner.authService.user?.avatarUrl?.trim() ?? '';
    }

    final normalizedSenderId = senderId.trim();
    if (normalizedSenderId.isEmpty) {
      return '';
    }

    if (isGroup) {
      final member = senderType == 2
          ? owner._findGroupMember(normalizedSenderId, memberType: 2)
          : owner._findGroupHumanMember(normalizedSenderId);
      if (member != null) {
        return owner._resolveGroupMemberAvatarUrl(member);
      }
    }

    if (senderType == 2) {
      final idx = owner.agentService.agents.indexWhere(
        (agent) => agent.id == normalizedSenderId,
      );
      if (idx != -1) {
        return owner.agentService.agents[idx].avatarUrl.trim();
      }
      return '';
    }

    final fs = owner._friendService;
    if (fs != null) {
      final avatarUrl = fs.getUserAvatarUrl(normalizedSenderId)?.trim() ?? '';
      if (avatarUrl.isNotEmpty) {
        return avatarUrl;
      }
    }
    return '';
  }

  String formatMessageContentForDisplay(String rawContent) {
    if (rawContent.isEmpty || !rawContent.contains('@')) {
      return rawContent;
    }

    return ChatNumericMentionResolver.replaceNumericMentions(
      rawContent,
      resolveDisplayName: _resolveMentionDisplayNameByUserId,
      resolveAliases: _resolveMentionAliasesByUserId,
    );
  }

  String? _resolveMentionDisplayNameByUserId(String rawUserId) {
    final userId = rawUserId.trim();
    if (userId.isEmpty) {
      return null;
    }

    final remarkName = _resolveMentionRemarkName(userId);
    if (remarkName.isNotEmpty && remarkName != userId) {
      return remarkName;
    }

    if (owner.isGroupChat) {
      final humanMember = owner._findGroupHumanMember(userId);
      if (humanMember != null) {
        final groupNickname = (humanMember['group_nickname'] ?? '')
            .toString()
            .trim();
        if (groupNickname.isNotEmpty && groupNickname != userId) {
          return groupNickname;
        }

        final nickname = _resolveMentionNickname(
          userId,
          humanMember: humanMember,
        );
        if (nickname.isNotEmpty && nickname != userId) {
          return nickname;
        }

        final account = resolveGroupMemberAccount(humanMember).trim();
        if (account.isNotEmpty && account != userId) {
          return account;
        }
      }

      final agentName = owner._resolveKnownAgentName(userId).trim();
      if (agentName.isNotEmpty && agentName != userId) {
        return agentName;
      }
    }

    final nickname = _resolveMentionNickname(userId);
    if (nickname.isNotEmpty && nickname != userId) {
      return nickname;
    }

    final username = _resolveMentionUsername(userId);
    if (username.isNotEmpty && username != userId) {
      return username;
    }

    return null;
  }

  String _resolveMentionRemarkName(String userId) {
    final fs = owner._friendService;
    if (fs == null) {
      return '';
    }
    return fs.getFriendRemarkName(userId)?.trim() ?? '';
  }

  String _resolveMentionNickname(
    String userId, {
    Map<String, dynamic>? humanMember,
  }) {
    if (humanMember != null) {
      final memberNickname = (humanMember['nickname'] ?? '').toString().trim();
      if (memberNickname.isNotEmpty && memberNickname != userId) {
        return memberNickname;
      }
    }

    final myId = owner.authService.userId?.trim() ?? '';
    if (myId.isNotEmpty && myId == userId) {
      final nickname = owner.authService.user?.nickname.trim() ?? '';
      if (nickname.isNotEmpty && nickname != userId) {
        return nickname;
      }
    }

    final fs = owner._friendService;
    if (fs == null) {
      return '';
    }

    final friend = fs.getFriendItem(userId);
    if (friend != null) {
      final nickname = friend.nickname.trim();
      final username = friend.username.trim();
      if (nickname.isNotEmpty && nickname != userId && nickname != username) {
        return nickname;
      }
    }

    final nickname = fs.getUserNickname(userId)?.trim() ?? '';
    final username = fs.getUserUsername(userId)?.trim() ?? '';
    if (nickname.isNotEmpty && nickname != userId && nickname != username) {
      return nickname;
    }

    return '';
  }

  String _resolveMentionUsername(String userId) {
    final myId = owner.authService.userId?.trim() ?? '';
    if (myId.isNotEmpty && myId == userId) {
      final username = owner.authService.user?.username.trim() ?? '';
      if (username.isNotEmpty && username != userId) {
        return username;
      }
    }

    final fs = owner._friendService;
    if (fs == null) {
      return '';
    }
    return fs.getUserUsername(userId)?.trim() ?? '';
  }

  Iterable<String> _resolveMentionAliasesByUserId(String rawUserId) {
    final userId = rawUserId.trim();
    if (userId.isEmpty) {
      return const <String>[];
    }

    final aliases = <String>[];
    final seen = <String>{};
    void addAlias(String value) {
      final trimmed = value.trim();
      if (trimmed.isEmpty || trimmed == userId || !seen.add(trimmed)) {
        return;
      }
      aliases.add(trimmed);
    }

    if (owner.isGroupChat) {
      final humanMember = owner._findGroupHumanMember(userId);
      if (humanMember != null) {
        addAlias((humanMember['group_nickname'] ?? '').toString());
        addAlias(resolveGroupMemberDisplayName(humanMember));
        addAlias(resolveGroupMemberAccount(humanMember));
        addAlias((humanMember['nickname'] ?? '').toString());
      }
    }

    final fs = owner._friendService;
    if (fs != null) {
      addAlias(fs.getFriendRemarkName(userId) ?? '');
      addAlias(fs.getUserNickname(userId) ?? '');
      addAlias(fs.getUserUsername(userId) ?? '');
    }

    if (owner.isGroupChat) {
      addAlias(owner._resolveKnownAgentName(userId));
    }

    final myId = owner.authService.userId?.trim() ?? '';
    if (myId.isNotEmpty && myId == userId) {
      final me = owner.authService.user;
      if (me != null) {
        addAlias(me.nickname);
        addAlias(me.username);
      }
    }

    return aliases;
  }

  String resolveGroupMemberDisplayName(Map<String, dynamic> member) {
    final memberId = (member['member_id'] ?? '').toString().trim();
    final memberType = owner._parseInt(member['member_type']);
    if (memberId.isEmpty) return 'Unknown';

    if (memberType == 2) {
      final resolved = owner._resolveKnownAgentName(memberId);
      if (resolved.isNotEmpty) {
        return resolved;
      }
      return 'Agent $memberId';
    }

    final memberNickname = (member['nickname'] ?? '').toString().trim();
    final myId = owner.authService.userId?.trim() ?? '';
    if (myId.isNotEmpty && memberId == myId) {
      if (memberNickname.isNotEmpty) {
        return memberNickname;
      }
      return myDisplayName;
    }

    if (memberNickname.isNotEmpty) {
      return memberNickname;
    }

    final fs = owner._friendService;
    if (fs != null) {
      final nickname = fs.getUserNickname(memberId)?.trim() ?? '';
      if (nickname.isNotEmpty) {
        return nickname;
      }
    }
    return memberId;
  }

  String resolveGroupMemberAccount(Map<String, dynamic> member) {
    final memberId = (member['member_id'] ?? '').toString().trim();
    if (memberId.isEmpty) return '';
    if (owner._parseInt(member['member_type']) != 1) return '';

    final myId = owner.authService.userId?.trim() ?? '';
    if (myId.isNotEmpty && memberId == myId) {
      final username = owner.authService.user?.username.trim() ?? '';
      if (username.isNotEmpty && username != memberId) {
        return username;
      }
    }

    final fs = owner._friendService;
    if (fs != null) {
      final username = fs.getUserUsername(memberId)?.trim() ?? '';
      if (username.isNotEmpty && username != memberId) {
        return username;
      }
    }

    return '';
  }
}
