import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';
import 'package:sentry_flutter/sentry_flutter.dart';
import '../models/conversation_summary_model.dart';
import '../models/session_model.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

part 'session_service_basic_api.dart';
part 'session_service_conversation_api.dart';
part 'session_service_snapshot_api.dart';
part 'session_service_group_manage.dart';

class SessionDetailResult {
  const SessionDetailResult({
    this.data,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final Map<String, dynamic>? data;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class SessionRenameResult {
  const SessionRenameResult({
    this.title,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final String? title;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class SessionMemberNicknameResult {
  const SessionMemberNicknameResult({
    this.groupNickname,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final String? groupNickname;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class SessionPinResult {
  const SessionPinResult({
    this.sessionId = '',
    this.isPinned = false,
    this.pinnedAt = 0,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final String sessionId;
  final bool isPinned;
  final int pinnedAt;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class SessionMuteResult {
  const SessionMuteResult({
    this.sessionId = '',
    this.isMuted = false,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final String sessionId;
  final bool isMuted;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class SessionAddMembersResult {
  const SessionAddMembersResult({
    this.data,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final Map<String, dynamic>? data;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class SessionInviteSettingResult {
  const SessionInviteSettingResult({
    this.allowMemberInvite = false,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final bool allowMemberInvite;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class SessionAllMembersMutedResult {
  const SessionAllMembersMutedResult({
    this.allMembersMuted = false,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final bool allMembersMuted;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class SessionMemberSpeakingResult {
  const SessionMemberSpeakingResult({
    this.memberId = '',
    this.memberType = 1,
    this.isSpeakMuted = false,
    this.canSpeakWhenAllMuted = false,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final String memberId;
  final int memberType;
  final bool isSpeakMuted;
  final bool canSpeakWhenAllMuted;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class SessionMemberAgentReceiveResult {
  const SessionMemberAgentReceiveResult({
    this.memberId = '',
    this.memberType = 1,
    this.agentReceiveMode = 1,
    this.agentReceiveBacklogCount = 8,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final String memberId;
  final int memberType;
  final int agentReceiveMode;
  final int agentReceiveBacklogCount;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class SessionLeaveResult {
  const SessionLeaveResult({
    this.sessionId = '',
    this.left = false,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final String sessionId;
  final bool left;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;
}

class WidgetSessionModerationResult {
  const WidgetSessionModerationResult({
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;

  bool get success => code == 0;
}

class WidgetDisplayConfig {
  const WidgetDisplayConfig({
    this.themeColor = '',
    this.buttonLabel = '',
    this.welcome = const {},
    this.position = '',
    this.autoExpand = false,
    this.title = '',
  });

  final String themeColor;
  final String buttonLabel;

  /// 按语言存欢迎语（key 为 locale 代码，如 "en_US"/"zh_CN"），访客侧按其
  /// 浏览器语言选取其中一份下发；与 [LocaleService.supportedLocales] 对齐。
  final Map<String, String> welcome;
  final String position;
  final bool autoExpand;
  final String title;

  factory WidgetDisplayConfig.fromJson(Map<String, dynamic> json) {
    final rawWelcome = json['welcome'];
    return WidgetDisplayConfig(
      themeColor: (json['theme_color'] ?? '').toString(),
      buttonLabel: (json['button_label'] ?? '').toString(),
      welcome: rawWelcome is Map
          ? rawWelcome.map((k, v) => MapEntry(k.toString(), v.toString()))
          : const {},
      position: (json['position'] ?? '').toString(),
      autoExpand: json['auto_expand'] == true,
      title: (json['title'] ?? '').toString(),
    );
  }

  Map<String, dynamic> toJson() => {
    if (themeColor.isNotEmpty) 'theme_color': themeColor,
    if (buttonLabel.isNotEmpty) 'button_label': buttonLabel,
    if (welcome.isNotEmpty) 'welcome': welcome,
    if (position.isNotEmpty) 'position': position,
    if (autoExpand) 'auto_expand': autoExpand,
    if (title.isNotEmpty) 'title': title,
  };

  WidgetDisplayConfig copyWith({
    String? themeColor,
    String? buttonLabel,
    Map<String, String>? welcome,
    String? position,
    bool? autoExpand,
    String? title,
  }) => WidgetDisplayConfig(
    themeColor: themeColor ?? this.themeColor,
    buttonLabel: buttonLabel ?? this.buttonLabel,
    welcome: welcome ?? this.welcome,
    position: position ?? this.position,
    autoExpand: autoExpand ?? this.autoExpand,
    title: title ?? this.title,
  );
}

class WidgetSiteModel {
  const WidgetSiteModel({
    required this.id,
    required this.siteKey,
    required this.siteName,
    required this.allowedOrigins,
    required this.status,
    required this.createdAt,
    required this.updatedAt,
    this.displayConfig = const WidgetDisplayConfig(),
  });

  final String id;
  final String siteKey;
  final String siteName;
  final List<String> allowedOrigins;
  final WidgetDisplayConfig displayConfig;
  final int status;
  final int createdAt;
  final int updatedAt;

  bool get isActive => status == 1;

  factory WidgetSiteModel.fromJson(Map<String, dynamic> json) {
    final rawOrigins = json['allowed_origins'];
    final origins = <String>[
      if (rawOrigins is List)
        for (final item in rawOrigins) item?.toString().trim() ?? '',
    ].where((e) => e.isNotEmpty).toList(growable: false);
    final rawCfg = json['display_config'];
    return WidgetSiteModel(
      id: (json['id'] ?? '').toString().trim(),
      siteKey: (json['site_key'] ?? '').toString().trim(),
      siteName: (json['site_name'] ?? '').toString().trim(),
      allowedOrigins: origins,
      displayConfig: rawCfg is Map<String, dynamic>
          ? WidgetDisplayConfig.fromJson(rawCfg)
          : const WidgetDisplayConfig(),
      status: _toStaticInt(json['status']),
      createdAt: _toStaticInt(json['created_at']),
      updatedAt: _toStaticInt(json['updated_at']),
    );
  }

  static int _toStaticInt(dynamic v) {
    if (v is int) return v;
    if (v is num) return v.toInt();
    return int.tryParse(v?.toString() ?? '') ?? 0;
  }
}

class WidgetSiteListResult {
  const WidgetSiteListResult({
    this.items = const <WidgetSiteModel>[],
    this.total = 0,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final List<WidgetSiteModel> items;
  final int total;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;

  bool get success => code == 0;
}

class WidgetSiteDetailResult {
  const WidgetSiteDetailResult({
    this.site,
    this.loaderUrl = '',
    this.embedCode = '',
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final WidgetSiteModel? site;
  final String loaderUrl;
  final String embedCode;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;

  bool get success => code == 0 && site != null;
}

class WidgetSiteCreateResult {
  const WidgetSiteCreateResult({
    this.site,
    this.siteSecret = '',
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final WidgetSiteModel? site;
  final String siteSecret;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;

  bool get success => code == 0 && site != null;
}

class WidgetSiteRotateSecretResult {
  const WidgetSiteRotateSecretResult({
    this.siteId = '',
    this.siteKey = '',
    this.siteSecret = '',
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final String siteId;
  final String siteKey;
  final String siteSecret;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;

  bool get success => code == 0;
}

class SessionSnapshot {
  const SessionSnapshot({
    required this.sessionId,
    required this.title,
    required this.type,
    required this.peerId,
    required this.peerType,
    required this.peerNickname,
    required this.peerUsername,
    required this.updatedAt,
    required this.unreadCount,
    required this.lastMessage,
    this.isPinned = false,
    this.pinnedAt = 0,
    this.isMuted = false,
    this.friendIsPinned = false,
    this.friendPinnedAt = 0,
    this.friendIsMuted = false,
    this.isVisitor = false,
  });

  final String sessionId;
  final String title;
  final String type;
  final String peerId;
  final int peerType;
  final String peerNickname;
  final String peerUsername;
  final int updatedAt;
  final int unreadCount;
  final String lastMessage;
  final bool isPinned;
  final int pinnedAt;
  final bool isMuted;
  final bool friendIsPinned;
  final int friendPinnedAt;
  final bool friendIsMuted;
  final bool isVisitor;
}

class SessionMessageHistoryResult {
  const SessionMessageHistoryResult({
    this.messages = const <Map<String, dynamic>>[],
    this.hasMore = false,
    this.nextBeforeMsgId = '',
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
    this.networkError = false,
  });

  final List<Map<String, dynamic>> messages;
  final bool hasMore;
  final String nextBeforeMsgId;
  final int code;
  final int httpStatus;
  final String message;
  final bool networkError;

  bool get success => code == 0;
}

class SessionSnapshotFetchResult {
  const SessionSnapshotFetchResult({
    required this.snapshots,
    required this.success,
    this.hasMore = false,
    this.nextOffset = 0,
    this.httpStatus = 200,
    this.rateLimited = false,
    this.networkError = false,
    this.cursor = 0,
  });

  final List<SessionSnapshot> snapshots;
  final bool success;
  final bool hasMore;
  final int nextOffset;
  final int httpStatus;
  final bool rateLimited;
  final bool networkError;
  // 服务端处理时刻（unix 秒）。客户端据此作为后续增量 /sessions/sync 的 since 起点。
  final int cursor;
}

/// /sessions/sync 增量同步结果：snapshots 为 since 之后有更新的会话；
/// deletedSessionIds 为 since 之后被移出的会话（退群 / 被踢 / 群解散），客户端据此清本地；
/// cursor 为服务端处理时刻，客户端下次以它作为 since 续拉。
class SessionSyncFetchResult {
  const SessionSyncFetchResult({
    required this.snapshots,
    required this.deletedSessionIds,
    required this.success,
    this.cursor = 0,
    this.hasMore = false,
    this.httpStatus = 200,
    this.rateLimited = false,
    this.networkError = false,
  });

  final List<SessionSnapshot> snapshots;
  final List<String> deletedSessionIds;
  final bool success;
  final int cursor;
  final bool hasMore;
  final int httpStatus;
  final bool rateLimited;
  final bool networkError;
}

class _SessionDetailRetryGuard {
  const _SessionDetailRetryGuard({
    required this.result,
    required this.retryAfterMs,
  });

  final SessionDetailResult result;
  final int retryAfterMs;
}

class _SessionDetailSuccessCache {
  const _SessionDetailSuccessCache({
    required this.result,
    required this.expiresAtMs,
  });

  final SessionDetailResult result;
  final int expiresAtMs;
}

class SessionService extends GetxService {
  SessionService();

  @visibleForTesting
  SessionService.forTest(Dio dio) {
    _dio = dio;
    _initialized = true;
    _attachHttpTimingInterceptor(_dio);
  }

  late final Dio _dio;
  bool _initialized = false;
  static const Duration _sessionDetailRequestInterval = Duration(
    milliseconds: 120,
  );
  static const Duration _sessionDetailRateLimitBackoff = Duration(seconds: 5);
  static const Duration _sessionDetailNetworkBackoff = Duration(seconds: 2);
  static const Duration _sessionDetailFailureBackoff = Duration(seconds: 1);
  static const Duration _sessionDetailCacheTtl = Duration(minutes: 3);

  Future<void> _sessionDetailQueue = Future<void>.value();
  final Map<String, Future<SessionDetailResult>> _sessionDetailInflight =
      <String, Future<SessionDetailResult>>{};
  final Map<String, _SessionDetailRetryGuard> _sessionDetailRetryGuards =
      <String, _SessionDetailRetryGuard>{};
  final Map<String, _SessionDetailSuccessCache> _sessionDetailSuccessCache =
      <String, _SessionDetailSuccessCache>{};

  void invalidateSessionDetailCache(String sessionId) {
    _sessionDetailSuccessCache.remove(sessionId.trim());
  }

  void clearSessionDetailCache() {
    _sessionDetailSuccessCache.clear();
  }

  Future<SessionService> init() async {
    final authService = Get.find<AuthService>();
    _dio = Dio(
      BaseOptions(
        baseUrl: AppRuntimeEndpoints.apiBaseUrl,
        connectTimeout: const Duration(seconds: 10),
        receiveTimeout: const Duration(seconds: 10),
      ),
    );
    _attachHttpTimingInterceptor(_dio);
    authService.attachAuthInterceptor(_dio);
    _initialized = true;
    return this;
  }

  bool get isInitialized => _initialized;

  static const _httpTimingStartedAtKey = 'grix_http_timing_started_at_ms';

  void _attachHttpTimingInterceptor(Dio dio) {
    dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          if (_shouldLogHttpTiming(options)) {
            options.extra[_httpTimingStartedAtKey] =
                DateTime.now().millisecondsSinceEpoch;
          }
          handler.next(options);
        },
        onResponse: (response, handler) {
          _logHttpTiming(response.requestOptions, response.statusCode ?? 0);
          handler.next(response);
        },
        onError: (error, handler) {
          _logHttpTiming(
            error.requestOptions,
            error.response?.statusCode ?? 0,
            error: true,
          );
          handler.next(error);
        },
      ),
    );
  }

  bool _shouldLogHttpTiming(RequestOptions options) {
    final path = options.path.trim();
    return path == '/sessions/create' ||
        path == '/sessions/open_latest' ||
        path == '/sessions/list' ||
        path == '/sessions/sync' ||
        path == '/sessions/detail' ||
        path == '/sessions/conversations' ||
        path == '/sessions/conversation_threads' ||
        path == '/messages/history';
  }

  void _logHttpTiming(
    RequestOptions options,
    int statusCode, {
    bool error = false,
  }) {
    final startedAtMs = _toInt(options.extra[_httpTimingStartedAtKey]);
    if (startedAtMs <= 0) {
      return;
    }
    final elapsedMs = DateTime.now().millisecondsSinceEpoch - startedAtMs;
    final method = options.method.toUpperCase();
    final path = options.path.trim();
    final fields = <String, Object?>{
      'method': method,
      'path': path,
      'status': statusCode,
      'elapsed_ms': elapsedMs,
      'error': error,
      if (options.queryParameters.isNotEmpty)
        'query_keys': options.queryParameters.keys.join(','),
    };
    debugPrint(
      '[SessionServiceHTTP] ${fields.entries.map((entry) => '${entry.key}=${entry.value}').join(' ')}',
    );
    Sentry.addBreadcrumb(
      Breadcrumb(
        category: 'session_service_http',
        message: '$method $path',
        data: fields,
        level: error ? SentryLevel.warning : SentryLevel.info,
      ),
    );
  }

  String get _unknownError => 'common_unknown_error'.tr;

  late final _SessionServiceConversationApi _conversationApi =
      _SessionServiceConversationApi(this);

  _SessionServiceBasicApi get _basicApi => _SessionServiceBasicApi(this);
  _SessionServiceSnapshotApi get _snapshotApi =>
      _SessionServiceSnapshotApi(this);
  _SessionServiceGroupManageApi get _groupManageApi =>
      _SessionServiceGroupManageApi(this);

  Future<String?> createSession(String peerId, int peerType) async {
    try {
      return await _basicApi.createSession(peerId, peerType);
    } finally {
      _conversationApi.clearFirstPageCache();
    }
  }

  Future<String?> openLatestSession(String peerId, int peerType) async {
    try {
      return await _basicApi.openLatestSession(peerId, peerType);
    } finally {
      _conversationApi.clearFirstPageCache();
    }
  }

  Future<String?> renameSession(String sessionId, String title) =>
      _basicApi.renameSession(sessionId, title);

  Future<SessionRenameResult> renameSessionResult(
    String sessionId,
    String title,
  ) => _basicApi.renameSessionResult(sessionId, title);

  Future<SessionMemberNicknameResult> setGroupNicknameResult(
    String sessionId,
    String nickname,
  ) => _basicApi.setGroupNicknameResult(sessionId, nickname);

  Future<SessionPinResult> setSessionPinnedResult(
    String sessionId, {
    required bool isPinned,
  }) async {
    final result = await _basicApi.setSessionPinnedResult(
      sessionId,
      isPinned: isPinned,
    );
    if (result.code == 0) {
      // Pin state feeds the conversation list; drop the 5s first-page cache
      // so a refresh within the TTL cannot write the stale pin state back.
      _conversationApi.clearFirstPageCache();
    }
    return result;
  }

  /// Invalidates the 5s conversation first-page cache. Called after any
  /// pin/unpin mutation (including friend-level pins via FriendService) so a
  /// refresh within the TTL cannot write stale pin state back.
  void invalidateConversationFirstPageCache() {
    _conversationApi.clearFirstPageCache();
  }

  Future<SessionMuteResult> setSessionMutedResult(
    String sessionId, {
    required bool isMuted,
  }) => _basicApi.setSessionMutedResult(sessionId, isMuted: isMuted);

  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) => _snapshotApi.fetchSessionSnapshotsResult(
    limit: limit,
    maxPages: maxPages,
  );

  Future<SessionSnapshotFetchResult> fetchSessionSnapshotPageResult({
    int limit = 40,
    int offset = 0,
  }) =>
      _snapshotApi.fetchSessionSnapshotPageResult(limit: limit, offset: offset);

  Future<SessionSyncFetchResult> fetchSessionSyncResult({
    required int since,
    int limit = 200,
  }) => _snapshotApi.fetchSessionSyncResult(since: since, limit: limit);

  Future<List<SessionSnapshot>> fetchSessionSnapshots({
    int limit = 200,
    int maxPages = 5,
  }) => _snapshotApi.fetchSessionSnapshots(limit: limit, maxPages: maxPages);

  Future<ConversationPageResult> fetchConversationPage({
    int limit = 30,
    String cursor = '',
  }) => _conversationApi.fetchConversationPage(limit: limit, cursor: cursor);

  Future<ConversationThreadPageResult> fetchConversationThreads({
    required String groupKey,
    int limit = 20,
    String cursor = '',
  }) => _conversationApi.fetchConversationThreads(
    groupKey: groupKey,
    limit: limit,
    cursor: cursor,
  );

  Future<Map<String, String>> fetchSessionTypes({
    int limit = 200,
    int maxPages = 5,
  }) => _snapshotApi.fetchSessionTypes(limit: limit, maxPages: maxPages);

  Future<Map<String, dynamic>?> fetchSessionDetail(String sessionId) =>
      _snapshotApi.fetchSessionDetail(sessionId);

  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) =>
      _snapshotApi.fetchSessionDetailResult(sessionId);

  Future<Map<String, dynamic>?> addGroupMembers({
    required String sessionId,
    required List<String> memberIds,
    List<int> memberTypes = const [],
  }) => _groupManageApi.addGroupMembers(
    sessionId: sessionId,
    memberIds: memberIds,
    memberTypes: memberTypes,
  );

  Future<SessionAddMembersResult> addGroupMembersResult({
    required String sessionId,
    required List<String> memberIds,
    List<int> memberTypes = const [],
  }) => _groupManageApi.addGroupMembersResult(
    sessionId: sessionId,
    memberIds: memberIds,
    memberTypes: memberTypes,
  );

  Future<SessionInviteSettingResult> updateGroupInviteSettingResult({
    required String sessionId,
    required bool allowMemberInvite,
  }) => _groupManageApi.updateGroupInviteSettingResult(
    sessionId: sessionId,
    allowMemberInvite: allowMemberInvite,
  );

  Future<SessionAllMembersMutedResult> updateGroupAllMembersMutedResult({
    required String sessionId,
    required bool allMembersMuted,
  }) => _groupManageApi.updateGroupAllMembersMutedResult(
    sessionId: sessionId,
    allMembersMuted: allMembersMuted,
  );

  Future<SessionMemberSpeakingResult> updateGroupMemberSpeakingResult({
    required String sessionId,
    required String memberId,
    int memberType = 1,
    bool? isSpeakMuted,
    bool? canSpeakWhenAllMuted,
  }) => _groupManageApi.updateGroupMemberSpeakingResult(
    sessionId: sessionId,
    memberId: memberId,
    memberType: memberType,
    isSpeakMuted: isSpeakMuted,
    canSpeakWhenAllMuted: canSpeakWhenAllMuted,
  );

  Future<SessionMemberAgentReceiveResult> updateGroupMemberAgentReceiveResult({
    required String sessionId,
    required String memberId,
    required int agentReceiveMode,
    int? agentReceiveBacklogCount,
    int memberType = 1,
  }) => _groupManageApi.updateGroupMemberAgentReceiveResult(
    sessionId: sessionId,
    memberId: memberId,
    memberType: memberType,
    agentReceiveMode: agentReceiveMode,
    agentReceiveBacklogCount: agentReceiveBacklogCount,
  );

  Future<Map<String, dynamic>?> removeGroupMembers({
    required String sessionId,
    required List<String> memberIds,
    List<int> memberTypes = const [],
  }) => _groupManageApi.removeGroupMembers(
    sessionId: sessionId,
    memberIds: memberIds,
    memberTypes: memberTypes,
  );

  Future<SessionLeaveResult> leaveGroupResult({required String sessionId}) =>
      _groupManageApi.leaveGroupResult(sessionId: sessionId);

  Future<Map<String, dynamic>?> updateGroupMemberRole({
    required String sessionId,
    required String memberId,
    int memberType = 1,
    required int role,
  }) => _groupManageApi.updateGroupMemberRole(
    sessionId: sessionId,
    memberId: memberId,
    memberType: memberType,
    role: role,
  );

  Future<Map<String, dynamic>?> transferGroupOwner({
    required String sessionId,
    required String memberId,
  }) => _groupManageApi.transferGroupOwner(
    sessionId: sessionId,
    memberId: memberId,
  );

  Future<Map<String, dynamic>?> dissolveGroup({required String sessionId}) =>
      _groupManageApi.dissolveGroup(sessionId: sessionId);

  Future<Map<String, dynamic>?> convertToGroup({
    required String sessionId,
    String name = '',
  }) async {
    try {
      return await _groupManageApi.convertToGroup(
        sessionId: sessionId,
        name: name,
      );
    } finally {
      _conversationApi.clearFirstPageCache();
    }
  }

  Future<String?> createGroupSession({
    required String name,
    List<String> memberIds = const [],
    List<int> memberTypes = const [],
  }) async {
    try {
      return await _basicApi.createGroupSession(
        name: name,
        memberIds: memberIds,
        memberTypes: memberTypes,
      );
    } finally {
      _conversationApi.clearFirstPageCache();
    }
  }

  Future<bool> deleteMessage({
    required String sessionId,
    required String msgId,
  }) => _basicApi.deleteMessage(sessionId: sessionId, msgId: msgId);

  Future<WidgetSessionModerationResult> closeVisitorSession(String sessionId) =>
      _basicApi.closeVisitorSession(sessionId);

  Future<WidgetSessionModerationResult> banVisitorSession(String sessionId) =>
      _basicApi.banVisitorSession(sessionId);

  Future<WidgetSiteListResult> fetchWidgetSites({
    int status = 0,
    int limit = 50,
    int offset = 0,
  }) =>
      _basicApi.fetchWidgetSites(status: status, limit: limit, offset: offset);

  Future<WidgetSiteCreateResult> createWidgetSite({
    required String siteName,
    required List<String> allowedOrigins,
    WidgetDisplayConfig? displayConfig,
  }) => _basicApi.createWidgetSite(
    siteName: siteName,
    allowedOrigins: allowedOrigins,
    displayConfig: displayConfig,
  );

  Future<WidgetSiteDetailResult> fetchWidgetSiteDetail(String siteId) =>
      _basicApi.fetchWidgetSiteDetail(siteId);

  Future<WidgetSessionModerationResult> updateWidgetSite({
    required String id,
    required String siteName,
    required List<String> allowedOrigins,
    required int status,
    WidgetDisplayConfig? displayConfig,
  }) => _basicApi.updateWidgetSite(
    id: id,
    siteName: siteName,
    allowedOrigins: allowedOrigins,
    status: status,
    displayConfig: displayConfig,
  );

  Future<WidgetSiteRotateSecretResult> rotateWidgetSiteSecret(String siteId) =>
      _basicApi.rotateWidgetSiteSecret(siteId);

  Future<WidgetSessionModerationResult> deleteWidgetSite(String siteId) =>
      _basicApi.deleteWidgetSite(siteId);

  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) => _basicApi.fetchMessageHistoryResult(
    sessionId: sessionId,
    beforeMsgId: beforeMsgId,
    limit: limit,
  );

  int _toInt(dynamic v) {
    if (v is int) return v;
    if (v is num) return v.toInt();
    if (v is String) return int.tryParse(v.trim()) ?? 0;
    return int.tryParse(v?.toString() ?? '') ?? 0;
  }

  bool _toBool(dynamic v) {
    if (v is bool) return v;
    if (v is num) return v != 0;
    final normalized = v?.toString().trim().toLowerCase() ?? '';
    return normalized == 'true' || normalized == '1';
  }

  String _normalizeSessionType(dynamic raw) {
    final iv = _toInt(raw);
    if (iv == 2) return 'group';
    if (iv == 1) return 'private';

    final text = raw?.toString().trim().toLowerCase() ?? '';
    if (text == 'group' || text == '2') return 'group';
    return 'private';
  }

  int _normalizeTimestamp(int ts) {
    if (ts > 0 && ts < 10000000000) {
      return ts * 1000;
    }
    return ts;
  }
}
