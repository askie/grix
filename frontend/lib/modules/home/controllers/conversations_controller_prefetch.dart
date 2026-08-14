part of 'conversations_controller.dart';

class _ConversationsControllerPrefetch {
  const _ConversationsControllerPrefetch(this.controller);

  final ConversationsController controller;

  List<SessionAvatarMember> getConversationAvatarMembers(
    ConversationListItem item,
  ) {
    return getAvatarMembersForSession(
      item.latestSession,
      controller.isGroupConversation(item),
    );
  }

  List<SessionAvatarMember> getAvatarMembersForSession(
    SessionModel session,
    bool isGroup,
  ) {
    if (!isGroup) {
      return const <SessionAvatarMember>[];
    }

    final sid = session.sessionId.trim();
    if (sid.isEmpty) {
      return const <SessionAvatarMember>[];
    }

    final cached = controller._groupAvatarMembersBySession[sid];
    if (cached != null) {
      if (controller._needsGroupAvatarRefresh(sid)) {
        unawaited(ensureGroupAvatarMembers(session, forceRefresh: true));
      }
      return cached;
    }

    final persisted = session.cachedGroupAvatarMembers;
    if (persisted.isNotEmpty) {
      controller._storeGroupAvatarMembers(
        sid,
        persisted,
        notify: false,
        persist: false,
      );
      unawaited(ensureGroupAvatarMembers(session, forceRefresh: true));
      return controller._groupAvatarMembersBySession[sid] ?? persisted;
    }

    unawaited(ensureGroupAvatarMembers(session));
    return const <SessionAvatarMember>[];
  }

  void watchConversationAvatar(ConversationListItem item) {
    watchSessionAvatar(
      item.latestSession,
      controller.isGroupConversation(item),
    );
  }

  void watchSessionAvatar(SessionModel session, bool isGroup) {
    if (isGroup) {
      final sid = session.sessionId.trim();
      if (sid.isNotEmpty) {
        controller._groupAvatarVersionForSession(sid).value;
      }
      return;
    }

    if (session.peerType == 2) {
      final agentId = session.peerId.trim();
      final agentService = controller._agentService;
      if (agentId.isNotEmpty && agentService != null) {
        agentService.agents.length;
        return;
      }
    }

    final resolvedPeerId = controller._resolvePrivatePeerId(session);
    if (resolvedPeerId.isNotEmpty) {
      controller._peerAvatarVersionForPeer(resolvedPeerId).value;
      return;
    }

    final sid = session.sessionId.trim();
    if (sid.isNotEmpty) {
      controller._privateAvatarVersionForSession(sid).value;
    }
  }

  void prefetchTopSessionDetails(List<ConversationListItem> items) {
    if (items.isEmpty) return;
    final prefetchCount =
        items.length < ConversationsController._topSessionDetailPrefetchCount
        ? items.length
        : ConversationsController._topSessionDetailPrefetchCount;
    for (var i = 0; i < prefetchCount; i++) {
      final session = items[i].latestSession;
      if (!_shouldPrefetchSessionDetail(session)) {
        continue;
      }
      enqueueSessionDetailPrefetch(session.sessionId);
    }
  }

  void warmupInitialConversationAvatars(List<ConversationListItem> items) {
    if (items.isEmpty) {
      return;
    }
    final warmupCount =
        items.length <
            ConversationsController._initialConversationAvatarWarmupCount
        ? items.length
        : ConversationsController._initialConversationAvatarWarmupCount;
    for (var index = 0; index < warmupCount; index++) {
      _warmupConversationAvatar(items[index]);
    }
  }

  bool _shouldPrefetchSessionDetail(SessionModel session) {
    if (session.type == 'group' && controller._sessionService == null) {
      return false;
    }
    return true;
  }

  void enqueueSessionDetailPrefetch(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (controller._sessionDetailPrefetchQueued.contains(sid)) return;
    if (controller._sessionDetailPrefetchQueue.length >=
        ConversationsController._maxSessionDetailPrefetchQueueSize) {
      return;
    }
    controller._sessionDetailPrefetchQueued.add(sid);
    controller._sessionDetailPrefetchQueue.add(sid);
    unawaited(drainSessionDetailPrefetchQueue());
  }

  Future<void> drainSessionDetailPrefetchQueue() async {
    if (controller._isDrainingSessionDetailPrefetchQueue) return;
    controller._isDrainingSessionDetailPrefetchQueue = true;
    try {
      while (controller._sessionDetailPrefetchQueue.isNotEmpty) {
        final sid = controller._sessionDetailPrefetchQueue.removeAt(0);

        final session = controller.imService.findSessionById(sid);
        if (session == null) {
          controller._sessionDetailPrefetchQueued.remove(sid);
          continue;
        }

        if (session.type == 'group') {
          await ensureGroupAvatarMembers(session);
        } else if (session.type == 'private') {
          controller._ensurePrivatePeerIdentity(session);
        }
        controller._sessionDetailPrefetchQueued.remove(sid);

        if (controller._sessionDetailPrefetchQueue.isNotEmpty) {
          await Future<void>.delayed(
            ConversationsController._sessionDetailPrefetchInterval,
          );
        }
      }
    } finally {
      controller._isDrainingSessionDetailPrefetchQueue = false;
      if (controller._sessionDetailPrefetchQueue.isNotEmpty) {
        unawaited(drainSessionDetailPrefetchQueue());
      }
    }
  }

  Future<bool> pruneUnavailableSessionIfNeeded(
    String sessionId,
    SessionDetailResult detailResult,
  ) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;

    if (detailResult.code != 4003 && detailResult.code != 4004) {
      return false;
    }

    controller._groupAvatarMembersBySession.remove(sid);
    controller._groupAvatarMemberVersions.remove(sid);
    controller._resolvedPrivatePeerIdsBySession.remove(sid);
    controller._sessionsWithAgentMembers.remove(sid);
    controller._sessionDetailPrefetchQueued.remove(sid);
    controller._sessionDetailPrefetchQueue.removeWhere((item) => item == sid);
    controller._groupAvatarVersionForSession(sid).value++;
    await controller.imService.deleteConversation(sid);
    return true;
  }

  Future<void> ensureGroupAvatarMembers(
    SessionModel session, {
    bool forceRefresh = false,
  }) async {
    final sid = session.sessionId.trim();
    if (sid.isEmpty || session.type != 'group') return;

    if (!forceRefresh &&
        controller._groupAvatarMembersBySession.containsKey(sid)) {
      return;
    }

    final sessionService = controller._sessionService;
    if (sessionService == null) return;
    if (!controller._inflightGroupAvatarLoads.add(sid)) return;

    try {
      final detailResult = await sessionService.fetchSessionDetailResult(sid);
      if (await pruneUnavailableSessionIfNeeded(sid, detailResult)) {
        return;
      }
      final detail = detailResult.data;
      if (detail == null) return;
      if (controller._parseInt(detail['session_type']) != 2) return;

      final sourceMembers = controller._parseGroupAvatarSourceMembers(
        detail['members'],
      );
      if (sourceMembers.isEmpty) {
        if (controller._groupAvatarMembersBySession[sid]?.isNotEmpty == true ||
            session.cachedGroupAvatarMembers.isNotEmpty) {
          return;
        }
        controller._storeGroupAvatarMembers(sid, const <SessionAvatarMember>[]);
        return;
      }

      await controller._prepareGroupAvatarDependencies(sourceMembers);
      final members = sourceMembers
          .map(controller._buildConversationAvatarMember)
          .toList(growable: false);
      controller._storeGroupAvatarMembers(sid, members);
    } finally {
      controller._inflightGroupAvatarLoads.remove(sid);
    }
  }

  void _warmupConversationAvatar(ConversationListItem item) {
    if (controller.isGroupConversation(item)) {
      _warmupGroupConversationAvatar(item.latestSession);
      return;
    }

    final session = item.latestSession;
    if (session.peerType == 2) {
      _warmupAgentConversationAvatar(session);
      return;
    }

    _warmupPrivateConversationAvatar(session);
  }

  void _warmupGroupConversationAvatar(SessionModel session) {
    final sid = session.sessionId.trim();
    if (sid.isEmpty) {
      return;
    }

    final cachedMembers = controller._groupAvatarMembersBySession[sid];
    if (cachedMembers != null) {
      if (controller._needsGroupAvatarRefresh(sid)) {
        unawaited(ensureGroupAvatarMembers(session, forceRefresh: true));
      }
      _warmupAvatarUrls(cachedMembers.map((member) => member.avatarUrl));
      return;
    }

    final persisted = session.cachedGroupAvatarMembers;
    if (persisted.isNotEmpty) {
      controller._storeGroupAvatarMembers(
        sid,
        persisted,
        notify: false,
        persist: false,
      );
      _warmupAvatarUrls(persisted.map((member) => member.avatarUrl));
      unawaited(ensureGroupAvatarMembers(session, forceRefresh: true));
      return;
    }

    unawaited(
      ensureGroupAvatarMembers(session).then((_) {
        final members = controller._groupAvatarMembersBySession[sid];
        if (members != null) {
          _warmupAvatarUrls(members.map((member) => member.avatarUrl));
        }
      }),
    );
  }

  void _warmupAgentConversationAvatar(SessionModel session) {
    final agentId = session.peerId.trim();
    final agentService = controller._agentService;
    if (agentId.isEmpty || agentService == null) {
      return;
    }

    final idx = agentService.agents.indexWhere((agent) => agent.id == agentId);
    if (idx != -1) {
      _warmupAvatarUrls([agentService.agents[idx].avatarUrl]);
      return;
    }
    if (agentService.agents.isEmpty) {
      unawaited(
        agentService.loadAgents().then((_) {
          final loadedIdx = agentService.agents.indexWhere(
            (agent) => agent.id == agentId,
          );
          if (loadedIdx != -1) {
            _warmupAvatarUrls([agentService.agents[loadedIdx].avatarUrl]);
          }
        }),
      );
    }
  }

  void _warmupPrivateConversationAvatar(SessionModel session) {
    final resolvedPeerId = controller._resolvePrivatePeerId(session);
    if (resolvedPeerId.isEmpty) {
      controller._ensurePrivatePeerIdentity(session);
      return;
    }

    final existingAvatarUrl =
        controller._friendService?.getUserAvatarUrl(resolvedPeerId)?.trim() ??
        '';
    if (existingAvatarUrl.isNotEmpty) {
      controller._lastKnownPeerAvatarUrl[resolvedPeerId] = existingAvatarUrl;
      _warmupAvatarUrls([existingAvatarUrl]);
      return;
    }

    final friendService = controller._friendService;
    if (friendService == null) {
      return;
    }
    if (!controller._inflightPrivateAvatarWarmups.add(resolvedPeerId)) {
      return;
    }

    unawaited(
      friendService
          .fetchUserProfile(resolvedPeerId)
          .then((_) {
            final avatarUrl =
                friendService.getUserAvatarUrl(resolvedPeerId)?.trim() ?? '';
            if (avatarUrl.isEmpty) {
              return;
            }
            controller._lastKnownPeerAvatarUrl[resolvedPeerId] = avatarUrl;
            _warmupAvatarUrls([avatarUrl]);
          })
          .whenComplete(() {
            controller._inflightPrivateAvatarWarmups.remove(resolvedPeerId);
          }),
    );
  }

  void _warmupAvatarUrls(Iterable<String> avatarUrls) {
    final cacheManager = UserImageCacheManager.current();
    if (cacheManager == null) {
      return;
    }
    final now = DateTime.now();
    for (final avatarUrl in avatarUrls) {
      final normalizedAvatarUrl = avatarUrl.trim();
      if (normalizedAvatarUrl.isEmpty) {
        continue;
      }
      final cacheKey = UserImageCacheManager.cacheKeyForImageUrl(
        normalizedAvatarUrl,
      );
      if (cacheKey.isEmpty) {
        continue;
      }
      final retryAfter =
          controller._failedConversationAvatarWarmupUntil[cacheKey];
      if (retryAfter != null && retryAfter.isAfter(now)) {
        continue;
      }
      controller._failedConversationAvatarWarmupUntil.remove(cacheKey);
      if (!controller._warmedConversationAvatarUrls.add(cacheKey)) {
        continue;
      }
      unawaited(
        cacheManager
            .getSingleFile(normalizedAvatarUrl, key: cacheKey)
            .then<void>(
              (_) {
                controller._failedConversationAvatarWarmupUntil.remove(
                  cacheKey,
                );
              },
              onError: (Object _, StackTrace __) {
                controller._warmedConversationAvatarUrls.remove(cacheKey);
                controller._failedConversationAvatarWarmupUntil[cacheKey] =
                    DateTime.now().add(
                      ConversationsController._avatarWarmupFailureBackoff,
                    );
              },
            ),
      );
    }
  }
}
