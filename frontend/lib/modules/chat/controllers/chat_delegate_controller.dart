part of 'chat_controller.dart';

class _ChatDelegateController {
  const _ChatDelegateController(this.owner);

  final ChatController owner;

  String get delegatedAgentId {
    final state = owner.imService.delegateStates[owner.sessionId];
    if (state == null) return '';
    return state['agent_id']?.toString().trim() ?? '';
  }

  String get delegatedAgentName {
    final agentId = delegatedAgentId;
    if (agentId.isEmpty) return '';

    final resolved = owner._resolveKnownAgentName(agentId);
    if (resolved.isNotEmpty) {
      return resolved;
    }
    return 'Agent $agentId';
  }

  int get delegatedMaxConsecutiveReplies {
    final state = owner.imService.delegateStates[owner.sessionId];
    if (state == null) return ChatController.delegateDefaultRounds;
    final v = owner._parseDelegateRounds(state['max_consecutive_replies']);
    if (v <= 0) return ChatController.delegateDefaultRounds;
    return v;
  }

  int get delegateRoundsDraft => owner._delegateRoundsDraft.value;
  bool get delegateRoundsDirty => owner._delegateRoundsDirty.value;

  List<SessionActivityModel> get currentSessionActivities =>
      owner.imService.sessionActivitiesFor(owner.sessionId);

  Map<String, dynamic>? get currentAgentOutputState =>
      owner.imService.agentOutputStateFor(owner.sessionId);

  bool get hasActiveAgentOutput => currentAgentOutputState != null;

  bool get hasVisibleStreamingAgentOutput =>
      owner.imService.hasStreamingAgentOutputForSession(owner.sessionId);

  bool get canStopCurrentAgentOutput =>
      currentAgentOutputState?['can_stop'] == true;

  bool get isCurrentAgentOutputStopping =>
      (currentAgentOutputState?['state']?.toString().trim() ?? '') ==
      'stopping';

  String get currentAgentOutputRunId =>
      currentAgentOutputState?['run_id']?.toString().trim() ?? '';

  String get currentAgentOutputAgentId =>
      currentAgentOutputState?['agent_id']?.toString().trim() ?? '';

  String get currentAgentOutputStreamMsgId =>
      currentAgentOutputState?['stream_msg_id']?.toString().trim() ?? '';

  bool get hasSessionActivity => currentSessionActivities.isNotEmpty;

  String get sessionActivityLabel {
    final activities = currentSessionActivities;
    if (activities.isEmpty) return '';
    if (activities.length == 1) {
      final name = _resolveActivityActorName(activities.first);
      return 'chat_composing_named'.trParams({'name': name});
    }
    return 'chat_composing_multi'.trParams({
      'count': activities.length.toString(),
    });
  }

  String get agentOutputLabel {
    final agentId = () {
      final fromState = currentAgentOutputAgentId;
      if (fromState.isNotEmpty) {
        return fromState;
      }
      return owner.imService.visibleStreamingAgentIdForSession(owner.sessionId);
    }();
    if (agentId.isNotEmpty) {
      final agentName = _resolveActivityAgentName(agentId).trim();
      if (agentName.isNotEmpty) {
        return agentName;
      }
    }

    final delegatedName = delegatedAgentName.trim();
    if (delegatedName.isNotEmpty) {
      return delegatedName;
    }

    if (agentId.isNotEmpty) {
      return agentId;
    }

    final fallback = 'profile_default_nickname'.tr.trim();
    if (fallback.isNotEmpty && fallback != 'profile_default_nickname') {
      return fallback;
    }
    return 'Agent';
  }

  void stopDelegate() {
    owner.imService.delegateStop(owner.sessionId);
  }

  bool stopAgentOutput() {
    final state = currentAgentOutputState;
    final runId = currentAgentOutputRunId;
    final streamMsgId = state?['stream_msg_id']?.toString().trim() ?? '';
    final currentState = state?['state']?.toString().trim() ?? '';
    if (!hasActiveAgentOutput || isCurrentAgentOutputStopping) {
      debugPrint(
        '⚠️ agent_output_stop click ignored session=${owner.sessionId} '
        'has_active=$hasActiveAgentOutput stopping=$isCurrentAgentOutputStopping '
        'run_id=${runId.isEmpty ? "-" : runId} '
        'state=${currentState.isEmpty ? "-" : currentState} '
        'stream_msg_id=${streamMsgId.isEmpty ? "-" : streamMsgId}',
      );
      return false;
    }
    debugPrint(
      '🛑 agent_output_stop click session=${owner.sessionId} '
      'run_id=${runId.isEmpty ? "-" : runId} '
      'state=${currentState.isEmpty ? "-" : currentState} '
      'stream_msg_id=${streamMsgId.isEmpty ? "-" : streamMsgId} '
      'can_stop=$canStopCurrentAgentOutput',
    );
    return owner.imService.stopAgentOutput(
      owner.sessionId,
      runId: currentAgentOutputRunId,
    );
  }

  bool shouldShowAgentOutputStopForMessage(
    MessageModel msg, {
    bool? isStreaming,
  }) {
    return canStopAgentOutputForMessage(msg, isStreaming: isStreaming);
  }

  bool canStopAgentOutputForMessage(MessageModel msg, {bool? isStreaming}) {
    if (!canStopCurrentAgentOutput) {
      return false;
    }
    if (msg.sessionId.trim() != owner.sessionId.trim()) {
      return false;
    }
    if (msg.senderType != 2 || msg.msgType != 4) {
      return false;
    }

    final anchorMsgId = _resolveStopAnchorMsgId();
    final msgId = msg.msgId.trim();
    if (anchorMsgId.isNotEmpty) {
      return anchorMsgId == msgId;
    }

    final isStreamingAgentMessage =
        isStreaming ?? owner.imService.isMessageStreaming(msgId);
    if (!isStreamingAgentMessage) {
      return false;
    }
    debugPrint(
      '👁️ agent_output_stop fallback visible session=${owner.sessionId} '
      'msg_id=${msgId.isEmpty ? "-" : msgId} '
      'run_id=${currentAgentOutputRunId.isEmpty ? "-" : currentAgentOutputRunId} '
      'reason=no_stop_anchor_msg_id',
    );
    return currentAgentOutputRunId.isNotEmpty;
  }

  bool isCurrentAgentOutputMessage(MessageModel msg) {
    if (msg.sessionId.trim() != owner.sessionId.trim()) {
      return false;
    }
    final streamMsgId = currentAgentOutputStreamMsgId;
    if (streamMsgId.isEmpty) {
      return false;
    }
    return streamMsgId == msg.msgId.trim();
  }

  bool stopAgentOutputForMessage(
    MessageModel msg, {
    String source = 'message',
    bool? isStreaming,
  }) {
    final msgId = msg.msgId.trim();
    final canStop =
        canStopCurrentAgentOutput &&
        msg.sessionId.trim() == owner.sessionId.trim();
    if (!canStop) {
      debugPrint(
        '⚠️ agent_output_stop message ignored session=${owner.sessionId} '
        'source=$source msg_id=${msgId.isEmpty ? "-" : msgId} '
        'run_id=${currentAgentOutputRunId.isEmpty ? "-" : currentAgentOutputRunId} '
        'stream_msg_id=${currentAgentOutputStreamMsgId.isEmpty ? "-" : currentAgentOutputStreamMsgId} '
        'can_stop=$canStopCurrentAgentOutput stopping=$isCurrentAgentOutputStopping',
      );
      return false;
    }
    debugPrint(
      '🛑 agent_output_stop message session=${owner.sessionId} '
      'source=$source msg_id=$msgId '
      'run_id=${currentAgentOutputRunId.isEmpty ? "-" : currentAgentOutputRunId}',
    );
    return owner.imService.stopAgentOutput(
      owner.sessionId,
      runId: currentAgentOutputRunId,
    );
  }

  bool get hasRunningExecutionForSession {
    if (isCurrentAgentOutputStopping) {
      return true;
    }
    if (hasVisibleStreamingAgentOutput) {
      return true;
    }
    if (hasActiveAgentOutput) {
      return true;
    }
    return false;
  }

  String _resolveStopAnchorMsgId() {
    final streamMsgId = currentAgentOutputStreamMsgId.trim();
    if (streamMsgId.isNotEmpty) {
      return streamMsgId;
    }
    final sessionId = owner.sessionId.trim();
    MessageModel? latest;
    for (final msg in owner.imService.currentMessages) {
      if (msg.sessionId.trim() != sessionId) {
        continue;
      }
      if (msg.senderType != 2 || msg.msgType != 4) {
        continue;
      }
      if (msg.isDeleted || msg.isRevoked) {
        continue;
      }
      if (latest == null) {
        latest = msg;
        continue;
      }
      if (msg.createdAt > latest.createdAt) {
        latest = msg;
        continue;
      }
      if (msg.createdAt == latest.createdAt &&
          msg.msgId.compareTo(latest.msgId) > 0) {
        latest = msg;
      }
    }
    return latest?.msgId.trim() ?? '';
  }

  void startDelegate(String agentId) {
    owner._delegateRoundsDraft.value = ChatController.delegateDefaultRounds;
    owner._delegateRoundsDirty.value = false;
    owner.imService.delegateStart(
      owner.sessionId,
      agentId,
      maxConsecutiveReplies: ChatController.delegateDefaultRounds,
    );
  }

  void increaseDelegateRounds() {
    _changeDelegateRounds(1);
  }

  void decreaseDelegateRounds() {
    _changeDelegateRounds(-1);
  }

  void saveDelegateRounds() {
    if (!owner._delegateRoundsDirty.value) return;
    final agentId = delegatedAgentId;
    if (agentId.isEmpty) return;
    owner.imService.delegateStart(
      owner.sessionId,
      agentId,
      maxConsecutiveReplies: owner._delegateRoundsDraft.value,
    );
    owner._delegateRoundsDirty.value = false;
  }

  void _changeDelegateRounds(int delta) {
    if (delegatedAgentId.isEmpty) return;
    final next = (owner._delegateRoundsDraft.value + delta)
        .clamp(
          ChatController.delegateMinRounds,
          ChatController.delegateMaxRounds,
        )
        .toInt();
    if (next == owner._delegateRoundsDraft.value) return;
    owner._delegateRoundsDraft.value = next;
    owner._delegateRoundsDirty.value = next != delegatedMaxConsecutiveReplies;
  }

  void syncDelegateRoundsDraftFromState() {
    final state = owner.imService.delegateStates[owner.sessionId];
    if (state == null) {
      owner._delegateRoundsDraft.value = ChatController.delegateDefaultRounds;
      owner._delegateRoundsDirty.value = false;
      owner.delegatePanelOpen.value = false;
      return;
    }
    final serverValue = delegatedMaxConsecutiveReplies;
    if (!owner._delegateRoundsDirty.value) {
      owner._delegateRoundsDraft.value = serverValue;
      return;
    }
    owner._delegateRoundsDirty.value =
        owner._delegateRoundsDraft.value != serverValue;
  }

  String _resolveActivityActorName(SessionActivityModel activity) {
    final actorType = activity.actorType.trim().toLowerCase();
    if (actorType == 'agent') {
      final agentName = _resolveActivityAgentName(activity.actorId).trim();
      if (agentName.isNotEmpty) {
        return agentName;
      }
      return 'Agent';
    }

    final userName = _resolveActivityUserName(activity.actorId).trim();
    if (userName.isNotEmpty) {
      return userName;
    }

    final fallback = 'profile_default_nickname'.tr.trim();
    if (fallback.isNotEmpty && fallback != 'profile_default_nickname') {
      return fallback;
    }
    return 'User';
  }

  String _resolveActivityAgentName(String rawAgentId) {
    return owner._resolveKnownAgentName(rawAgentId);
  }

  String _resolveActivityUserName(String rawUserId) {
    final userId = rawUserId.trim();
    if (userId.isEmpty) {
      return '';
    }

    final myId = owner.authService.userId?.trim() ?? '';
    if (myId.isNotEmpty && userId == myId) {
      return owner.myDisplayName;
    }

    if (owner.isGroupChat) {
      final member = owner._findGroupHumanMember(userId);
      if (member != null) {
        final displayName = owner.resolveGroupMemberDisplayName(member).trim();
        if (displayName.isNotEmpty && displayName != userId) {
          return displayName;
        }
      }
    }

    final fs = owner._friendService;
    if (fs != null) {
      final nickname = fs.getUserNickname(userId)?.trim() ?? '';
      if (nickname.isNotEmpty) {
        return nickname;
      }
      final username = fs.getUserUsername(userId)?.trim() ?? '';
      if (username.isNotEmpty) {
        return username;
      }
    }

    if (!owner.isGroupChat) {
      final resolved = owner
          .resolveSenderName(
            senderId: userId,
            isMine: false,
            isGroup: false,
            senderType: 1,
          )
          .trim();
      if (resolved.isNotEmpty && resolved != userId) {
        return resolved;
      }
    }
    return '';
  }
}
