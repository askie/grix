part of 'session_service.dart';

class _SessionServiceConversationApi {
  _SessionServiceConversationApi(this._service);

  static const Duration _firstPageCacheTtl = Duration(seconds: 5);

  final SessionService _service;
  Future<ConversationPageResult>? _firstPageInFlight;
  ConversationPageResult? _firstPageCache;
  int _firstPageCacheAtMs = 0;
  int _firstPageCacheLimit = 0;
  int _firstPageCacheEpoch = 0;

  Dio get _dio => _service._dio;
  String get _unknownError => _service._unknownError;

  void clearFirstPageCache() {
    _firstPageCacheEpoch++;
    _firstPageInFlight = null;
    _firstPageCache = null;
    _firstPageCacheAtMs = 0;
    _firstPageCacheLimit = 0;
  }

  Future<ConversationPageResult> fetchConversationPage({
    int limit = 30,
    String cursor = '',
  }) async {
    final normalizedLimit = limit <= 0 ? 30 : limit;
    final normalizedCursor = cursor.trim();
    if (normalizedCursor.isEmpty) {
      final nowMs = DateTime.now().millisecondsSinceEpoch;
      final cached = _firstPageCache;
      if (cached != null &&
          _firstPageCacheLimit == normalizedLimit &&
          nowMs - _firstPageCacheAtMs < _firstPageCacheTtl.inMilliseconds) {
        return cached;
      }
      final inFlight = _firstPageInFlight;
      if (inFlight != null && _firstPageCacheLimit == normalizedLimit) {
        return inFlight;
      }
      late final Future<ConversationPageResult> future;
      final requestEpoch = _firstPageCacheEpoch;
      _firstPageCacheLimit = normalizedLimit;
      future =
          _fetchConversationPageFromNetwork(
                limit: normalizedLimit,
                cursor: normalizedCursor,
              )
              .then((result) {
                if (result.success &&
                    identical(_firstPageInFlight, future) &&
                    requestEpoch == _firstPageCacheEpoch &&
                    _firstPageCacheLimit == normalizedLimit) {
                  _firstPageCache = result;
                  _firstPageCacheAtMs = DateTime.now().millisecondsSinceEpoch;
                }
                return result;
              })
              .whenComplete(() {
                if (identical(_firstPageInFlight, future)) {
                  _firstPageInFlight = null;
                }
              });
      _firstPageInFlight = future;
      return future;
    }
    return _fetchConversationPageFromNetwork(
      limit: normalizedLimit,
      cursor: normalizedCursor,
    );
  }

  Future<ConversationPageResult> _fetchConversationPageFromNetwork({
    required int limit,
    required String cursor,
  }) async {
    try {
      final resp = await _dio.get(
        '/sessions/conversations',
        queryParameters: {
          'limit': limit,
          if (cursor.isNotEmpty) 'cursor': cursor,
        },
      );
      final httpStatus = resp.statusCode ?? 0;
      if (httpStatus != 200 || resp.data['code'] != 0) {
        final msg = resp.data['msg'] ?? _unknownError;
        debugPrint('Fetch conversations failed: $msg');
        return ConversationPageResult(
          success: false,
          httpStatus: httpStatus,
          rateLimited: httpStatus == 429,
        );
      }
      final data = resp.data['data'];
      if (data is! Map) {
        return ConversationPageResult(success: false, httpStatus: httpStatus);
      }
      final rawList = data['list'];
      if (rawList is! List) {
        return ConversationPageResult(success: false, httpStatus: httpStatus);
      }
      final items = <ConversationSummaryModel>[];
      for (final raw in rawList) {
        if (raw is! Map) continue;
        final json = Map<String, dynamic>.from(raw);
        final item = ConversationSummaryModel.fromJson(json);
        if (item.groupKey.trim().isEmpty ||
            item.latestSessionId.trim().isEmpty) {
          continue;
        }
        items.add(item);
      }
      return ConversationPageResult(
        items: items,
        hasMore: _service._toBool(data['has_more']),
        nextCursor: data['next_cursor']?.toString() ?? '',
        httpStatus: httpStatus,
      );
    } on DioException catch (e) {
      return ConversationPageResult(
        success: false,
        httpStatus: e.response?.statusCode ?? 0,
        rateLimited: e.response?.statusCode == 429,
        networkError: e.response == null,
      );
    } catch (e) {
      debugPrint('Fetch conversations error: $e');
      return const ConversationPageResult(success: false, httpStatus: 0);
    }
  }

  Future<ConversationThreadPageResult> fetchConversationThreads({
    required String groupKey,
    int limit = 20,
    String cursor = '',
  }) async {
    final normalizedGroupKey = groupKey.trim();
    if (normalizedGroupKey.isEmpty) {
      return const ConversationThreadPageResult(success: false);
    }
    final normalizedLimit = limit <= 0 ? 20 : limit;
    try {
      final resp = await _dio.get(
        '/sessions/conversation_threads',
        queryParameters: {
          'group_key': normalizedGroupKey,
          'limit': normalizedLimit,
          if (cursor.trim().isNotEmpty) 'cursor': cursor.trim(),
        },
      );
      final httpStatus = resp.statusCode ?? 0;
      if (httpStatus != 200 || resp.data['code'] != 0) {
        final msg = resp.data['msg'] ?? _unknownError;
        debugPrint('Fetch conversation threads failed: $msg');
        return ConversationThreadPageResult(
          groupKey: normalizedGroupKey,
          success: false,
          httpStatus: httpStatus,
          rateLimited: httpStatus == 429,
        );
      }
      final data = resp.data['data'];
      if (data is! Map) {
        return ConversationThreadPageResult(
          groupKey: normalizedGroupKey,
          success: false,
          httpStatus: httpStatus,
        );
      }
      final rawList = data['list'];
      if (rawList is! List) {
        return ConversationThreadPageResult(
          groupKey: normalizedGroupKey,
          success: false,
          httpStatus: httpStatus,
        );
      }
      final sessions = <SessionModel>[];
      for (final raw in rawList) {
        if (raw is! Map) continue;
        final json = Map<String, dynamic>.from(raw);
        final sessionId = json['session_id']?.toString().trim() ?? '';
        if (sessionId.isEmpty) continue;
        final sessionType = _service._normalizeSessionType(
          json['session_type'],
        );
        final updatedAt = _service._normalizeTimestamp(
          _service._toInt(json['updated_at']),
        );
        // 展示时间取「最后一条可见消息」的时间，与点进会话看到的最后一条对齐；
        // 无可见消息(0)时回退到会话活跃时间 updatedAt。
        final lastMsgTime = _service._normalizeTimestamp(
          _service._toInt(json['last_msg_time']),
        );
        final peer = json['peer'];
        final peerMap = peer is Map ? peer : const <String, dynamic>{};
        sessions.add(
          SessionModel(
            sessionId: sessionId,
            title: json['title']?.toString() ?? '',
            type: sessionType,
            peerId: peerMap['id']?.toString() ?? '',
            peerType: _service._toInt(peerMap['type']),
            peerNickname: peerMap['nickname']?.toString() ?? '',
            peerUsername: peerMap['username']?.toString() ?? '',
            updatedAt: updatedAt,
            isPinned: _service._toBool(json['is_pinned']),
            isMuted: _service._toBool(json['is_muted']),
            pinnedAt: _service._normalizeTimestamp(
              _service._toInt(json['pinned_at']),
            ),
            friendIsPinned: _service._toBool(json['friend_is_pinned']),
            friendPinnedAt: _service._normalizeTimestamp(
              _service._toInt(json['friend_pinned_at']),
            ),
            isVisitor: _service._toBool(json['is_visitor']),
            unreadCount: _service._toInt(json['unread']),
            lastMessage: json['last_msg']?.toString() ?? '',
            lastMessageTime: lastMsgTime > 0 ? lastMsgTime : updatedAt,
          ),
        );
      }
      return ConversationThreadPageResult(
        groupKey: data['group_key']?.toString() ?? normalizedGroupKey,
        sessions: sessions,
        hasMore: _service._toBool(data['has_more']),
        nextCursor: data['next_cursor']?.toString() ?? '',
        httpStatus: httpStatus,
      );
    } on DioException catch (e) {
      return ConversationThreadPageResult(
        groupKey: normalizedGroupKey,
        success: false,
        httpStatus: e.response?.statusCode ?? 0,
        rateLimited: e.response?.statusCode == 429,
        networkError: e.response == null,
      );
    } catch (e) {
      debugPrint('Fetch conversation threads error: $e');
      return ConversationThreadPageResult(
        groupKey: normalizedGroupKey,
        success: false,
        httpStatus: 0,
      );
    }
  }
}
