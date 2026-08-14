part of 'conversations_controller.dart';

class _ConversationsControllerIdentity {
  const _ConversationsControllerIdentity(this.controller);

  final ConversationsController controller;

  String getPrivatePeerDisplayName(SessionModel session) {
    if (session.type != 'private') {
      return controller._getDisplayTitle(session);
    }
    final fromSession = controller._resolveSessionPeerDisplayName(session);
    if (fromSession.isNotEmpty) {
      return fromSession;
    }

    final peerId = resolvePrivatePeerId(session);
    if (peerId.isEmpty) {
      controller._enqueueSessionDetailPrefetch(session.sessionId);
      return '';
    }

    final resolved = controller._resolvedPrivatePeerNames[peerId]?.trim() ?? '';
    if (resolved.isNotEmpty) {
      return resolved;
    }

    final nickname =
        controller._friendService?.getUserNickname(peerId)?.trim() ?? '';
    if (nickname.isNotEmpty) {
      controller._resolvedPrivatePeerNames[peerId] = nickname;
      return nickname;
    }

    ensurePrivatePeerNickname(peerId);
    return '';
  }

  String getConversationAvatarUrl(ConversationListItem item) {
    return getAvatarUrlForSession(
      item.latestSession,
      controller.isGroupConversation(item),
    );
  }

  String getAvatarUrlForSession(SessionModel session, bool isGroup) {
    if (isGroup) {
      return '';
    }

    if (session.peerType == 2) {
      final agentId = session.peerId.trim();
      if (agentId.isEmpty) {
        return '';
      }
      final agentService = controller._agentService;
      if (agentService == null) {
        return '';
      }
      final idx = agentService.agents.indexWhere(
        (agent) => agent.id == agentId,
      );
      if (idx != -1) {
        return agentService.agents[idx].avatarUrl.trim();
      }
      if (agentService.agents.isEmpty) {
        unawaited(agentService.loadAgents());
      }
      return '';
    }

    final resolvedPeerId = resolvePrivatePeerId(session);
    if (resolvedPeerId.isEmpty) {
      controller._enqueueSessionDetailPrefetch(session.sessionId);
      return '';
    }

    final avatarUrl =
        controller._friendService?.getUserAvatarUrl(resolvedPeerId)?.trim() ??
        '';
    if (avatarUrl.isNotEmpty) {
      return avatarUrl;
    }

    ensurePrivatePeerNickname(resolvedPeerId);
    return '';
  }

  void ensurePrivatePeerNickname(String peerId) {
    final normalized = peerId.trim();
    if (normalized.isEmpty) return;
    if (!controller._inflightPeerNameLoads.add(normalized)) return;

    final fs = controller._friendService;
    if (fs == null) {
      controller._inflightPeerNameLoads.remove(normalized);
      return;
    }

    fs
        .fetchUserProfile(normalized)
        .then((nickname) {
          final name = nickname?.trim() ?? '';
          if (name.isNotEmpty) {
            controller._resolvedPrivatePeerNames[normalized] = name;
            controller._peerNameRefreshVersion.value++;
          }
        })
        .whenComplete(() {
          controller._inflightPeerNameLoads.remove(normalized);
        });
  }

  String resolvePrivatePeerId(SessionModel session) {
    final fromSession = session.peerId.trim();
    if (fromSession.isNotEmpty && session.peerType != 2) {
      controller._resolvedPrivatePeerIdsBySession[session.sessionId] =
          fromSession;
      return fromSession;
    }
    return controller._resolvedPrivatePeerIdsBySession[session.sessionId]
            ?.trim() ??
        '';
  }

  void ensurePrivatePeerIdentity(SessionModel session) {
    final sid = session.sessionId.trim();
    if (sid.isEmpty) return;
    if (controller._inflightPeerIdLoads.contains(sid)) return;
    final sessionService = controller._sessionService;
    if (sessionService == null) return;

    controller._inflightPeerIdLoads.add(sid);
    sessionService
        .fetchSessionDetailResult(sid)
        .then((detailResult) async {
          if (await controller._pruneUnavailableSessionIfNeeded(
            sid,
            detailResult,
          )) {
            return;
          }
          final data = detailResult.data;
          if (data == null) return;
          final sessionType = controller._parseInt(data['session_type']);
          if (sessionType != 1) return;

          final myId = controller._authService?.userId?.trim() ?? '';
          final members = data['members'];
          if (members is! List) return;
          for (final item in members) {
            if (item is! Map) continue;
            final memberType = controller._parseInt(item['member_type']);
            if (memberType != 1) continue;
            final memberId = (item['member_id'] ?? '').toString().trim();
            if (memberId.isEmpty || memberId == myId) continue;
            controller._resolvedPrivatePeerIdsBySession[sid] = memberId;
            controller._peerNameRefreshVersion.value++;
            controller._privateAvatarVersionForSession(sid).value++;
            // Seed the avatar URL baseline so the next profileCacheWorker
            // pass does not falsely detect a change.
            controller._lastKnownPeerAvatarUrl[memberId] =
                controller._friendService?.getUserAvatarUrl(memberId)?.trim() ??
                    '';
            ensurePrivatePeerNickname(memberId);
            return;
          }
        })
        .whenComplete(() {
          controller._inflightPeerIdLoads.remove(sid);
        });
  }
}
