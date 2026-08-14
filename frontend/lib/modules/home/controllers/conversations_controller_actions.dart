part of 'conversations_controller.dart';

class _ConversationsControllerActions {
  const _ConversationsControllerActions(this.controller);

  final ConversationsController controller;

  void handleAvatarTap(ConversationListItem item) {
    if (!controller.canOpenAccountInfo(item)) {
      return;
    }

    if (!controller._isPrivateConversation(item)) {
      final session = item.latestSession;
      Get.toNamed(
        AppRoutes.groupInfo,
        arguments: {
          'session_id': session.sessionId,
          'title': controller.getConversationListTitle(item),
        },
        parameters: {'session_id': session.sessionId},
      );
      return;
    }

    final session = item.latestSession;
    final resolvedPeerId = controller._resolvePrivatePeerId(session);
    if (resolvedPeerId.isEmpty) {
      controller._ensurePrivatePeerIdentity(session);
    }
    final avatarUrl = controller.getConversationAvatarUrl(item);
    final displayName = controller.getPrivatePeerDisplayName(session).trim();

    Get.toNamed(
      AppRoutes.accountInfo,
      arguments: {
        'group_key': item.groupKey,
        'session_id': session.sessionId,
        'peer_id': resolvedPeerId,
        'peer_type': session.peerType.toString(),
        'nickname': displayName,
        'username': session.peerUsername.trim(),
        'avatar_url': avatarUrl,
        'title': controller.getConversationListTitle(item),
      },
      parameters: {
        'group_key': item.groupKey,
        'session_id': session.sessionId,
        'peer_id': resolvedPeerId,
        'peer_type': session.peerType.toString(),
      },
    );
  }

  void handleConversationTap(BuildContext context, ConversationListItem item) {
    // 单 thread 可直达；多 thread 必须先 await 服务端 threads，再决定直达/弹窗，
    // 避免用脏本地 unreadCount 误判。
    if (item.threadCount <= 1) {
      openChat(
        item.latestSession,
        initialGroupAvatarMembers: controller.getConversationAvatarMembers(
          item,
        ),
      );
      return;
    }

    unawaited(_openConversationThreads(context, item));
  }

  Future<void> _openConversationThreads(
    BuildContext context,
    ConversationListItem item,
  ) async {
    // 先拉 API threads 并批量 patch 未读；失败则回退本地，不卡死点击。
    final apiResult = await controller._fetchAndSyncThreadUnreadFromServer(
      item,
    );
    List<SessionModel> threads;
    var hasMore = false;
    var nextCursor = '';
    if (apiResult.success && apiResult.sessions.isNotEmpty) {
      final localForGroup = controller._resolveLocalSessionsForGroup(
        item.groupKey,
      );
      // sync 后内存已含服务端未读/override；merge 用本地 unread，再兜底 override。
      threads = controller.getThreadSessionsByLatestActivityDesc(
        controller._applyUnreadOverridesToThreads(
          ConversationsController.mergeApiThreadsWithLocalPreview(
            apiThreads: apiResult.sessions,
            localSessions: localForGroup.isNotEmpty
                ? localForGroup
                : item.sessions,
          ),
        ),
      );
      hasMore = apiResult.hasMore;
      nextCursor = apiResult.nextCursor;
    } else {
      final localResult = await controller.fetchConversationThreadSessions(
        item,
      );
      threads = controller._applyUnreadOverridesToThreads(localResult.sessions);
      hasMore = localResult.hasMore;
      nextCursor = localResult.nextCursor;
    }

    if (threads.length <= 1) {
      openChat(
        threads.isEmpty ? item.latestSession : threads.first,
        initialGroupAvatarMembers: controller.getConversationAvatarMembers(
          item,
        ),
      );
      return;
    }
    // 唯一未读 session 直达：基于（优先 API）对账后的 unreadCount
    final unreadThreads = threads.where((s) => s.unreadCount > 0).toList();
    if (unreadThreads.length == 1) {
      openChat(
        unreadThreads.first,
        initialGroupAvatarMembers: controller.getConversationAvatarMembers(
          item,
        ),
      );
      return;
    }
    if (!context.mounted) {
      return;
    }
    _showConversationThreadsSheet(
      context,
      item,
      threads,
      hasMore: hasMore,
      nextCursor: nextCursor,
    );
  }

  void _showConversationThreadsSheet(
    BuildContext context,
    ConversationListItem item,
    List<SessionModel> threads, {
    bool hasMore = false,
    String nextCursor = '',
  }) {
    final isVisitorGroup =
        item.groupKey == ConversationsController.visitorGroupKey;
    // 客服分组弹窗：固定标题、不提供新建会话入口
    final canCreateFreshSession =
        !isVisitorGroup && controller.canCreateFreshPrivateSession(item);
    final canOpenInfo =
        !isVisitorGroup && controller.canOpenAccountInfo(item);
    final peerAvatarUrl = controller.getConversationAvatarUrl(item);
    final peerAvatarTitle = controller.getAvatarTitle(item);
    final peerAvatarColor = AppTheme.getAvatarColor(controller.getAvatarSeed(item));
    final peerIsGroup = controller.isGroupConversation(item);
    final peerNickname = controller.getConversationHeaderTitle(item);
    final orderedSessions = controller.getThreadSessionsByLatestActivityDesc(
      threads,
    );
    var isLoadingMore = false;
    var isCreatingFreshSession = false;

    showModalBottomSheet(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) {
        return StatefulBuilder(
          builder: (sheetContext, setSheetState) {
            return Container(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Container(
                    width: 36,
                    height: 4,
                    margin: const EdgeInsets.only(bottom: 12),
                    decoration: BoxDecoration(
                      color: Theme.of(
                        sheetContext,
                      ).colorScheme.outline.withValues(alpha: 0.3),
                      borderRadius: BorderRadius.circular(2),
                    ),
                  ),
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
                    child: Row(
                      children: [
                        GestureDetector(
                          onTap: canOpenInfo
                              ? () {
                                  Get.back();
                                  controller.handleAvatarTap(item);
                                }
                              : null,
                          child: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              if (isVisitorGroup)
                                Container(
                                  width: 28,
                                  height: 28,
                                  decoration: BoxDecoration(
                                    color: Theme.of(sheetContext).primaryColor.withValues(alpha: 0.12),
                                    shape: BoxShape.circle,
                                  ),
                                  child: Icon(
                                    Icons.support_agent_rounded,
                                    size: 16,
                                    color: Theme.of(sheetContext).primaryColor.withValues(alpha: 0.8),
                                  ),
                                )
                              else
                                SessionAvatar(
                                  isGroup: peerIsGroup,
                                  avatarTitle: peerAvatarTitle,
                                  avatarColor: peerAvatarColor,
                                  avatarUrl: peerAvatarUrl,
                                  size: 28,
                                ),
                              const SizedBox(width: 6),
                              ConstrainedBox(
                                constraints: const BoxConstraints(maxWidth: 80),
                                child: Text(
                                  isVisitorGroup
                                      ? 'conversations_customer_service_title'.tr
                                      : peerNickname,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: TextStyle(
                                    fontSize: 12,
                                    color: Theme.of(sheetContext).colorScheme.secondary,
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ),
                        const SizedBox(width: 8),
                        const Spacer(),
                        Text(
                          controller._threadCountLabel(item.threadCount),
                          style: TextStyle(
                            fontSize: 12,
                            color: Theme.of(
                              sheetContext,
                            ).colorScheme.secondary.withValues(alpha: 0.7),
                          ),
                        ),
                        if (canCreateFreshSession) ...[
                          const SizedBox(width: 8),
                          // 立即用创建中页面替换 sheet；建会话网络请求在新页面后台进行，
                          // 成功后原位换成真实聊天页。
                          IconButton(
                            tooltip: 'conversations_new_session'.tr,
                            onPressed: () async {
                              if (isCreatingFreshSession) {
                                return;
                              }
                              isCreatingFreshSession = true;
                              setSheetState(() {});
                              try {
                                final sid = await createFreshPrivateSession(
                                  item,
                                  replaceCurrentRoute: true,
                                );
                                if (sid == null || sid.isEmpty) {
                                  return;
                                }
                              } finally {
                                if (sheetContext.mounted) {
                                  isCreatingFreshSession = false;
                                  setSheetState(() {});
                                }
                              }
                            },
                            icon: isCreatingFreshSession
                                ? SizedBox(
                                    width: 18,
                                    height: 18,
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                      color: Theme.of(
                                        sheetContext,
                                      ).colorScheme.primary,
                                    ),
                                  )
                                : const Icon(Icons.add_rounded),
                            visualDensity: const VisualDensity(
                              horizontal: -2,
                              vertical: -2,
                            ),
                            splashRadius: 18,
                          ),
                        ],
                      ],
                    ),
                  ),
                  Flexible(
                    child: NotificationListener<ScrollNotification>(
                      onNotification: (notification) {
                        if (notification is ScrollEndNotification &&
                            notification.metrics.pixels >=
                                notification.metrics.maxScrollExtent - 100 &&
                            hasMore &&
                            !isLoadingMore) {
                          isLoadingMore = true;
                          setSheetState(() {});
                          controller
                              ._fetchAndSyncThreadUnreadFromServer(
                            item,
                            cursor: nextCursor,
                          )
                              .then((moreResult) async {
                            ConversationThreadPageResult effective =
                                moreResult;
                            if (!moreResult.success ||
                                moreResult.sessions.isEmpty) {
                              // API 失败时回退本地分页口径，避免无法加载更多。
                              effective = await controller
                                  .fetchConversationThreadSessions(
                                item,
                                cursor: nextCursor,
                              );
                            } else {
                              final localForGroup = controller
                                  ._resolveLocalSessionsForGroup(item.groupKey);
                              effective = ConversationThreadPageResult(
                                groupKey: moreResult.groupKey,
                                sessions: controller
                                    ._applyUnreadOverridesToThreads(
                                  ConversationsController
                                      .mergeApiThreadsWithLocalPreview(
                                    apiThreads: moreResult.sessions,
                                    localSessions: localForGroup.isNotEmpty
                                        ? localForGroup
                                        : item.sessions,
                                  ),
                                ),
                                success: true,
                                hasMore: moreResult.hasMore,
                                nextCursor: moreResult.nextCursor,
                              );
                            }
                            if (!sheetContext.mounted) return;
                            if (effective.success &&
                                effective.sessions.isNotEmpty) {
                              final existingIds = orderedSessions
                                  .map((s) => s.sessionId)
                                  .toSet();
                              final deduped = effective.sessions
                                  .where((s) => !existingIds.contains(s.sessionId))
                                  .toList();
                              if (deduped.isEmpty) {
                                isLoadingMore = false;
                                setSheetState(() {});
                                return;
                              }
                              final newSessions =
                                  controller.getThreadSessionsByLatestActivityDesc(
                                [...orderedSessions, ...deduped],
                              );
                              orderedSessions
                                ..clear()
                                ..addAll(newSessions);
                              hasMore = effective.hasMore;
                              nextCursor = effective.nextCursor;
                            }
                            isLoadingMore = false;
                            setSheetState(() {});
                          });
                        }
                        return false;
                      },
                      child: ListView.builder(
                        shrinkWrap: true,
                        itemCount: orderedSessions.length + (hasMore ? 1 : 0),
                        itemBuilder: (context, index) {
                          if (index >= orderedSessions.length) {
                            return const Padding(
                              padding: EdgeInsets.symmetric(vertical: 12),
                              child: Center(
                                child: SizedBox(
                                  width: 16,
                                  height: 16,
                                  child: CircularProgressIndicator(strokeWidth: 2),
                                ),
                              ),
                            );
                          }
                          final session = orderedSessions[index];
                          return _ThreadSessionTile(
                            key: ValueKey(session.sessionId),
                            sessionId: session.sessionId,
                            controller: controller,
                            conversationItem: item,
                            fallbackSession: session,
                            onPinToggled: (toggledSessionId) {
                              if (!sheetContext.mounted) return;
                              // 只刷新被操作行的会话对象再重排：比较器是
                              // 全序（末尾 sessionId 决胜），其余行保持打开
                              // 时捕获的数据，不会因最新 activityAt 位移。
                              final refreshed =
                                  controller.getThreadSessionsByLatestActivityDesc([
                                for (final s in orderedSessions)
                                  s.sessionId == toggledSessionId
                                      ? controller.imService.sessions
                                          .firstWhere(
                                          (x) => x.sessionId == s.sessionId,
                                          orElse: () => s,
                                        )
                                      : s,
                              ]);
                              orderedSessions
                                ..clear()
                                ..addAll(refreshed);
                              setSheetState(() {});
                            },
                            onTap: (session) {
                              Get.back();
                              openChat(
                                session,
                                initialGroupAvatarMembers:
                                    controller.isGroupConversation(item)
                                    ? controller.getConversationAvatarMembers(item)
                                    : const <SessionAvatarMember>[],
                              );
                            },
                          );
                        },
                      ),
                    ),
                  ),
                  SizedBox(height: MediaQuery.of(sheetContext).padding.bottom),
                ],
              ),
            );
          },
        );
      },
    );
  }

  Future<String?> createFreshPrivateSession(
    ConversationListItem item, {
    bool openChat = true,
    bool replaceCurrentRoute = false,
  }) async {
    if (!controller.canCreateFreshPrivateSession(item)) {
      return null;
    }

    final peerTarget = await _resolvePeerTargetForFreshSession(
      item.latestSession,
    );
    if (peerTarget == null) {
      CustomToast.show('contacts_create_session_failed'.tr);
      return null;
    }

    final sid = await ChatRouteNavigator.createAndOpenPrivateChat(
      peerId: peerTarget.peerId,
      peerType: peerTarget.peerType,
      fallbackTitle: _resolveFreshSessionRouteTitle(item).trim(),
      openChat: openChat,
      replaceCurrentRoute: replaceCurrentRoute,
    );
    if (sid == null) {
      CustomToast.show('contacts_create_session_failed'.tr);
      return null;
    }
    return sid;
  }

  Future<_FreshPrivateSessionPeerTarget?> _resolvePeerTargetForFreshSession(
    SessionModel session,
  ) async {
    if (session.type.trim().toLowerCase() != 'private') {
      return null;
    }

    final peerType = session.peerType;
    if (peerType <= 0) {
      return null;
    }

    if (peerType == 2) {
      final agentId = session.peerId.trim();
      if (agentId.isEmpty) {
        return null;
      }
      return _FreshPrivateSessionPeerTarget(peerId: agentId, peerType: 2);
    }

    final directPeerId = controller._resolvePrivatePeerId(session).trim();
    if (directPeerId.isNotEmpty) {
      return _FreshPrivateSessionPeerTarget(
        peerId: directPeerId,
        peerType: peerType,
      );
    }

    final sessionService = controller._sessionService;
    if (sessionService == null) {
      return null;
    }
    final detailResult = await sessionService.fetchSessionDetailResult(
      session.sessionId,
    );
    final detail = detailResult.data;
    if (detail == null) {
      return null;
    }

    final sessionType = controller._parseInt(detail['session_type']);
    if (sessionType != 1) {
      return null;
    }

    final members = detail['members'];
    if (members is! List) {
      return null;
    }

    final myUserId = controller._authService?.userId?.trim() ?? '';
    if (myUserId.isEmpty) {
      return null;
    }
    for (final member in members) {
      if (member is! Map) {
        continue;
      }
      final memberType = controller._parseInt(member['member_type']);
      if (memberType != peerType) {
        continue;
      }
      final memberId = (member['member_id'] ?? '').toString().trim();
      if (memberId.isEmpty || memberId == myUserId) {
        continue;
      }
      controller._resolvedPrivatePeerIdsBySession[session.sessionId] = memberId;
      return _FreshPrivateSessionPeerTarget(
        peerId: memberId,
        peerType: peerType,
      );
    }

    return null;
  }

  String _resolveFreshSessionRouteTitle(ConversationListItem item) {
    final latest = item.latestSession;
    final candidates = <String>[
      resolvePrivateRouteTitle(latest),
      controller.getConversationHeaderTitle(item),
      controller.getConversationListTitle(item),
      latest.peerNickname,
      latest.peerUsername,
      latest.peerId,
    ];
    for (final candidate in candidates) {
      final normalized = candidate.trim();
      if (normalized.isNotEmpty) {
        return normalized;
      }
    }
    return '';
  }

  void openChat(
    SessionModel session, {
    List<SessionAvatarMember> initialGroupAvatarMembers =
        const <SessionAvatarMember>[],
  }) {
    final displayTitle = session.type == 'private'
        ? resolvePrivateRouteTitle(session)
        : controller.getDisplayTitle(session);
    ChatRouteNavigator.toChat(
      sessionId: session.sessionId,
      title: displayTitle,
      type: session.type,
      initialGroupAvatarMembers: initialGroupAvatarMembers,
    );
  }

  String resolvePrivateRouteTitle(SessionModel session) {
    final peerDisplayName = controller
        .getPrivatePeerDisplayName(session)
        .trim();
    if (peerDisplayName.isNotEmpty) {
      return peerDisplayName;
    }

    final username = session.peerUsername.trim();
    if (username.isNotEmpty) {
      return username;
    }

    if (session.peerType == 2) {
      final threadTitle = controller._getPrivateConversationThreadTitle(
        session,
      );
      if (threadTitle.isNotEmpty) {
        return threadTitle;
      }
    }

    return '';
  }

  void showSessionMenu(BuildContext pageContext, ConversationListItem item) {
    // 防重复触发：菜单未关闭前再次长按直接忽略。
    SheetGuard.run<void>(
      'session_menu',
      () => _showSessionMenuSheet(pageContext, item),
    );
  }

  Future<void> _showSessionMenuSheet(
    BuildContext pageContext,
    ConversationListItem item,
  ) {
    final latest = item.latestSession;
    return showModalBottomSheet<void>(
      context: pageContext,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) => SafeArea(
        top: false,
        child: ConstrainedBox(
          constraints: BoxConstraints(
            maxHeight: MediaQuery.of(sheetContext).size.height * 0.65,
          ),
          child: SingleChildScrollView(
            child: Container(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Container(
                    width: 36,
                    height: 4,
                    margin: const EdgeInsets.only(bottom: 12),
                    decoration: BoxDecoration(
                      color: Theme.of(
                        sheetContext,
                      ).colorScheme.outline.withValues(alpha: 0.3),
                      borderRadius: BorderRadius.circular(2),
                    ),
                  ),
                  ListTile(
                    leading: Icon(
                      item.isPinned
                          ? Icons.push_pin_rounded
                          : Icons.push_pin_outlined,
                    ),
                    title: Text(
                      item.isPinned
                          ? 'conversations_unpin'.tr
                          : 'conversations_pin'.tr,
                    ),
                    onTap: () async {
                      if (!popSheetOnce(sheetContext)) return;
                      await controller.setSessionGroupPinned(
                        item,
                        isPinned: !item.isPinned,
                      );
                    },
                  ),
                  ListTile(
                    leading: Icon(
                      item.unreadCount > 0
                          ? Icons.mark_chat_read_outlined
                          : Icons.mark_chat_unread_outlined,
                    ),
                    title: Text(
                      item.unreadCount > 0
                          ? 'conversations_mark_read'.tr
                          : 'conversations_mark_unread'.tr,
                    ),
                    onTap: () {
                      if (!popSheetOnce(sheetContext)) return;
                      if (item.unreadCount > 0) {
                        controller.markSessionGroupRead(item);
                      } else {
                        controller.markSessionGroupUnread(item);
                      }
                    },
                  ),
                  ListTile(
                    leading: Icon(
                      item.isMuted
                          ? Icons.notifications_active_outlined
                          : Icons.notifications_off_outlined,
                    ),
                    title: Text(
                      item.isMuted
                          ? 'conversations_unmute_notifications'.tr
                          : 'conversations_mute_notifications'.tr,
                    ),
                    onTap: () async {
                      if (!popSheetOnce(sheetContext)) return;
                      final ok = await controller.setSessionGroupMuted(
                        item,
                        isMuted: !item.isMuted,
                      );
                      if (!ok) {
                        CustomToast.show('chat_notification_update_failed'.tr);
                      }
                    },
                  ),

                  Obx(() {
                    final isFavorited = controller
                        .isSessionFavorited(latest.sessionId);
                    return ListTile(
                      leading: Icon(
                        isFavorited
                            ? Icons.bookmark_rounded
                            : Icons.bookmark_border_rounded,
                        color: isFavorited
                            ? AppTheme.primaryColor
                            : null,
                      ),
                      title: Text(
                        isFavorited
                            ? 'conversations_unfavorite'.tr
                            : 'conversations_favorite'.tr,
                      ),
                      onTap: () async {
                        if (!popSheetOnce(sheetContext)) return;
                        await controller.toggleSessionFavorite(
                            latest.sessionId);
                      },
                    );
                  }),

                  ListTile(
                    leading: const Icon(
                      Icons.delete_outline_rounded,
                      color: AppTheme.errorColor,
                    ),
                    title: Text(
                      'common_delete'.tr,
                      style: const TextStyle(color: AppTheme.errorColor),
                    ),
                    onTap: () async {
                      if (!popSheetOnce(sheetContext)) return;
                      final confirmed = await showDeleteConfirm(pageContext);
                      if (confirmed) {
                        await controller.deleteSession(latest);
                      }
                    },
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Future<bool> showDeleteConfirm(
    BuildContext context, {
    String? title,
    String? content,
  }) {
    return showAppConfirmDialog(
      context: context,
      title: title ?? 'common_confirm'.tr,
      message:
          '${content ?? 'conversations_delete_confirm'.tr}\n${'conversations_delete_local_only'.tr}',
      confirmText: 'common_delete'.tr,
      isDestructive: true,
    );
  }
}

class _FreshPrivateSessionPeerTarget {
  const _FreshPrivateSessionPeerTarget({
    required this.peerId,
    required this.peerType,
  });

  final String peerId;
  final int peerType;
}

/// 会话线程列表中的单个 tile，内部用 Obx 独立响应自身 session 数据变化，
/// 避免整个列表因任意一条摘要更新而全量重建。
class _ThreadSessionTile extends StatelessWidget {
  const _ThreadSessionTile({
    super.key,
    required this.sessionId,
    required this.controller,
    required this.conversationItem,
    this.fallbackSession,
    required this.onTap,
    this.onPinToggled,
  });

  final String sessionId;
  final ConversationsController controller;
  final ConversationListItem conversationItem;
  final SessionModel? fallbackSession;
  final void Function(SessionModel session) onTap;

  /// 置顶/取消置顶成功后回调（带被操作的 sessionId）：让宿主列表重排。
  final void Function(String sessionId)? onPinToggled;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final idx = controller.imService.sessions.indexWhere(
        (s) => s.sessionId == sessionId,
      );
      final session = idx == -1
          ? fallbackSession
          : controller.imService.sessions[idx];
      if (session == null) return const SizedBox.shrink();

      final unread = session.unreadCount;
      final threadTitle = controller.getSessionThreadTitle(session);
      final preview = controller.getSessionThreadPreview(session);
      final displayTime = session.lastMessageTime > 0
          ? session.lastMessageTime
          : session.activityAt;
      final timeLabel = controller.formatTime(displayTime);
      final showPreview = preview.isNotEmpty && preview != threadTitle;

      return ListTile(
        onTap: () => onTap(session),
        onLongPress: () => _showTileMenu(context, session),
        title: Row(
          children: [
            SessionStatusIcon(
              isPinned: session.isPinned,
              isActive: controller.imService
                  .hasSessionLiveActivity(session.sessionId),
              spacing: 6,
            ),
            Expanded(
              child: Text(
                threadTitle,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(
                  fontSize: 15,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            // 语音中徽标：标出正在通话的会话，与折叠入口的徽标保持一致
            if (Get.isRegistered<CallController>() &&
                Get.find<CallController>().hasVoiceCallForSession(
                  session.sessionId,
                )) ...[
              const SizedBox(width: 6),
              const Icon(Icons.mic, size: 12, color: Colors.blueAccent),
              const SizedBox(width: 2),
              Text(
                'call_ai_voice_active'.tr,
                style: const TextStyle(
                  fontSize: 11,
                  color: Colors.blueAccent,
                  fontWeight: FontWeight.w500,
                ),
              ),
            ],
            if (session.isMuted) ...[
              const SizedBox(width: 6),
              Container(width: 6, height: 6, color: AppTheme.unreadBadgeColor),
            ],
            if (timeLabel.isNotEmpty) ...[
              const SizedBox(width: 6),
              Text(
                timeLabel,
                style: TextStyle(
                  color: Theme.of(
                    context,
                  ).colorScheme.secondary.withValues(alpha: 0.7),
                  fontSize: 12,
                ),
              ),
            ],
          ],
        ),
        subtitle: showPreview
            ? Text(
                preview,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  color: Theme.of(
                    context,
                  ).colorScheme.secondary.withValues(alpha: 0.9),
                  fontSize: 13,
                ),
              )
            : null,
        isThreeLine: false,
        dense: !showPreview,
        minVerticalPadding: 8,
        visualDensity: const VisualDensity(vertical: -1),
        contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 2),
        trailing: unread <= 0 || session.isMuted
            ? null
            : Container(
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                decoration: BoxDecoration(
                  color: AppTheme.unreadBadgeColor,
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Text(
                  unread > 99 ? '99+' : unread.toString(),
                  style: const TextStyle(
                    color: Colors.white,
                    fontSize: 10,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
      );
    });
  }

  void _showTileMenu(BuildContext context, SessionModel session) {
    // 防重复触发：菜单未关闭前再次长按直接忽略。
    SheetGuard.run<void>(
      'session_tile_menu',
      () => _showTileMenuSheet(context, session),
    );
  }

  Future<void> _showTileMenuSheet(BuildContext context, SessionModel session) {
    return showModalBottomSheet<void>(
      context: context,
      backgroundColor: Theme.of(context).colorScheme.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: Icon(
                session.isPinned
                    ? Icons.push_pin_outlined
                    : Icons.push_pin_rounded,
              ),
              title: Text(
                session.isPinned
                    ? 'conversations_unpin'.tr
                    : 'conversations_pin'.tr,
              ),
              onTap: () async {
                if (!popSheetOnce(sheetContext)) return;
                final success = await controller.imService.setSessionPinned(
                  session.sessionId,
                  isPinned: !session.isPinned,
                );
                if (success) onPinToggled?.call(session.sessionId);
              },
            ),
            ListTile(
              leading: Icon(
                session.isMuted
                    ? Icons.notifications_active_outlined
                    : Icons.notifications_off_outlined,
              ),
              title: Text(
                session.isMuted
                    ? 'conversations_unmute_notifications'.tr
                    : 'conversations_mute_notifications'.tr,
              ),
              onTap: () async {
                if (!popSheetOnce(sheetContext)) return;
                await controller.imService.setSessionMuted(
                  session.sessionId,
                  isMuted: !session.isMuted,
                );
              },
            ),
            Obx(() {
              final isFavorited =
                  controller.isSessionFavorited(session.sessionId);
              return ListTile(
                leading: Icon(
                  isFavorited
                      ? Icons.bookmark_rounded
                      : Icons.bookmark_border_rounded,
                  color: isFavorited ? AppTheme.primaryColor : null,
                ),
                title: Text(
                  isFavorited
                      ? 'conversations_unfavorite'.tr
                      : 'conversations_favorite'.tr,
                ),
                onTap: () async {
                  if (!popSheetOnce(sheetContext)) return;
                  await controller.toggleSessionFavorite(session.sessionId);
                },
              );
            }),
          ],
        ),
      ),
    );
  }
}
