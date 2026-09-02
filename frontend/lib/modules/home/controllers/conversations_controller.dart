import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';
import '../../../app/routes/app_routes.dart';
import '../../../app/themes/app_theme.dart';
import '../../../data/models/conversation_summary_model.dart';
import '../../../data/models/session_model.dart';
import '../../../data/providers/agent_service.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/friend_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../data/providers/local_db.dart';
import '../../../data/providers/session_service.dart';
import '../../../data/providers/user_session_favorite_service.dart';
import '../../account_info/services/account_info_navigator.dart';
import '../../call/call_controller.dart';
import '../../chat/services/chat_route_navigator.dart';
import '../../../shared/models/session_avatar_member.dart';
import '../../../shared/utils/chat_draft_index.dart';
import '../../../shared/utils/chat_message_preview.dart';
import '../../../shared/utils/chat_numeric_mention_resolver.dart';
import '../../../shared/utils/sheet_guard.dart';
import '../../../shared/utils/user_image_cache_manager.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/widgets/app_dialog_style.dart';
import '../../../shared/widgets/session_avatar.dart';
import '../../../shared/widgets/session_status_icon.dart';
import 'home_controller.dart';
import '../services/friend_qr_flow_service.dart';
import '../services/home_sidebar_host.dart';

part 'conversations_controller_actions.dart';
part 'conversations_controller_identity.dart';
part 'conversations_controller_prefetch.dart';
part 'conversations_controller_unread_mentions.dart';

class ConversationListItem {
  const ConversationListItem({
    required this.groupKey,
    required this.latestSession,
    required this.sessions,
    required this.unreadCount,
    this.hasUnreadMention = false,
    this.badgeUnreadCount = 0,
    this.hasMutedUnread = false,
    this.isMuted = false,
    required this.isPinned,
    required this.pinnedAt,
    this.threadCountOverride,
  });

  final String groupKey;
  final SessionModel latestSession;
  final List<SessionModel> sessions;
  final int unreadCount;
  final bool hasUnreadMention;
  final int badgeUnreadCount;
  final bool hasMutedUnread;
  final bool isMuted;
  final bool isPinned;
  final int pinnedAt;
  final int? threadCountOverride;

  int get threadCount => threadCountOverride ?? sessions.length;
  bool get hasVisibleUnread => badgeUnreadCount > 0;
  bool get shouldShowMutedUnreadMarker => hasMutedUnread;

  @override
  bool operator ==(Object other) {
    if (identical(this, other)) {
      return true;
    }
    return other is ConversationListItem &&
        other.groupKey == groupKey &&
        other.latestSession == latestSession &&
        listEquals(other.sessions, sessions) &&
        other.unreadCount == unreadCount &&
        other.hasUnreadMention == hasUnreadMention &&
        other.badgeUnreadCount == badgeUnreadCount &&
        other.hasMutedUnread == hasMutedUnread &&
        other.isMuted == isMuted &&
        other.isPinned == isPinned &&
        other.pinnedAt == pinnedAt &&
        other.threadCountOverride == threadCountOverride;
  }

  @override
  int get hashCode => Object.hash(
    groupKey,
    latestSession,
    Object.hashAll(sessions),
    unreadCount,
    hasUnreadMention,
    badgeUnreadCount,
    hasMutedUnread,
    isMuted,
    isPinned,
    pinnedAt,
    threadCountOverride,
  );
}

class ConversationsController extends GetxController {
  static const bool _useConversationListApi = bool.fromEnvironment(
    'USE_CONVERSATION_LIST_API',
    defaultValue: true,
  );
  static const Duration _conversationRealtimeRefreshDelay = Duration(
    milliseconds: 800,
  );
  static const Duration _conversationRealtimeRefreshMinInterval = Duration(
    seconds: 5,
  );
  static const int _maxSessionDetailPrefetchQueueSize = 24;
  static const int _topSessionDetailPrefetchCount = 8;
  static const int _initialConversationAvatarWarmupCount = 8;
  static const int _targetVisibleConversationGroups = 20;
  static const Duration _sessionDetailPrefetchInterval = Duration(
    milliseconds: 2200,
  );
  static const Duration _avatarWarmupFailureBackoff = Duration(minutes: 5);
  static const Duration _pageVisibleRefreshMaxAge = Duration(seconds: 120);
  static const Duration _conversationPageMinInterval = Duration(seconds: 1);
  // 实时活动驱动的"纯重排"合并窗口：窗口内的多次顺序变化只落地一次，
  // 避免两个活跃会话每来一条消息就轮流被拽到顶导致列表频繁上下跳。
  static const Duration _reorderCoalesceWindow = Duration(milliseconds: 1500);
  // 去抖阈值：活跃时间差小于该值的会话视为"同一档"，保持当前相对顺序，
  // 不因毫秒级/本地与后端口径的微小差异而互相换位。
  static const int _reorderHysteresisMs = 2000;
  // 合并同一短窗内多次 sessions Rx 通知，避免每条实时写入都同步全表重排。
  static const Duration _sessionsRebuildDebounce = Duration(milliseconds: 100);

  final ImService imService = Get.find<ImService>();
  final AgentService? _agentService = Get.isRegistered<AgentService>()
      ? Get.find<AgentService>()
      : null;
  final FriendService? _friendService = Get.isRegistered<FriendService>()
      ? Get.find<FriendService>()
      : null;
  final SessionService? _sessionService = Get.isRegistered<SessionService>()
      ? Get.find<SessionService>()
      : null;
  final AuthService? _authService = Get.isRegistered<AuthService>()
      ? Get.find<AuthService>()
      : null;
  final HomeController? _homeController = Get.isRegistered<HomeController>()
      ? Get.find<HomeController>()
      : null;
  final FriendQrFlowService _friendQrFlowService =
      Get.find<FriendQrFlowService>();
  final _favoriteService = UserSessionFavoriteService();
  final _favoritedSessionIds = <String>{}.obs;
  late final _ConversationsControllerIdentity _identity =
      _ConversationsControllerIdentity(this);
  late final _ConversationsControllerPrefetch _prefetch =
      _ConversationsControllerPrefetch(this);
  late final _ConversationsControllerActions _actions =
      _ConversationsControllerActions(this);
  late final _ConversationsUnreadMentions _unreadMentions =
      _ConversationsUnreadMentions(this);
  final _resolvedPrivatePeerNames = <String, String>{};
  final _resolvedPrivatePeerIdsBySession = <String, String>{};
  final _lastKnownPeerAvatarUrl = <String, String>{};
  final _groupAvatarMembersBySession = <String, List<SessionAvatarMember>>{};
  final _peerAvatarUrlBySession = <String, String>{};
  final _groupAvatarMemberVersions = <String, int>{};
  final _inflightPeerNameLoads = <String>{};
  final _inflightPeerIdLoads = <String>{};
  final _inflightGroupAvatarLoads = <String>{};
  final _sessionDetailPrefetchQueue = <String>[];
  final _sessionDetailPrefetchQueued = <String>{};
  final _inflightPrivateAvatarWarmups = <String>{};
  final _warmedConversationAvatarUrls = <String>{};
  final _failedConversationAvatarWarmupUntil = <String, DateTime>{};
  final _peerNameRefreshVersion = 0.obs;
  final _groupAvatarVersionBySession = <String, RxInt>{};
  final _peerAvatarVersionByPeerId = <String, RxInt>{};
  final _privateAvatarVersionBySession = <String, RxInt>{};
  final _sessionsWithAgentMembers = <String>{};
  Worker? _friendListWorker;
  Worker? _profileCacheWorker;
  Worker? _agentListWorker;
  Worker? _sessionsWorker;
  Worker? _sessionsLoadWorker;
  Worker? _currentSessionWorker;
  Worker? _streamingPreviewWorker;
  Worker? _searchQueryWorker;
  bool _isDrainingSessionDetailPrefetchQueue = false;
  bool _pageVisibleRefreshInFlight = false;
  Timer? _deferredPageVisibleRefreshTimer;
  Timer? _deferredConversationSummaryRefreshTimer;
  Timer? _deferredSessionsRebuildTimer;
  Timer? _reorderCommitTimer;
  List<ConversationListItem>? _pendingReorderItems;

  /// sessionId -> cached groupKey; invalidated when identity fields change.
  final Map<String, _CachedConversationGroupKey> _conversationGroupKeyCache =
      <String, _CachedConversationGroupKey>{};

  /// Last optimistic activity snapshot; skip reorder when unchanged.
  Map<String, int>? _lastOptimisticActivityByGroup;

  final searchQuery = ''.obs;
  final _groupedSessions = <ConversationListItem>[].obs;
  final _conversationSummaryItems = <ConversationListItem>[];

  /// Accumulated conversation summaries for LocalDb pin reconcile across pages.
  final _conversationPinReconcileSummaries = <ConversationSummaryModel>[];
  final _hasUnfilteredSessions = false.obs;
  bool _conversationListApiActive = false;
  bool _conversationPageInFlight = false;
  bool _conversationHasMore = true;
  String _conversationNextCursor = '';
  DateTime _conversationNextAllowedAt = DateTime.fromMillisecondsSinceEpoch(0);
  DateTime _conversationLastRefreshAttemptAt =
      DateTime.fromMillisecondsSinceEpoch(0);

  @override
  void onInit() {
    super.onInit();
    final fs = _friendService;
    if (fs != null) {
      _friendListWorker = ever(fs.friendList, (_) {
        _peerNameRefreshVersion.value++;
      });
      _profileCacheWorker = ever(fs.profileCacheVersion, (_) {
        _peerNameRefreshVersion.value++;
        // Only bump avatar versions for peers whose avatar URL actually changed,
        // avoiding a full-list avatar rebuild on every profile cache update.
        for (final entry in _resolvedPrivatePeerIdsBySession.entries) {
          final peerId = entry.value;
          final newUrl = _friendService?.getUserAvatarUrl(peerId)?.trim() ?? '';
          final oldUrl = _lastKnownPeerAvatarUrl[peerId] ?? '';
          if (newUrl != oldUrl) {
            _lastKnownPeerAvatarUrl[peerId] = newUrl;
            _peerAvatarVersionForPeer(peerId).value++;
          }
        }
      });
      if (fs.friendList.isEmpty) {
        unawaited(fs.loadFriendList());
      }
    }

    final agentService = _agentService;
    if (agentService != null) {
      _agentListWorker = ever(agentService.agents, (_) {
        _peerNameRefreshVersion.value++;
        for (final sid in _sessionsWithAgentMembers.toList()) {
          _groupAvatarMembersBySession.remove(sid);
          _groupAvatarMemberVersions.remove(sid);
          _groupAvatarVersionForSession(sid).value++;
        }
      });
    }

    _rebuildGroupedSessionsImmediately();
    _sessionsWorker = ever(imService.sessions, (_) => _onSessionsChanged());
    _sessionsLoadWorker = ever(
      imService.sessionsLoadTick,
      (_) => _rebuildGroupedSessionsImmediately(),
    );
    _currentSessionWorker = ever<String?>(imService.currentSessionIdRx, (_) {
      _rebuildGroupedSessionsImmediately();
    });
    _streamingPreviewWorker = ever<int>(
      imService.streamingSessionPreviewTickRx,
      (_) => _groupedSessions.refresh(),
    );
    _searchQueryWorker = debounce(searchQuery, (_) {
      final keyword = searchQuery.value.trim();
      if (keyword.isEmpty) {
        _rebuildGroupedSessionsImmediately();
      } else {
        _performDbSearch(keyword);
      }
    }, time: const Duration(milliseconds: 200));
    reloadFavoriteIds();
    // 冷启动补齐草稿索引，让列表不进聊天页也能挂"草稿"标记。
    unawaited(
      ChatDraftIndex.ensureLoaded(
        _authService?.userId?.trim() ?? LocalDb.activeUserId?.trim() ?? '',
      ),
    );
  }

  Future<void> reloadFavoriteIds() async {
    try {
      final ids = await _favoriteService.listIds();
      _favoritedSessionIds.assignAll(ids);
    } catch (_) {}
  }

  bool isSessionFavorited(String sessionId) =>
      _favoritedSessionIds.contains(sessionId);

  Future<bool> toggleSessionFavorite(String sessionId) async {
    final wasFavorited = _favoritedSessionIds.contains(sessionId);
    if (wasFavorited) {
      final ok = await _favoriteService.remove(sessionId);
      if (ok) _favoritedSessionIds.remove(sessionId);
      return ok;
    } else {
      final ok = await _favoriteService.add(sessionId);
      if (ok) _favoritedSessionIds.add(sessionId);
      return ok;
    }
  }

  @override
  void onClose() {
    _friendListWorker?.dispose();
    _profileCacheWorker?.dispose();
    _agentListWorker?.dispose();
    _sessionsWorker?.dispose();
    _sessionsLoadWorker?.dispose();
    _currentSessionWorker?.dispose();
    _streamingPreviewWorker?.dispose();
    _searchQueryWorker?.dispose();
    _deferredPageVisibleRefreshTimer?.cancel();
    _deferredPageVisibleRefreshTimer = null;
    _deferredConversationSummaryRefreshTimer?.cancel();
    _deferredConversationSummaryRefreshTimer = null;
    _deferredSessionsRebuildTimer?.cancel();
    _deferredSessionsRebuildTimer = null;
    _cancelPendingReorderCommit();
    _sessionDetailPrefetchQueue.clear();
    _sessionDetailPrefetchQueued.clear();
    _conversationGroupKeyCache.clear();
    _lastOptimisticActivityByGroup = null;
    _unreadMentions.dispose();
    _clearRxVersionMap(_groupAvatarVersionBySession);
    _clearRxVersionMap(_peerAvatarVersionByPeerId);
    _clearRxVersionMap(_privateAvatarVersionBySession);
    super.onClose();
  }

  List<ConversationListItem> get groupedSessions =>
      List<ConversationListItem>.unmodifiable(_groupedSessions);

  bool get hasUnfilteredSessions => _hasUnfilteredSessions.value;

  /// 当前账户是否已拥有可用 agent（自建 + 他人共享）；用于消息列表在
  /// 未创建任何 agent 时展示「极速接入」入口。服务未注册时视为「有」，
  /// 避免异常状态下误弹入口。
  ///
  /// 只读当前已缓存的 agent 列表，不在此处触发加载——AI 列表页才是
  /// agent 数据的加载入口，消息列表不应绕过它提前拉取（见
  /// home_view_keep_alive_test「进入 AI 分页才刷新 agent 列表」的约束）。
  bool get hasAnyAgent {
    final agentService = _agentService;
    if (agentService == null) {
      return true;
    }
    return agentService.allAccessibleAgents.isNotEmpty;
  }

  Future<void> openAgentQuickOnboard() async {
    await Get.toNamed(AppRoutes.agentQuickSetup);
    unawaited(_agentService?.loadAgents());
  }

  Future<void> triggerRefreshForVisiblePage() async {
    _rebuildGroupedSessionsImmediately();
    await refreshSessionsOnPageVisible();
  }

  Future<void> refreshSessionsOnPageVisible() async {
    if (!_isConversationsTabActive()) {
      return;
    }
    if (_pageVisibleRefreshInFlight) {
      return;
    }
    // Always rebuild with existing data immediately so the user sees the
    // list right away, even while a session-window sync is still in flight.
    _rebuildGroupedSessionsForUnreadAlignmentIfNeeded();

    if (imService.isSessionWindowSyncInflight) {
      _deferredPageVisibleRefreshTimer ??= Timer(
        const Duration(milliseconds: 300),
        () {
          _deferredPageVisibleRefreshTimer = null;
          if (!_isConversationsTabActive()) {
            return;
          }
          unawaited(refreshSessionsOnPageVisible());
        },
      );
      return;
    }
    _deferredPageVisibleRefreshTimer?.cancel();
    _deferredPageVisibleRefreshTimer = null;
    _pageVisibleRefreshInFlight = true;
    try {
      if (await _refreshConversationPageIfEnabled()) {
        return;
      }
      if (imService.sessions.isEmpty) {
        await imService.refreshSessionsWindowNow();
        _rebuildGroupedSessionsForUnreadAlignmentIfNeeded();
        await _loadMoreSessionsForSparseConversationListIfNeeded();
        return;
      }
      await imService.refreshSessionsIfStale(maxAge: _pageVisibleRefreshMaxAge);
      _rebuildGroupedSessionsForUnreadAlignmentIfNeeded();
      await _loadMoreSessionsForSparseConversationListIfNeeded();
    } finally {
      _pageVisibleRefreshInFlight = false;
    }
  }

  Future<void> loadMoreSessionsForVisibleListIfNeeded() async {
    if (!_isConversationsTabActive()) {
      return;
    }
    if (searchQuery.value.trim().isNotEmpty) {
      return;
    }
    if (_conversationListApiActive) {
      await _loadMoreConversationPageIfNeeded();
      return;
    }
    final loaded = await imService.loadMoreSessionWindowIfNeeded();
    if (loaded) {
      _rebuildGroupedSessionsImmediately();
    }
  }

  Future<void> _loadMoreSessionsForSparseConversationListIfNeeded() async {
    if (_conversationListApiActive) {
      return;
    }
    if (searchQuery.value.trim().isNotEmpty) {
      return;
    }
    if (_groupedSessions.length >= _targetVisibleConversationGroups) {
      return;
    }
    final loaded = await imService.loadMoreSessionWindowIfNeeded();
    if (loaded) {
      _rebuildGroupedSessionsImmediately();
    }
  }

  Future<bool> _refreshConversationPageIfEnabled() async {
    if (!_useConversationListApi ||
        _sessionService == null ||
        !_sessionService.isInitialized) {
      return false;
    }
    if (_conversationPageInFlight) {
      return _conversationListApiActive;
    }
    if (DateTime.now().isBefore(_conversationNextAllowedAt)) {
      return false;
    }
    _conversationPageInFlight = true;
    try {
      _conversationLastRefreshAttemptAt = DateTime.now();
      final result = await _sessionService.fetchConversationPage(
        limit: _targetVisibleConversationGroups,
      );
      if (!result.success) {
        if (result.rateLimited) {
          _conversationNextAllowedAt = DateTime.now().add(
            const Duration(seconds: 60),
          );
        }
        _conversationListApiActive = false;
        return false;
      }
      _conversationListApiActive = true;
      _conversationHasMore = result.hasMore;
      _conversationNextCursor = result.nextCursor;
      _conversationNextAllowedAt = DateTime.now().add(
        _conversationPageMinInterval,
      );
      // Write pin truth back to LocalDb so weak-network local rebuilds do not
      // resurrect stale is_pinned / friend_is_pinned rows.
      _conversationPinReconcileSummaries
        ..clear()
        ..addAll(result.items);
      unawaited(
        imService.reconcilePinsFromConversationSummaries(
          List<ConversationSummaryModel>.from(
            _conversationPinReconcileSummaries,
          ),
          hasMore: result.hasMore,
        ),
      );
      for (final summary in result.items) {
        _syncPeerMuteFromSummary(summary);
      }
      final newItems = result.items
          .map(_buildConversationItemFromSummary)
          .toList();
      final incomingKeys = newItems.map((item) => item.groupKey).toSet();
      final tail = _conversationSummaryItems
          .where((item) => !incomingKeys.contains(item.groupKey))
          .toList();
      _conversationSummaryItems
        ..clear()
        ..addAll(newItems)
        ..addAll(tail)
        ..sort(_compareConversationItems);
      _applyConversationSummaryItems(throttleReorder: true);
      return true;
    } finally {
      _conversationPageInFlight = false;
    }
  }

  Future<void> _loadMoreConversationPageIfNeeded() async {
    if (!_conversationListApiActive ||
        !_conversationHasMore ||
        _conversationPageInFlight ||
        _sessionService == null ||
        !_sessionService.isInitialized) {
      return;
    }
    final now = DateTime.now();
    if (now.isBefore(_conversationNextAllowedAt)) {
      return;
    }
    _conversationPageInFlight = true;
    try {
      final result = await _sessionService.fetchConversationPage(
        limit: _targetVisibleConversationGroups,
        cursor: _conversationNextCursor,
      );
      if (!result.success) {
        if (result.rateLimited) {
          _conversationNextAllowedAt = DateTime.now().add(
            const Duration(seconds: 60),
          );
        }
        return;
      }
      _conversationHasMore = result.hasMore;
      _conversationNextCursor = result.nextCursor;
      _conversationNextAllowedAt = DateTime.now().add(
        _conversationPageMinInterval,
      );
      final existing = _conversationSummaryItems
          .map((item) => item.groupKey)
          .toSet();
      final seenReconcileKeys = _conversationPinReconcileSummaries
          .map((summary) => summary.groupKey)
          .toSet();
      for (final summary in result.items) {
        _syncPeerMuteFromSummary(summary);
        final item = _buildConversationItemFromSummary(summary);
        if (existing.add(item.groupKey)) {
          _conversationSummaryItems.add(item);
        }
        if (seenReconcileKeys.add(summary.groupKey)) {
          _conversationPinReconcileSummaries.add(summary);
        }
      }
      // Once loaded pages include an unpinned row (or end), clear stale local
      // pins that were skipped while the first page was still all-pinned.
      unawaited(
        imService.reconcilePinsFromConversationSummaries(
          List<ConversationSummaryModel>.from(
            _conversationPinReconcileSummaries,
          ),
          hasMore: result.hasMore,
        ),
      );
      _conversationSummaryItems.sort(_compareConversationItems);
      _applyConversationSummaryItems();
    } finally {
      _conversationPageInFlight = false;
    }
  }

  ConversationListItem _buildConversationItemFromSummary(
    ConversationSummaryModel summary,
  ) {
    final latest = _mergeCachedGroupAvatarMembersFromLocal(
      summary.toLatestSessionModel(),
    );
    // 摘要行初始只有 latest：若 latest 有 override 则尊重它；多 thread 的
    // 完整 per-session 汇总在 _applyConversationSummaryItems 用本地组会话重算。
    final override = imService.unreadOverrideForSession(latest.sessionId);
    final effectiveUnread = override ?? summary.unread;
    final isMuted = latest.type == 'private' && latest.peerId.trim().isNotEmpty
        ? imService.isPeerMuted(latest.peerId)
        : summary.isMuted;
    final effectiveBadge = override != null
        ? (isMuted ? 0 : override)
        : (isMuted ? 0 : summary.badgeUnread);
    return ConversationListItem(
      groupKey: summary.groupKey,
      latestSession: latest,
      sessions: <SessionModel>[latest],
      unreadCount: effectiveUnread,
      hasUnreadMention:
          effectiveUnread > 0 &&
          _unreadMentions.hasUnreadMention(latest.sessionId),
      badgeUnreadCount: effectiveBadge,
      hasMutedUnread: effectiveUnread > effectiveBadge,
      isMuted: isMuted,
      isPinned: summary.isPinned,
      pinnedAt: summary.pinnedAt,
      threadCountOverride: summary.threadCount <= 0 ? 1 : summary.threadCount,
    );
  }

  SessionModel _mergeCachedGroupAvatarMembersFromLocal(SessionModel session) {
    if (session.type != 'group' ||
        session.cachedGroupAvatarMembers.isNotEmpty) {
      return session;
    }
    final sid = session.sessionId.trim();
    if (sid.isEmpty) {
      return session;
    }
    final local = imService.findSessionById(sid);
    final cachedMembers =
        local?.cachedGroupAvatarMembers ?? const <SessionAvatarMember>[];
    if (cachedMembers.isEmpty) {
      return session;
    }
    return session.copyWith(cachedGroupAvatarMembers: cachedMembers);
  }

  void _applyConversationSummaryItems({bool throttleReorder = false}) {
    // Summary items changed; force the next optimistic pass to recompute.
    _lastOptimisticActivityByGroup = null;
    // API 摘要路径也必须同步 @提及 状态：mention 标记的解析与清除只存在于
    // syncWithSessions 里，若只让本地全量重建路径（_rebuildGroupedSessions）
    // 调用它，摘要路径激活后已读会话的高亮将永久残留。
    _unreadMentions.syncWithSessions(imService.sessions);
    var items = List<ConversationListItem>.from(_conversationSummaryItems);
    // 将所有访客会话合并为单个 visitor:group 条目（API 路径每条访客独占一行）
    final visitorItems = items.where((i) => i.latestSession.isVisitor).toList();
    if (visitorItems.isNotEmpty) {
      items.removeWhere((i) => i.latestSession.isVisitor);
      final allSessions = visitorItems.expand((i) => i.sessions).toList();
      final latest = visitorItems
          .reduce(
            (a, b) => a.latestSession.activityAt >= b.latestSession.activityAt
                ? a
                : b,
          )
          .latestSession;
      items.add(
        ConversationListItem(
          groupKey: visitorGroupKey,
          latestSession: latest,
          sessions: allSessions,
          unreadCount: visitorItems.fold(0, (s, i) => s + i.unreadCount),
          hasUnreadMention: visitorItems.any((i) => i.hasUnreadMention),
          badgeUnreadCount: visitorItems.fold(
            0,
            (s, i) => s + i.badgeUnreadCount,
          ),
          hasMutedUnread: visitorItems.any((i) => i.hasMutedUnread),
          isMuted: visitorItems.every((i) => i.isMuted),
          isPinned: visitorItems.any((i) => i.isPinned),
          pinnedAt: visitorItems.fold(
            0,
            (m, i) => i.pinnedAt > m ? i.pinnedAt : m,
          ),
          threadCountOverride: visitorItems.fold<int>(
            0,
            (s, i) => s + i.threadCount,
          ),
        ),
      );
    }
    // 合并后重新排序，确保 visitor:group 按 activityAt 参与整体排序
    if (visitorItems.isNotEmpty) {
      items.sort(_compareConversationItems);
    }
    // 从本地 imService.sessions 实时同步未读数，确保列表与底部栏一致。
    // 预构建 groupKey → sessions 映射，避免逐项遍历全量 sessions。
    final localSessionsByGroup = <String, List<SessionModel>>{};
    for (final s in imService.sessions) {
      final gk = _buildConversationGroupKey(s);
      localSessionsByGroup.putIfAbsent(gk, () => <SessionModel>[]).add(s);
    }
    for (var i = 0; i < items.length; i++) {
      var item = items[i];
      List<SessionModel>? localSessions = localSessionsByGroup[item.groupKey];
      if (localSessions == null || localSessions.isEmpty) {
        // session 可能以空 peerId 加入内存（_upsertSessionInMemory 在
        // peer identity backfill 完成前产生），导致 groupKey 不匹配。
        // 尝试按 latestSession.sessionId 直接查找。
        final latestSid = item.latestSession.sessionId.trim();
        if (latestSid.isNotEmpty) {
          final idx = imService.sessions.indexWhere(
            (s) => s.sessionId == latestSid,
          );
          if (idx >= 0) {
            localSessions = [imService.sessions[idx]];
          }
        }
      }

      // 实时消息会先推进本地 session.activityAt，随后到达的会话摘要
      // 快照可能因最终一致性仍携带旧时间。如果直接使用该快照排序，
      // 会话就会在“本地置顶 → 旧快照回退 → 新快照再置顶”之间上下跳。
      // 这里只以当前本地状态作为下界：API 的更新时间仍可正常前移，
      // 撤回/删除导致的本地时间回落也不会被永久状态阻断。
      if (localSessions != null && localSessions.isNotEmpty) {
        final merged = mergeLatestActivityFloor(item, localSessions);
        if (!identical(merged, item)) {
          item = merged;
          items[i] = merged;
        }
      }

      final effective = _effectiveUnreadForConversationItem(
        item: item,
        localSessions: localSessions,
      );
      final effectiveUnread = effective.unread;
      final effectiveBadge = effective.badge;
      final hasMutedUnread = effectiveUnread > effectiveBadge;
      final hasUnreadMention =
          effectiveUnread > 0 &&
          item.sessions.any(
            (session) => _unreadMentions.hasUnreadMention(session.sessionId),
          );
      if (effectiveUnread == item.unreadCount &&
          effectiveBadge == item.badgeUnreadCount &&
          hasMutedUnread == item.hasMutedUnread &&
          hasUnreadMention == item.hasUnreadMention) {
        continue;
      }
      items[i] = ConversationListItem(
        groupKey: item.groupKey,
        latestSession: item.latestSession,
        sessions: item.sessions,
        unreadCount: effectiveUnread,
        hasUnreadMention: hasUnreadMention,
        badgeUnreadCount: effectiveBadge,
        hasMutedUnread: hasMutedUnread,
        isMuted: item.isMuted,
        isPinned: item.isPinned,
        pinnedAt: item.pinnedAt,
        threadCountOverride: item.threadCountOverride,
      );
    }
    // 未读兜底补行：底部角标按"全部本地会话的未读"求和，而列表行只来自服务端
    // 会话页快照。当一个有未读的本地会话尚未进入服务端快照（新会话、快照窗口
    // 之外、或会话页刷新被节流/不在会话页时未触发），列表就会缺这一行，而底部
    // 已经计数——表现为"底部有数、分组里却找不到未读"。这里用内存中已有的会话
    // （私聊需已带对端身份，访客组不依赖对端身份）就地把缺失的未读行补出来，
    // 纯本地、无网络请求、不改变刷新频率；只有在底部与列表确实对不上时才会
    // 发生补行，性能开销可忽略。
    {
      final presentGroupKeys = items.map((i) => i.groupKey).toSet();
      for (final entry in localSessionsByGroup.entries) {
        final groupKey = entry.key;
        if (presentGroupKeys.contains(groupKey)) continue;
        final rep = entry.value.first;
        // 访客组整组渲染为合成的"访客"行，不依赖单个会话的对端身份，
        // 不受下面的占位判断限制。
        final isPeerlessPrivateStub =
            groupKey != visitorGroupKey &&
            rep.type.trim().toLowerCase() == 'private' &&
            rep.peerId.trim().isEmpty;
        if (isPeerlessPrivateStub) continue;
        final groupBadge = entry.value.fold<int>(
          0,
          (sum, s) => sum + imService.notificationUnreadForSession(s),
        );
        if (groupBadge <= 0) continue;
        items.add(
          _buildConversationItemFromLocalSessions(groupKey, entry.value),
        );
      }
      items.sort(_compareConversationItems);
    }
    _hasUnfilteredSessions.value = _conversationSummaryItems.isNotEmpty;
    _prefetchTopSessionDetails(items);
    _warmupInitialConversationAvatars(items);
    _commitGroupedSessions(items, throttleReorder: throttleReorder);
  }

  /// 防止延迟到达的 API 摘要用旧 activityAt 覆盖实时消息已经
  /// 推进的本地会话时间。只合并排序时间，摘要文本、未读和置顶字段
  /// 仍由各自的对账逻辑处理。
  ///
  /// 会话时间的口径是「最后一条可见消息」（[SessionModel.activityAt]），
  /// 所以地板要写进 lastMessageTime；写 updatedAt 抬不动已有可见消息的行。
  @visibleForTesting
  static ConversationListItem mergeLatestActivityFloor(
    ConversationListItem item,
    Iterable<SessionModel> localSessions,
  ) {
    var latestActivityAt = item.latestSession.activityAt;
    var cachedGroupAvatarMembers = item.latestSession.cachedGroupAvatarMembers;
    for (final session in localSessions) {
      if (session.activityAt > latestActivityAt) {
        latestActivityAt = session.activityAt;
      }
      if (cachedGroupAvatarMembers.isEmpty &&
          session.sessionId == item.latestSession.sessionId &&
          session.cachedGroupAvatarMembers.isNotEmpty) {
        cachedGroupAvatarMembers = session.cachedGroupAvatarMembers;
      }
    }
    if (latestActivityAt == item.latestSession.activityAt &&
        listEquals(
          cachedGroupAvatarMembers,
          item.latestSession.cachedGroupAvatarMembers,
        )) {
      return item;
    }
    return ConversationListItem(
      groupKey: item.groupKey,
      latestSession: item.latestSession.copyWith(
        lastMessageTime: latestActivityAt,
        cachedGroupAvatarMembers: cachedGroupAvatarMembers,
      ),
      sessions: item.sessions,
      unreadCount: item.unreadCount,
      hasUnreadMention: item.hasUnreadMention,
      badgeUnreadCount: item.badgeUnreadCount,
      hasMutedUnread: item.hasMutedUnread,
      isMuted: item.isMuted,
      isPinned: item.isPinned,
      pinnedAt: item.pinnedAt,
      threadCountOverride: item.threadCountOverride,
    );
  }

  /// 将最新会话列表落地到 [_groupedSessions]。
  ///
  /// - 非实时路径（用户置顶/删除、打开会话、加载更多、搜索等）：直接落地，保证即时反馈。
  /// - 实时活动路径（[throttleReorder] 为 true）：采用"前沿节流"——
  ///   不在冷却窗内的第一次重排立即落地（新消息照常即时置顶），
  ///   窗内后续的相对顺序变化合并、到窗口末尾只落地一次；
  ///   会话增删与未读/标题等内容变化始终即时；再叠加去抖阈值过滤毫秒级微小换位，
  ///   从而既保留"新消息即时上浮"，又消除两个活跃会话轮流被拽到顶导致的频繁上下跳。
  void _commitGroupedSessions(
    List<ConversationListItem> items, {
    required bool throttleReorder,
  }) {
    if (listEquals(_groupedSessions, items)) {
      // 显示已与严格顺序一致：清掉积压的重排（若有），但保留冷却窗以继续限频。
      _pendingReorderItems = null;
      return;
    }

    if (!throttleReorder) {
      _cancelPendingReorderCommit();
      _groupedSessions.assignAll(items);
      return;
    }

    final currentOrder = <String, int>{};
    for (var i = 0; i < _groupedSessions.length; i++) {
      currentOrder[_groupedSessions[i].groupKey] = i;
    }

    // 结构变化（新增/删除会话）立即生效，不参与节流。
    // 已有显示时仍走 reorderWithHysteresis 口径，避免增删瞬间已有条目从
    // "hysteresis 顺序"翻到"全量构建顺序"。首次加载（无当前显示）则按
    // 全量构建口径直接落地——没有 currentOrder 可稳定时不能退化到 groupKey。
    final structuralChange =
        items.length != _groupedSessions.length ||
        items.any((item) => !currentOrder.containsKey(item.groupKey));
    if (structuralChange) {
      _cancelPendingReorderCommit();
      if (_groupedSessions.isEmpty) {
        _groupedSessions.assignAll(items);
      } else {
        _groupedSessions.assignAll(
          reorderWithHysteresis(items, currentOrder, _reorderHysteresisMs),
        );
      }
      return;
    }

    // 去抖后的目标顺序：活跃时间同档的会话保持当前相对顺序，不因微小差异换位。
    final settled = reorderWithHysteresis(
      items,
      currentOrder,
      _reorderHysteresisMs,
    );
    final orderUnchanged = listEquals(_groupedSessions, settled);

    if (_reorderCommitTimer == null) {
      // 不在冷却窗内：直接落地最新顺序（含内容刷新），实现"新消息即时上浮"。
      if (!orderUnchanged) {
        _groupedSessions.assignAll(settled);
        _scheduleReorderCooldown();
      } else {
        // 仅内容变化（未读/标题），顺序不变：刷新内容但不开启冷却、不触发动画。
        _groupedSessions.assignAll(settled);
      }
      return;
    }

    // 冷却窗内：先把最新内容就地刷新到当前显示顺序（角标即时、顺序不动、无动画），
    // 真正的重排推迟到窗口末尾合并落地。
    final byKey = <String, ConversationListItem>{
      for (final item in items) item.groupKey: item,
    };
    final inPlace = <ConversationListItem>[
      for (final existing in _groupedSessions) byKey[existing.groupKey]!,
    ];
    if (!listEquals(_groupedSessions, inPlace)) {
      _groupedSessions.assignAll(inPlace);
    }
    _pendingReorderItems = orderUnchanged ? null : settled;
  }

  void _scheduleReorderCooldown() {
    _reorderCommitTimer = Timer(
      _reorderCoalesceWindow,
      _onReorderCooldownElapsed,
    );
  }

  void _onReorderCooldownElapsed() {
    final pending = _pendingReorderItems;
    _pendingReorderItems = null;
    if (pending == null) {
      // 窗内无积压的重排 → 结束冷却。
      _reorderCommitTimer = null;
      return;
    }
    // 用当前最新内容重映射目标顺序（窗口期间内容可能又变过），合并落地一次。
    final byKey = <String, ConversationListItem>{
      for (final item in _groupedSessions) item.groupKey: item,
    };
    final committed = <ConversationListItem>[
      for (final item in pending) byKey[item.groupKey] ?? item,
    ];
    if (!listEquals(_groupedSessions, committed)) {
      _groupedSessions.assignAll(committed);
    }
    // 仍有活跃churn，继续维持冷却窗，下一轮再合并。
    _scheduleReorderCooldown();
  }

  void _cancelPendingReorderCommit() {
    _reorderCommitTimer?.cancel();
    _reorderCommitTimer = null;
    _pendingReorderItems = null;
  }

  /// 去抖排序：在统一排序规则基础上，对活跃时间差小于 [hysteresisMs] 的会话
  /// 按"当前显示顺序"保持稳定，避免微小差异（毫秒级、本地与后端口径差）引发换位。
  /// 用时间分档保证比较的传递性（同一档内退化为当前顺序，跨档才允许新档上浮）。
  /// 置顶组内的优先级与 [_compareConversationItems] 对齐：活动时间分档 → pinnedAt → 当前顺序，
  /// 避免两条路径轮流落地导致置顶组反复换位。
  @visibleForTesting
  static List<ConversationListItem> reorderWithHysteresis(
    List<ConversationListItem> items,
    Map<String, int> currentOrder,
    int hysteresisMs,
  ) {
    final sorted = List<ConversationListItem>.from(items);
    sorted.sort((a, b) {
      if (a.isPinned != b.isPinned) {
        return b.isPinned ? 1 : -1;
      }
      final bucketA = a.latestSession.activityAt ~/ hysteresisMs;
      final bucketB = b.latestSession.activityAt ~/ hysteresisMs;
      if (bucketA != bucketB) {
        return bucketB.compareTo(bucketA);
      }
      if (a.isPinned && b.isPinned) {
        final pinCompare = b.pinnedAt.compareTo(a.pinnedAt);
        if (pinCompare != 0) return pinCompare;
      }
      final ia = currentOrder[a.groupKey] ?? (1 << 30);
      final ib = currentOrder[b.groupKey] ?? (1 << 30);
      if (ia != ib) {
        return ia.compareTo(ib);
      }
      return a.groupKey.compareTo(b.groupKey);
    });
    return sorted;
  }

  Future<ConversationThreadPageResult> fetchConversationThreadSessions(
    ConversationListItem item, {
    String cursor = '',
  }) async {
    // visitor:group 是前端合成 key，直接从内存查所有访客 session，不走后端
    if (item.groupKey == visitorGroupKey) {
      final visitors = imService.sessions.where((s) => s.isVisitor).toList();
      return ConversationThreadPageResult(
        sessions: getThreadSessionsByLatestActivityDesc(
          visitors.isNotEmpty ? visitors : item.sessions,
        ),
      );
    }
    // 统一从本地 imService.sessions 按 groupKey 过滤，与用户资料页口径一致：
    // 按会话级 isPinned 置顶排序，避免后端 API 按好友级置顶排序导致
    // 会话级置顶的旧会话被分页截断、与资料页不一致。
    final groupKey = item.groupKey.trim();
    final seen = <String>{};
    final matched = <SessionModel>[];
    for (final session in imService.sessions) {
      if (!seen.add(session.sessionId)) continue;
      if (_buildConversationGroupKey(session) != groupKey) continue;
      matched.add(session);
    }
    if (matched.isEmpty) {
      return ConversationThreadPageResult(
        sessions: getThreadSessionsByLatestActivityDesc(item.sessions),
      );
    }
    return ConversationThreadPageResult(
      sessions: getThreadSessionsByLatestActivityDesc(matched),
    );
  }

  /// 组未读合并：per-session `override ?? local`，再求和；仅当组内每个本地
  /// session 都有 override 时才允许整组低于服务端摘要下界（例如全部已读）。
  /// 禁止用 latestSession 一条 override 接管整组。
  @visibleForTesting
  ({int unread, int badge}) effectiveUnreadForConversationItemForTest({
    required ConversationListItem item,
    required List<SessionModel>? localSessions,
  }) => _effectiveUnreadForConversationItem(
    item: item,
    localSessions: localSessions,
  );

  ({int unread, int badge}) _effectiveUnreadForConversationItem({
    required ConversationListItem item,
    required List<SessionModel>? localSessions,
  }) {
    if (localSessions == null || localSessions.isEmpty) {
      final override = imService.unreadOverrideForSession(
        item.latestSession.sessionId,
      );
      if (override != null) {
        return (unread: override, badge: item.isMuted ? 0 : override);
      }
      return (unread: item.unreadCount, badge: item.badgeUnreadCount);
    }

    var localUnread = 0;
    var localBadge = 0;
    var allHaveOverride = true;
    for (final session in localSessions) {
      final override = imService.unreadOverrideForSession(session.sessionId);
      if (override == null) {
        allHaveOverride = false;
        localUnread += imService.totalUnreadForSession(session);
        localBadge += imService.notificationUnreadForSession(session);
        continue;
      }
      final safe = override < 0 ? 0 : override;
      final withOverride = session.copyWith(unreadCount: safe);
      localUnread += imService.totalUnreadForSession(withOverride);
      localBadge += imService.notificationUnreadForSession(withOverride);
    }

    if (allHaveOverride) {
      return (unread: localUnread, badge: localBadge);
    }
    // 本地求和只是下界：不能用瞬时本地 0 把服务端摘要往下清（防角标闪烁）。
    return (
      unread: localUnread > item.unreadCount ? localUnread : item.unreadCount,
      badge: localBadge > item.badgeUnreadCount
          ? localBadge
          : item.badgeUnreadCount,
    );
  }

  /// 拉取该分组服务端线程列表，并把未读批量同步到本地。
  /// 返回 API 结果；失败时 success=false，调用方回退本地。
  /// 不主动触发列表全表重建（至多由一次 sessions Rx → debounce 对齐）。
  Future<ConversationThreadPageResult> _fetchAndSyncThreadUnreadFromServer(
    ConversationListItem item, {
    int limit = 60,
    String cursor = '',
  }) async {
    if (_sessionService == null || !_sessionService.isInitialized) {
      return const ConversationThreadPageResult(success: false);
    }
    if (item.groupKey == visitorGroupKey) {
      return const ConversationThreadPageResult(success: false);
    }

    try {
      final result = await _sessionService.fetchConversationThreads(
        groupKey: item.groupKey,
        limit: limit,
        cursor: cursor,
      );
      if (!result.success || result.sessions.isEmpty) {
        return result;
      }
      await imService.syncSessionUnreadCountsFromServer(result.sessions);
      return result;
    } catch (e, stack) {
      debugPrint('fetchAndSyncThreadUnreadFromServer error: $e\n$stack');
      return const ConversationThreadPageResult(success: false);
    }
  }

  /// 用服务端线程列表做弹窗/直达依据，本地同 session 补充标题/预览。
  @visibleForTesting
  static List<SessionModel> mergeApiThreadsWithLocalPreview({
    required List<SessionModel> apiThreads,
    required List<SessionModel> localSessions,
  }) {
    if (apiThreads.isEmpty) {
      return const <SessionModel>[];
    }
    final localById = <String, SessionModel>{
      for (final session in localSessions)
        if (session.sessionId.trim().isNotEmpty) session.sessionId: session,
    };
    return apiThreads
        .map((api) {
          final local = localById[api.sessionId];
          if (local == null) return api;
          final title = api.title.trim().isNotEmpty ? api.title : local.title;
          final lastMessage = api.lastMessage.trim().isNotEmpty
              ? api.lastMessage
              : local.lastMessage;
          final lastMessageTime = api.lastMessageTime > 0
              ? api.lastMessageTime
              : local.lastMessageTime;
          final peerNickname = api.peerNickname.trim().isNotEmpty
              ? api.peerNickname
              : local.peerNickname;
          final peerUsername = api.peerUsername.trim().isNotEmpty
              ? api.peerUsername
              : local.peerUsername;
          final peerId = api.peerId.trim().isNotEmpty
              ? api.peerId
              : local.peerId;
          // 未读以本地（含 override / 批量 sync 结果）为准，避免弹窗仍显示服务端旧值。
          final unreadCount = local.unreadCount;
          return api.copyWith(
            title: title,
            lastMessage: lastMessage,
            lastMessageTime: lastMessageTime,
            peerNickname: peerNickname,
            peerUsername: peerUsername,
            peerId: peerId,
            peerType: api.peerType != 0 ? api.peerType : local.peerType,
            unreadCount: unreadCount,
            cachedGroupAvatarMembers: api.cachedGroupAvatarMembers.isNotEmpty
                ? api.cachedGroupAvatarMembers
                : local.cachedGroupAvatarMembers,
          );
        })
        .toList(growable: false);
  }

  /// API 线程在本地不存在时，仍应用 override。
  List<SessionModel> _applyUnreadOverridesToThreads(
    List<SessionModel> threads,
  ) {
    return threads
        .map((session) {
          final override = imService.unreadOverrideForSession(
            session.sessionId,
          );
          if (override == null) return session;
          final safe = override < 0 ? 0 : override;
          if (session.unreadCount == safe) return session;
          return session.copyWith(unreadCount: safe);
        })
        .toList(growable: false);
  }

  bool _isConversationsTabActive() {
    final homeController = _homeController;
    if (homeController == null) {
      return true;
    }
    return homeController.currentIndex.value == HomeTab.conversations.index;
  }

  void _rebuildGroupedSessionsForUnreadAlignmentIfNeeded() {
    if (searchQuery.value.trim().isNotEmpty) {
      return;
    }
    final groupedBadgeUnreadTotal = _groupedSessions.fold<int>(
      0,
      (sum, item) => sum + item.badgeUnreadCount,
    );
    if (groupedBadgeUnreadTotal == imService.notificationUnread) {
      return;
    }
    _rebuildGroupedSessionsImmediately();
  }

  static int _compareByConversationActivity(SessionModel a, SessionModel b) {
    final activityCompare = b.activityAt.compareTo(a.activityAt);
    if (activityCompare != 0) return activityCompare;
    return SessionModel.compareByPriority(a, b);
  }

  /// 会话列表项的统一排序规则：置顶优先 → 最近活动 → 置顶时间 → 分组键。
  /// 列表重建与置顶操作后的即时重排共用同一套规则，避免顺序口径分叉。
  static int _compareConversationItems(
    ConversationListItem a,
    ConversationListItem b,
  ) {
    if (a.isPinned != b.isPinned) {
      return b.isPinned ? 1 : -1;
    }
    final activityCompare = _compareByConversationActivity(
      a.latestSession,
      b.latestSession,
    );
    if (activityCompare != 0) return activityCompare;
    if (a.isPinned && b.isPinned) {
      final pinCompare = b.pinnedAt.compareTo(a.pinnedAt);
      if (pinCompare != 0) return pinCompare;
    }
    return a.groupKey.compareTo(b.groupKey);
  }

  List<SessionModel> getThreadSessionsByLatestActivityDesc(
    List<SessionModel> sessions,
  ) {
    final ordered = List<SessionModel>.from(sessions);
    ordered.sort(_compareByThreadActivityDesc);
    return ordered;
  }

  static int _compareByThreadActivityDesc(SessionModel a, SessionModel b) {
    if (a.isPinned != b.isPinned) {
      return b.isPinned ? 1 : -1;
    }
    final activityCompare = b.activityAt.compareTo(a.activityAt);
    if (activityCompare != 0) return activityCompare;
    return a.sessionId.compareTo(b.sessionId);
  }

  void _onSessionsChanged() {
    if (searchQuery.value.trim().isNotEmpty) return;
    if (_conversationListApiActive) {
      _deferredSessionsRebuildTimer?.cancel();
      _deferredSessionsRebuildTimer = null;
      _scheduleGroupedSessionsRebuild();
      return;
    }

    if (_groupedSessions.isEmpty || imService.sessions.isEmpty) {
      _deferredSessionsRebuildTimer?.cancel();
      _deferredSessionsRebuildTimer = null;
      _scheduleGroupedSessionsRebuild();
      return;
    }

    _deferredSessionsRebuildTimer?.cancel();
    _deferredSessionsRebuildTimer = Timer(_sessionsRebuildDebounce, () {
      _deferredSessionsRebuildTimer = null;
      _scheduleGroupedSessionsRebuild();
    });
  }

  void _scheduleGroupedSessionsRebuild() {
    if (searchQuery.value.trim().isNotEmpty) return;
    if (_conversationListApiActive) {
      final activityReordered = _applyOptimisticActivityReorder();
      // 快速路径：仅当列表未读总数与实际不一致时才全量同步，
      // 避免非未读变化（打字状态、心跳等）触发无效遍历。
      final displayedBadgeTotal = _groupedSessions.fold<int>(
        0,
        (sum, item) => sum + item.badgeUnreadCount,
      );
      // 列表上还挂着 @提及 高亮时不能走跳过分支：免打扰会话已读后角标总数
      // 不变，只有重放摘要条目才能让高亮及时消失。
      final hasDisplayedMention = _groupedSessions.any(
        (item) => item.hasUnreadMention,
      );
      if (activityReordered ||
          displayedBadgeTotal != imService.notificationUnread ||
          hasDisplayedMention) {
        _applyConversationSummaryItems(throttleReorder: true);
      }
      _scheduleConversationSummaryRefreshFromRealtime();
      return;
    }
    _rebuildGroupedSessions();
  }

  /// @提及 异步解析结果落地后刷新列表。API 摘要路径不经过
  /// [_rebuildGroupedSessions]，必须直接重放摘要条目高亮才能上/下屏。
  void _onUnreadMentionStateChanged() {
    if (searchQuery.value.trim().isNotEmpty) return;
    if (_conversationListApiActive) {
      _applyConversationSummaryItems();
      return;
    }
    _rebuildGroupedSessions();
  }

  /// 精简乐观重排：只用本地 sessions 的最新 activityAt 更新排序位置，
  /// 不替换 latestSession 的其他字段，避免与后端数据口径冲突导致列表跳动。
  bool _applyOptimisticActivityReorder() {
    if (_conversationSummaryItems.isEmpty) return false;
    final sessions = imService.sessions;
    if (sessions.isEmpty) return false;

    // 收集每个 groupKey 的最大 activityAt
    // 访客 session 在 summary items 里用各自的 sessionId 索引（非合成 visitor:group）
    final maxActivityByGroup = <String, int>{};
    for (final session in sessions) {
      final key = session.isVisitor
          ? session.sessionId
          : _buildConversationGroupKey(session);
      final current = maxActivityByGroup[key] ?? 0;
      if (session.activityAt > current) {
        maxActivityByGroup[key] = session.activityAt;
      }
    }

    final previous = _lastOptimisticActivityByGroup;
    if (previous != null &&
        previous.length == maxActivityByGroup.length &&
        maxActivityByGroup.entries.every(
          (entry) => previous[entry.key] == entry.value,
        )) {
      return false;
    }
    _lastOptimisticActivityByGroup = Map<String, int>.of(maxActivityByGroup);

    bool changed = false;
    for (int i = 0; i < _conversationSummaryItems.length; i++) {
      final item = _conversationSummaryItems[i];
      final key = item.latestSession.isVisitor
          ? item.latestSession.sessionId
          : item.groupKey;
      final newActivity = maxActivityByGroup[key];
      if (newActivity == null || newActivity <= item.latestSession.activityAt) {
        continue;
      }
      // 只提升会话时间（lastMessageTime，即 activityAt 的口径），其余字段不变
      _conversationSummaryItems[i] = ConversationListItem(
        groupKey: item.groupKey,
        latestSession: item.latestSession.copyWith(
          lastMessageTime: newActivity,
        ),
        sessions: item.sessions,
        unreadCount: item.unreadCount,
        hasUnreadMention: item.hasUnreadMention,
        badgeUnreadCount: item.badgeUnreadCount,
        hasMutedUnread: item.hasMutedUnread,
        isMuted: item.isMuted,
        isPinned: item.isPinned,
        pinnedAt: item.pinnedAt,
        threadCountOverride: item.threadCount,
      );
      changed = true;
    }

    if (changed) {
      _conversationSummaryItems.sort(_compareConversationItems);
    }
    return changed;
  }

  void _scheduleConversationSummaryRefreshFromRealtime() {
    if (!_isConversationsTabActive() ||
        _conversationPageInFlight ||
        _deferredConversationSummaryRefreshTimer != null) {
      return;
    }
    final now = DateTime.now();
    final earliestByRealtime = _conversationLastRefreshAttemptAt.add(
      _conversationRealtimeRefreshMinInterval,
    );
    var targetAt = now.add(_conversationRealtimeRefreshDelay);
    if (targetAt.isBefore(earliestByRealtime)) {
      targetAt = earliestByRealtime;
    }
    if (targetAt.isBefore(_conversationNextAllowedAt)) {
      targetAt = _conversationNextAllowedAt;
    }
    final delay = targetAt.difference(now);
    _deferredConversationSummaryRefreshTimer = Timer(
      delay.isNegative ? Duration.zero : delay,
      () {
        _deferredConversationSummaryRefreshTimer = null;
        if (!_isConversationsTabActive() || !_conversationListApiActive) {
          return;
        }
        unawaited(
          _refreshConversationPageIfEnabled().then((refreshed) {
            if (!refreshed && _conversationListApiActive) {
              _scheduleConversationSummaryRefreshFromRealtime();
            }
          }),
        );
      },
    );
  }

  void _rebuildGroupedSessionsImmediately() {
    if (searchQuery.value.trim().isNotEmpty) return;
    if (_conversationListApiActive) {
      _applyConversationSummaryItems();
      return;
    }
    _rebuildGroupedSessions();
  }

  void _rebuildGroupedSessions() {
    final grouped = <String, List<SessionModel>>{};

    _unreadMentions.syncWithSessions(imService.sessions);
    for (final session in imService.sessions) {
      final key = _buildConversationGroupKey(session);
      grouped.putIfAbsent(key, () => <SessionModel>[]).add(session);
    }

    _hasUnfilteredSessions.value = grouped.isNotEmpty;

    final items = <ConversationListItem>[];
    for (final entry in grouped.entries) {
      if (entry.value.isEmpty) continue;
      items.add(
        _buildConversationItemFromLocalSessions(entry.key, entry.value),
      );
    }

    items.sort(_compareConversationItems);
    _prefetchTopSessionDetails(items);
    _warmupInitialConversationAvatars(items);
    if (listEquals(_groupedSessions, items)) {
      return;
    }
    _groupedSessions.assignAll(items);
  }

  int _dbSearchVersion = 0;

  Future<void> _performDbSearch(String keyword) async {
    final version = ++_dbSearchVersion;
    final rows = await LocalDb.searchSessionRecords([keyword]);
    if (_dbSearchVersion != version) return;

    final grouped = <String, List<SessionModel>>{};
    for (final row in rows) {
      final session = SessionModel.fromJson(row);
      final key = _buildConversationGroupKey(session);
      grouped.putIfAbsent(key, () => <SessionModel>[]).add(session);
    }

    final items = <ConversationListItem>[];
    for (final entry in grouped.entries) {
      if (entry.value.isEmpty) continue;
      items.add(
        _buildConversationItemFromLocalSessions(entry.key, entry.value),
      );
    }

    items.sort(_compareConversationItems);
    _groupedSessions.assignAll(items);
  }

  /// 用一组本地会话（同一分组的多个线程）聚合出一个会话列表行。
  /// 未读/角标/置顶/静音口径与 API 路径的本地对齐完全一致，供非 API 全量重建
  /// 与 API 路径的"未读兜底补行"共用，避免两处聚合口径分叉。
  ConversationListItem _buildConversationItemFromLocalSessions(
    String groupKey,
    List<SessionModel> groupSessions,
  ) {
    final sessions = List<SessionModel>.from(groupSessions)
      ..sort(SessionModel.compareByPriority);
    final latest = sessions.reduce((currentLatest, candidate) {
      return _compareByConversationActivity(currentLatest, candidate) <= 0
          ? currentLatest
          : candidate;
    });
    final unreadCount = sessions.fold<int>(
      0,
      (sum, session) => sum + imService.totalUnreadForSession(session),
    );
    final badgeUnreadCount = sessions.fold<int>(
      0,
      (sum, session) => sum + imService.notificationUnreadForSession(session),
    );
    final isPrivateGroup = groupKey.startsWith('private:');
    final isMuted = isPrivateGroup
        ? sessions.any(
            (session) =>
                session.friendIsMuted || imService.isPeerMuted(session.peerId),
          )
        : sessions.isNotEmpty && sessions.every((session) => session.isMuted);
    final hasMutedUnread = unreadCount > badgeUnreadCount;
    var isPinned = false;
    var pinnedAt = 0;
    if (isPrivateGroup) {
      // Private chats: only friend-level pin controls group pin status
      for (final session in sessions) {
        if (session.friendIsPinned) {
          isPinned = true;
          if (session.friendPinnedAt > pinnedAt) {
            pinnedAt = session.friendPinnedAt;
          }
        }
      }
    } else {
      for (final session in sessions) {
        if (!session.isPinned) continue;
        isPinned = true;
        if (session.pinnedAt > pinnedAt) {
          pinnedAt = session.pinnedAt;
        }
      }
    }
    return ConversationListItem(
      groupKey: groupKey,
      latestSession: latest,
      sessions: sessions,
      unreadCount: unreadCount,
      hasUnreadMention: sessions.any(
        (session) => _unreadMentions.hasUnreadMention(session.sessionId),
      ),
      badgeUnreadCount: badgeUnreadCount,
      hasMutedUnread: hasMutedUnread,
      isMuted: isMuted,
      isPinned: isPinned,
      pinnedAt: pinnedAt,
    );
  }

  void updateSearchQuery(String query) {
    searchQuery.value = query;
  }

  Future<void> openUserQrScanner() async {
    await _friendQrFlowService.openUserQrScanner();
  }

  Future<void> deleteSession(SessionModel session) async {
    await imService.deleteConversation(session.sessionId);
    if (session.isVisitor) {
      // visitor summary items use per-session backend groupKey, not the synthetic visitor:group
      final sid = session.sessionId;
      if (_conversationSummaryItems.any(
        (item) => item.latestSession.sessionId == sid,
      )) {
        _conversationSummaryItems.removeWhere(
          (item) => item.latestSession.sessionId == sid,
        );
        _applyConversationSummaryItems();
      }
    } else {
      _removeConversationItemByGroupKey(_buildConversationGroupKey(session));
    }
  }

  Future<void> deleteSessionGroup(ConversationListItem item) async {
    for (final session in item.sessions) {
      await imService.deleteConversation(session.sessionId);
    }
    _removeConversationItemByGroupKey(item.groupKey);
  }

  /// Immediately removes a conversation group from the API-driven summary
  /// list so the UI updates without waiting for the delayed API refresh.
  void _removeConversationItemByGroupKey(String groupKey) {
    final removed = _conversationSummaryItems.any(
      (item) => item.groupKey == groupKey,
    );
    if (!removed) return;
    _conversationSummaryItems.removeWhere((item) => item.groupKey == groupKey);
    _applyConversationSummaryItems();
  }

  void markSessionRead(SessionModel session) {
    imService.clearUnread(session.sessionId);
  }

  void markSessionGroupRead(ConversationListItem item) {
    for (final session in item.sessions) {
      imService.clearUnread(session.sessionId);
    }
  }

  void markSessionGroupUnread(ConversationListItem item) {
    if (item.sessions.isEmpty) return;
    // For grouped sessions, mark the most recent thread as unread (count=1).
    final latest = item.sessions.reduce((a, b) {
      return b.activityAt > a.activityAt ? b : a;
    });
    imService.markUnread(latest.sessionId);
  }

  Future<void> setSessionGroupPinned(
    ConversationListItem item, {
    required bool isPinned,
  }) async {
    if (_isPrivateConversation(item)) {
      // 直接取 session 上的 peerId，不经过 _resolvePrivatePeerId，
      // 因为 _resolvePrivatePeerId 会跳过 peerType==2（Agent），
      // 导致 Agent 私聊永远拿不到 peerId，落入 session-level fallback。
      // 而后端私聊外层 is_pinned 只认 user_peer_pins（对端级），
      // session-level pin 不影响消息列表置顶状态，取消置顶就会无效。
      final peerId = item.latestSession.peerId.trim();
      final fs = _friendService;

      // Try friend-level pin first (works for human friends and agents)
      if (peerId.isNotEmpty && fs != null) {
        final friendSuccess = await fs.setFriendPinned(
          friendUserId: peerId,
          isPinned: isPinned,
        );
        if (friendSuccess) {
          final now = DateTime.now().millisecondsSinceEpoch;
          await imService.applyLocalFriendPin(
            sessionIds: item.sessions.map((s) => s.sessionId).toList(),
            isPinned: isPinned,
            pinnedAt: isPinned ? now : 0,
          );
          _applyImmediatePinReorderToSummary(item.groupKey, isPinned);
          if (!_conversationListApiActive) {
            _rebuildGroupedSessionsImmediately();
          }
          return;
        }
      }

      // Fall back to session-level pin (non-friend or agent)
      for (final session in item.sessions) {
        await imService.setSessionPinned(session.sessionId, isPinned: isPinned);
      }
      _applyImmediatePinReorderToSummary(item.groupKey, isPinned);
    } else {
      // Group chats: use session-level pin
      for (final session in item.sessions) {
        await imService.setSessionPinned(session.sessionId, isPinned: isPinned);
      }
      _applyImmediatePinReorderToSummary(item.groupKey, isPinned);
    }
  }

  /// API active 模式下，会话顺序由后端分页结果驱动，本地不主动重排。
  /// 但置顶/取消置顶必须即时生效，所以这里直接更新目标分组的置顶字段，
  /// 再用与列表重建一致的规则就地重排，避免等待下一次后端刷新。
  /// 不依赖 imService.sessions（API 模式下其可能为空）。
  void _applyImmediatePinReorderToSummary(String groupKey, bool isPinned) {
    if (!_conversationListApiActive) return;
    // visitor:group 是合成 key，需要更新所有访客 summary 条目
    if (groupKey == visitorGroupKey) {
      final pinnedAt = isPinned ? DateTime.now().millisecondsSinceEpoch : 0;
      bool changed = false;
      for (int i = 0; i < _conversationSummaryItems.length; i++) {
        final item = _conversationSummaryItems[i];
        if (!item.latestSession.isVisitor || item.isPinned == isPinned) {
          continue;
        }
        _conversationSummaryItems[i] = ConversationListItem(
          groupKey: item.groupKey,
          latestSession: item.latestSession,
          sessions: item.sessions,
          unreadCount: item.unreadCount,
          hasUnreadMention: item.hasUnreadMention,
          badgeUnreadCount: item.badgeUnreadCount,
          hasMutedUnread: item.hasMutedUnread,
          isMuted: item.isMuted,
          isPinned: isPinned,
          pinnedAt: pinnedAt,
          threadCountOverride: item.threadCount,
        );
        changed = true;
      }
      if (changed) {
        _conversationSummaryItems.sort(_compareConversationItems);
        _applyConversationSummaryItems();
      }
      return;
    }
    final idx = _conversationSummaryItems.indexWhere(
      (it) => it.groupKey == groupKey,
    );
    if (idx < 0) return;
    final item = _conversationSummaryItems[idx];
    if (item.isPinned == isPinned) return;
    final pinnedAt = isPinned ? DateTime.now().millisecondsSinceEpoch : 0;
    _conversationSummaryItems[idx] = ConversationListItem(
      groupKey: item.groupKey,
      latestSession: item.latestSession,
      sessions: item.sessions,
      unreadCount: item.unreadCount,
      hasUnreadMention: item.hasUnreadMention,
      badgeUnreadCount: item.badgeUnreadCount,
      hasMutedUnread: item.hasMutedUnread,
      isMuted: item.isMuted,
      isPinned: isPinned,
      pinnedAt: pinnedAt,
      threadCountOverride: item.threadCount,
    );
    _conversationSummaryItems.sort(_compareConversationItems);
    _applyConversationSummaryItems();
  }

  Future<bool> setSessionGroupMuted(
    ConversationListItem item, {
    required bool isMuted,
  }) async {
    if (_isPrivateConversation(item)) {
      return _setPrivateConversationMuted(item, isMuted: isMuted);
    }
    var success = true;
    for (final session in item.sessions) {
      if (session.isMuted == isMuted) {
        continue;
      }
      final muted = await imService.setSessionMuted(
        session.sessionId,
        isMuted: isMuted,
      );
      if (!muted) {
        success = false;
      }
    }
    if (!success) {
      await imService.refreshSessionsNow();
      return false;
    }
    _applyMuteToVisibleList(item.groupKey, isMuted: isMuted);
    return true;
  }

  Future<bool> _setPrivateConversationMuted(
    ConversationListItem item, {
    required bool isMuted,
  }) async {
    var peerId = item.latestSession.peerId.trim();
    if (peerId.isEmpty && item.groupKey.startsWith('private:')) {
      final parts = item.groupKey.split(':');
      if (parts.length >= 3) {
        peerId = parts.sublist(2).join(':');
      }
    }
    final fs = _friendService;
    if (peerId.isEmpty || fs == null) {
      return false;
    }
    final ok = await fs.setFriendMuted(friendUserId: peerId, isMuted: isMuted);
    if (!ok) {
      return false;
    }
    final sessionIds = <String>{
      for (final session in item.sessions)
        if (session.sessionId.trim().isNotEmpty) session.sessionId.trim(),
      for (final session in _resolveLocalSessionsForGroup(item.groupKey))
        if (session.sessionId.trim().isNotEmpty) session.sessionId.trim(),
    };
    await imService.applyLocalFriendMute(
      peerId: peerId,
      sessionIds: sessionIds.toList(),
      isMuted: isMuted,
    );
    _applyMuteToVisibleList(item.groupKey, isMuted: isMuted);
    return true;
  }

  void _applyMuteToVisibleList(String groupKey, {required bool isMuted}) {
    _applyImmediateMuteToSummary(groupKey, isMuted: isMuted);
    if (!_conversationListApiActive) {
      _rebuildGroupedSessionsImmediately();
    }
  }

  void _syncPeerMuteFromSummary(ConversationSummaryModel summary) {
    if (summary.conversationType != 'private') {
      return;
    }
    final peerId = summary.peerId.trim();
    if (peerId.isEmpty) {
      return;
    }
    imService.reconcilePeerMuteFromServer(peerId, summary.isMuted);
  }

  void _applyImmediateMuteToSummary(String groupKey, {required bool isMuted}) {
    if (!_conversationListApiActive) return;
    var changed = false;
    if (groupKey == visitorGroupKey) {
      for (var i = 0; i < _conversationSummaryItems.length; i++) {
        final item = _conversationSummaryItems[i];
        if (!item.latestSession.isVisitor || item.isMuted == isMuted) {
          continue;
        }
        _conversationSummaryItems[i] = _copyConversationItemWithMute(
          item,
          isMuted: isMuted,
        );
        changed = true;
      }
    } else {
      final idx = _conversationSummaryItems.indexWhere(
        (it) => it.groupKey == groupKey,
      );
      if (idx < 0) return;
      final item = _conversationSummaryItems[idx];
      if (item.isMuted == isMuted) return;
      _conversationSummaryItems[idx] = _copyConversationItemWithMute(
        item,
        isMuted: isMuted,
      );
      changed = true;
    }
    if (changed) {
      _applyConversationSummaryItems();
    }
  }

  ConversationListItem _copyConversationItemWithMute(
    ConversationListItem item, {
    required bool isMuted,
  }) {
    final private = _isPrivateConversation(item);
    return ConversationListItem(
      groupKey: item.groupKey,
      latestSession: item.latestSession.copyWith(
        isMuted: private ? item.latestSession.isMuted : isMuted,
        friendIsMuted: private ? isMuted : item.latestSession.friendIsMuted,
      ),
      sessions: [
        for (final session in item.sessions)
          session.copyWith(
            isMuted: private ? session.isMuted : isMuted,
            friendIsMuted: private ? isMuted : session.friendIsMuted,
          ),
      ],
      unreadCount: item.unreadCount,
      hasUnreadMention: item.hasUnreadMention,
      badgeUnreadCount: isMuted ? 0 : item.unreadCount,
      hasMutedUnread: isMuted && item.unreadCount > 0,
      isMuted: isMuted,
      isPinned: item.isPinned,
      pinnedAt: item.pinnedAt,
      threadCountOverride: item.threadCount,
    );
  }

  String _getDisplayTitle(SessionModel session) =>
      imService.resolveSessionDisplayTitle(session);

  String getDisplayTitle(SessionModel session) => _getDisplayTitle(session);

  String getPrivatePeerDisplayName(SessionModel session) {
    return _identity.getPrivatePeerDisplayName(session);
  }

  String getConversationHeaderTitle(ConversationListItem item) {
    return _getConversationPrimaryTitle(item.latestSession);
  }

  String getConversationListTitle(ConversationListItem item) {
    _peerNameRefreshVersion.value; // reactive: per-tile Obx subscribes here
    if (item.groupKey == visitorGroupKey) {
      return 'conversations_visitor_group_title'.tr;
    }
    return _getConversationPrimaryTitle(item.latestSession);
  }

  String getConversationMetaLine(ConversationListItem item) {
    if (_isPrivateConversation(item)) {
      final headerTitle = _getConversationPrimaryTitle(item.latestSession);
      final threadTitle = _getPrivateConversationThreadTitle(
        item.latestSession,
      );
      final threadSuffix = item.threadCount > 1
          ? _threadCountLabel(item.threadCount)
          : '';
      if (threadTitle.isNotEmpty && threadTitle != headerTitle) {
        if (threadSuffix.isEmpty) {
          return threadTitle;
        }
        return '$threadTitle $threadSuffix';
      }
      if (threadSuffix.isNotEmpty) {
        return threadSuffix;
      }
      return headerTitle;
    }

    final title = _getStableConversationTitle(item.latestSession);
    final threadSuffix = item.threadCount > 1
        ? ' ${_threadCountLabel(item.threadCount)}'
        : '';
    if (threadSuffix.isEmpty) return title;
    return '$title$threadSuffix';
  }

  String getConversationLatestSummary(ConversationListItem item) {
    final streamingSummary = _getConversationStreamingSummary(item);
    if (streamingSummary.isNotEmpty) {
      return _normalizeThreadText(streamingSummary);
    }
    final source = _resolveConversationPreviewSession(item);
    if (source != null) {
      return _normalizeThreadText(source.lastMessage);
    }
    return _normalizeThreadText(item.latestSession.lastMessage);
  }

  /// 摘要行右侧的时间：与摘要文本取自同一条会话，避免摘要回退到未读线程后
  /// 时间仍停在另一条已读线程上。
  int getConversationDisplayTime(ConversationListItem item) {
    // 正在流式回复时摘要显示的是流式正文，时间跟着组内最新，不要回退到未读线程。
    if (_getConversationStreamingSummary(item).isNotEmpty) {
      return item.latestSession.displayTime;
    }
    final unread = _latestUnreadGroupPreview(item);
    if (unread != null && unread.displayTime > 0) {
      return unread.displayTime;
    }
    return item.latestSession.displayTime;
  }

  /// 摘要取哪一条会话：未读线程优先，其次本地最新。返回 null 表示沿用服务端摘要。
  SessionModel? _resolveConversationPreviewSession(ConversationListItem item) {
    // 同一联系人下多条线程折叠成一行时，已读的最新线程会盖住仍未读的线程，
    // 用户看不到还没读的内容；未读线程优先能让摘要在读完后自然回落到最新。
    final unread = _latestUnreadGroupPreview(item);
    if (unread != null) {
      return unread;
    }
    // API 摘要行不合并本地 lastMessage；出错后本地成功回复（含正文+卡片）
    // 可能已经更新 imService.sessions，列表仍显示服务端停在错误句的 last_msg。
    final localPreview = _latestLocalGroupPreview(item);
    if (localPreview != null &&
        localPreview.lastMessage.trim().isNotEmpty &&
        localPreview.lastMessageTime >= item.latestSession.lastMessageTime) {
      return localPreview;
    }
    return null;
  }

  /// 组内仍未读且有摘要的会话中最活跃的一条；没有未读会话时返回 null。
  SessionModel? _latestUnreadGroupPreview(ConversationListItem item) {
    if (item.unreadCount <= 0) return null;
    SessionModel? best;
    final seen = <String>{};
    void consider(SessionModel session) {
      final sid = session.sessionId.trim();
      if (sid.isEmpty || !seen.add(sid)) return;
      if (_effectiveSessionUnread(session) <= 0) return;
      if (session.lastMessage.trim().isEmpty) return;
      if (best == null || _compareByConversationActivity(best!, session) > 0) {
        best = session;
      }
    }

    // 先看本地会话：已读状态先落在本地，避免服务端摘要行的陈旧未读数把
    // 已经读完的线程继续钉在摘要上。
    for (final session in imService.sessions) {
      if (_buildConversationGroupKey(session) == item.groupKey) {
        consider(session);
      }
    }
    for (final session in item.sessions) {
      consider(session);
    }
    return best;
  }

  int _effectiveSessionUnread(SessionModel session) {
    final override = imService.unreadOverrideForSession(session.sessionId);
    if (override != null) {
      return override < 0 ? 0 : override;
    }
    return imService.totalUnreadForSession(session);
  }

  SessionModel? _latestLocalGroupPreview(ConversationListItem item) {
    SessionModel? best;
    void consider(SessionModel session) {
      if (session.lastMessage.trim().isEmpty) return;
      if (best == null ||
          session.lastMessageTime > best!.lastMessageTime ||
          (session.lastMessageTime == best!.lastMessageTime &&
              session.activityAt > best!.activityAt)) {
        best = session;
      }
    }

    for (final session in item.sessions) {
      consider(session);
    }
    for (final session in imService.sessions) {
      if (_buildConversationGroupKey(session) == item.groupKey) {
        consider(session);
      }
    }
    return best;
  }

  String _getConversationStreamingSummary(ConversationListItem item) {
    if (!imService.hasStreamingSessionPreviews) return '';

    String latestText = '';
    var latestAt = 0;
    final seen = <String>{};

    void consider(SessionModel session) {
      final sid = session.sessionId.trim();
      if (sid.isEmpty || !seen.add(sid)) return;
      final text = imService.streamingSessionPreviewForSession(sid);
      if (text.isEmpty) return;
      final updatedAt = imService.streamingSessionPreviewUpdatedAtForSession(
        sid,
      );
      if (latestText.isEmpty || updatedAt >= latestAt) {
        latestText = text;
        latestAt = updatedAt;
      }
    }

    for (final session in item.sessions) {
      consider(session);
    }
    // The conversation-page API may only carry its representative/latest
    // thread. Include local sibling threads so an active response in an older
    // thread is still visible on the grouped conversation row.
    for (final session in imService.sessions) {
      if (_buildConversationGroupKey(session) == item.groupKey) {
        consider(session);
      }
    }
    return latestText;
  }

  String getConversationSecondaryText(ConversationListItem item) {
    return getConversationLatestSummary(item);
  }

  String getSessionThreadTitle(SessionModel session) {
    final sid = session.sessionId.trim();
    final explicitTitle = _normalizeThreadTitle(session.title);
    if (explicitTitle.isNotEmpty && explicitTitle != sid) {
      return explicitTitle;
    }

    final resolvedTitle = _normalizeThreadTitle(getDisplayTitle(session));
    if (resolvedTitle.isNotEmpty && resolvedTitle != sid) {
      return resolvedTitle;
    }

    final fallbackFromMessage = _normalizeThreadTitle(
      _replaceMentionsForPreview(session.lastMessage),
    );
    if (fallbackFromMessage.isNotEmpty) {
      return fallbackFromMessage;
    }

    return 'conversations_thread_untitled'.tr;
  }

  /// 会话没有可展示摘要时返回空串（调用方据此隐藏摘要行），
  /// 不再用 "..." 占位——那看起来像内容丢失或加载未完成。
  String getSessionThreadPreview(SessionModel session) {
    final streaming = imService.streamingSessionPreviewForSession(
      session.sessionId,
    );
    if (streaming.isNotEmpty) {
      return _normalizeThreadText(streaming);
    }
    return _normalizeThreadText(session.lastMessage);
  }

  String getAvatarTitle(ConversationListItem item) {
    return _getConversationPrimaryTitle(item.latestSession);
  }

  String getAvatarSeed(ConversationListItem item) {
    final session = item.latestSession;
    if (session.type != 'private') return session.sessionId;
    final peerId = session.peerId.trim();
    if (peerId.isEmpty) return session.sessionId;
    if (session.peerType == 2) return 'agent:$peerId';
    return peerId;
  }

  String getConversationAvatarUrl(ConversationListItem item) {
    return _identity.getConversationAvatarUrl(item);
  }

  bool isGroupConversation(ConversationListItem item) {
    return !_isPrivateConversation(item);
  }

  // Session-centric avatar API. Lets any page (e.g. the favorites list) reuse the
  // single avatar-resolution implementation by passing a [SessionModel] directly,
  // without synthesizing a [ConversationListItem]. Backed by the same caches and
  // reactive versions as the conversation-list rendering path.
  bool isGroupSession(SessionModel session) => session.type == 'group';

  String avatarUrlForSession(SessionModel session) {
    return _identity.getAvatarUrlForSession(session, isGroupSession(session));
  }

  List<SessionAvatarMember> avatarMembersForSession(SessionModel session) {
    return _prefetch.getAvatarMembersForSession(
      session,
      isGroupSession(session),
    );
  }

  void watchSessionAvatar(SessionModel session) {
    _prefetch.watchSessionAvatar(session, isGroupSession(session));
  }

  bool canOpenAccountInfo(ConversationListItem item) {
    return item.latestSession.sessionId.trim().isNotEmpty;
  }

  bool canCreateFreshPrivateSession(ConversationListItem item) {
    if (_sessionService == null || !_isPrivateConversation(item)) {
      return false;
    }
    final session = item.latestSession;
    if (session.peerType <= 0) {
      return false;
    }
    if (session.peerType == 2) {
      return session.peerId.trim().isNotEmpty;
    }
    if (_resolvePrivatePeerId(session).isNotEmpty) {
      return true;
    }
    return session.sessionId.trim().isNotEmpty;
  }

  void handleAvatarTap(ConversationListItem item) {
    _actions.handleAvatarTap(item);
  }

  List<SessionAvatarMember> getConversationAvatarMembers(
    ConversationListItem item,
  ) {
    return _prefetch.getConversationAvatarMembers(item);
  }

  RxInt _groupAvatarVersionForSession(String sid) =>
      _groupAvatarVersionBySession.putIfAbsent(sid, () => 0.obs);

  RxInt _peerAvatarVersionForPeer(String peerId) =>
      _peerAvatarVersionByPeerId.putIfAbsent(peerId, () => 0.obs);

  RxInt _privateAvatarVersionForSession(String sid) =>
      _privateAvatarVersionBySession.putIfAbsent(sid, () => 0.obs);

  bool _needsGroupAvatarRefresh(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    final cachedVersion = _groupAvatarMemberVersions[sid];
    if (cachedVersion == null) return true;
    return cachedVersion != imService.getSessionMemberEventVersion(sid);
  }

  void watchConversationAvatar(ConversationListItem item) {
    _prefetch.watchConversationAvatar(item);
  }

  void _clearRxVersionMap(Map<String, RxInt> versions) {
    // These Rx values may still be observed by mounted Obx widgets while the
    // route is tearing down. Clearing our references is enough; closing them
    // here can race with widget disposal and crash inside GetX subscription
    // cleanup.
    versions.clear();
  }

  void _prefetchTopSessionDetails(List<ConversationListItem> items) {
    _prefetch.prefetchTopSessionDetails(items);
  }

  void _warmupInitialConversationAvatars(List<ConversationListItem> items) {
    _prefetch.warmupInitialConversationAvatars(items);
  }

  void _enqueueSessionDetailPrefetch(String sessionId) {
    _prefetch.enqueueSessionDetailPrefetch(sessionId);
  }

  String formatTime(int timestamp) {
    if (timestamp <= 0) return '';
    final now = DateTime.now();
    final date = DateTime.fromMillisecondsSinceEpoch(timestamp);
    final diff = now.difference(date);

    if (diff.inDays == 0 && now.day == date.day) {
      return DateFormat('HH:mm').format(date);
    } else if (diff.inDays < 7) {
      switch (date.weekday) {
        case 1:
          return 'time_monday'.tr;
        case 2:
          return 'time_tuesday'.tr;
        case 3:
          return 'time_wednesday'.tr;
        case 4:
          return 'time_thursday'.tr;
        case 5:
          return 'time_friday'.tr;
        case 6:
          return 'time_saturday'.tr;
        case 7:
          return 'time_sunday'.tr;
        default:
          return '';
      }
    } else {
      return DateFormat('MM/dd').format(date);
    }
  }

  void handleConversationTap(BuildContext context, ConversationListItem item) {
    _actions.handleConversationTap(context, item);
  }

  Future<String?> createFreshPrivateSession(
    ConversationListItem item, {
    bool openChat = true,
    bool replaceCurrentRoute = false,
  }) {
    return _actions.createFreshPrivateSession(
      item,
      openChat: openChat,
      replaceCurrentRoute: replaceCurrentRoute,
    );
  }

  void _storeGroupAvatarMembers(
    String sessionId,
    List<SessionAvatarMember> members, {
    bool notify = true,
    bool persist = true,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final normalized = List<SessionAvatarMember>.unmodifiable(members.take(9));
    final previous = _groupAvatarMembersBySession[sid];
    final changed = previous == null || !listEquals(previous, normalized);
    _groupAvatarMembersBySession[sid] = normalized;
    _groupAvatarMemberVersions[sid] = imService.getSessionMemberEventVersion(
      sid,
    );
    if (normalized.any((m) => m.memberType == 2)) {
      _sessionsWithAgentMembers.add(sid);
    } else {
      _sessionsWithAgentMembers.remove(sid);
    }
    if (persist) {
      unawaited(LocalDb.upsertSessionGroupAvatarMembers(sid, normalized));
    }
    if (notify && changed) {
      _groupAvatarVersionForSession(sid).value++; // only triggers this session
    }
  }

  Future<bool> _pruneUnavailableSessionIfNeeded(
    String sessionId,
    SessionDetailResult detailResult,
  ) async {
    return _prefetch.pruneUnavailableSessionIfNeeded(sessionId, detailResult);
  }

  List<_GroupAvatarSourceMember> _parseGroupAvatarSourceMembers(
    dynamic membersRaw,
  ) {
    if (membersRaw is! List) return const <_GroupAvatarSourceMember>[];

    final members = <_GroupAvatarSourceMember>[];
    for (final item in membersRaw) {
      if (item is! Map) continue;
      final memberId = (item['member_id'] ?? '').toString().trim();
      if (memberId.isEmpty) continue;
      members.add(
        _GroupAvatarSourceMember(
          memberId: memberId,
          memberType: _parseInt(item['member_type']),
          nickname: (item['nickname'] ?? '').toString().trim(),
        ),
      );
    }
    return members;
  }

  Future<void> _prepareGroupAvatarDependencies(
    List<_GroupAvatarSourceMember> members,
  ) async {
    final fs = _friendService;
    final userIds = <String>[];
    var hasAgentMember = false;
    final myUserId = _authService?.userId?.trim() ?? '';

    for (final member in members) {
      if (member.memberType == 2) {
        hasAgentMember = true;
        continue;
      }
      if (member.memberId == myUserId) {
        continue;
      }
      userIds.add(member.memberId);
    }

    if (fs != null && userIds.isNotEmpty) {
      await fs.ensureUserProfiles(userIds);
    }

    final agentService = _agentService;
    if (agentService != null && hasAgentMember && agentService.agents.isEmpty) {
      await agentService.loadAgents();
    }
  }

  SessionAvatarMember _buildConversationAvatarMember(
    _GroupAvatarSourceMember member,
  ) {
    return SessionAvatarMember(
      memberId: member.memberId,
      memberType: member.memberType,
      displayName: _resolveGroupAvatarMemberDisplayName(member),
      avatarUrl: _resolveGroupAvatarMemberAvatarUrl(member),
    );
  }

  String _resolveGroupAvatarMemberDisplayName(_GroupAvatarSourceMember member) {
    if (member.memberType == 2) {
      if (member.nickname.isNotEmpty) {
        return member.nickname;
      }
      final agentService = _agentService;
      if (agentService != null) {
        final idx = agentService.agents.indexWhere(
          (agent) => agent.id == member.memberId,
        );
        if (idx != -1) {
          final name = agentService.agents[idx].agentName.trim();
          if (name.isNotEmpty) {
            return name;
          }
        }
      }
      return 'Agent';
    }

    final myUserId = _authService?.userId?.trim() ?? '';
    if (myUserId.isNotEmpty && member.memberId == myUserId) {
      final me = _authService?.user;
      final nickname = me?.nickname.trim() ?? '';
      if (nickname.isNotEmpty) return nickname;
      final username = me?.username.trim() ?? '';
      if (username.isNotEmpty) return username;
    }

    if (member.nickname.isNotEmpty) {
      return member.nickname;
    }

    final fs = _friendService;
    if (fs != null) {
      final nickname = fs.getUserNickname(member.memberId)?.trim() ?? '';
      if (nickname.isNotEmpty) {
        return nickname;
      }
    }

    return member.memberId;
  }

  String _resolveGroupAvatarMemberAvatarUrl(_GroupAvatarSourceMember member) {
    if (member.memberType == 2) {
      final agentService = _agentService;
      if (agentService == null) return '';
      final idx = agentService.agents.indexWhere(
        (agent) => agent.id == member.memberId,
      );
      if (idx == -1) return '';
      return agentService.agents[idx].avatarUrl.trim();
    }

    final myUserId = _authService?.userId?.trim() ?? '';
    if (myUserId.isNotEmpty && member.memberId == myUserId) {
      return _authService?.user?.avatarUrl?.trim() ?? '';
    }

    return _friendService?.getUserAvatarUrl(member.memberId)?.trim() ?? '';
  }

  String _resolvePrivatePeerId(SessionModel session) {
    return _identity.resolvePrivatePeerId(session);
  }

  bool _isPrivateConversation(ConversationListItem item) {
    return item.groupKey.startsWith('private:') ||
        item.latestSession.type == 'private';
  }

  String _getConversationPrimaryTitle(SessionModel session) {
    if (session.type == 'private') {
      final peerTitle = getPrivatePeerDisplayName(session).trim();
      if (peerTitle.isNotEmpty) {
        return peerTitle;
      }

      final threadTitle = _getPrivateConversationThreadTitle(session);
      if (threadTitle.isNotEmpty) {
        return threadTitle;
      }

      return 'conversations_thread_untitled'.tr;
    }

    return _getStableConversationTitle(session);
  }

  String _getPrivateConversationThreadTitle(SessionModel session) {
    final sid = session.sessionId.trim();
    final explicitTitle = _normalizeThreadTitle(session.title);
    if (explicitTitle.isEmpty || explicitTitle == sid) {
      return '';
    }
    if (session.peerType == 2) {
      final storedPeerTitle = _normalizeThreadTitle(
        _resolveSessionPeerDisplayName(session),
      );
      if (storedPeerTitle.isNotEmpty && explicitTitle == storedPeerTitle) {
        return '';
      }
    }
    return explicitTitle;
  }

  String _resolveSessionPeerDisplayName(SessionModel session) {
    final nickname = session.peerNickname.trim();
    if (nickname.isNotEmpty) return nickname;
    final username = session.peerUsername.trim();
    if (username.isNotEmpty) return username;
    return '';
  }

  String _getStableConversationTitle(SessionModel session) {
    return _getDisplayTitle(session).trim();
  }

  String _threadCountLabel(int count) {
    final translated = 'conversations_thread_count'.trParams({
      'count': '$count',
    }).trim();
    if (translated.isEmpty ||
        translated == 'conversations_thread_count' ||
        translated == 'conversations_thread_count_inline') {
      return '$count个会话';
    }
    return translated;
  }

  String _normalizeThreadText(String raw) {
    return ChatMessagePreview.summarize(_replaceMentionsForPreview(raw));
  }

  // 标题清洗保留下划线等合法标记，不当作消息正文去 Markdown 化。
  String _normalizeThreadTitle(String raw) {
    return ChatMessagePreview.summarizeTitle(raw);
  }

  // 会话列表预览/搜索面：把消息原文里的 @用户ID 替换成 @显示名，
  // 优先备注 > 昵称 > 用户名 > agent 名；解析不到时保留原文不动。
  String _replaceMentionsForPreview(String raw) {
    if (raw.isEmpty || !raw.contains('@')) {
      return raw;
    }
    return ChatNumericMentionResolver.replaceNumericMentions(
      raw,
      resolveDisplayName: _resolveMentionDisplayNameForPreview,
      resolveAliases: _resolveMentionAliasesForPreview,
    );
  }

  String? _resolveMentionDisplayNameForPreview(String rawUserId) {
    final userId = rawUserId.trim();
    if (userId.isEmpty) return null;

    final myId = _authService?.userId?.trim() ?? '';
    if (myId.isNotEmpty && myId == userId) {
      final me = _authService?.user;
      final nickname = me?.nickname.trim() ?? '';
      if (nickname.isNotEmpty && nickname != userId) return nickname;
      final username = me?.username.trim() ?? '';
      if (username.isNotEmpty && username != userId) return username;
    }

    final fs = _friendService;
    if (fs != null) {
      final remark = fs.getFriendRemarkName(userId)?.trim() ?? '';
      if (remark.isNotEmpty && remark != userId) return remark;

      final friend = fs.getFriendItem(userId);
      if (friend != null) {
        final nickname = friend.nickname.trim();
        if (nickname.isNotEmpty && nickname != userId) return nickname;
      }

      final nickname = fs.getUserNickname(userId)?.trim() ?? '';
      if (nickname.isNotEmpty && nickname != userId) return nickname;

      final username = fs.getUserUsername(userId)?.trim() ?? '';
      if (username.isNotEmpty && username != userId) return username;
    }

    final agentName = _resolveAgentNameById(userId);
    if (agentName.isNotEmpty && agentName != userId) return agentName;

    return null;
  }

  Iterable<String> _resolveMentionAliasesForPreview(String rawUserId) {
    final userId = rawUserId.trim();
    if (userId.isEmpty) return const <String>[];
    final aliases = <String>{};
    void add(String? value) {
      final trimmed = value?.trim() ?? '';
      if (trimmed.isEmpty || trimmed == userId) return;
      aliases.add(trimmed);
    }

    final fs = _friendService;
    if (fs != null) {
      add(fs.getFriendRemarkName(userId));
      add(fs.getUserNickname(userId));
      add(fs.getUserUsername(userId));
    }

    final myId = _authService?.userId?.trim() ?? '';
    if (myId.isNotEmpty && myId == userId) {
      final me = _authService?.user;
      add(me?.nickname);
      add(me?.username);
    }

    add(_resolveAgentNameById(userId));
    return aliases;
  }

  String _resolveAgentNameById(String userId) {
    final agentService = _agentService;
    if (agentService == null) return '';
    for (final agent in agentService.agents) {
      if (agent.id == userId) {
        return agent.agentName.trim();
      }
    }
    return '';
  }

  void _ensurePrivatePeerIdentity(SessionModel session) {
    _identity.ensurePrivatePeerIdentity(session);
  }

  int _parseInt(dynamic v) {
    if (v is int) return v;
    if (v is num) return v.toInt();
    return int.tryParse(v?.toString() ?? '') ?? 0;
  }

  static const String visitorGroupKey = 'visitor:group';

  String _buildConversationGroupKey(SessionModel session) {
    final sid = session.sessionId;
    final cached = _conversationGroupKeyCache[sid];
    if (cached != null &&
        cached.isVisitor == session.isVisitor &&
        cached.type == session.type &&
        cached.peerType == session.peerType &&
        cached.peerId == session.peerId) {
      return cached.groupKey;
    }
    final groupKey = _computeConversationGroupKey(session);
    _conversationGroupKeyCache[sid] = _CachedConversationGroupKey(
      groupKey: groupKey,
      isVisitor: session.isVisitor,
      type: session.type,
      peerType: session.peerType,
      peerId: session.peerId,
    );
    return groupKey;
  }

  String _computeConversationGroupKey(SessionModel session) {
    if (session.isVisitor) return visitorGroupKey;
    final type = session.type.trim().toLowerCase();
    if (type == 'private') {
      final peerId = session.peerId.trim();
      if (peerId.isNotEmpty) {
        return 'private:${session.peerType}:$peerId';
      }
    }
    return 'session:${session.sessionId}';
  }

  /// 从本地 imService.sessions 中按 groupKey 查找同组的所有 session
  List<SessionModel> _resolveLocalSessionsForGroup(String groupKey) {
    return imService.sessions
        .where((s) => _buildConversationGroupKey(s) == groupKey)
        .toList();
  }

  /// 判断点击某个多 thread 会话时是否应直达唯一未读 session。
  /// 返回 null 表示应展示 thread 列表。
  @visibleForTesting
  SessionModel? resolveDirectChatTarget(ConversationListItem item) {
    if (item.threadCount <= 1) {
      return item.latestSession;
    }
    final allSessions = item.sessions.length >= item.threadCount
        ? item.sessions
        : _resolveLocalSessionsForGroup(item.groupKey);
    if (allSessions.length >= item.threadCount) {
      final unreadSessions = allSessions
          .where((s) => s.unreadCount > 0)
          .toList();
      if (unreadSessions.length == 1) {
        return unreadSessions.first;
      }
    }
    return null;
  }

  void showSessionMenu(BuildContext context, ConversationListItem item) {
    _actions.showSessionMenu(context, item);
  }
}

class _CachedConversationGroupKey {
  const _CachedConversationGroupKey({
    required this.groupKey,
    required this.isVisitor,
    required this.type,
    required this.peerType,
    required this.peerId,
  });

  final String groupKey;
  final bool isVisitor;
  final String type;
  final int peerType;
  final String peerId;
}

class _GroupAvatarSourceMember {
  const _GroupAvatarSourceMember({
    required this.memberId,
    required this.memberType,
    required this.nickname,
  });

  final String memberId;
  final int memberType;
  final String nickname;
}
