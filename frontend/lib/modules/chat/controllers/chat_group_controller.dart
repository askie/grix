part of 'chat_controller.dart';

class _ChatGroupController {
  const _ChatGroupController(this.owner);

  final ChatController owner;

  int get myGroupRole {
    if (!owner.isGroupChat) return 0;
    final myId = owner.authService.userId?.trim() ?? '';
    if (myId.isEmpty) return 0;
    for (final member in owner._groupMembers) {
      if (owner._parseInt(member['member_type']) != 1) continue;
      final memberId = (member['member_id'] ?? '').toString().trim();
      if (memberId != myId) continue;
      return owner._parseInt(member['role']);
    }
    return 0;
  }

  bool get isGroupOwner => myGroupRole == 3;
  bool get canDissolveGroup => isGroupOwner;
  bool get canLeaveGroup {
    final role = myGroupRole;
    return role == 1 || role == 2;
  }

  bool get canInviteGroupMembers {
    final role = myGroupRole;
    if (role == 2 || role == 3) {
      return true;
    }
    if (role != 1) {
      return false;
    }
    if (!owner._allowMemberInvite.value) {
      return false;
    }
    final threshold = owner._memberInviteThreshold.value;
    if (threshold <= 0) {
      return false;
    }
    return owner.groupMemberCount <= threshold;
  }

  bool get canManageGroupMembers {
    final role = myGroupRole;
    return role == 2 || role == 3;
  }

  bool get canManageGroupSpeaking => canManageGroupMembers;

  bool get canCurrentUserSpeak {
    return currentUserSpeakingBlockedReason.isEmpty;
  }

  String get currentUserSpeakingBlockedReason {
    if (!owner.isGroupChat) {
      return '';
    }

    final myId = owner.authService.userId?.trim() ?? '';
    if (myId.isEmpty) {
      return '';
    }

    final me = owner._findGroupHumanMember(myId);
    if (me == null) {
      return '';
    }

    if (owner._parseBool(me['is_speak_muted'])) {
      return 'chat_send_blocked_member_muted'.tr;
    }

    final role = owner._parseInt(me['role']);
    if (role == 2 || role == 3) {
      return '';
    }

    if (owner._allMembersMuted.value &&
        !owner._parseBool(me['can_speak_when_all_muted'])) {
      return 'chat_send_blocked_all_members_muted'.tr;
    }
    return '';
  }

  String get groupMemberInviteRestrictionReason {
    final role = myGroupRole;
    if (role == 2 || role == 3) {
      return '';
    }
    if (role != 1) {
      return '';
    }
    if (!owner._allowMemberInvite.value) {
      return 'chat_member_invite_disabled'.tr;
    }
    final threshold = owner._memberInviteThreshold.value;
    if (threshold > 0 && owner.groupMemberCount > threshold) {
      return 'chat_member_invite_threshold_reached'.trParams({
        'count': '$threshold',
      });
    }
    return '';
  }

  int get groupMemberCount => owner._groupMemberCount.value;
  List<Map<String, dynamic>> get groupMembers => owner._groupMembers;
  bool get allMembersMuted => owner._allMembersMuted.value;
  bool get allowMemberInvite => owner._allowMemberInvite.value;
  int get memberInviteThreshold => owner._memberInviteThreshold.value;
  String get lastInviteToGroupErrorMessage =>
      owner._lastInviteToGroupErrorMessage;

  bool get canReportGroup {
    return owner.isGroupChat && owner.sessionId.trim().isNotEmpty;
  }

  Future<void> ensureFriendListLoaded() async {
    final fs = owner._friendService;
    if (fs == null) return;
    if (fs.friendList.isEmpty) {
      await fs.loadFriendList();
    }
  }

  List<FriendItem> get invitableFriends {
    final fs = owner._friendService;
    if (fs == null) return const <FriendItem>[];

    final existingMemberIDs = <String>{};
    for (final member in owner._groupMembers) {
      if (owner._parseInt(member['member_type']) != 1) continue;
      final memberId = (member['member_id'] ?? '').toString().trim();
      if (memberId.isEmpty) continue;
      existingMemberIDs.add(memberId);
    }

    return fs.friendList.where((friend) {
      final userId = friend.userId.trim();
      return userId.isNotEmpty && !existingMemberIDs.contains(userId);
    }).toList();
  }

  List<AgentModel> get invitableAgents {
    final existingMemberIDs = <String>{};
    for (final member in owner._groupMembers) {
      if (owner._parseInt(member['member_type']) != 2) continue;
      final memberId = (member['member_id'] ?? '').toString().trim();
      if (memberId.isEmpty) continue;
      existingMemberIDs.add(memberId);
    }
    return owner.agentService.agents.where((agent) {
      // 语音 agent 仅用于实时语音通话，不可拉入群聊
      if (agent.mediaCapability == 'voice') return false;
      return !existingMemberIDs.contains(agent.id);
    }).toList();
  }

  Future<int> inviteToGroup({
    List<String> userIds = const [],
    List<String> agentIds = const [],
  }) async {
    owner._lastInviteToGroupErrorMessage = '';
    if (!owner.isGroupChat) return -1;
    if (!canInviteGroupMembers) {
      owner._lastInviteToGroupErrorMessage =
          groupMemberInviteRestrictionReason.isNotEmpty
          ? groupMemberInviteRestrictionReason
          : 'chat_add_members_failed'.tr;
      return -1;
    }
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) {
      owner._lastInviteToGroupErrorMessage = 'chat_add_members_failed'.tr;
      return -1;
    }

    final memberIds = <String>[];
    final memberTypes = <int>[];

    for (final raw in userIds) {
      final id = raw.trim();
      if (id.isEmpty) continue;
      if (!memberIds.contains(id)) {
        memberIds.add(id);
        memberTypes.add(1);
      }
    }

    for (final raw in agentIds) {
      final id = raw.trim();
      if (id.isEmpty) continue;
      if (!memberIds.contains(id)) {
        memberIds.add(id);
        memberTypes.add(2);
      }
    }

    if (memberIds.isEmpty) return 0;
    final result = await owner.sessionService.addGroupMembersResult(
      sessionId: sid,
      memberIds: memberIds,
      memberTypes: memberTypes,
    );
    if (result.code != 0 || result.data == null) {
      owner._lastInviteToGroupErrorMessage = _resolveInviteToGroupErrorMessage(
        result,
      );
      return -1;
    }

    owner.sessionService.invalidateSessionDetailCache(sid);
    await refreshSessionDetail();
    return owner._parseInt(result.data?['added_count']);
  }

  Future<bool> updateGroupInviteSetting(bool allowMemberInvite) async {
    if (!owner.isGroupChat) return false;
    if (!canManageGroupMembers) return false;

    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return false;

    final result = await owner.sessionService.updateGroupInviteSettingResult(
      sessionId: sid,
      allowMemberInvite: allowMemberInvite,
    );
    if (result.code != 0) {
      return false;
    }

    await refreshSessionDetail();
    return true;
  }

  Future<bool> updateGroupAllMembersMuted(bool allMembersMuted) async {
    if (!owner.isGroupChat) return false;
    if (!canManageGroupSpeaking) return false;

    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return false;

    final result = await owner.sessionService.updateGroupAllMembersMutedResult(
      sessionId: sid,
      allMembersMuted: allMembersMuted,
    );
    if (result.code != 0) {
      return false;
    }

    await refreshSessionDetail();
    return true;
  }

  bool canRemoveGroupMember(Map<String, dynamic> member) {
    final memberId = (member['member_id'] ?? '').toString().trim();
    if (memberId.isEmpty) return false;
    final memberType = owner._parseInt(member['member_type']);
    final targetRole = owner._parseInt(member['role']);
    final myId = owner.authService.userId?.trim() ?? '';

    if (myGroupRole == 1) {
      if (memberType != 2) return false;
      final agentIdx = owner.agentService.agents.indexWhere(
        (current) => current.id == memberId,
      );
      if (agentIdx == -1) return false;
      final agent = owner.agentService.agents[agentIdx];
      if (agent.ownerID.trim() != myId) return false;
      return true;
    }

    if (!canManageGroupMembers) return false;

    if (memberType == 1 && myId.isNotEmpty && memberId == myId) {
      return false;
    }
    if (targetRole == 3) return false;
    if (myGroupRole == 2 && targetRole != 1) return false;
    return true;
  }

  bool canLeaveGroupMember(Map<String, dynamic> member) {
    if (!canLeaveGroup) return false;
    if (owner._parseInt(member['member_type']) != 1) return false;
    final memberId = (member['member_id'] ?? '').toString().trim();
    final myId = owner.authService.userId?.trim() ?? '';
    return memberId.isNotEmpty && myId.isNotEmpty && memberId == myId;
  }

  bool canPromoteGroupMember(Map<String, dynamic> member) {
    if (!isGroupOwner) return false;
    final memberType = owner._parseInt(member['member_type']);
    if (memberType != 1) return false;
    final memberId = (member['member_id'] ?? '').toString().trim();
    final myId = owner.authService.userId?.trim() ?? '';
    if (memberId.isEmpty || memberId == myId) return false;
    return owner._parseInt(member['role']) == 1;
  }

  bool canDemoteGroupMember(Map<String, dynamic> member) {
    if (!isGroupOwner) return false;
    final memberType = owner._parseInt(member['member_type']);
    if (memberType != 1) return false;
    final memberId = (member['member_id'] ?? '').toString().trim();
    final myId = owner.authService.userId?.trim() ?? '';
    if (memberId.isEmpty || memberId == myId) return false;
    return owner._parseInt(member['role']) == 2;
  }

  bool canTransferGroupOwner(Map<String, dynamic> member) {
    if (!isGroupOwner) return false;
    final memberType = owner._parseInt(member['member_type']);
    if (memberType != 1) return false;
    final memberId = (member['member_id'] ?? '').toString().trim();
    final myId = owner.authService.userId?.trim() ?? '';
    if (memberId.isEmpty || memberId == myId) return false;
    return owner._parseInt(member['role']) != 3;
  }

  bool canUpdateGroupMemberSpeaking(Map<String, dynamic> member) {
    if (!canManageGroupSpeaking) return false;
    final memberId = (member['member_id'] ?? '').toString().trim();
    if (memberId.isEmpty) return false;

    final memberType = owner._parseInt(member['member_type']);
    final targetRole = owner._parseInt(member['role']);
    final myId = owner.authService.userId?.trim() ?? '';
    if (memberType == 1 && myId.isNotEmpty && memberId == myId) {
      return false;
    }
    if (targetRole == 3) return false;
    if (myGroupRole == 2 && targetRole != 1) return false;
    return true;
  }

  bool canToggleGroupMemberSpeakWhitelist(Map<String, dynamic> member) {
    if (!owner._allMembersMuted.value) {
      return false;
    }
    if (!canUpdateGroupMemberSpeaking(member)) {
      return false;
    }
    return owner._parseInt(member['role']) == 1;
  }

  bool canUpdateGroupMemberAgentReceive(Map<String, dynamic> member) {
    if (!owner.isGroupChat) {
      return false;
    }
    return owner._parseBool(member['agent_receive_editable']);
  }

  int groupMemberAgentReceiveMode(Map<String, dynamic> member) {
    return owner._parseInt(member['agent_receive_mode']);
  }

  bool isGroupMemberSpeakMuted(Map<String, dynamic> member) {
    return owner._parseBool(member['is_speak_muted']);
  }

  bool canGroupMemberSpeakWhenAllMuted(Map<String, dynamic> member) {
    return owner._parseBool(member['can_speak_when_all_muted']);
  }

  Future<bool> updateGroupMemberSpeaking(
    Map<String, dynamic> member, {
    bool? isSpeakMuted,
    bool? canSpeakWhenAllMuted,
  }) async {
    if (!owner.isGroupChat) return false;
    if (!canUpdateGroupMemberSpeaking(member)) return false;

    final sid = owner.sessionId.trim();
    final memberId = (member['member_id'] ?? '').toString().trim();
    if (sid.isEmpty || memberId.isEmpty) return false;

    final result = await owner.sessionService.updateGroupMemberSpeakingResult(
      sessionId: sid,
      memberId: memberId,
      memberType: owner._parseInt(member['member_type']),
      isSpeakMuted: isSpeakMuted,
      canSpeakWhenAllMuted: canSpeakWhenAllMuted,
    );
    if (result.code != 0) {
      return false;
    }

    await refreshSessionDetail();
    return true;
  }

  Future<bool> updateGroupMemberAgentReceive(
    Map<String, dynamic> member, {
    required int mode,
  }) async {
    if (!owner.isGroupChat) return false;
    if (!canUpdateGroupMemberAgentReceive(member)) return false;

    final sid = owner.sessionId.trim();
    final memberId = (member['member_id'] ?? '').toString().trim();
    if (sid.isEmpty || memberId.isEmpty) {
      return false;
    }

    final result = await owner.sessionService
        .updateGroupMemberAgentReceiveResult(
          sessionId: sid,
          memberId: memberId,
          memberType: owner._parseInt(member['member_type']),
          agentReceiveMode: mode,
        );
    if (result.code != 0) {
      return false;
    }

    owner.sessionService.invalidateSessionDetailCache(sid);
    _applyGroupMemberAgentReceiveResult(member, result);
    await refreshSessionDetail();
    _applyGroupMemberAgentReceiveResult(member, result);
    return true;
  }

  void _applyGroupMemberAgentReceiveResult(
    Map<String, dynamic> member,
    SessionMemberAgentReceiveResult result,
  ) {
    final memberId = result.memberId.trim().isNotEmpty
        ? result.memberId.trim()
        : (member['member_id'] ?? '').toString().trim();
    if (memberId.isEmpty) return;

    final memberType = result.memberType > 0
        ? result.memberType
        : owner._parseInt(member['member_type']);
    for (var i = 0; i < owner._groupMembers.length; i++) {
      final current = owner._groupMembers[i];
      if ((current['member_id'] ?? '').toString().trim() != memberId) {
        continue;
      }
      if (owner._parseInt(current['member_type']) != memberType) {
        continue;
      }
      owner._groupMembers[i] = {
        ...current,
        'agent_receive_mode': result.agentReceiveMode,
        'agent_receive_backlog_count': result.agentReceiveBacklogCount,
      };
      owner._groupMembers.refresh();
      return;
    }
  }

  Future<int> removeGroupMember(Map<String, dynamic> member) async {
    if (!owner.isGroupChat) return -1;
    if (!canRemoveGroupMember(member)) return -1;
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return -1;

    final memberId = (member['member_id'] ?? '').toString().trim();
    if (memberId.isEmpty) return -1;
    final memberType = owner._parseInt(member['member_type']);
    if (memberType != 1 && memberType != 2) return -1;

    final result = await owner.sessionService.removeGroupMembers(
      sessionId: sid,
      memberIds: [memberId],
      memberTypes: [memberType],
    );
    if (result == null) return -1;

    owner.sessionService.invalidateSessionDetailCache(sid);
    await refreshSessionDetail();
    return owner._parseInt(result['removed_count']);
  }

  Future<bool> leaveGroup() async {
    if (!owner.isGroupChat) return false;
    if (!canLeaveGroup) return false;

    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return false;

    final result = await owner.sessionService.leaveGroupResult(sessionId: sid);
    if (result.code != 0) {
      return false;
    }

    owner._groupAccessLostHandled = true;
    owner._resetGroupSessionState();
    await owner.imService.deleteConversation(sid);
    return true;
  }

  Future<bool> updateGroupMemberRole(
    Map<String, dynamic> member, {
    required int role,
  }) async {
    if (!owner.isGroupChat) return false;
    if (role != 1 && role != 2) return false;
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return false;

    final memberId = (member['member_id'] ?? '').toString().trim();
    final memberType = owner._parseInt(member['member_type']);
    if (memberId.isEmpty || memberType != 1) return false;

    if (role == 2 && !canPromoteGroupMember(member)) return false;
    if (role == 1 && !canDemoteGroupMember(member)) return false;

    final result = await owner.sessionService.updateGroupMemberRole(
      sessionId: sid,
      memberId: memberId,
      memberType: memberType,
      role: role,
    );
    if (result == null) return false;

    await refreshSessionDetail();
    return true;
  }

  Future<bool> transferGroupOwner(Map<String, dynamic> member) async {
    if (!owner.isGroupChat) return false;
    if (!isGroupOwner) return false;

    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return false;

    final memberId = (member['member_id'] ?? '').toString().trim();
    final memberType = owner._parseInt(member['member_type']);
    if (memberId.isEmpty || memberType != 1) return false;
    if (!canTransferGroupOwner(member)) return false;

    final result = await owner.sessionService.transferGroupOwner(
      sessionId: sid,
      memberId: memberId,
    );
    if (result == null) return false;

    await refreshSessionDetail();
    return true;
  }

  Future<bool> dissolveGroup() async {
    if (!owner.isGroupChat) return false;
    if (!isGroupOwner) return false;

    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return false;

    final result = await owner.sessionService.dissolveGroup(sessionId: sid);
    if (result == null) return false;

    owner._groupAccessLostHandled = true;
    owner._resetGroupSessionState();
    await owner.imService.deleteConversation(sid);
    return true;
  }

  Future<bool> setMyGroupNickname(String rawNickname) async {
    if (!owner.isGroupChat) return false;

    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return false;

    try {
      final result = await owner.sessionService.setGroupNicknameResult(
        sid,
        rawNickname,
      );
      if (result.code != 0) {
        return false;
      }
      await refreshSessionDetail();
      return true;
    } catch (e) {
      debugPrint('setMyGroupNickname error: $e');
      return false;
    }
  }

  Future<bool> renameCurrentSession(String rawTitle) async {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return false;

    try {
      final renameResult = await owner.sessionService.renameSessionResult(
        sid,
        rawTitle,
      );
      final shouldFallbackLocal =
          renameResult.code == 4004 || renameResult.httpStatus == 404;
      if (renameResult.code != 0 && !shouldFallbackLocal) {
        return false;
      }

      final normalizedType = owner.imService.resolveSessionTypeById(
        sid,
        fallback: owner.chatType,
      );
      var appliedTitle = (renameResult.title ?? '').trim();
      if (shouldFallbackLocal) {
        appliedTitle = _normalizeLocalSessionTitle(rawTitle);
      }

      await owner.imService.setSessionDisplayTitle(
        sid,
        appliedTitle,
        type: normalizedType,
      );

      if (!shouldFallbackLocal) {
        await owner.imService.refreshSessionsNow();
      }
      owner.chatTitle = owner.imService.resolveSessionDisplayTitleById(
        sid,
        fallbackTitle: appliedTitle,
        type: normalizedType,
      );
      return true;
    } catch (e) {
      debugPrint('renameCurrentSession error: $e');
      return false;
    }
  }

  Future<void> refreshSessionDetail({bool forceTypeProbe = false}) async {
    final sid = owner.sessionId.trim();
    if (sid.isEmpty) return;
    if (!owner.isGroupChat && !forceTypeProbe) return;

    final detailResult = await owner.sessionService.fetchSessionDetailResult(
      sid,
    );
    final detail = detailResult.data;
    if (owner.isGroupChat && _isAccessLost(detailResult)) {
      await _handleGroupAccessLost();
      return;
    }
    if (detail == null) {
      owner._applyVisitorSessionDetail(null);
      return;
    }
    owner._applyVisitorSessionDetail(detail);

    final sessionType = owner._parseInt(detail['session_type']);
    if (sessionType == 1) {
      owner.chatType = 'private';
      owner._resetGroupSessionState();
      owner._refreshPrivatePeerNickname();
      owner._refreshPrivatePeerAvatar();
      return;
    }
    if (sessionType != 2) {
      return;
    }
    owner.chatType = 'group';
    owner._privatePeerNickname.value = '';
    owner._privatePeerUserId.value = '';
    owner._privatePeerAvatarUrl.value = '';

    final memberCount = owner._parseInt(detail['member_count']);
    owner._groupMemberCount.value = memberCount;
    owner._allMembersMuted.value = owner._parseBool(
      detail['all_members_muted'],
    );
    owner._allowMemberInvite.value = owner._parseBool(
      detail['allow_member_invite'],
    );
    owner._memberInviteThreshold.value = owner._parseInt(
      detail['member_invite_threshold'],
    );

    final membersRaw = detail['members'];
    if (membersRaw is List) {
      final disallowedNicknames = _buildDisallowedMemberNicknameSet(
        sessionId: sid,
        detail: detail,
      );
      final normalizedNicknameFrequency = _buildNormalizedNicknameFrequency(
        membersRaw,
      );
      final parsed = <Map<String, dynamic>>[];
      for (final item in membersRaw) {
        if (item is! Map) continue;
        final mid = (item['member_id'] ?? '').toString().trim();
        if (mid.isEmpty) continue;
        final memberType = owner._parseInt(item['member_type']);
        final role = owner._parseInt(item['role']);
        final nickname = _sanitizeGroupMemberNickname(
          rawNickname: item['nickname'],
          disallowedNicknames: disallowedNicknames,
          normalizedNicknameFrequency: normalizedNicknameFrequency,
        );
        final groupNickname = (item['group_nickname'] ?? '').toString().trim();
        // 已读游标是雪花消息号，保持字符串避免 Web 端 int 精度丢失。
        final serverLastReadMsgId = (item['last_read_msg_id'] ?? '')
            .toString()
            .trim();
        parsed.add({
          'member_id': mid,
          'member_type': memberType,
          'role': role,
          'last_read_msg_id': serverLastReadMsgId,
          'agent_receive_mode': owner._parseInt(item['agent_receive_mode']),
          'agent_receive_backlog_count': owner._parseInt(
            item['agent_receive_backlog_count'],
          ),
          'agent_receive_editable': owner._parseBool(
            item['agent_receive_editable'],
          ),
          'is_speak_muted': owner._parseBool(item['is_speak_muted']),
          'can_speak_when_all_muted': owner._parseBool(
            item['can_speak_when_all_muted'],
          ),
          'nickname': nickname,
          'group_nickname': groupNickname,
        });
        if (memberType == 1) {
          owner._ensureProfileLoaded(mid);
        }
      }
      owner._initialGroupAvatarMembers = const <SessionAvatarMember>[];
      owner._groupMembers.assignAll(parsed);
      owner._refreshGroupMemberDisplayState();
      return;
    }
    owner._initialGroupAvatarMembers = const <SessionAvatarMember>[];
    owner._groupMembers.clear();
    owner._groupAgentMemberIds.clear();
    owner._memberDisplayNameCache.clear();
    owner._refreshMentionSuggestionState();
  }

  Set<String> _buildDisallowedMemberNicknameSet({
    required String sessionId,
    required Map<String, dynamic> detail,
  }) {
    final disallowed = <String>{};
    void collect(dynamic value) {
      final normalized = _normalizeNicknameComparisonText(
        value?.toString() ?? '',
      );
      if (normalized.isEmpty) {
        return;
      }
      disallowed.add(normalized);
    }

    collect(detail['title']);
    collect(owner.chatTitle);
    collect(
      owner.imService.resolveSessionDisplayTitleById(
        sessionId,
        fallbackTitle: owner.chatTitle,
        type: owner.chatType,
      ),
    );
    return disallowed;
  }

  Map<String, int> _buildNormalizedNicknameFrequency(List membersRaw) {
    final frequency = <String, int>{};
    for (final item in membersRaw) {
      if (item is! Map) {
        continue;
      }
      final normalized = _normalizeNicknameComparisonText(
        (item['nickname'] ?? '').toString(),
      );
      if (normalized.isEmpty) {
        continue;
      }
      frequency[normalized] = (frequency[normalized] ?? 0) + 1;
    }
    return frequency;
  }

  String _sanitizeGroupMemberNickname({
    required dynamic rawNickname,
    required Set<String> disallowedNicknames,
    required Map<String, int> normalizedNicknameFrequency,
  }) {
    final nickname = (rawNickname ?? '').toString().trim();
    if (nickname.isEmpty) {
      return '';
    }
    final normalized = _normalizeNicknameComparisonText(nickname);
    if (normalized.isEmpty) {
      return '';
    }
    final appearsOnMultipleMembers =
        (normalizedNicknameFrequency[normalized] ?? 0) > 1;
    if (appearsOnMultipleMembers && disallowedNicknames.contains(normalized)) {
      return '';
    }
    return nickname;
  }

  String _normalizeNicknameComparisonText(String raw) {
    return raw
        .split(RegExp(r'\s+'))
        .where((part) => part.trim().isNotEmpty)
        .join(' ')
        .trim();
  }

  bool _isAccessLost(SessionDetailResult result) {
    return result.code == 4003 || result.code == 4004;
  }

  Future<void> _handleGroupAccessLost() async {
    if (owner._groupAccessLostHandled) return;
    owner._groupAccessLostHandled = true;

    owner._resetGroupSessionState();
    await owner.imService.revokeSessionAccess(owner.sessionId);
    final reason = owner.imService.getSessionAccessRevokedReason(
      owner.sessionId,
    );
    final toastKey = reason == 'group_banned'
        ? 'chat_group_banned'
        : 'chat_removed_from_group';
    CustomToast.show(toastKey.tr, isError: false);

    if (ChatPaneHost.closeIfActive(owner.sessionId)) return;
    if (Get.key.currentState == null) return;
    if (Get.currentRoute == AppRoutes.chat &&
        (Get.key.currentState?.canPop() ?? false)) {
      Get.back();
      return;
    }
    if (!AppRoutes.isCurrentHomePath) {
      RootRouteNavigator.toHome();
    }
  }

  String _normalizeLocalSessionTitle(String rawTitle) {
    final normalized = rawTitle
        .split(RegExp(r'\s+'))
        .where((part) => part.trim().isNotEmpty)
        .join(' ')
        .trim();
    if (normalized.isEmpty) return '';

    final runes = normalized.runes.toList();
    if (runes.length <= 255) return normalized;
    return String.fromCharCodes(runes.take(255));
  }

  String _resolveInviteToGroupErrorMessage(SessionAddMembersResult result) {
    switch (result.code) {
      case 40031:
        return 'chat_member_invite_disabled'.tr;
      case 40032:
        final threshold = owner._memberInviteThreshold.value;
        if (threshold > 0) {
          return 'chat_member_invite_threshold_reached'.trParams({
            'count': '$threshold',
          });
        }
        break;
      case 40033:
        return 'chat_target_group_invite_rejected'.tr;
    }

    final localReason = groupMemberInviteRestrictionReason;
    if (localReason.isNotEmpty) {
      return localReason;
    }

    final message = result.message.trim();
    if (message.isNotEmpty) {
      return message;
    }
    return 'chat_add_members_failed'.tr;
  }
}
