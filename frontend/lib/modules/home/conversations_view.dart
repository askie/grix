import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:get/get.dart';
import '../../app/routes/app_route_observer.dart';
import '../../app/routes/app_routes.dart';
import '../../app/themes/app_theme.dart';
import '../../modules/call/call_controller.dart';
import '../../platform/platform_capability.dart';
import '../../shared/utils/chat_draft_index.dart';
import '../../shared/widgets/session_status_icon.dart';
import '../system/grix_connector_service.dart';
import 'controllers/contacts_controller.dart';
import 'controllers/conversations_controller.dart';
import 'controllers/home_controller.dart';
import 'widgets/conversation_reorder_sliver_list.dart';
import 'widgets/contact_quick_actions.dart';
import 'widgets/session_avatar_view.dart';
import '../chat/services/chat_pane_host.dart';
import '../ai/widgets/agent_quick_access_button.dart';

class ConversationsView extends StatefulWidget {
  const ConversationsView({super.key});

  static ValueKey<String> sessionTileKey(String groupKey) {
    return ConversationReorderSliverList.tileKey(groupKey);
  }

  @override
  State<ConversationsView> createState() => _ConversationsViewState();
}

class _ConversationsViewState extends State<ConversationsView>
    with WidgetsBindingObserver, RouteAware {
  static const Duration _scrollAnimationDuration = Duration(milliseconds: 260);
  static const Curve _scrollAnimationCurve = Curves.easeOutCubic;
  static const double _estimatedSearchBarHeight = 64;
  static const double _estimatedSessionTileHeight = 82;
  static const double _loadMoreScrollThreshold = 360;
  static const Duration _iOSWebPopRefreshDelay = Duration(milliseconds: 180);

  final ConversationsController controller =
      Get.find<ConversationsController>();
  final HomeController? _homeController = Get.isRegistered<HomeController>()
      ? Get.find<HomeController>()
      : null;
  final ScrollController _scrollController = ScrollController();
  final TextEditingController _searchController = TextEditingController();
  final Map<String, GlobalKey> _sessionTileKeys = <String, GlobalKey>{};
  ModalRoute<dynamic>? _route;
  Worker? _messagesTabRetapWorker;
  Worker? _messagesTabVisibilityWorker;
  Timer? _deferredPopRefreshTimer;
  bool _loadMoreCheckInFlight = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _scrollController.addListener(_handleScroll);
    final homeController = _homeController;
    if (homeController != null) {
      _messagesTabRetapWorker = ever<int>(homeController.messagesTabRetapTick, (
        _,
      ) {
        unawaited(_scrollUnreadConversationToTop());
      });
      _messagesTabVisibilityWorker = ever<int>(homeController.currentIndex, (
        index,
      ) {
        if (index == HomeTab.conversations.index) {
          unawaited(controller.refreshSessionsOnPageVisible());
        }
      });
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_isMessagesTabActive) {
        return;
      }
      unawaited(controller.refreshSessionsOnPageVisible());
    });
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final nextRoute = ModalRoute.of(context);
    if (nextRoute == null || identical(_route, nextRoute)) return;
    if (_route != null) appRouteObserver.unsubscribe(this);
    _route = nextRoute;
    appRouteObserver.subscribe(this, nextRoute);
  }

  @override
  void didPopNext() {
    if (_isIOSSafariWebRuntime) {
      _deferredPopRefreshTimer?.cancel();
      _deferredPopRefreshTimer = Timer(_iOSWebPopRefreshDelay, () {
        if (!mounted || !_isMessagesTabActive) {
          return;
        }
        unawaited(controller.triggerRefreshForVisiblePage());
      });
      return;
    }
    unawaited(controller.triggerRefreshForVisiblePage());
  }

  @override
  void dispose() {
    appRouteObserver.unsubscribe(this);
    WidgetsBinding.instance.removeObserver(this);
    _deferredPopRefreshTimer?.cancel();
    _messagesTabRetapWorker?.dispose();
    _messagesTabVisibilityWorker?.dispose();
    _scrollController.removeListener(_handleScroll);
    _scrollController.dispose();
    _searchController.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed || !_isMessagesTabActive) {
      return;
    }
    unawaited(controller.refreshSessionsOnPageVisible());
  }

  bool get _isMessagesTabActive {
    final homeController = _homeController;
    if (homeController == null) {
      return true;
    }
    return homeController.currentIndex.value == HomeTab.conversations.index;
  }

  bool get _isIOSSafariWebRuntime {
    if (!kIsWeb) {
      return false;
    }
    return defaultTargetPlatform == TargetPlatform.iOS;
  }

  /// 桌面端首页 Agent 工具栏显隐切换入口，与工具栏本身同条件（仅桌面端）。
  bool get _showAgentToolbarToggle {
    return PlatformCapability.isDesktop &&
        _homeController != null &&
        Get.isRegistered<GrixConnectorService>();
  }

  void _handleScroll() {
    if (!_isMessagesTabActive || _loadMoreCheckInFlight) {
      return;
    }
    if (!_scrollController.hasClients) {
      return;
    }
    final position = _scrollController.position;
    if (position.extentAfter > _loadMoreScrollThreshold) {
      return;
    }
    _loadMoreCheckInFlight = true;
    unawaited(
      controller.loadMoreSessionsForVisibleListIfNeeded().whenComplete(() {
        _loadMoreCheckInFlight = false;
      }),
    );
  }

  Future<void> _scrollUnreadConversationToTop() async {
    if (!_scrollController.hasClients) {
      return;
    }
    final target = _findTopUnreadConversation();
    if (target == null) {
      return;
    }
    final targetIndex = controller.groupedSessions.indexWhere(
      (item) => item.groupKey == target.groupKey,
    );
    if (targetIndex < 0) {
      return;
    }

    final key = _sessionTileKey(target.groupKey);
    if (key.currentContext == null) {
      final position = _scrollController.position;
      final estimatedOffset =
          _estimatedSearchBarHeight + targetIndex * _estimatedSessionTileHeight;
      final targetOffset = estimatedOffset.clamp(0.0, position.maxScrollExtent);
      if ((_scrollController.offset - targetOffset).abs() > 1) {
        await _scrollController.animateTo(
          targetOffset,
          duration: _scrollAnimationDuration,
          curve: _scrollAnimationCurve,
        );
        await WidgetsBinding.instance.endOfFrame;
      }
    }

    if (!mounted) {
      return;
    }
    final targetRenderObject = key.currentContext?.findRenderObject();
    if (targetRenderObject == null) {
      return;
    }
    final preciseOffset = _resolveTargetOffset(targetRenderObject);
    if ((_scrollController.offset - preciseOffset).abs() <= 1) {
      return;
    }
    await _scrollController.animateTo(
      preciseOffset,
      duration: _scrollAnimationDuration,
      curve: _scrollAnimationCurve,
    );
  }

  ConversationListItem? _findTopUnreadConversation() {
    for (final item in controller.groupedSessions) {
      if (item.unreadCount > 0) {
        return item;
      }
    }
    return null;
  }

  GlobalKey _sessionTileKey(String groupKey) {
    return _sessionTileKeys.putIfAbsent(
      groupKey,
      () => GlobalKey(debugLabel: 'conversation_tile_$groupKey'),
    );
  }

  void _pruneSessionTileKeys(List<ConversationListItem> sessions) {
    final activeKeys = sessions.map((item) => item.groupKey).toSet();
    _sessionTileKeys.removeWhere(
      (groupKey, _) => !activeKeys.contains(groupKey),
    );
  }

  double _resolveTargetOffset(RenderObject renderObject) {
    final viewport = RenderAbstractViewport.of(renderObject);
    final offset = viewport.getOffsetToReveal(renderObject, 0).offset;
    final position = _scrollController.position;
    return offset.clamp(position.minScrollExtent, position.maxScrollExtent);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      backgroundColor: theme.scaffoldBackgroundColor,
      appBar: AppBar(
        title: Text(
          'conversations_title'.tr,
          style: theme.textTheme.titleLarge?.copyWith(fontSize: 18),
        ),
        actions: [
          if (_showAgentToolbarToggle)
            Obx(() {
              final homeController = _homeController!;
              final visible = homeController.agentToolbarVisible.value;
              return IconButton(
                icon: Icon(
                  visible
                      ? Icons.visibility_outlined
                      : Icons.visibility_off_outlined,
                ),
                tooltip: visible
                    ? 'conversations_agent_toolbar_hide'.tr
                    : 'conversations_agent_toolbar_show'.tr,
                onPressed: homeController.toggleAgentToolbarVisibility,
              );
            }),
          IconButton(
            icon: const Icon(Icons.bookmark_border_rounded),
            tooltip: 'favorites_title'.tr,
            onPressed: () => Get.toNamed(AppRoutes.favorites),
          ),
          PopupMenuButton<ContactQuickAction>(
            tooltip: '',
            icon: Container(
              margin: const EdgeInsets.only(right: 4),
              padding: const EdgeInsets.all(6),
              decoration: BoxDecoration(
                color: theme.primaryColor.withValues(alpha: 0.1),
                borderRadius: BorderRadius.circular(10),
              ),
              child: Icon(
                Icons.add_rounded,
                color: theme.primaryColor,
                size: 20,
              ),
            ),
            offset: const Offset(0, 44),
            onSelected: (action) {
              switch (action) {
                case ContactQuickAction.addFriend:
                case ContactQuickAction.newGroup:
                  ContactQuickActions.handleSelection(
                    context,
                    Get.find<ContactsController>(),
                    action,
                  );
                  return;
                case ContactQuickAction.scanUserQr:
                  controller.openUserQrScanner();
                  return;
              }
            },
            itemBuilder: (context) => [
              PopupMenuItem<ContactQuickAction>(
                value: ContactQuickAction.scanUserQr,
                child: Row(
                  children: [
                    const Icon(
                      Icons.qr_code_scanner_rounded,
                      color: AppTheme.primaryColor,
                      size: 20,
                    ),
                    const SizedBox(width: 10),
                    Text('conversations_scan_user_qr'.tr),
                  ],
                ),
              ),
              PopupMenuItem<ContactQuickAction>(
                value: ContactQuickAction.addFriend,
                child: Row(
                  children: [
                    const Icon(
                      Icons.person_add_alt_1_rounded,
                      color: AppTheme.successColor,
                      size: 20,
                    ),
                    const SizedBox(width: 10),
                    Text('contacts_add_friend'.tr),
                  ],
                ),
              ),
              PopupMenuItem<ContactQuickAction>(
                value: ContactQuickAction.newGroup,
                child: Row(
                  children: [
                    const Icon(
                      Icons.group_add_rounded,
                      color: AppTheme.primaryColor,
                      size: 20,
                    ),
                    const SizedBox(width: 10),
                    Text('contacts_new_group'.tr),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(width: 8),
        ],
      ),
      body: Obx(() {
        final sessions = controller.groupedSessions;
        _pruneSessionTileKeys(sessions);

        return CustomScrollView(
          controller: _scrollController,
          key: const PageStorageKey<String>('home_conversations_scroll'),
          slivers: [
            SliverToBoxAdapter(child: _buildSearchBar(theme)),
            if (sessions.isEmpty)
              SliverFillRemaining(
                hasScrollBody: false,
                child: _buildEmptyState(theme, context),
              )
            else ...[
              ConversationReorderSliverList(
                sessions: sessions,
                itemBuilder: (context, item) {
                  return KeyedSubtree(
                    key: _sessionTileKey(item.groupKey),
                    child: _SessionTile(item: item, controller: controller),
                  );
                },
              ),
              if (!controller.hasAnyAgent)
                SliverToBoxAdapter(child: _buildAgentQuickAccessBanner()),
            ],
          ],
        );
      }),
    );
  }

  Widget _buildSearchBar(ThemeData theme) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      color: theme.appBarTheme.backgroundColor,
      child: Obx(() {
        final hasQuery = controller.searchQuery.value.isNotEmpty;
        return TextField(
          controller: _searchController,
          onChanged: controller.updateSearchQuery,
          decoration: InputDecoration(
            hintText: 'conversations_search'.tr,
            hintStyle: TextStyle(
              color: theme.colorScheme.secondary.withValues(alpha: 0.5),
              fontSize: 14,
            ),
            prefixIcon: Icon(
              Icons.search_rounded,
              color: theme.colorScheme.secondary.withValues(alpha: 0.5),
            ),
            suffixIcon: hasQuery
                ? IconButton(
                    icon: Icon(
                      Icons.clear_rounded,
                      size: 18,
                      color: theme.colorScheme.secondary.withValues(alpha: 0.6),
                    ),
                    onPressed: () {
                      _searchController.clear();
                      controller.searchQuery.value = '';
                    },
                    splashRadius: 14,
                    padding: EdgeInsets.zero,
                    constraints: const BoxConstraints(),
                  )
                : null,
            isDense: true,
          ),
        );
      }),
    );
  }

  /// 未创建任何 agent 时，附在消息列表下方的「极速接入」提示条，
  /// 让用户在手机端也能随时把各类 agent 接入进来。
  Widget _buildAgentQuickAccessBanner() {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 16),
      child: Center(
        child: AgentQuickAccessButton(
          onPressed: controller.openAgentQuickOnboard,
        ),
      ),
    );
  }

  Widget _buildEmptyState(ThemeData theme, BuildContext context) {
    if (!controller.hasUnfilteredSessions) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Container(
              width: 80,
              height: 80,
              decoration: BoxDecoration(
                color: theme.colorScheme.secondary.withValues(alpha: 0.05),
                shape: BoxShape.circle,
              ),
              child: Icon(
                Icons.chat_bubble_outline_rounded,
                size: 32,
                color: theme.colorScheme.secondary.withValues(alpha: 0.3),
              ),
            ),
            const SizedBox(height: 16),
            Text(
              'conversations_empty'.tr,
              style: TextStyle(
                color: theme.colorScheme.secondary.withValues(alpha: 0.6),
                fontSize: 14,
              ),
            ),
            if (!controller.hasAnyAgent) ...[
              const SizedBox(height: 16),
              AgentQuickAccessButton(
                onPressed: controller.openAgentQuickOnboard,
              ),
            ],
          ],
        ),
      );
    } else {
      return Center(
        child: Text(
          'conversations_no_match'.tr,
          style: TextStyle(
            color: theme.colorScheme.secondary.withValues(alpha: 0.6),
            fontSize: 14,
          ),
        ),
      );
    }
  }
}

class _SessionTile extends StatelessWidget {
  final ConversationListItem item;
  final ConversationsController controller;

  const _SessionTile({required this.item, required this.controller});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final session = item.latestSession;
    final avatarTitle = controller.getAvatarTitle(item);
    final secondaryText = controller.getConversationSecondaryText(item);
    final hasSecondaryText = secondaryText.trim().isNotEmpty;
    final hasVisibleUnread = item.hasVisibleUnread;
    final hasUnreadMention = item.hasUnreadMention;
    final showMutedUnreadMarker = item.shouldShowMutedUnreadMarker;

    final isVisitorGroup =
        item.groupKey == ConversationsController.visitorGroupKey;
    // Color assigned by string hash
    final avatarColor = AppTheme.getAvatarColor(controller.getAvatarSeed(item));
    final avatar = isVisitorGroup
        ? _VisitorGroupAvatar(theme: theme)
        : SessionAvatarView(
            session: item.latestSession,
            avatarTitle: avatarTitle,
            avatarColor: avatarColor,
          );

    return InkWell(
      onTap: () => controller.handleConversationTap(context, item),
      onLongPress: () => controller.showSessionMenu(context, item),
      child: Obx(() {
        // Desktop chat pane: keep the opened conversation highlighted.
        final isPaneActive =
            ChatPaneHost.activeSessionIdRx.value == session.sessionId;
        return Container(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
          decoration: BoxDecoration(
            color: isPaneActive
                ? theme.colorScheme.primary.withValues(alpha: 0.08)
                : hasUnreadMention
                ? AppTheme.warningColor.withValues(alpha: 0.08)
                : null,
            border: Border(
              bottom: BorderSide(
                color: hasUnreadMention
                    ? AppTheme.warningColor.withValues(alpha: 0.22)
                    : theme.colorScheme.outline.withValues(alpha: 0.08),
                width: 1,
              ),
            ),
          ),
          child: Row(
            children: [
              // Head
              Stack(
                clipBehavior: Clip.none,
                children: [
                  if (!isVisitorGroup && controller.canOpenAccountInfo(item))
                    InkWell(
                      onTap: () => controller.handleAvatarTap(item),
                      splashFactory: NoSplash.splashFactory,
                      highlightColor: theme.colorScheme.primary.withValues(
                        alpha: 0.06,
                      ),
                      borderRadius: BorderRadius.circular(24),
                      child: avatar,
                    )
                  else
                    avatar,
                  if (item.badgeUnreadCount > 0)
                    Positioned(
                      top: 0,
                      right: 0,
                      // 角标中心锚定头像右上角：先贴右上角(top:0,right:0)，再按自身
                      // 尺寸平移半格(右半宽、上半高)。与角标实际大小(随字体缩放/位数
                      // 变化)无关，各设备各字号都让角标中心落在头像右上角，不写死偏移。
                      child: FractionalTranslation(
                        translation: const Offset(0.5, -0.5),
                        child: Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 6,
                            vertical: 2,
                          ),
                          decoration: BoxDecoration(
                            color: AppTheme.unreadBadgeColor,
                            borderRadius: BorderRadius.circular(10),
                            border: Border.all(
                              color: theme.scaffoldBackgroundColor,
                              width: 2,
                            ),
                          ),
                          constraints: const BoxConstraints(
                            minWidth: 20,
                            minHeight: 20,
                          ),
                          child: Text(
                            item.badgeUnreadCount > 99
                                ? '99+'
                                : item.badgeUnreadCount.toString(),
                            style: const TextStyle(
                              color: Colors.white,
                              fontSize: 10,
                              fontWeight: FontWeight.w700,
                              height: 1,
                            ),
                            textAlign: TextAlign.center,
                          ),
                        ),
                      ),
                    ),
                ],
              ),
              const SizedBox(width: 14),
              // Message Content
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      mainAxisAlignment: MainAxisAlignment.spaceBetween,
                      children: [
                        Expanded(
                          child: Row(
                            children: [
                              Obx(() {
                                final active = item.sessions.any(
                                  (s) => controller.imService
                                      .hasSessionLiveActivity(s.sessionId),
                                );
                                return SessionStatusIcon(
                                  isPinned: item.isPinned,
                                  isActive: active,
                                  pinSize: 14,
                                  spacing: 6,
                                  pinColor: theme.primaryColor.withValues(
                                    alpha: 0.9,
                                  ),
                                );
                              }),
                              Expanded(
                                child: Row(
                                  children: [
                                    Expanded(
                                      child: Obx(
                                        () => Text(
                                          controller.getConversationListTitle(
                                            item,
                                          ),
                                          style: TextStyle(
                                            fontSize: 15,
                                            fontWeight:
                                                hasVisibleUnread ||
                                                    hasUnreadMention
                                                ? FontWeight.w700
                                                : FontWeight.w600,
                                            color: hasUnreadMention
                                                ? AppTheme.warningColor
                                                : theme.colorScheme.onSurface,
                                          ),
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                      ),
                                    ),
                                    if (isVisitorGroup &&
                                        item.threadCount > 1) ...[
                                      const SizedBox(width: 6),
                                      Container(
                                        padding: const EdgeInsets.symmetric(
                                          horizontal: 6,
                                          vertical: 2,
                                        ),
                                        decoration: BoxDecoration(
                                          color: theme.colorScheme.secondary
                                              .withValues(alpha: 0.1),
                                          borderRadius: BorderRadius.circular(
                                            6,
                                          ),
                                        ),
                                        child: Text(
                                          '${item.threadCount}',
                                          style: TextStyle(
                                            fontSize: 10,
                                            fontWeight: FontWeight.w600,
                                            color: theme.colorScheme.secondary
                                                .withValues(alpha: 0.7),
                                            height: 1.1,
                                          ),
                                        ),
                                      ),
                                    ] else if (!isVisitorGroup &&
                                        session.isVisitor) ...[
                                      const SizedBox(width: 6),
                                      Container(
                                        padding: const EdgeInsets.symmetric(
                                          horizontal: 6,
                                          vertical: 2,
                                        ),
                                        decoration: BoxDecoration(
                                          color: theme.primaryColor.withValues(
                                            alpha: 0.12,
                                          ),
                                          borderRadius: BorderRadius.circular(
                                            6,
                                          ),
                                        ),
                                        child: Text(
                                          'common_visitor'.tr,
                                          style: TextStyle(
                                            fontSize: 10,
                                            fontWeight: FontWeight.w600,
                                            color: theme.primaryColor,
                                            height: 1.1,
                                          ),
                                        ),
                                      ),
                                    ],
                                    if (hasUnreadMention) ...[
                                      const SizedBox(width: 6),
                                      _UnreadMentionBadge(
                                        label: 'conversations_mention_badge'.tr,
                                      ),
                                    ],
                                    Obx(() {
                                      ChatDraftIndex.version.value;
                                      final hasDraft = item.sessions.any(
                                        (s) => ChatDraftIndex.hasDraft(
                                          s.sessionId,
                                        ),
                                      );
                                      if (!hasDraft) {
                                        return const SizedBox.shrink();
                                      }
                                      return Row(
                                        mainAxisSize: MainAxisSize.min,
                                        children: [
                                          const SizedBox(width: 6),
                                          _DraftBadge(
                                            label:
                                                'conversations_draft_badge'.tr,
                                          ),
                                        ],
                                      );
                                    }),
                                    if (showMutedUnreadMarker) ...[
                                      const SizedBox(width: 6),
                                      Container(
                                        width: 6,
                                        height: 6,
                                        color: AppTheme.unreadBadgeColor,
                                      ),
                                    ],
                                  ],
                                ),
                              ),
                            ],
                          ),
                        ),
                        if (session.displayTime > 0) ...[
                          const SizedBox(width: 10),
                          Text(
                            controller.formatTime(session.displayTime),
                            style: TextStyle(
                              fontSize: 11,
                              color: hasUnreadMention
                                  ? AppTheme.warningColor
                                  : hasVisibleUnread
                                  ? theme.primaryColor
                                  : theme.colorScheme.secondary.withValues(
                                      alpha: 0.6,
                                    ),
                              fontWeight: hasVisibleUnread || hasUnreadMention
                                  ? FontWeight.w600
                                  : FontWeight.w400,
                            ),
                          ),
                        ],
                      ],
                    ),
                    if (hasSecondaryText) ...[
                      const SizedBox(height: 4),
                      Row(
                        children: [
                          Expanded(
                            child: Text(
                              secondaryText,
                              style: TextStyle(
                                fontSize: 13,
                                color: hasUnreadMention
                                    ? AppTheme.warningColor.withValues(
                                        alpha: 0.95,
                                      )
                                    : theme.colorScheme.secondary.withValues(
                                        alpha: hasVisibleUnread ? 0.9 : 0.6,
                                      ),
                                fontWeight: hasVisibleUnread || hasUnreadMention
                                    ? FontWeight.w500
                                    : FontWeight.w400,
                              ),
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                          // 语音中徽标：AI 正在代接该会话的访客通话
                          if (Get.isRegistered<CallController>())
                            Obx(() {
                              final hasVoice = item.sessions.any(
                                (s) => Get.find<CallController>()
                                    .hasVoiceCallForSession(s.sessionId),
                              );
                              if (!hasVoice) return const SizedBox.shrink();
                              return Padding(
                                padding: const EdgeInsets.only(left: 6),
                                child: Row(
                                  mainAxisSize: MainAxisSize.min,
                                  children: [
                                    const Icon(
                                      Icons.mic,
                                      size: 12,
                                      color: Colors.blueAccent,
                                    ),
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
                                ),
                              );
                            }),
                        ],
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        );
      }),
    );
  }
}

class _VisitorGroupAvatar extends StatelessWidget {
  final ThemeData theme;

  const _VisitorGroupAvatar({required this.theme});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 50,
      height: 50,
      decoration: BoxDecoration(
        color: theme.primaryColor.withValues(alpha: 0.12),
        shape: BoxShape.circle,
      ),
      child: Icon(
        Icons.support_agent_rounded,
        size: 26,
        color: theme.primaryColor.withValues(alpha: 0.8),
      ),
    );
  }
}

class _UnreadMentionBadge extends StatelessWidget {
  const _UnreadMentionBadge({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
      decoration: BoxDecoration(
        color: AppTheme.warningColor.withValues(alpha: 0.18),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: const TextStyle(
          color: AppTheme.warningColor,
          fontSize: 10,
          fontWeight: FontWeight.w700,
          height: 1,
        ),
      ),
    );
  }
}

/// 草稿标记：与 @提及 徽标同位展示，但用中性配色、不参与行高亮。
class _DraftBadge extends StatelessWidget {
  const _DraftBadge({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    final secondary = Theme.of(context).colorScheme.secondary;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
      decoration: BoxDecoration(
        color: secondary.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: TextStyle(
          color: secondary.withValues(alpha: 0.85),
          fontSize: 10,
          fontWeight: FontWeight.w600,
          height: 1,
        ),
      ),
    );
  }
}
