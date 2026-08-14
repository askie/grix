import 'dart:convert';
import 'package:get/get.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:uuid/uuid.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:sentry_flutter/sentry_flutter.dart';

import '../../app/routes/app_routes.dart';
import '../../app/routes/root_route_navigator.dart';
import 'app_badge_service.dart';
import 'push_filter_service.dart';
import 'local_db.dart';
import 'local_db_change_bus.dart';
import 'local_llm_service.dart';
import 'auth_service.dart';
import 'friend_service.dart';
import 'session_service.dart';
import '../models/message_model.dart';
import '../models/agent_toolbar_model.dart';
import '../models/session_activity_model.dart';
import '../models/conversation_summary_model.dart';
import '../models/session_model.dart';
import '../../shared/utils/strict_int_parser.dart';
import '../../shared/utils/chat_message_preview.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/utils/app_region_config.dart';
import '../../shared/utils/app_storage_service.dart';
import '../../shared/utils/device_identity.dart';
import '../../shared/widgets/message_bubble.dart';
import '../../shared/services/in_app_notification_service.dart';
import '../../shared/mcp/app_mcp_server.dart';
import '../../modules/call/call_controller.dart';
import '../../modules/chat/message_cards/models/chat_exec_approval_card_data.dart';
import '../../modules/chat/message_cards/services/chat_message_card_codec.dart';
import '../../modules/chat/services/conversation_audit_preference_service.dart';

part 'im_service_activity.dart';
part 'im_service_connection.dart';
part 'im_service_downstream.dart';
part 'im_service_message_sync.dart';
part 'im_service_message_window.dart';
part 'im_service_outbound.dart';
part 'im_service_agent_state.dart';
part 'im_service_agent_toolbar.dart';
part 'im_service_event_lifecycle.dart';
part 'im_service_runtime.dart';
part 'im_service_mcp_frame.dart';
part 'im_service_stream_preview.dart';
part 'im_service_sessions.dart';
part 'im_service_sync_state.dart';
part 'im_service_call.dart';

enum ImConnectionStage {
  disconnected,
  connecting,
  authenticating,
  connected,
  reconnecting,
  kicked,
  authFailed,
}

enum PeerPresenceState { online, offline, unknown }

class _StreamDiagStats {
  _StreamDiagStats({required this.sessionId, required this.createdAtMs});

  final String sessionId;
  final int createdAtMs;
  int firstChunkAtMs = 0;
  int lastChunkAtMs = 0;
  int chunkCount = 0;
  int totalChars = 0;
  int queueLagSamples = 0;
  int queueLagTotalMs = 0;
  int firstQueueLagMs = 0;
  int maxQueueLagMs = 0;
  int maxInterChunkGapMs = 0;
  int firstUiAtMs = 0;
  int uiUpdateCount = 0;
}

class _MessageCursor {
  const _MessageCursor({required this.createdAt, required this.msgId});

  final int createdAt;
  final String msgId;
}

class _CachedSessionWindowState {
  _CachedSessionWindowState({
    required this.sessionId,
    required this.messages,
    required this.oldestCursor,
    required this.newestCursor,
    required this.hasOlder,
    required this.hasNewer,
    required this.cachedAtMs,
  });

  final String sessionId;
  final List<MessageModel> messages;
  final _MessageCursor? oldestCursor;
  final _MessageCursor? newestCursor;
  final bool hasOlder;
  final bool hasNewer;
  final int cachedAtMs;
}

class _RemoteHistorySyncResult {
  const _RemoteHistorySyncResult({
    required this.hasMore,
    this.requestFailed = false,
  });

  final bool hasMore;
  final bool requestFailed;
}

/// 比较两个雪花消息号字符串的数值大小。
///
/// Web 端（编译为 JS）整数只有 53 位精度，19 位雪花号一旦转 int 尾部会被舍入，
/// 因此已读游标全程以字符串保存、用本函数做数值比较（不能用字符串字典序，
/// "10" < "9" 会错）。空串 / 非法值视为 0（最小）。返回 <0 / 0 / >0。
int _compareMsgId(String a, String b) {
  final ba = BigInt.tryParse(a.trim()) ?? BigInt.zero;
  final bb = BigInt.tryParse(b.trim()) ?? BigInt.zero;
  return ba.compareTo(bb);
}

/// 消息号字符串是否为有效正值（非空、可解析、> 0）。
bool _isValidMsgId(String s) {
  final v = BigInt.tryParse(s.trim());
  return v != null && v > BigInt.zero;
}

class _PendingReadState {
  const _PendingReadState({
    required this.lastReadMsgId,
    this.retryCount = 0,
    this.nextSendMs = 0,
    this.lastSentMsgId = '',
  });

  // lastReadMsgId / lastSentMsgId 是雪花消息号，用字符串保存以避免 Web 端 int
  // 精度丢失；retryCount 是次数、nextSendMs 是毫秒时间戳（均小于 2^53，用 int 安全）。
  final String lastReadMsgId;
  final int retryCount;
  final int nextSendMs;
  final String lastSentMsgId;

  _PendingReadState copyWith({
    String? lastReadMsgId,
    int? retryCount,
    int? nextSendMs,
    String? lastSentMsgId,
  }) {
    return _PendingReadState(
      lastReadMsgId: lastReadMsgId ?? this.lastReadMsgId,
      retryCount: retryCount ?? this.retryCount,
      nextSendMs: nextSendMs ?? this.nextSendMs,
      lastSentMsgId: lastSentMsgId ?? this.lastSentMsgId,
    );
  }
}

class EventLifecycleQueueItem {
  const EventLifecycleQueueItem({
    required this.eventId,
    required this.sessionId,
    required this.messageId,
    required this.clientMsgId,
    required this.contentPreview,
    required this.state,
    required this.queuePosition,
    required this.actions,
    required this.updatedAt,
    this.content = '',
    this.held = false,
    this.heldReason = '',
  });

  final String eventId;
  final String sessionId;
  final String messageId;
  final String clientMsgId;
  final String contentPreview;
  final String state;
  final int queuePosition;
  final List<String> actions;
  final int updatedAt;

  /// 任务全文（connector 快照新增字段）；老服务端缺失时为空串，
  /// 展示/编辑请用 [fullContent] 回退到 contentPreview。
  final String content;

  /// 是否被 hold（暂停）。
  final bool held;

  /// hold 原因：manual（用户手动暂停）/ editing（编辑流程自动 hold）。
  final String heldReason;

  bool get canCancel => actions.contains('cancel') || actions.contains('stop');

  /// 编辑回填用全文：content 缺失（老服务端）时回退 contentPreview。
  String get fullContent => content.isNotEmpty ? content : contentPreview;

  EventLifecycleQueueItem copyWith({
    String? eventId,
    String? sessionId,
    String? messageId,
    String? clientMsgId,
    String? contentPreview,
    String? state,
    int? queuePosition,
    List<String>? actions,
    int? updatedAt,
    String? content,
    bool? held,
    String? heldReason,
  }) {
    return EventLifecycleQueueItem(
      eventId: eventId ?? this.eventId,
      sessionId: sessionId ?? this.sessionId,
      messageId: messageId ?? this.messageId,
      clientMsgId: clientMsgId ?? this.clientMsgId,
      contentPreview: contentPreview ?? this.contentPreview,
      state: state ?? this.state,
      queuePosition: queuePosition ?? this.queuePosition,
      actions: actions ?? this.actions,
      updatedAt: updatedAt ?? this.updatedAt,
      content: content ?? this.content,
      held: held ?? this.held,
      heldReason: heldReason ?? this.heldReason,
    );
  }
}

/// event_hold / queue_edit 的回执结果（含 5 秒超时语义：
/// 老 backend/connector 不认识新命令时不会有任何回执，按 timedOut 收口）。
class EventLifecycleCmdResult {
  const EventLifecycleCmdResult({
    required this.ok,
    this.held = false,
    this.error = '',
    this.timedOut = false,
  });

  final bool ok;
  final bool held;
  final String error;
  final bool timedOut;
}

/// 工具栏消息队列的展示排序（倒序）：最新的排队消息在最上，正在执行的
/// running 在最下。
///
/// 数据语义：connector 推送 queued 态时带 `queue_position`（从 1 开始，越大越
/// 新入队）；running 态不带 position（解析为 0）。因此按 position 从大到小排列
/// 即可让最新入队的消息置顶，position 为 0 的 running 自然沉到队尾。
/// 把展示序（倒序：最新排队在上）的排队项还原成队列真实顺序（队头在前），
/// 供拖动排序后组装 queue_reorder 的 ordered_event_ids。
List<String> queueOrderFromDisplay(
  List<EventLifecycleQueueItem> displayQueued,
) {
  return displayQueued.reversed.map((e) => e.eventId).toList(growable: false);
}

/// 由 ReorderableListView 的 onReorder 回调计算拖动后的队列真实顺序
/// （队头在前）。displayItems 为完整展示列表（排队段在前、running 沉底），
/// oldIndex/newIndex 为 Flutter 原始回调值（newIndex 未做删除位修正）。
/// 拖动无效（拖的是 running 项 / 位置未变）时返回 null。
List<String>? computeReorderedQueueIds({
  required List<EventLifecycleQueueItem> displayItems,
  required int oldIndex,
  required int newIndex,
}) {
  final queuedCount = displayItems.where((e) => e.queuePosition > 0).length;
  if (oldIndex < 0 || oldIndex >= queuedCount) {
    return null;
  }
  var target = newIndex;
  if (target > oldIndex) {
    target -= 1;
  }
  if (target >= queuedCount) {
    target = queuedCount - 1;
  }
  if (target < 0) {
    target = 0;
  }
  if (target == oldIndex) {
    return null;
  }
  final display = displayItems.where((e) => e.queuePosition > 0).toList();
  final moved = display.removeAt(oldIndex);
  display.insert(target, moved);
  return queueOrderFromDisplay(display);
}

List<EventLifecycleQueueItem> orderQueueItemsForDisplay(
  List<EventLifecycleQueueItem> items,
) {
  final sorted = List<EventLifecycleQueueItem>.from(items);
  sorted.sort((a, b) {
    if (a.queuePosition != b.queuePosition) {
      return b.queuePosition.compareTo(a.queuePosition);
    }
    if (a.updatedAt != b.updatedAt) {
      return b.updatedAt.compareTo(a.updatedAt);
    }
    return b.eventId.compareTo(a.eventId);
  });
  return sorted;
}

class _LocalUnreadOverride {
  const _LocalUnreadOverride({
    required this.unreadCount,
    required this.setAtMs,
  });

  final int unreadCount;
  final int setAtMs;
}

class _LocalPinOverride {
  const _LocalPinOverride({
    required this.isPinned,
    required this.isFriendPin,
    required this.pinnedAt,
    required this.setAtMs,
  });

  /// Whether the session or friend is pinned.
  final bool isPinned;

  /// True = friend-level pin (friend_is_pinned), False = session-level pin (is_pinned).
  final bool isFriendPin;

  /// The pinned_at timestamp (0 when unpinned).
  final int pinnedAt;

  /// When this override was registered (ms since epoch).
  final int setAtMs;
}

class _PinOverrideDecision {
  const _PinOverrideDecision({
    required this.keepLocalOverride,
    required this.effectiveIsPinned,
  });

  final bool keepLocalOverride;
  final bool effectiveIsPinned;
}

class ImService extends GetxService {
  // Web 端按页面域名解析所属区域 WS（全球区接口域名 gb.grix.im 下需跨域连
  // ws.grix.im，不能简单同源）；App 端回退编译注入的默认 WS。
  static String get defaultWsUrl => resolveDefaultWsUrl();
  static const int defaultDelegateMaxConsecutiveReplies = 10;
  static const bool _streamDiagFlag = bool.fromEnvironment(
    'STREAM_DIAG',
    defaultValue: false,
  );
  static const Duration _dbOpTimeout = Duration(seconds: 6);
  static const int _initialRenderCacheHydrationLimit = 30;
  static const String _openClawElevatedUnavailableMarker =
      'elevated is not available right now';
  static const String _openClawToolGateMarker = 'tools.elevated';
  static const String _openClawFixKeysMarker = 'fix-it keys';
  Worker? _sessionsBadgeWorker;
  Worker? _currentSessionBadgeWorker;

  ImService() {
    _sessionsBadgeWorker = ever<List<SessionModel>>(sessions, (_) {
      _syncSystemUnreadBadge();
    });
    _currentSessionBadgeWorker = ever<String?>(_currentSessionId, (_) {
      _syncSystemUnreadBadge();
    });
  }

  final _isConnected = false.obs;
  final _isAuthenticated = false.obs;
  final _connectionStage = ImConnectionStage.disconnected.obs;
  final _connectionBannerVisibilityTick = 0.obs;
  bool get isConnected => _isConnected.value;
  bool get isAuthenticated => _isAuthenticated.value;
  bool get isSuspendedForAppBackground => _isSuspendedForAppBackground;
  ImConnectionStage get connectionStage => _connectionStage.value;
  Rx<ImConnectionStage> get connectionStageRx => _connectionStage;
  static const Duration _initialConnectionBannerDelay = Duration(seconds: 6);
  static const Duration _connectionLossBannerDelay = Duration(seconds: 5);

  /// 连接横幅延迟判定所用的时钟（毫秒）。默认真实时间；测试可注入可控时钟，
  /// 确定性推进时间来覆盖 6 秒/5 秒边界，避免真实 sleep 在边界上偶发抖动。
  @visibleForTesting
  static int Function() nowMsProvider = () =>
      DateTime.now().millisecondsSinceEpoch;
  static const Duration _transientConnectionBannerSuppressDuration = Duration(
    seconds: 5,
  );
  Timer? _initialConnectionBannerDelayTimer;
  int _initialConnectionBannerReadyAtMs = 0;
  Timer? _connectionLossBannerDelayTimer;
  int _connectionLossBannerReadyAtMs = 0;
  Timer? _connectionBannerSuppressionTimer;
  int _connectionBannerSuppressedUntilMs = 0;
  bool _hasEstablishedRealtimeSession = false;
  bool _hasPendingInitialConnection = false;

  bool get shouldShowConnectionBanner =>
      _ImServiceRuntime(this).shouldShowConnectionBanner;

  String get connectionBannerTextKey =>
      _ImServiceRuntime(this).connectionBannerTextKey;

  void suppressConnectionBannerTemporarily({
    Duration duration = _transientConnectionBannerSuppressDuration,
  }) {
    _ImServiceRuntime(
      this,
    ).suppressConnectionBannerTemporarily(duration: duration);
  }

  WebSocketChannel? _channel;
  StreamSubscription? _wsSubscription;
  Timer? _heartbeatTimer;
  Timer? _reconnectTimer;
  Timer? _authHandshakeTimer;
  Timer? _connectWatchdogTimer;
  String? _wsUrl;
  bool _allowReconnect = true;
  bool _isConnecting = false;
  int _connectEpoch = 0;
  int _reconnectAttempts = 0;
  int _lastPongAtMs = 0;

  /// auth_ack / re_auth_ack 的可重试码：服务端自己暂时不可用（存储层故障等），
  /// 凭证没有问题。收到它要保留会话继续重连，绝不能清会话回登录页——服务端会用
  /// 它把"我坏了"和"你的凭证失效了"分开，客户端不必再去猜文案。
  /// 与后端 `ws/protocol.AuthCodeRetryable` 对应。
  static const int authCodeRetryable = 10003;
  static const Duration _baseReconnectDelay = Duration(seconds: 1);
  static const Duration _maxReconnectDelay = Duration(seconds: 30);
  static const Duration _connectReadyTimeout = Duration(seconds: 10);

  /// 单次建连尝试的兜底看门狗：正常路径在 ready 超时（10 秒）内必然收敛，
  /// 它只兜"底层 connect 或清理动作悬死导致 _isConnecting 永不复位"的场景
  /// （桌面端休眠唤醒后出现过：自动重连与手动重试全部被守卫静默吞掉）。
  @visibleForTesting
  static Duration connectAttemptHardTimeout = const Duration(seconds: 20);

  /// 建连通道工厂，默认走真实 WebSocket；测试注入可控通道来复现
  /// "ready 悬死 / 清理 cancel 悬死或抛错"等唤醒场景。
  @visibleForTesting
  static WebSocketChannel Function(Uri uri)? channelConnectorForTest;

  @visibleForTesting
  static bool Function(String op)? failDbWriteOpForTest;

  static const Duration _authHandshakeTimeout = Duration(seconds: 12);
  static const Duration _heartbeatInterval = Duration(seconds: 30);
  static const Duration _pongTimeout = Duration(seconds: 90);
  static const Duration _realtimeAuthFastPathMinRemaining = Duration(
    seconds: 30,
  );
  static const String _friendEventSeqKeyPrefix = 'friend_event_seq_';
  static const String _pendingReadStatesKeyPrefix = 'pending_read_states_';
  static const String _inboxSeqCursorKeyPrefix = 'inbox_seq_cursor_';
  static const String _bootstrapInboxSeqFloorKeyPrefix =
      'bootstrap_inbox_seq_floor_';
  static const String _deletedSessionsKeyPrefix = 'deleted_sessions_';
  static const String _revokedSessionsKeyPrefix = 'revoked_sessions_';
  static const String _sessionSyncCursorKeyPrefix = 'session_sync_cursor_';
  int _lastFriendEventSeq = 0;
  int _lastInboxSeqCursor = 0;
  int _bootstrapInboxSeqFloor = 0;
  int _lastSessionSyncCursor = 0;
  bool _friendEventSeqLoaded = false;
  bool _inboxSeqCursorLoaded = false;
  bool _sessionSyncCursorLoaded = false;
  bool _sessionSyncIncrementalInFlight = false;
  bool _bootstrapInboxSeqFloorLoaded = false;
  bool _pendingReadStatesLoaded = false;
  bool _deletedSessionsLoaded = false;
  bool _revokedSessionsLoaded = false;
  bool _prefsUnavailableLogged = false;
  bool _friendSyncInFlight = false;
  bool _pendingResendInFlight = false;
  int _lastPullSyncRequestMs = 0;
  int _pendingPullSyncCursorFloor = 0;
  int _lastPullSyncDrainSessionsRefreshMs = 0;

  /// Persist-fail pull retries use a dedicated backoff so cursor correctness
  /// retries do not storm at the normal 2s throttle cadence.
  int _persistFailPullSyncStreak = 0;
  int _lastPersistFailPullSyncScheduleMs = 0;
  bool _pendingPersistFailPullSync = false;
  static const List<int> _persistFailPullSyncBackoffMs = <int>[
    2000,
    5000,
    15000,
    30000,
  ];
  final _pendingReadStatesBySession = <String, _PendingReadState>{};
  final _localUnreadOverrides = <String, _LocalUnreadOverride>{};
  final _localPinOverrides = <String, _LocalPinOverride>{};

  /// Register a friend-level pin override for the given sessions.
  /// This protects locally-set friend pin state from being overwritten
  /// by stale server snapshots until the server catches up.
  void registerFriendPinOverrides(
    List<String> sessionIds, {
    required bool isPinned,
    required int pinnedAt,
  }) {
    final now = DateTime.now().millisecondsSinceEpoch;
    for (final sid in sessionIds) {
      final s = sid.trim();
      if (s.isEmpty) continue;
      _localPinOverrides[s] = _LocalPinOverride(
        isPinned: isPinned,
        isFriendPin: true,
        pinnedAt: pinnedAt,
        setAtMs: now,
      );
    }
  }

  final _locallyDeletedSessions = <String, int>{};
  final _locallyRevokedSessions = <String, int>{};
  final _sessionHistoryResetInFlightAtMs = <String, int>{};
  final _sessionHistoryResetInFlightDeletedAtMs = <String, int>{};
  int _lastSessionHistoryResetSeq = 0;
  final _downstreamLagLastLogAtMsByCmd = <String, int>{};
  final _downstreamLagSuppressedByCmd = <String, int>{};
  Timer? _pendingReadRetryTimer;
  Timer? _pullSyncThrottleTimer;
  Timer? _sessionHistoryResetRetryTimer;
  Timer? _editPreviewFlushTimer;
  Completer<void>? _editPreviewFlushCompleter;
  final _pendingEditPreviewBySessionId = <String, Map<String, dynamic>>{};
  final _sendAckTimers = <String, Timer>{};
  int _consecutiveSendAckTimeouts = 0;
  bool _isSuspendedForAppBackground = false;
  String _realtimeAppState = 'foreground';
  static const Duration _sendAckTimeout = Duration(seconds: 15);
  static const Duration _sendAckRecoveryTimeout = Duration(seconds: 5);
  static const int _sendAckTimeoutReconnectThreshold = 2;
  static const Duration _localStartAckTimeout = Duration(seconds: 3);
  static const int _localStartAckMaxAttempts = 3;
  static const int _pendingReadBaseRetryMs = 1000;
  static const int _pendingReadMaxRetryMs = 30000;
  static const int _pullSyncThrottleWindowMs = 2000;
  static const int _sessionHistoryResetRetryMs = 30000;
  static const int _downstreamLagLogIntervalMs = 1000;
  static int? sessionHistoryResetRetryMsForTest;

  // Feature flag: when true, current session window updates are driven by
  // LocalDbChangeBus events instead of direct UI writes in downstream handlers.
  static const bool _dbChangeEventDrivenWindow = true;

  StreamSubscription<LocalMessageChange>? _dbChangeSubscription;
  Future<void> _downstreamQueue = Future.value();
  Future<void> _streamDownstreamQueue = Future.value();
  static const Set<String> _streamDownstreamCommands = {
    'stream_chunk',
    'stream_finish',
    'stream_error',
  };
  final _sessionTypeHints = <String, String>{};
  final _inflightSessionAccessProbe = <String>{};
  // 缺 peer 信息的私聊会话补拉详情回填：已尝试集合 + 单飞标记 + 单批上限。
  final _peerIdentityBackfillAttempted = <String>{};
  bool _peerIdentityBackfillInFlight = false;
  static const int _peerIdentityBackfillBatchLimit = 6;
  final _streamDiagByMsgId = <String, _StreamDiagStats>{};
  final _localLlm = LocalLlmService();
  final _localInferenceInFlight =
      <String>{}; // sessionIds with active local inference
  final _pendingLocalInferenceStarts = <String, _PendingLocalInferenceStart>{};
  final _pendingLocalInferenceStartTimers =
      <String, Timer>{}; // requestKey -> start ack retry timer
  final _localStreamRenderMsgIds =
      <String, String>{}; // sessionId -> current UI/render msgId
  final _localStreamServerMsgIds =
      <String, String>{}; // sessionId -> server msgId
  final _localStreamQuotedMessageIds =
      <String, String>{}; // sessionId -> quoted message id
  final _localStreamChunkSeqs =
      <String, int>{}; // sessionId -> chunkSeq counter
  Timer? _localStreamThrottleTimer;
  final _localStreamPendingDeltas =
      <String, StringBuffer>{}; // sessionId -> merged delta
  Timer? _sessionActivityCleanupTimer;
  Timer? _composingRenewTimer;
  Timer? _composingIdleTimer;
  Timer? _sessionViewingRenewTimer;
  Timer? _initialSessionLoadTimer;
  Timer? _initialLoadRetryTimer;
  int _initialLoadRetryCount = 0;
  static const int _maxInitialLoadRetries = 3;
  static const Duration _initialLoadRetryDelay = Duration(seconds: 2);
  String _composingSessionId = '';
  String _viewingSessionId = '';
  bool _composingActive = false;
  bool _viewingActive = false;
  static const Duration _composingRenewInterval = Duration(seconds: 2);
  // 输入框长时间无文本变化时自动结束 composing 的空闲超时：
  // 续期循环只由服务端 TTL 续命，人离开电脑后会对端会一直显示"正在输入"，
  // 因此需要在本地兜底超时（每次文本变化都会重新计时）。
  static const Duration _composingIdleTimeout = Duration(seconds: 60);
  static const Duration _sessionViewingRenewInterval = Duration(seconds: 4);
  static const Duration _streamGapRecoveryDelay = Duration(seconds: 2);
  static const int _streamGapRecoveryMaxAttempts = 3;
  // agent 流式输出的"无活动"看门狗阈值：正常流式期间 chunk 持续到达并不断
  // 刷新活动时间；一旦 agent 进程重启/崩溃或网络闪断导致终态包
  // （stream_finish / event_result）丢失，msgId 会永久残留在
  // _activeStreamingMsgIds 里，顶住 agentOutputStates 的 90 秒 stale 清理，
  // 聊天页"正在输入"胶囊永不消失。取 5 分钟：远大于正常 chunk 间隔（秒级）
  // 与断线重连恢复窗口，不会被短暂卡顿误伤；又足够短，僵尸流不会长期占住
  // 胶囊。
  static const Duration _streamingIdleTimeout = Duration(minutes: 5);
  // 看门狗周期清扫间隔：无需随 chunk 高频检查，60s 一次即可把残留额外延迟
  // 控制在阈值 + 一个间隔以内。
  static const Duration _streamingWatchdogInterval = Duration(seconds: 60);

  // 当前会话的消息流 (固定窗口 + 游标分页，避免长会话常驻过多消息)
  final currentMessages = <MessageModel>[].obs;
  final _currentMessageIds = <String>{};
  final _currentClientMessageIds = <String>{};
  final _activeStreamingMsgIds = <String>{};
  // 每个 streaming msgId 最近一次活动时间（epoch 毫秒）：加入集合与每个
  // chunk 到达时都会刷新；流式看门狗据此判定僵尸流。
  final _streamingActivityAtByMsgId = <String, int>{};
  Timer? _streamingWatchdogTimer;
  final _streamExpectedChunkSeqByMsg = <String, int>{};
  final _streamPendingChunkSeqByMsg = <String, Set<int>>{};
  final _streamGapRecoveryAttemptsByMsg = <String, int>{};
  final _streamGapRecoveryTimersByMsg = <String, Timer>{};
  final _locallyStoppedStreamMsgIds = <String>{};
  final _streamingPlaceholders = <String, MessageModel>{};
  final _streamingSessionPreviewTexts = <String, String>{}.obs;
  final _streamingSessionPreviewTick = 0.obs;
  final _streamingSessionPreviewUpdatedAt = <String, int>{};
  final _streamingSessionPreviewOwnerBySession = <String, String>{};
  final _streamingSessionPreviewStates =
      <String, _StreamingSessionPreviewState>{};
  final _hiddenAgentOutputMessages = <String, MessageModel>{};
  final _pendingLocalStopStateBySession = <String, Map<String, dynamic>>{};
  final _pendingLocalStopStreamMsgIdBySession = <String, String>{};
  final _resolvedSessionComposingAtByParticipant = <String, int>{};
  final sessionActivities = <String, List<SessionActivityModel>>{}.obs;
  final agentOutputStates = <String, Map<String, dynamic>>{}.obs;

  /// Pending backfill futures/keys for deduplication.
  Future<void>? _pendingInitialWindowBackfill;
  final _pendingOlderBackfillKeys = <String>{};

  /// Grace-period for agent delivery timeout: when the backend declares a
  /// timeout but the agent is still running, we defer showing the error for
  /// this duration. If streaming starts within the window the timeout is
  /// silently discarded.
  static const int _deliveryTimeoutGracePeriodMs = 20 * 1000;
  static const int _agentDeliveryOrderCacheLimit = 4096;
  final _pendingDeliveryTimeoutTimers = <String, _DeliveryTimeoutGracePeriod>{};
  final _agentDeliveryOrderByMsgId = <String, Map<String, int>>{};

  final agentToolbars = <String, AgentToolbarModel>{}.obs;
  final _agentToolbarLoadingItemBySession = <String, String>{}.obs;
  final _agentToolbarPendingActionBySession = <String, String>{}.obs;
  final _agentToolbarPendingSelectBySession =
      <String, _PendingAgentToolbarSelect>{}.obs;
  final _agentToolbarTargetAgentIdBySession = <String, String>{}.obs;
  final eventLifecycleQueues = <String, List<EventLifecycleQueueItem>>{}.obs;

  /// event_hold / queue_edit 回执等待表（key: 'sessionId|eventId'）。
  /// 回执不带请求 seq，按会话+事件关联；5s 超时收口见
  /// im_service_event_lifecycle.dart 的 _awaitLifecycleCmdResult。
  final _eventHoldPending = <String, Completer<EventLifecycleCmdResult>>{};
  final _queueEditPending = <String, Completer<EventLifecycleCmdResult>>{};
  Future<void>? _sessionsAuthoritativeRefreshFuture;
  bool _sessionsAuthoritativeRefreshIsFull = false;
  int _lastAuthoritativeSessionRefreshAtMs = 0;
  int _lastSessionWindowRefreshAttemptAtMs = 0;
  bool _sessionWindowPaginationHasMore = false;
  int _sessionWindowPaginationNextOffset = 0;
  bool _sessionWindowPaginationInFlight = false;
  int _sessionWindowPaginationNextAllowedAtMs = 0;
  int _sessionWindowSyncInflight = 0;
  int _sessionWindowSyncInflightSinceMs = 0;
  int _sessionWindowSyncPriorityUntilMs = 0;
  static const int _sessionWindowSyncInflightMaxBlockMs = 8000;

  // 会话列表
  final sessions = <SessionModel>[].obs;
  final _visitorSessionIds = <String>{};

  // Incremented after loadSessions completes; listeners can use this to bypass
  // debounce and rebuild immediately for authoritative session data.
  final sessionsLoadTick = 0.obs;

  // 当前正在查看的 session_id
  final _currentSessionId = RxnString(null);
  String? get currentSessionId => _currentSessionId.value;
  RxnString get currentSessionIdRx => _currentSessionId;
  bool get isSessionWindowSyncInflight {
    if (_sessionWindowSyncInflight > 0) {
      final nowMs = DateTime.now().millisecondsSinceEpoch;
      final inflightSinceMs = _sessionWindowSyncInflightSinceMs;
      if (inflightSinceMs > 0 &&
          nowMs - inflightSinceMs > _sessionWindowSyncInflightMaxBlockMs) {
        _sessionWindowSyncInflight = 0;
        _sessionWindowSyncInflightSinceMs = 0;
      } else {
        return true;
      }
    }
    if (_sessionWindowSyncInflight > 0) {
      return true;
    }
    return DateTime.now().millisecondsSinceEpoch <
        _sessionWindowSyncPriorityUntilMs;
  }

  bool isCurrentSession(String sessionId) =>
      _ImServiceRuntime(this).isCurrentSession(sessionId);
  bool get hasOlderMessages => _hasOlderMessages;
  bool get hasNewerMessages => _hasNewerMessages;
  bool isMessageStreaming(String msgId) =>
      msgId.isNotEmpty && _activeStreamingMsgIds.contains(msgId);

  /// 将 msgId 标记为活跃流式消息并刷新其最近活动时间。
  /// 所有往 [_activeStreamingMsgIds] 的 add 点与流式 chunk 到达点都必须走
  /// 这里，保证看门狗的活动时间记录不遗漏。
  void _markStreamingActivity(String msgId) {
    final normalizedMsgId = msgId.trim();
    if (normalizedMsgId.isEmpty) {
      return;
    }
    _activeStreamingMsgIds.add(normalizedMsgId);
    _streamingActivityAtByMsgId[normalizedMsgId] =
        DateTime.now().millisecondsSinceEpoch;
    _ensureStreamingWatchdogTimer();
  }

  void _ensureStreamingWatchdogTimer() {
    if (_streamingWatchdogTimer?.isActive ?? false) {
      return;
    }
    _streamingWatchdogTimer = Timer.periodic(
      ImService.streamingWatchdogIntervalForTest ?? _streamingWatchdogInterval,
      (_) => _sweepStaleStreamingMessages(),
    );
  }

  /// 僵尸流看门狗：某个 streaming msgId 超过 [_streamingIdleTimeout] 没有任何
  /// chunk 活动，说明终态包（stream_finish / event_result）大概率已丢失
  /// （agent 重启/崩溃/网络闪断）。只摘除"正在流式"标记及关联的
  /// placeholder / preview / gap 跟踪状态，保留 currentMessages 里已有的
  /// 消息气泡（内容保留，只是不再视为"正在流式"）。
  void _sweepStaleStreamingMessages() {
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final idleTimeoutMs =
        (ImService.streamingIdleTimeoutForTest ?? _streamingIdleTimeout)
            .inMilliseconds;
    var sweptAny = false;
    for (final msgId in _streamingActivityAtByMsgId.keys.toList()) {
      final lastActivityAt = _streamingActivityAtByMsgId[msgId] ?? 0;
      if (!_activeStreamingMsgIds.contains(msgId)) {
        // 流已正常结束，活动时间是孤儿记录，顺手清掉。
        _streamingActivityAtByMsgId.remove(msgId);
        continue;
      }
      if (nowMs - lastActivityAt < idleTimeoutMs) {
        continue;
      }
      _activeStreamingMsgIds.remove(msgId);
      _streamingActivityAtByMsgId.remove(msgId);
      _streamingPlaceholders.remove(msgId);
      _locallyStoppedStreamMsgIds.remove(msgId);
      _discardStreamingSessionPreview(msgId);
      _clearStreamChunkGapTrackingForMessage(msgId);
      debugPrint(
        '🧹 swept zombie streaming message msg_id=$msgId '
        'last_activity_at=$lastActivityAt now=$nowMs',
      );
      sweptAny = true;
    }
    if (sweptAny) {
      // 僵尸流会一直顶住 agentOutputStates 的 90 秒 stale 清理
      // （hasStreamingAgentOutputForSession 为 true），摘掉标记后补跑一次。
      _pruneStaleAgentOutputStates();
    }
    if (_activeStreamingMsgIds.isEmpty && _streamingActivityAtByMsgId.isEmpty) {
      _streamingWatchdogTimer?.cancel();
      _streamingWatchdogTimer = null;
    }
  }

  String streamingSessionPreviewForSession(String sessionId) =>
      _streamingSessionPreviewForSession(sessionId);
  int streamingSessionPreviewUpdatedAtForSession(String sessionId) =>
      _streamingSessionPreviewUpdatedAtForSession(sessionId);
  RxInt get streamingSessionPreviewTickRx => _streamingSessionPreviewTick;
  bool get hasStreamingSessionPreviews {
    _streamingSessionPreviewTick.value;
    return _streamingSessionPreviewTexts.isNotEmpty;
  }

  bool hasStreamingAgentOutputForSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    bool matches(MessageModel msg) {
      final msgId = msg.msgId.trim();
      return msg.sessionId == sid &&
          msg.senderType == 2 &&
          msg.msgType == 4 &&
          msgId.isNotEmpty &&
          _activeStreamingMsgIds.contains(msgId);
    }

    if (currentMessages.any(matches)) {
      return true;
    }
    return _streamingPlaceholders.values.any(matches);
  }

  String visibleStreamingAgentIdForSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return '';

    bool matches(MessageModel msg) {
      final msgId = msg.msgId.trim();
      return msg.sessionId == sid &&
          msg.senderType == 2 &&
          msg.msgType == 4 &&
          msgId.isNotEmpty &&
          _activeStreamingMsgIds.contains(msgId);
    }

    final candidates = <String, MessageModel>{};
    for (final msg in currentMessages) {
      if (!matches(msg)) {
        continue;
      }
      candidates[msg.msgId] = msg;
    }
    for (final msg in _streamingPlaceholders.values) {
      if (!matches(msg)) {
        continue;
      }
      candidates.putIfAbsent(msg.msgId, () => msg);
    }
    if (candidates.isEmpty) {
      return '';
    }

    final ordered = candidates.values.toList(growable: false)
      ..sort(_compareMessageOrder);
    return ordered.last.senderId.trim();
  }

  Map<String, dynamic>? agentOutputStateFor(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return null;
    final state = agentOutputStates[sid];
    if (state == null) return null;
    return Map<String, dynamic>.from(state);
  }

  List<SessionActivityModel> sessionActivitiesFor(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return const <SessionActivityModel>[];
    final items = sessionActivities[sid] ?? const <SessionActivityModel>[];
    final myUserId = Get.isRegistered<AuthService>()
        ? (Get.find<AuthService>().userId?.trim() ?? '')
        : '';
    return items
        .where((item) {
          if (!item.active || item.isExpired) return false;
          if ((item.kind).trim() != 'composing') return false;
          if (item.actorType == 'human' &&
              myUserId.isNotEmpty &&
              item.actorId == myUserId) {
            return false;
          }
          return true;
        })
        .toList(growable: false);
  }

  /// 会话级实时活动指示：agent 正在输出，或有人正在 composing。
  /// 与聊天页状态指示器使用同一组事件源（agentOutputStates / sessionActivities），
  /// 保证列表页绿点与聊天页提示由相同事件驱动、保持一致。
  bool hasSessionLiveActivity(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    if (agentOutputStateFor(sid) != null) return true;
    return sessionActivitiesFor(sid).isNotEmpty;
  }

  void updateSessionComposing(String sessionId, {required bool active}) {
    _updateSessionComposingImpl(sessionId, active: active);
  }

  void enterSession(
    String sessionId, {
    Duration initialLoadDelay = Duration.zero,
  }) {
    _enterSessionImpl(sessionId, initialLoadDelay: initialLoadDelay);
  }

  Future<void> loadOlderForCurrentSession() {
    return _loadOlderForCurrentSessionImpl();
  }

  Future<void> loadNewerForCurrentSession() {
    return _loadNewerForCurrentSessionImpl();
  }

  bool hasMessageInCurrentWindow(String msgId) {
    return _hasMessageInCurrentWindow(msgId.trim());
  }

  Future<void> forceReloadSessionWindow(
    String sessionId, {
    bool triggerPullSync = true,
  }) {
    return _forceReloadSessionWindowImpl(
      sessionId,
      triggerPullSync: triggerPullSync,
    );
  }

  void leaveSession([String? explicitSessionId]) {
    _leaveSessionImpl(explicitSessionId);
  }

  void connect(String wsUrl) {
    _connectImpl(wsUrl);
  }

  /// 确保 WS 已连接。
  /// Web 端始终按当前页面域名解析所属区域 WS（不读 _wsUrl，避免冷启动时
  /// _wsUrl 未初始化而 no-op，也避免服务器返回值污染导致跨域）。
  /// App 端用 _wsUrl（由 init 预载 / applyAuthPayload 写入），绝不回落
  /// 编译时默认值，防止全球区用户连到国区端点。
  void ensureConnected() {
    if (kIsWeb) {
      _connectImpl(resolveDefaultWsUrl());
      return;
    }
    final url = _wsUrl;
    if (url == null || url.isEmpty) return;
    _connectImpl(url);
  }

  /// 更新目标 WS 端点（不立即发起连接）。App 端专用：由 applyAuthPayload 在
  /// 登录/注册响应中写入，后续 ensureConnected() 使用该端点。
  /// Web 端忽略：连接一律按页面域名解析，不依赖此字段。
  void updateWsEndpoint(String url) {
    if (kIsWeb || url.isEmpty) return;
    _wsUrl = url;
  }

  void reAuthWithLatestToken() {
    _reAuthWithLatestTokenImpl();
  }

  void setRealtimeAppState(String appState) {
    _setRealtimeAppStateImpl(appState);
  }

  /// 恢复前台后主动拉一次 pull_sync 对账。若 WS 在后台期间保持连接，恢复前台
  /// 不会经过重连→auth_ack→pull_sync 链路，被 defer 的系统角标同步会一直等不
  /// 到权威刷新，图标角标（可能被离线推送写成陈旧值）整个前台期都不会校正。
  /// 这里补一次 pull_sync，让服务端未读快照落地并触发角标的权威同步。
  /// 未连接/未鉴权时不发：重连成功后 auth_ack 链路本身会触发 pull_sync。
  void reconcileUnreadBadgeOnResume() {
    if (!_isConnected.value || !_isAuthenticated.value) {
      return;
    }
    _triggerPullSyncThrottled();
  }

  void refreshDelegateStates() {
    _refreshDelegateStatesImpl();
  }

  void refreshAgentOnlineStates() {
    _refreshAgentOnlineStatesImpl();
  }

  Future<
    ({
      List<Map<String, dynamic>> files,
      String? currentPath,
      String? machineName,
    })
  >
  requestAgentFileList({
    required String agentId,
    required String sessionId,
    String? parentId,
    bool showHidden = false,
    List<String>? allowedExtensions,
  }) async {
    final completer = Completer<List<Map<String, dynamic>>>();
    final reqAt = DateTime.now().millisecondsSinceEpoch;
    debugPrint(
      '[file-list-diag] front >> request agentId=$agentId session=$sessionId '
      'parent=${parentId ?? "<root>"} showHidden=$showHidden '
      'extCount=${allowedExtensions?.length ?? 0} t=$reqAt',
    );
    final seq = _sendAgentFileListPacket(
      agentId: agentId,
      sessionId: sessionId,
      parentId: parentId,
      showHidden: showHidden,
      allowedExtensions: allowedExtensions,
    );
    if (seq == 0) {
      debugPrint('[file-list-diag] front !! send failed (seq=0)');
      completer.completeError(Exception('发送文件列表请求失败'));
    } else {
      debugPrint(
        '[file-list-diag] front -> sent seq=$seq seqType=${seq.runtimeType}',
      );
      _fileListPending[seq] = completer;
      Future.delayed(const Duration(seconds: 15), () {
        if (_fileListPending.remove(seq) != null && !completer.isCompleted) {
          _fileListCurrentPath.remove(seq);
          _fileListMachineName.remove(seq);
          final waited = DateTime.now().millisecondsSinceEpoch - reqAt;
          debugPrint(
            '[file-list-diag] front !! timeout seq=$seq waited=${waited}ms '
            'pendingKeys=${_fileListPending.keys.toList()}',
          );
          completer.completeError(Exception('文件列表请求超时'));
        }
      });
    }
    try {
      final files = await completer.future;
      final waited = DateTime.now().millisecondsSinceEpoch - reqAt;
      debugPrint(
        '[file-list-diag] front << done seq=$seq waited=${waited}ms count=${files.length}',
      );
      final currentPath = _fileListCurrentPath.remove(seq);
      final machineName = _fileListMachineName.remove(seq);
      return (files: files, currentPath: currentPath, machineName: machineName);
    } catch (e) {
      final waited = DateTime.now().millisecondsSinceEpoch - reqAt;
      debugPrint(
        '[file-list-diag] front << error seq=$seq waited=${waited}ms err=$e',
      );
      rethrow;
    }
  }

  Future<Map<String, dynamic>> requestConversationAuditManifest({
    required String sessionId,
    required String msgId,
    String? agentId,
  }) {
    return _requestConversationAudit(
      command: 'audit_get_manifest',
      sessionId: sessionId,
      msgId: msgId,
      agentId: agentId,
    );
  }

  Future<Map<String, dynamic>> requestConversationAuditSpans({
    required String sessionId,
    required String msgId,
    String? agentId,
    int? revision,
    String? cursor,
    int? limit,
  }) {
    return _requestConversationAudit(
      command: 'audit_list_spans',
      sessionId: sessionId,
      msgId: msgId,
      agentId: agentId,
      revision: revision,
      cursor: cursor,
      limit: limit,
    );
  }

  Future<Map<String, dynamic>> requestConversationAuditContentChunk({
    required String sessionId,
    required String msgId,
    required String contentId,
    String? agentId,
    int? revision,
    String? cursor,
    int? maxBytes,
  }) {
    return _requestConversationAudit(
      command: 'audit_get_content_chunk',
      sessionId: sessionId,
      msgId: msgId,
      agentId: agentId,
      revision: revision,
      cursor: cursor,
      contentId: contentId,
      maxBytes: maxBytes,
    );
  }

  Future<Map<String, dynamic>> _requestConversationAudit({
    required String command,
    required String sessionId,
    required String msgId,
    String? agentId,
    int? revision,
    String? cursor,
    int? limit,
    String? contentId,
    int? maxBytes,
  }) async {
    final completer = Completer<Map<String, dynamic>>();
    final seq = _sendConversationAuditPacket(
      command: command,
      sessionId: sessionId,
      msgId: msgId,
      agentId: agentId,
      revision: revision,
      cursor: cursor,
      limit: limit,
      contentId: contentId,
      maxBytes: maxBytes,
    );
    if (seq == 0) {
      completer.completeError(
        Exception('chat_audit_detail_send_request_failed'.tr),
      );
    } else {
      _conversationAuditPending[seq] = completer;
      Future.delayed(const Duration(seconds: 15), () {
        if (_conversationAuditPending.remove(seq) != null &&
            !completer.isCompleted) {
          completer.completeError(
            Exception('chat_audit_detail_request_timeout'.tr),
          );
        }
      });
    }
    return completer.future;
  }

  /// 工具栏一键上传技能（docs/architecture/39）：把本机某个技能上传进技能库。
  /// 成功即视为已入库；库里已有同名技能会被覆盖，调用方需在调用前自行确认。
  Future<void> requestSkillUpload({
    required String agentId,
    required String sessionId,
    required String name,
  }) async {
    final completer = Completer<void>();
    final seq = _sendAgentSkillUploadPacket(
      agentId: agentId,
      sessionId: sessionId,
      name: name,
    );
    if (seq == 0) {
      completer.completeError(Exception('发送上传请求失败'));
    } else {
      _skillUploadPending[seq] = completer;
      Future.delayed(const Duration(seconds: 15), () {
        if (_skillUploadPending.remove(seq) != null && !completer.isCompleted) {
          completer.completeError(Exception('上传请求超时'));
        }
      });
    }
    return completer.future;
  }

  /// 技能库启用（方案 v2）：把已同步库技能软链到当前 Agent 的全局/本项目目录。
  Future<Map<String, dynamic>> requestSkillEnable({
    required String agentId,
    required String sessionId,
    required String name,
    required String scope,
    String? force,
  }) {
    return _awaitSkillLibraryAction(
      pending: _skillEnablePending,
      send: () => _sendAgentSkillEnablePacket(
        agentId: agentId,
        sessionId: sessionId,
        name: name,
        scope: scope,
        force: force,
      ),
      sendFailedMessage: '发送启用请求失败',
      timeoutMessage: '启用请求超时',
    );
  }

  /// 技能库卸载启用（方案 v2）：仅删除指向库源的软链/损坏链接。
  Future<Map<String, dynamic>> requestSkillDisable({
    required String agentId,
    required String sessionId,
    required String name,
    required String scope,
  }) {
    return _awaitSkillLibraryAction(
      pending: _skillDisablePending,
      send: () => _sendAgentSkillDisablePacket(
        agentId: agentId,
        sessionId: sessionId,
        name: name,
        scope: scope,
      ),
      sendFailedMessage: '发送卸载请求失败',
      timeoutMessage: '卸载请求超时',
    );
  }

  /// 技能弹窗下拉刷新：让 agent 的 connector/插件立即重扫本地技能与技能库，
  /// 成功时回执里带最新工具栏快照（含 commands + library_skills），调用方据此
  /// 一次性刷新两个 Tab。后端等插件最长 15s，这里多留 5s 余量。
  Future<AgentToolbarModel> requestSkillRefresh({
    required String agentId,
    required String sessionId,
  }) async {
    final completer = Completer<AgentToolbarModel>();
    final seq = _sendAgentSkillRefreshPacket(
      agentId: agentId,
      sessionId: sessionId,
    );
    if (seq == 0) {
      completer.completeError(Exception('发送刷新请求失败'));
    } else {
      _skillRefreshPending[seq] = completer;
      Future.delayed(const Duration(seconds: 20), () {
        if (_skillRefreshPending.remove(seq) != null &&
            !completer.isCompleted) {
          completer.completeError(Exception('刷新请求超时'));
        }
      });
    }
    return completer.future;
  }

  Future<Map<String, dynamic>> _awaitSkillLibraryAction({
    required Map<int, Completer<Map<String, dynamic>>> pending,
    required int Function() send,
    required String sendFailedMessage,
    required String timeoutMessage,
  }) async {
    final completer = Completer<Map<String, dynamic>>();
    final seq = send();
    if (seq == 0) {
      completer.completeError(Exception(sendFailedMessage));
    } else {
      pending[seq] = completer;
      Future.delayed(const Duration(seconds: 15), () {
        if (pending.remove(seq) != null && !completer.isCompleted) {
          completer.completeError(Exception(timeoutMessage));
        }
      });
    }
    return completer.future;
  }

  Future<Map<String, dynamic>> requestAgentCreateFolder({
    required String agentId,
    required String sessionId,
    String? parentId,
    required String name,
  }) async {
    final completer = Completer<Map<String, dynamic>>();
    final seq = _sendAgentCreateFolderPacket(
      agentId: agentId,
      sessionId: sessionId,
      parentId: parentId,
      name: name,
    );
    if (seq == 0) {
      completer.completeError(Exception('发送创建文件夹请求失败'));
    } else {
      _createFolderPending[seq] = completer;
      Future.delayed(const Duration(seconds: 15), () {
        if (_createFolderPending.remove(seq) != null &&
            !completer.isCompleted) {
          completer.completeError(Exception('创建文件夹请求超时'));
        }
      });
    }
    return completer.future;
  }

  Future<List<Map<String, dynamic>>> requestAgentSessionBindings({
    required String agentId,
    required String sessionId,
  }) async {
    final completer = Completer<List<Map<String, dynamic>>>();
    final seq = _sendAgentSessionBindingsListPacket(
      agentId: agentId,
      sessionId: sessionId,
    );
    if (seq == 0) {
      completer.completeError(Exception('发送会话列表请求失败'));
    } else {
      _sessionBindingsPending[seq] = completer;
      Future.delayed(const Duration(seconds: 15), () {
        if (_sessionBindingsPending.remove(seq) != null &&
            !completer.isCompleted) {
          completer.completeError(Exception('会话列表请求超时'));
        }
      });
    }
    return completer.future;
  }

  Future<Map<String, dynamic>> requestAgentSessionBind({
    required String agentId,
    required String sessionId,
    required String cwd,
    String agentSessionId = '',
    String title = '',
  }) async {
    final completer = Completer<Map<String, dynamic>>();
    final seq = _sendAgentSessionBindPacket(
      agentId: agentId,
      sessionId: sessionId,
      cwd: cwd,
      agentSessionId: agentSessionId,
      title: title,
    );
    if (seq == 0) {
      completer.completeError(Exception('发送会话绑定请求失败'));
    } else {
      _sessionBindPending[seq] = completer;
      Future.delayed(const Duration(seconds: 20), () {
        if (_sessionBindPending.remove(seq) != null && !completer.isCompleted) {
          completer.completeError(Exception('会话绑定请求超时'));
        }
      });
    }
    return completer.future;
  }

  Future<void> applyLocalMessageRevoke({
    required String sessionId,
    required String msgId,
    String dbOpLabel = 'deleteMessage(local_revoke)',
    bool reloadSessions = true,
    int? authoritativeUnreadCount,
  }) async {
    await _applyLocalMessageRevokeImpl(
      sessionId: sessionId,
      msgId: msgId,
      dbOpLabel: dbOpLabel,
      reloadSessions: reloadSessions,
      authoritativeUnreadCount: authoritativeUnreadCount,
    );
  }

  void removeMessageFromCurrentSession(String msgId) {
    _removeMessageFromCurrentSessionImpl(msgId);
  }

  @visibleForTesting
  void upsertUIMessageForTest(MessageModel msg) {
    _upsertUIMessageForTestImpl(msg);
  }

  @visibleForTesting
  void debugAddStreamingMessageForTest(String msgId) {
    final normalizedMsgId = msgId.trim();
    if (normalizedMsgId.isEmpty) {
      return;
    }
    _markStreamingActivity(normalizedMsgId);
  }

  Future<void> sendMessage(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) {
    return _sendMessageImpl(
      content,
      sessionId,
      extra: extra,
      quotedMessageId: quotedMessageId,
      visibleTo: visibleTo,
      updateCurrentSessionUi: updateCurrentSessionUi,
    );
  }

  void delegateStart(
    String sessionId,
    String agentId, {
    int? maxConsecutiveReplies,
  }) {
    _delegateStartImpl(
      sessionId,
      agentId,
      maxConsecutiveReplies: maxConsecutiveReplies,
    );
  }

  void delegateStop(String sessionId) {
    _delegateStopImpl(sessionId);
  }

  bool stopAgentOutput(String sessionId, {String? runId}) {
    return _stopAgentOutputImpl(sessionId, runId: runId);
  }

  bool stopStreamingMessageLocally(String sessionId, String msgId) {
    return _stopStreamingMessageLocallyImpl(sessionId, msgId);
  }

  Future<void> retryMessage(String? clientMsgId, {String? msgId}) {
    return _retryMessageImpl(clientMsgId, msgId: msgId);
  }

  Future<void> loadSessions({bool refreshFromServer = true}) {
    return _ImServiceSessions(
      this,
    ).loadSessions(refreshFromServer: refreshFromServer);
  }

  Future<void> refreshSessionsNow() {
    return _ImServiceSessions(this).refreshSessionsNow();
  }

  Future<void> refreshSessionsWindowNow() {
    return _ImServiceSessions(this).refreshSessionsWindowNow();
  }

  Future<void> refreshSessionsIfStale({
    Duration maxAge = const Duration(seconds: 45),
  }) {
    return _ImServiceSessions(this).refreshSessionsIfStale(maxAge: maxAge);
  }

  bool get canLoadMoreSessionWindow {
    return _ImServiceSessions(this).canLoadMoreSessionWindow;
  }

  bool get isLoadingMoreSessionWindow => _sessionWindowPaginationInFlight;

  Future<bool> loadMoreSessionWindowIfNeeded({bool force = false}) {
    return _ImServiceSessions(this).loadMoreSessionWindowIfNeeded(force: force);
  }

  Future<void> bindSessionDisplayTitle(
    String sessionId,
    String title, {
    String type = 'private',
    String peerId = '',
    int peerType = 0,
  }) {
    return _ImServiceSessions(this).bindSessionDisplayTitle(
      sessionId,
      title,
      type: type,
      peerId: peerId,
      peerType: peerType,
    );
  }

  Future<void> setSessionDisplayTitle(
    String sessionId,
    String title, {
    String type = 'private',
    String peerId = '',
    int peerType = 0,
  }) {
    return _ImServiceSessions(this).setSessionDisplayTitle(
      sessionId,
      title,
      type: type,
      peerId: peerId,
      peerType: peerType,
    );
  }

  String resolveSessionDisplayTitle(SessionModel session) {
    return _ImServiceSessions(this).resolveSessionDisplayTitle(session);
  }

  String resolveSessionTypeById(
    String sessionId, {
    String fallback = 'private',
  }) {
    return _ImServiceSessions(
      this,
    ).resolveSessionTypeById(sessionId, fallback: fallback);
  }

  SessionModel? findSessionById(String sessionId) {
    return _ImServiceSessions(this).findSessionById(sessionId);
  }

  Future<void> syncPrivateAgentSessions(
    Map<String, String> currentAgentNames, {
    Map<String, String> previousAgentNames = const <String, String>{},
  }) {
    return _ImServiceSessions(this).syncPrivateAgentSessions(
      currentAgentNames,
      previousAgentNames: previousAgentNames,
    );
  }

  PeerPresenceState resolveSessionPeerPresence(String sessionId) {
    return _ImServiceSessions(this).resolveSessionPeerPresence(sessionId);
  }

  bool isAgentChannelOnline(String agentId) {
    return _ImServiceSessions(this).isAgentChannelOnline(agentId);
  }

  bool hasAgentChannelState(String agentId) {
    return _ImServiceSessions(this).hasAgentChannelState(agentId);
  }

  bool hasSessionTypeHint(String sessionId) {
    return _ImServiceSessions(this).hasSessionTypeHint(sessionId);
  }

  bool hasSessionDisplayTitleById(String sessionId) {
    return _ImServiceSessions(this).hasSessionDisplayTitleById(sessionId);
  }

  int getSessionMemberEventVersion(String sessionId) {
    return _ImServiceSessions(this).getSessionMemberEventVersion(sessionId);
  }

  int getSessionAccessRevokedVersion(String sessionId) {
    return _ImServiceSessions(this).getSessionAccessRevokedVersion(sessionId);
  }

  String getSessionAccessRevokedReason(String sessionId) {
    return _ImServiceSessions(this).getSessionAccessRevokedReason(sessionId);
  }

  int getSessionReadVersion(String sessionId) {
    return _ImServiceSessions(this).getSessionReadVersion(sessionId);
  }

  String getSessionReadCursor(String sessionId, String memberId) {
    return _ImServiceSessions(this).getSessionReadCursor(sessionId, memberId);
  }

  String resolveSessionDisplayTitleById(
    String sessionId, {
    String fallbackTitle = '',
    String type = 'private',
  }) {
    return _ImServiceSessions(this).resolveSessionDisplayTitleById(
      sessionId,
      fallbackTitle: fallbackTitle,
      type: type,
    );
  }

  void clearUnread(String sessionId) {
    _ImServiceSessions(this).clearUnread(sessionId);
  }

  void markUnread(String sessionId) {
    _ImServiceSessions(this).markUnread(sessionId);
  }

  Future<bool> setSessionPinned(String sessionId, {required bool isPinned}) {
    return _ImServiceSessions(
      this,
    ).setSessionPinned(sessionId, isPinned: isPinned);
  }

  /// Persist friend-level pin to memory + LocalDb and protect it from stale
  /// snapshot overwrites until the server catches up.
  Future<void> applyLocalFriendPin({
    required List<String> sessionIds,
    required bool isPinned,
    required int pinnedAt,
  }) {
    return _ImServiceSessions(this).applyLocalFriendPin(
      sessionIds: sessionIds,
      isPinned: isPinned,
      pinnedAt: pinnedAt,
    );
  }

  /// Write conversation-list pin truth back to LocalDb/memory and clear stale
  /// local pins once the first page contains the complete pinned set.
  Future<void> reconcilePinsFromConversationSummaries(
    List<ConversationSummaryModel> items, {
    required bool hasMore,
  }) {
    return _ImServiceSessions(
      this,
    ).reconcilePinsFromConversationSummaries(items, hasMore: hasMore);
  }

  Future<bool> setSessionMuted(String sessionId, {required bool isMuted}) {
    return _ImServiceSessions(
      this,
    ).setSessionMuted(sessionId, isMuted: isMuted);
  }

  Future<void> deleteConversation(String sessionId) {
    return _ImServiceSessions(this)._deleteConversationImpl(sessionId);
  }

  Future<void> revokeSessionAccess(String sessionId) {
    return _ImServiceSessions(this).revokeSessionAccess(sessionId);
  }

  bool get _streamDiagEnabled => kDebugMode && _streamDiagFlag;

  // pull_sync 多批补拉（hasMore 链）期间，会话列表整刷的最小间隔。
  // 末批（hasMore=false）不受节流约束，必刷一次收尾。
  static const Duration _pullSyncDrainSessionsRefreshInterval = Duration(
    milliseconds: 1500,
  );
  static const Duration _sessionWindowRefreshMinInterval = Duration(seconds: 5);

  // 历史窗口控制
  static const int _messagePageSize = 40;
  static const int _initialMessageLimit = 30;
  static const int _coldStartSessionSnapshotLimit = 200;
  static const int _coldStartSessionSnapshotMaxPages = 5;
  static const int _sessionWindowPaginationLimit = 40;
  static const Duration _sessionWindowPaginationInterval = Duration(
    milliseconds: 1200,
  );
  static const Duration _sessionWindowPaginationNetworkBackoff = Duration(
    seconds: 12,
  );
  static const Duration _sessionWindowPaginationRateLimitBackoff = Duration(
    seconds: 60,
  );
  static const int _maxRemoteHistoryEmptyPageSkips = 5;
  static const int _residentMessageCap = 200;
  static const int _llmHistoryLimit = 30;
  static const int _sessionWindowCacheMaxEntries = 3;
  static const int _sessionWindowCacheMessageLimit = _initialMessageLimit;
  static const int _sessionWindowCacheMaxContentChars = 256 * 1024;
  static const Duration _sessionWindowCacheTtl = Duration(minutes: 5);
  _MessageCursor? _oldestHistoryCursor;
  _MessageCursor? _newestHistoryCursor;
  bool _hasOlderMessages = true;
  bool _hasNewerMessages = false;
  bool _isLoadingOlderMessages = false;
  bool _isLoadingNewerMessages = false;

  // Small LRU of recent chat windows. Entries are trimmed to the lightweight
  // first-paint size and expire quickly so visiting many chats cannot retain
  // an unbounded number of message objects.
  final Map<String, _CachedSessionWindowState> _cachedSessionWindows = {};

  // Delegate states: sessionId -> {agent_id, active}
  final delegateStates = <String, Map<String, dynamic>>{}.obs;
  final voiceDelegateStates = <String, String>{}.obs;
  final agentStates = <String, Map<String, dynamic>>{}.obs;
  final lastAgentDeliveryError = Rxn<Map<String, dynamic>>();
  final sessionMemberEventVersions = <String, int>{}.obs;
  final sessionAccessRevokedVersions = <String, int>{}.obs;
  final _sessionAccessRevokedReasons = <String, String>{};
  final sessionReadVersions = <String, int>{}.obs;
  final _sessionReadCursorBySession = <String, Map<String, String>>{};
  final _agentStateExpiryTick = 0.obs;
  Timer? _agentStateExpiryTimer;
  Timer? _staleAgentOutputTimer;
  bool _deferSystemUnreadBadgeSync = false;
  final _fileListPending = <int, Completer<List<Map<String, dynamic>>>>{};
  final _fileListCurrentPath = <int, String?>{};
  final _fileListMachineName = <int, String?>{};
  final _conversationAuditPending = <int, Completer<Map<String, dynamic>>>{};
  final _createFolderPending = <int, Completer<Map<String, dynamic>>>{};
  final _skillUploadPending = <int, Completer<void>>{};
  final _skillEnablePending = <int, Completer<Map<String, dynamic>>>{};
  final _skillDisablePending = <int, Completer<Map<String, dynamic>>>{};
  final _skillRefreshPending = <int, Completer<AgentToolbarModel>>{};
  final _sessionBindingsPending =
      <int, Completer<List<Map<String, dynamic>>>>{};
  final _sessionBindPending = <int, Completer<Map<String, dynamic>>>{};
  int _nextLocalActionSeq = DateTime.now().microsecondsSinceEpoch;

  // 总未读数
  int get totalUnread => _ImServiceRuntime(this).totalUnread;

  // 提醒未读数（排除已屏蔽会话）
  int get notificationUnread => _ImServiceRuntime(this).notificationUnread;

  // 单会话总未读（包含已关闭提醒）
  int totalUnreadForSession(SessionModel session) =>
      _ImServiceRuntime(this).totalUnreadForSession(session);

  // 单会话提醒未读（排除已关闭提醒）
  int notificationUnreadForSession(SessionModel session) =>
      _ImServiceRuntime(this).notificationUnreadForSession(session);

  /// Returns the local unread override for [sessionId], or null if none.
  /// Used by the conversation list to respect local clearUnread operations
  /// when rendering items built from the server conversation API.
  int? unreadOverrideForSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return null;
    final override = _localUnreadOverrides[sid];
    return override?.unreadCount;
  }

  /// 用服务端返回的会话未读数修复本地内存/DB。
  /// 用于会话分组弹窗打开后：列表行走服务端摘要（session_members.unread_count 求和），
  /// 弹窗会话行只读本地 imService.sessions，两者在某些 race 或跨设备已读后会分叉；
  /// 这里仅同步服务端返回的这些会话，并保留本地 override 优先。
  ///
  /// 批量路径：一次内存 `sessions.value` 更新 + 单事务 DB，避免逐条
  /// `_setSessionUnreadCountLocal` 触发 N 次 Rx/resort/写库放大。
  Future<void> syncSessionUnreadCountsFromServer(
    Iterable<SessionModel> serverSessions,
  ) async {
    await _ImServiceSessions(this).syncSessionUnreadCountsFromServerBatch(
      serverSessions,
    );
  }

  void deferSystemUnreadBadgeSyncUntilAuthoritativeRefresh() {
    _ImServiceRuntime(
      this,
    ).deferSystemUnreadBadgeSyncUntilAuthoritativeRefresh();
  }

  Future<void> syncSystemUnreadBadgeNow({
    bool force = false,
    bool authoritative = false,
  }) {
    return _ImServiceRuntime(
      this,
    ).syncSystemUnreadBadgeNow(force: force, authoritative: authoritative);
  }

  Future<ImService> init() {
    return _ImServiceRuntime(this).init();
  }

  void suspendForAppBackground() {
    _ImServiceRuntime(this).suspendForAppBackground();
  }

  @visibleForTesting
  Future<void> handleDownstreamForTest(String payloadStr) {
    return _handleDownstream(payloadStr);
  }

  @visibleForTesting
  void redirectToHomeAfterAuthSuccessForTest() {
    _redirectToHomeAfterAuthSuccess();
  }

  @visibleForTesting
  void handleSocketPayloadForTest(String payloadStr) {
    _handleSocketPayload(payloadStr);
  }

  @visibleForTesting
  void blockDownstreamQueueForTest(Future<void> blocker) {
    _downstreamQueue = blocker;
  }

  @visibleForTesting
  int get lastPongAtMsForTest => _lastPongAtMs;

  @visibleForTesting
  Future<void> resendPendingMessagesFromDbForTest() {
    return _resendPendingMessagesFromDb();
  }

  @visibleForTesting
  Future<void> triggerAuthForTest() {
    return _triggerAuth();
  }

  @visibleForTesting
  void handleAuthHandshakeTimeoutForTest({String phase = 'auth'}) {
    _handleAuthHandshakeTimeout(phase: phase);
  }

  @visibleForTesting
  void observeInboxSeqForTest(int seq) {
    _observeInboxSeq(seq);
  }

  @visibleForTesting
  int get pendingPullSyncCursorFloorForTest => _pendingPullSyncCursorFloor;

  @visibleForTesting
  bool get hasPullSyncThrottleTimerForTest =>
      _pullSyncThrottleTimer?.isActive ?? false;

  @visibleForTesting
  int get persistFailPullSyncStreakForTest => _persistFailPullSyncStreak;

  @visibleForTesting
  bool get pendingPersistFailPullSyncForTest => _pendingPersistFailPullSync;

  @visibleForTesting
  void setPersistFailPullSyncScheduleMsForTest(int value) {
    _lastPersistFailPullSyncScheduleMs = value;
  }

  @visibleForTesting
  void setLastPullSyncRequestMsForTest(int value) {
    _lastPullSyncRequestMs = value;
  }

  @visibleForTesting
  int resolvePullSyncCursorForTest(int localMaxInboxSeq) {
    return _resolvePullSyncCursor(localMaxInboxSeq);
  }

  @visibleForTesting
  int resolveInitialInboxSeqCursorForTest({
    required int localMaxInboxSeq,
    required int persistedInboxSeq,
    required int localMessageCount,
    int bootstrapInboxSeqFloor = 0,
  }) {
    return _resolveInitialInboxSeqCursor(
      localMaxInboxSeq: localMaxInboxSeq,
      persistedInboxSeq: persistedInboxSeq,
      localMessageCount: localMessageCount,
      bootstrapInboxSeqFloor: bootstrapInboxSeqFloor,
    );
  }

  @visibleForTesting
  Future<void> refreshActiveSessionOnReconnectForTest() {
    return refreshActiveSessionOnReconnect();
  }

  @visibleForTesting
  void setCurrentSessionForTest(String? sessionId) {
    _currentSessionId.value = sessionId;
    if (sessionId != null) {
      _startDbChangeSubscription();
    } else {
      _cancelDbChangeSubscription();
    }
  }

  @visibleForTesting
  void startSessionViewingForTest(String sessionId) {
    _startSessionViewing(sessionId);
  }

  @visibleForTesting
  void seedRealtimeStateForTest({
    String? wsUrl,
    bool connected = false,
    bool authenticated = false,
    bool allowReconnect = true,
    bool hasEstablishedRealtimeSession = false,
    bool pendingInitialConnection = false,
    int reconnectAttempts = 0,
    ImConnectionStage? stage,
  }) {
    _wsUrl = wsUrl;
    _allowReconnect = allowReconnect;
    _reconnectAttempts = reconnectAttempts;
    _isConnected.value = connected;
    _isAuthenticated.value = authenticated;
    _hasEstablishedRealtimeSession = hasEstablishedRealtimeSession;
    _hasPendingInitialConnection = pendingInitialConnection;
    if (stage != null) {
      _setConnectionStage(stage);
    } else {
      _syncConnectionBannerDelayForStage(_connectionStage.value);
    }
  }

  @visibleForTesting
  void restoreCurrentSessionRealtimeStateForTest() {
    _restoreCurrentSessionRealtimeState();
  }

  @visibleForTesting
  void markAgentOutputStoppingLocallyForTest(
    String sessionId, {
    String? runId,
  }) {
    _markAgentOutputStoppingLocally(sessionId, runId: runId);
  }

  @visibleForTesting
  void seedDeletedSessionForTest(String sessionId, {required int deletedAtMs}) {
    final sid = sessionId.trim();
    if (sid.isEmpty || deletedAtMs <= 0) return;
    _deletedSessionsLoaded = true;
    _locallyDeletedSessions[sid] = deletedAtMs;
    _persistDeletedSessions();
  }

  @visibleForTesting
  void markSessionHistoryResetInFlightForTest(
    String sessionId, {
    required int sentAtMs,
    int deletedAtMs = 0,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty || sentAtMs <= 0) return;
    _sessionHistoryResetInFlightAtMs[sid] = sentAtMs;
    if (deletedAtMs > 0) {
      _sessionHistoryResetInFlightDeletedAtMs[sid] = deletedAtMs;
    } else {
      _sessionHistoryResetInFlightDeletedAtMs.remove(sid);
    }
  }

  @visibleForTesting
  bool hasRecentSessionHistoryResetInFlightForTest(
    String sessionId, {
    required int nowMs,
    int deletedAtMs = 0,
  }) {
    return _hasRecentSessionHistoryResetInFlight(
      sessionId,
      nowMs: nowMs,
      deletedAtMs: deletedAtMs,
    );
  }

  /// Whether the user has locally deleted this session's conversation.
  /// Used by the conversation list to suppress re-appearance of deleted
  /// sessions when the backend conversation API doesn't yet filter them.
  bool isSessionLocallyDeleted(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    return _locallyDeletedSessions.containsKey(sid);
  }

  @visibleForTesting
  bool isSessionLocallyDeletedForTest(String sessionId) {
    return isSessionLocallyDeleted(sessionId);
  }

  @visibleForTesting
  void seedRevokedSessionForTest(String sessionId, {required int revokedAtMs}) {
    final sid = sessionId.trim();
    if (sid.isEmpty || revokedAtMs <= 0) return;
    _revokedSessionsLoaded = true;
    _locallyRevokedSessions[sid] = revokedAtMs;
    _persistRevokedSessions();
  }

  @visibleForTesting
  bool isSessionLocallyRevokedForTest(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    return _locallyRevokedSessions.containsKey(sid);
  }

  @visibleForTesting
  String? get wsUrlForTest => _wsUrl;

  @visibleForTesting
  bool get hasReconnectTimerForTest => _reconnectTimer?.isActive ?? false;

  @visibleForTesting
  int get reconnectAttemptsForTest => _reconnectAttempts;

  @visibleForTesting
  bool get isSuspendedForAppBackgroundForTest => _isSuspendedForAppBackground;

  @visibleForTesting
  void startSendAckTimerForTest(String clientMsgId, {bool isDelegate = false}) {
    _startSendAckTimer(clientMsgId, isDelegate: isDelegate);
  }

  @visibleForTesting
  Future<void> handleSendAckTimeoutForTest(
    String clientMsgId, {
    bool isDelegate = false,
  }) {
    return _handleSendAckTimeout(clientMsgId, isDelegate: isDelegate);
  }

  @visibleForTesting
  void handleDisconnectForTest({ImConnectionStage? finalStage}) {
    _handleDisconnect(finalStage: finalStage);
  }

  @visibleForTesting
  int get activeSendAckTimerCountForTest => _sendAckTimers.length;

  @visibleForTesting
  bool get isSessionViewingActiveForTest => _viewingActive;

  @visibleForTesting
  String get viewingSessionIdForTest => _viewingSessionId;

  @visibleForTesting
  bool get isSessionComposingActiveForTest => _composingActive;

  @visibleForTesting
  bool get hasComposingIdleTimerForTest =>
      _composingIdleTimer?.isActive ?? false;

  /// 测试用：覆盖 composing 空闲超时时长，为 null 时使用
  /// [_composingIdleTimeout] 默认值。
  @visibleForTesting
  static Duration? composingIdleTimeoutForTest;

  /// 测试用：覆盖流式看门狗的空闲阈值与清扫间隔，为 null 时使用
  /// [_streamingIdleTimeout] / [_streamingWatchdogInterval] 默认值。
  @visibleForTesting
  static Duration? streamingIdleTimeoutForTest;

  @visibleForTesting
  static Duration? streamingWatchdogIntervalForTest;

  /// 测试用：直接把某个 streaming msgId 的活动时间钉到指定时刻，
  /// 确定性构造"超过空闲阈值"的僵尸流场景。
  @visibleForTesting
  void debugSetStreamingActivityAtForTest(String msgId, int atMs) {
    _streamingActivityAtByMsgId[msgId.trim()] = atMs;
  }

  /// 测试用：摘除某个 streaming msgId 的活跃标记与活动时间，并在没有其它
  /// 活跃流时停掉看门狗。widget 测试（fake async）里用它收尾，避免看门狗
  /// 周期计时器泄漏成 pending timer。
  @visibleForTesting
  void debugRemoveStreamingMessageForTest(String msgId) {
    final normalizedMsgId = msgId.trim();
    _activeStreamingMsgIds.remove(normalizedMsgId);
    _streamingActivityAtByMsgId.remove(normalizedMsgId);
    if (_activeStreamingMsgIds.isEmpty && _streamingActivityAtByMsgId.isEmpty) {
      _streamingWatchdogTimer?.cancel();
      _streamingWatchdogTimer = null;
    }
  }

  @visibleForTesting
  void sweepStaleStreamingMessagesForTest() {
    _sweepStaleStreamingMessages();
  }

  @visibleForTesting
  bool get hasStreamingWatchdogTimerForTest =>
      _streamingWatchdogTimer?.isActive ?? false;

  @visibleForTesting
  static int? sessionWindowCacheNowMsForTest;

  @visibleForTesting
  static void Function()? initialRenderHydrationStartedForTest;

  @visibleForTesting
  Future<void> loadInitialWindowForTest(String sessionId) async {
    _currentSessionId.value = sessionId;
    _resetMessageWindowState();
    currentMessages.clear();
    _clearCurrentMessageIndexes();
    _initialLoadRetryCount = 0;
    await _loadInitialMessages(sessionId);
    await _pendingInitialWindowBackfill;
  }

  @visibleForTesting
  void scheduleInitialRenderHydrationForTest(
    String sessionId,
    List<String> contents,
  ) {
    _scheduleInitialRenderCacheHydration(sessionId, contents);
  }

  @visibleForTesting
  bool get hasInitialLoadRetryTimerForTest =>
      _initialLoadRetryTimer?.isActive ?? false;

  @visibleForTesting
  int get initialLoadRetryCountForTest => _initialLoadRetryCount;

  @visibleForTesting
  Map<String, dynamic>? get lastAgentDeliveryErrorForTest =>
      lastAgentDeliveryError.value == null
      ? null
      : Map<String, dynamic>.from(lastAgentDeliveryError.value!);

  @visibleForTesting
  void primeLocalStreamForTest({
    required String sessionId,
    required String renderMsgId,
    String? quotedMessageId,
  }) {
    final sid = sessionId.trim();
    final msgId = renderMsgId.trim();
    if (sid.isEmpty || msgId.isEmpty) return;
    _localInferenceInFlight.add(sid);
    _localStreamRenderMsgIds[sid] = msgId;
    final normalizedQuotedMessageId = quotedMessageId?.trim() ?? '';
    if (normalizedQuotedMessageId.isNotEmpty) {
      _localStreamQuotedMessageIds[sid] = normalizedQuotedMessageId;
    }
  }

  @visibleForTesting
  void finalizeLocalStreamRenderMessageForTest({
    required String sessionId,
    required String msgId,
    required String agentId,
    required String finalContent,
    String? quotedMessageId,
    String status = 'success',
  }) {
    _finalizeLocalStreamRenderMessage(
      sessionId: sessionId,
      msgId: msgId,
      agentId: agentId,
      finalContent: finalContent,
      quotedMessageId: quotedMessageId,
      status: status,
    );
  }

  @visibleForTesting
  int get residentMessageCapForTest => _residentMessageCap;

  @visibleForTesting
  List<String> get cachedSessionWindowIdsForTest =>
      List<String>.unmodifiable(_cachedSessionWindows.keys);

  @visibleForTesting
  int streamGapRecoveryAttemptsForTest(String msgId) {
    final normalizedMsgId = msgId.trim();
    if (normalizedMsgId.isEmpty) {
      return 0;
    }
    return _streamGapRecoveryAttemptsByMsg[normalizedMsgId] ?? 0;
  }

  void onStreamUiUpdated(String msgId) {
    _onStreamUiUpdatedImpl(msgId);
  }

  void _redirectToLogin() {
    _redirectToLoginImpl();
  }

  void _redirectToHomeAfterAuthSuccess() {
    _redirectToHomeAfterAuthSuccessImpl();
  }

  void _updateUIMessage(String msgId, MessageModel msg) {
    _updateUIMessageImpl(msgId, msg);
  }

  Future<void> _handleSendNack(
    String clientMsgId, {
    required int code,
    required String message,
  }) async {
    _cancelSendAckTimer(clientMsgId);

    final idx = currentMessages.indexWhere((e) => e.clientMsgId == clientMsgId);
    final memStatus = idx != -1 ? (currentMessages[idx].status ?? '') : '';
    final local = await LocalDb.getMessageByLocalSeq(clientMsgId);
    final dbStatus = local?['status']?.toString() ?? '';
    final effectiveStatus = dbStatus.isNotEmpty ? dbStatus : memStatus;
    if (!effectiveStatus.startsWith('sending')) {
      debugPrint(
        'ℹ️ ignore stale send_nack: client_msg_id=$clientMsgId '
        'effective_status=$effectiveStatus code=$code msg=$message',
      );
      return;
    }
    final isDelegate =
        memStatus.contains('delegate') || dbStatus.contains('delegate');
    final failedStatus = isDelegate ? 'failed_delegate' : 'failed';

    await LocalDb.updateMessageStatusByLocalSeq(clientMsgId, failedStatus);
    if (idx != -1) {
      currentMessages[idx] = currentMessages[idx].copyWith(
        status: failedStatus,
      );
    }
    debugPrint(
      '❌ send_nack: client_msg_id=$clientMsgId code=$code msg=$message',
    );
    if (message.isNotEmpty) {
      CustomToast.show(_mapSendNackMessage(message));
    }
  }

  void _recordPongReceipt() {
    _recordPongReceiptImpl();
  }

  String _mapSendNackMessage(String message) {
    return _mapSendNackMessageImpl(message);
  }

  String? describeAgentDeliveryStatus(String? status) {
    return _describeAgentDeliveryStatusImpl(status);
  }

  bool isAgentDeliveryStatusError(String? status) {
    return _isAgentDeliveryStatusErrorImpl(status);
  }

  List<EventLifecycleQueueItem> queueItemsForSession(String sessionId) {
    return _queueItemsForSessionImpl(sessionId);
  }

  int queueCountForSession(String sessionId) {
    return _queueCountForSessionImpl(sessionId);
  }

  void sendEventCancel({
    required String sessionId,
    required EventLifecycleQueueItem item,
  }) {
    _sendEventCancelImpl(sessionId: sessionId, item: item);
  }

  void sendQueueClear({required String sessionId}) {
    _sendQueueClearImpl(sessionId: sessionId);
  }

  /// 拖动排序后提交排队消息的新顺序（队头在前，不含 running 项）。
  /// 本地先乐观生效，随后由 queue_reorder_result 与权威 queue_snapshot 收敛。
  void sendQueueReorder({
    required String sessionId,
    required List<String> orderedEventIds,
  }) {
    _sendQueueReorderImpl(
      sessionId: sessionId,
      orderedEventIds: orderedEventIds,
    );
  }

  /// 主动从服务端拉取一次队列快照（覆盖本地缓存）。
  /// 调用时机由 UI 层决定：会话视图被打开、WS 重连成功、app 回前台等。
  void pullQueueSnapshot({required String sessionId}) {
    _sendQueueSnapshotQueryImpl(sessionId: sessionId);
  }

  /// 暂停/恢复某个排队任务，等待回执（默认 5s 超时，超时 timedOut=true）。
  /// 本地先乐观翻转 held，随后由回执与权威 queue_snapshot 收敛。
  Future<EventLifecycleCmdResult> sendEventHold({
    required String sessionId,
    required String eventId,
    required bool hold,
    String reason = 'manual',
    int? ttlMs,
  }) {
    return _sendEventHoldImpl(
      sessionId: sessionId,
      eventId: eventId,
      hold: hold,
      reason: reason,
      ttlMs: ttlMs,
    );
  }

  /// 改写某个排队任务的全文，等待回执（默认 5s 超时，超时 timedOut=true）。
  /// 成功后 connector 会自动解除该任务的 hold 并推权威 queue_snapshot。
  Future<EventLifecycleCmdResult> sendQueueEdit({
    required String sessionId,
    required String eventId,
    required String content,
  }) {
    return _sendQueueEditImpl(
      sessionId: sessionId,
      eventId: eventId,
      content: content,
    );
  }

  int _toInt(dynamic v) => _toIntImpl(v);

  bool _toBool(dynamic v) => _toBoolImpl(v);

  List<int> _resolveMentionUserIds(dynamic rawMentionUserIds, String content) {
    return _resolveMentionUserIdsImpl(rawMentionUserIds, content);
  }

  Map<String, dynamic>? _decodeExtraMap(dynamic rawExtra) {
    return _decodeExtraMapImpl(rawExtra);
  }

  void dispatchSendMessagePacket({
    required String sessionId,
    required String clientMsgId,
    required String content,
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    required bool delegateOrigin,
  }) {
    _sendMessagePacket(
      sessionId: sessionId,
      clientMsgId: clientMsgId,
      content: content,
      extra: extra,
      quotedMessageId: quotedMessageId,
      visibleTo: visibleTo,
      delegateOrigin: delegateOrigin,
    );
  }

  void dispatchRetryMessagePacket({
    required String sessionId,
    required String msgId,
  }) {
    _retryMessagePacket(sessionId: sessionId, msgId: msgId);
  }

  int _resolveDelegateMaxConsecutiveReplies(
    dynamic raw, {
    required String sessionId,
  }) {
    return _resolveDelegateMaxConsecutiveRepliesImpl(raw, sessionId: sessionId);
  }

  String _toId(dynamic v) => _toIdImpl(v);

  int _requireIntLike(dynamic v, {required String fieldName}) {
    return _requireIntLikeImpl(v, fieldName: fieldName);
  }

  void syncNow() {
    _syncNowImpl();
  }

  void disconnect({ImConnectionStage stage = ImConnectionStage.disconnected}) {
    _disconnectImpl(stage: stage);
  }

  Future<void> resetForAccountSwitch() {
    return _resetForAccountSwitchImpl();
  }

  @override
  void onClose() {
    _onCloseImpl();
    super.onClose();
  }

  Future<void> loadSessionsForCurrentUser() {
    return _loadSessionsForCurrentUserImpl();
  }
}

class _DeliveryTimeoutGracePeriod {
  final String sessionId;
  final Map<String, dynamic> payload;
  Timer? timer;
  _DeliveryTimeoutGracePeriod({required this.sessionId, required this.payload});
}
