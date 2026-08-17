import '../../shared/utils/strict_int_parser.dart';
import 'session_model.dart';

class ConversationPageResult {
  const ConversationPageResult({
    this.items = const <ConversationSummaryModel>[],
    this.success = true,
    this.hasMore = false,
    this.nextCursor = '',
    this.httpStatus = 200,
    this.rateLimited = false,
    this.networkError = false,
  });

  final List<ConversationSummaryModel> items;
  final bool success;
  final bool hasMore;
  final String nextCursor;
  final int httpStatus;
  final bool rateLimited;
  final bool networkError;
}

class ConversationThreadPageResult {
  const ConversationThreadPageResult({
    this.groupKey = '',
    this.sessions = const <SessionModel>[],
    this.success = true,
    this.hasMore = false,
    this.nextCursor = '',
    this.httpStatus = 200,
    this.rateLimited = false,
    this.networkError = false,
  });

  final String groupKey;
  final List<SessionModel> sessions;
  final bool success;
  final bool hasMore;
  final String nextCursor;
  final int httpStatus;
  final bool rateLimited;
  final bool networkError;
}

class ConversationSummaryModel {
  const ConversationSummaryModel({
    required this.groupKey,
    required this.conversationType,
    required this.latestSessionId,
    this.title = '',
    this.peerId = '',
    this.peerType = 0,
    this.peerNickname = '',
    this.peerUsername = '',
    this.sessionType = 1,
    this.isVisitor = false,
    this.lastMsg = '',
    this.lastMsgTime = 0,
    this.unread = 0,
    this.badgeUnread = 0,
    this.updatedAt = 0,
    this.latestActiveAt = 0,
    this.isPinned = false,
    this.pinnedAt = 0,
    this.isMuted = false,
    this.threadCount = 1,
    this.hasMoreThreads = false,
  });

  final String groupKey;
  final String conversationType;
  final String latestSessionId;
  final String title;
  final String peerId;
  final int peerType;
  final String peerNickname;
  final String peerUsername;
  final int sessionType;
  final bool isVisitor;
  final String lastMsg;
  // lastMsgTime 为「最后一条可见消息」的时间(ms)，用于列表展示时间，与点进会话看到的
  // 最后一条对齐；为 0 表示无可见消息，展示回退到活跃时间。
  final int lastMsgTime;
  final int unread;
  final int badgeUnread;
  final int updatedAt;
  final int latestActiveAt;
  final bool isPinned;
  final int pinnedAt;
  final bool isMuted;
  final int threadCount;
  final bool hasMoreThreads;

  factory ConversationSummaryModel.fromJson(Map<String, dynamic> json) {
    final peer = json['peer'];
    final peerMap = peer is Map ? peer : const <String, dynamic>{};
    return ConversationSummaryModel(
      groupKey: json['group_key']?.toString() ?? '',
      conversationType: json['conversation_type']?.toString() ?? '',
      latestSessionId: json['latest_session_id']?.toString() ?? '',
      title: json['title']?.toString() ?? '',
      peerId: peerMap['id']?.toString() ?? json['peer_id']?.toString() ?? '',
      peerType: _readInt(peerMap, 'type', fallback: json['peer_type']),
      peerNickname:
          peerMap['nickname']?.toString() ??
          json['peer_nickname']?.toString() ??
          '',
      peerUsername:
          peerMap['username']?.toString() ??
          json['peer_username']?.toString() ??
          '',
      sessionType: _readInt(json, 'session_type', defaultValue: 1),
      isVisitor: _readBool(json, 'is_visitor'),
      lastMsg: json['last_msg']?.toString() ?? '',
      lastMsgTime: _normalizeTimestamp(_readInt(json, 'last_msg_time')),
      unread: _readInt(json, 'unread'),
      badgeUnread: _readInt(json, 'badge_unread'),
      updatedAt: _normalizeTimestamp(_readInt(json, 'updated_at')),
      latestActiveAt: _normalizeTimestamp(_readInt(json, 'latest_active_at')),
      isPinned: _readBool(json, 'is_pinned'),
      pinnedAt: _normalizeTimestamp(_readInt(json, 'pinned_at')),
      isMuted: _readBool(json, 'is_muted'),
      threadCount: _readInt(json, 'thread_count', defaultValue: 1),
      hasMoreThreads: _readBool(json, 'has_more_threads'),
    );
  }

  SessionModel toLatestSessionModel() {
    final normalizedType = conversationType == 'group' || sessionType == 2
        ? 'group'
        : 'private';
    final activeAt = latestActiveAt > 0 ? latestActiveAt : updatedAt;
    // 展示时间取「最后一条可见消息」的时间；无可见消息(0)时回退到活跃时间。
    // updatedAt 仍保留活跃时间，activityAt 据此排序——agent 后台干活置顶能力不变。
    final displayTime = lastMsgTime > 0 ? lastMsgTime : activeAt;
    return SessionModel(
      sessionId: latestSessionId,
      title: title,
      type: normalizedType,
      peerId: peerId,
      peerType: peerType,
      peerNickname: peerNickname,
      peerUsername: peerUsername,
      updatedAt: activeAt,
      isPinned: normalizedType == 'group' ? isPinned : false,
      isMuted: normalizedType == 'group' ? isMuted : false,
      pinnedAt: normalizedType == 'group' ? pinnedAt : 0,
      friendIsPinned: normalizedType == 'private' ? isPinned : false,
      friendPinnedAt: normalizedType == 'private' ? pinnedAt : 0,
      friendIsMuted: normalizedType == 'private' ? isMuted : false,
      isVisitor: isVisitor,
      unreadCount: unread,
      lastMessage: lastMsg,
      lastMessageTime: displayTime,
    );
  }

  static int _readInt(
    Map<dynamic, dynamic> json,
    String key, {
    int defaultValue = 0,
    dynamic fallback,
  }) {
    final value = json[key] ?? fallback;
    if (value == null) return defaultValue;
    return StrictIntParser.parse(value, fieldName: 'Conversation.$key');
  }

  static bool _readBool(Map<dynamic, dynamic> json, String key) {
    final value = json[key];
    if (value == null) return false;
    if (value is bool) return value;
    final parsed = StrictIntParser.tryParse(value);
    if (parsed == null) return false;
    return parsed != 0;
  }

  static int _normalizeTimestamp(int ts) {
    if (ts <= 0) return 0;
    if (ts < 100000000000) {
      return ts * 1000;
    }
    return ts;
  }
}
