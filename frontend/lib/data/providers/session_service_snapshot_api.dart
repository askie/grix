part of 'session_service.dart';

class _SessionServiceSnapshotApi {
  _SessionServiceSnapshotApi(this._service);

  final SessionService _service;

  Dio get _dio => _service._dio;
  String get _unknownError => _service._unknownError;
  Future<void> get _sessionDetailQueue => _service._sessionDetailQueue;
  Map<String, Future<SessionDetailResult>> get _sessionDetailInflight =>
      _service._sessionDetailInflight;
  Map<String, _SessionDetailRetryGuard> get _sessionDetailRetryGuards =>
      _service._sessionDetailRetryGuards;
  Map<String, _SessionDetailSuccessCache> get _sessionDetailSuccessCache =>
      _service._sessionDetailSuccessCache;
  Duration get _sessionDetailRequestInterval =>
      SessionService._sessionDetailRequestInterval;
  Duration get _sessionDetailCacheTtl => SessionService._sessionDetailCacheTtl;
  Duration get _sessionDetailRateLimitBackoff =>
      SessionService._sessionDetailRateLimitBackoff;
  Duration get _sessionDetailNetworkBackoff =>
      SessionService._sessionDetailNetworkBackoff;
  Duration get _sessionDetailFailureBackoff =>
      SessionService._sessionDetailFailureBackoff;
  int _toInt(dynamic v) => _service._toInt(v);
  bool _toBool(dynamic v) => _service._toBool(v);
  String _normalizeSessionType(dynamic raw) =>
      _service._normalizeSessionType(raw);
  int _normalizeTimestamp(int ts) => _service._normalizeTimestamp(ts);

  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    final snapshots = <SessionSnapshot>[];
    var offset = 0;
    var page = 0;
    var hasMore = true;
    var success = true;

    var httpStatus = 200;
    var rateLimited = false;
    var networkError = false;
    // 用首页 cursor 作为基线游标：拉取期间发生的新变化下次增量 sync 仍会因
    // since ≤ 首页 cursor 而被重新拉到，避免漏掉。
    var cursor = 0;

    while (hasMore && page < maxPages) {
      page++;
      final pageResult = await fetchSessionSnapshotPageResult(
        limit: limit,
        offset: offset,
      );
      httpStatus = pageResult.httpStatus;
      rateLimited = pageResult.rateLimited;
      networkError = pageResult.networkError;
      if (!pageResult.success) {
        success = false;
        break;
      }
      if (page == 1) {
        cursor = pageResult.cursor;
      }
      snapshots.addAll(pageResult.snapshots);
      hasMore = pageResult.hasMore;
      offset = pageResult.nextOffset;
      if (pageResult.snapshots.isEmpty) break;
    }
    // 注意：hasMore 不再降级为失败。会话数超过 limit*maxPages 的账号永远拉不完，
    // 若把「没拉完」当失败，增量同步的基线游标就永远建立不起来，每次刷新都退回
    // 全量拉 maxPages 页。是否拉完由调用方读 hasMore 自行判断——只有需要整表
    // 对账（删除本地多余会话）的路径才必须要求 hasMore == false。
    return SessionSnapshotFetchResult(
      snapshots: snapshots,
      success: success,
      hasMore: hasMore,
      nextOffset: offset,
      httpStatus: httpStatus,
      rateLimited: rateLimited,
      networkError: networkError,
      cursor: cursor,
    );
  }

  Future<SessionSnapshotFetchResult> fetchSessionSnapshotPageResult({
    int limit = 40,
    int offset = 0,
  }) async {
    final snapshots = <SessionSnapshot>[];
    final normalizedLimit = limit <= 0 ? 40 : limit;
    final normalizedOffset = offset < 0 ? 0 : offset;
    try {
      final resp = await _dio.get(
        '/sessions/list',
        queryParameters: {'limit': normalizedLimit, 'offset': normalizedOffset},
      );
      final httpStatus = resp.statusCode ?? 0;
      if (httpStatus != 200 || resp.data['code'] != 0) {
        final msg = resp.data['msg'] ?? _unknownError;
        debugPrint('Fetch session types failed: $msg');
        return SessionSnapshotFetchResult(
          snapshots: const [],
          success: false,
          httpStatus: httpStatus,
          rateLimited: httpStatus == 429,
          nextOffset: normalizedOffset,
        );
      }

      final data = resp.data['data'];
      if (data is! Map) {
        return SessionSnapshotFetchResult(
          snapshots: const [],
          success: false,
          httpStatus: httpStatus,
          nextOffset: normalizedOffset,
        );
      }
      final rawList = data['list'];
      if (rawList is! List) {
        return SessionSnapshotFetchResult(
          snapshots: const [],
          success: false,
          httpStatus: httpStatus,
          nextOffset: normalizedOffset,
        );
      }

      for (final item in rawList) {
        final snapshot = _parseSnapshotItem(item);
        if (snapshot != null) {
          snapshots.add(snapshot);
        }
      }

      return SessionSnapshotFetchResult(
        snapshots: snapshots,
        success: true,
        hasMore: data['has_more'] == true,
        nextOffset: normalizedOffset + rawList.length,
        httpStatus: httpStatus,
        cursor: _toInt(data['cursor']),
      );
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      final httpStatus = e.response?.statusCode ?? 0;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Fetch session types error: $errMsg');
      return SessionSnapshotFetchResult(
        snapshots: const [],
        success: false,
        httpStatus: httpStatus,
        rateLimited: httpStatus == 429,
        networkError: e.response == null,
        nextOffset: normalizedOffset,
      );
    } catch (e) {
      debugPrint('Fetch session snapshots unexpected error: $e');
      return SessionSnapshotFetchResult(
        snapshots: const [],
        success: false,
        networkError: true,
        nextOffset: normalizedOffset,
      );
    }
  }

  SessionSnapshot? _parseSnapshotItem(dynamic item) {
    if (item is! Map) return null;
    final sid = item['session_id']?.toString().trim() ?? '';
    if (sid.isEmpty) return null;
    final sessionType = _normalizeSessionType(item['session_type']);
    final updatedAt = _normalizeTimestamp(_toInt(item['updated_at']));
    final unread = _toInt(item['unread']);
    final lastMsg = item['last_msg']?.toString() ?? '';
    final isPinned = _toBool(item['is_pinned']);
    final pinnedAt = _normalizeTimestamp(_toInt(item['pinned_at']));
    final isMuted = _toBool(item['is_muted']);
    final friendIsPinned = _toBool(item['friend_is_pinned']);
    final friendPinnedAt = _normalizeTimestamp(_toInt(item['friend_pinned_at']));
    final friendIsMuted = _toBool(item['friend_is_muted']);
    final isVisitor = _toBool(item['is_visitor']);
    String peerId = '';
    int peerType = 0;
    String peerNickname = '';
    String peerUsername = '';
    final rawTitle = item['title']?.toString().trim() ?? '';
    final peer = item['peer'];
    if (peer is Map) {
      peerId = peer['id']?.toString().trim() ?? '';
      peerType = _toInt(peer['type']);
      peerNickname = peer['nickname']?.toString().trim() ?? '';
      peerUsername = peer['username']?.toString().trim() ?? '';
    }
    return SessionSnapshot(
      sessionId: sid,
      title: rawTitle,
      type: sessionType,
      peerId: peerId,
      peerType: peerType,
      peerNickname: peerNickname,
      peerUsername: peerUsername,
      updatedAt: updatedAt,
      unreadCount: unread < 0 ? 0 : unread,
      lastMessage: lastMsg,
      isPinned: isPinned,
      pinnedAt: pinnedAt,
      isMuted: isMuted,
      friendIsPinned: friendIsPinned,
      friendPinnedAt: friendPinnedAt,
      friendIsMuted: friendIsMuted,
      isVisitor: isVisitor,
    );
  }

  Future<SessionSyncFetchResult> fetchSessionSyncResult({
    required int since,
    int limit = 200,
  }) async {
    final normalizedLimit = limit <= 0 ? 200 : limit;
    final normalizedSince = since < 0 ? 0 : since;
    try {
      final resp = await _dio.get(
        '/sessions/sync',
        queryParameters: {'since': normalizedSince, 'limit': normalizedLimit},
      );
      final httpStatus = resp.statusCode ?? 0;
      if (httpStatus != 200 || resp.data['code'] != 0) {
        final msg = resp.data['msg'] ?? _unknownError;
        debugPrint('Fetch session sync failed: $msg');
        return SessionSyncFetchResult(
          snapshots: const [],
          deletedSessionIds: const [],
          success: false,
          httpStatus: httpStatus,
          rateLimited: httpStatus == 429,
        );
      }
      final data = resp.data['data'];
      if (data is! Map) {
        return SessionSyncFetchResult(
          snapshots: const [],
          deletedSessionIds: const [],
          success: false,
          httpStatus: httpStatus,
        );
      }
      final snapshots = <SessionSnapshot>[];
      final rawList = data['list'];
      if (rawList is List) {
        for (final item in rawList) {
          final snapshot = _parseSnapshotItem(item);
          if (snapshot != null) {
            snapshots.add(snapshot);
          }
        }
      }
      final deletedSessionIds = <String>[];
      final rawDeleted = data['deleted_session_ids'];
      if (rawDeleted is List) {
        for (final id in rawDeleted) {
          final sid = id?.toString().trim() ?? '';
          if (sid.isNotEmpty) {
            deletedSessionIds.add(sid);
          }
        }
      }
      return SessionSyncFetchResult(
        snapshots: snapshots,
        deletedSessionIds: deletedSessionIds,
        success: true,
        cursor: _toInt(data['cursor']),
        hasMore: data['has_more'] == true,
        httpStatus: httpStatus,
      );
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      final httpStatus = e.response?.statusCode ?? 0;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Fetch session sync error: $errMsg');
      return SessionSyncFetchResult(
        snapshots: const [],
        deletedSessionIds: const [],
        success: false,
        httpStatus: httpStatus,
        rateLimited: httpStatus == 429,
        networkError: e.response == null,
      );
    } catch (e) {
      debugPrint('Fetch session sync unexpected error: $e');
      return const SessionSyncFetchResult(
        snapshots: [],
        deletedSessionIds: [],
        success: false,
        networkError: true,
      );
    }
  }

  Future<List<SessionSnapshot>> fetchSessionSnapshots({
    int limit = 200,
    int maxPages = 5,
  }) async {
    final result = await fetchSessionSnapshotsResult(
      limit: limit,
      maxPages: maxPages,
    );
    return result.snapshots;
  }

  Future<Map<String, String>> fetchSessionTypes({
    int limit = 200,
    int maxPages = 5,
  }) async {
    final snapshots = await fetchSessionSnapshots(
      limit: limit,
      maxPages: maxPages,
    );
    final typeMap = <String, String>{};
    for (final item in snapshots) {
      final sid = item.sessionId.trim();
      if (sid.isEmpty) continue;
      typeMap[sid] = item.type;
    }
    return typeMap;
  }

  Future<Map<String, dynamic>?> fetchSessionDetail(String sessionId) async {
    final result = await fetchSessionDetailResult(sessionId);
    return result.data;
  }

  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return SessionDetailResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }

    final cached = _readSessionDetailSuccessCache(sid);
    if (cached != null) {
      return cached;
    }

    final guarded = _readSessionDetailRetryGuard(sid);
    if (guarded != null) {
      return guarded;
    }

    final inflight = _sessionDetailInflight[sid];
    if (inflight != null) {
      return inflight;
    }

    final request =
        _enqueueSessionDetailRequest(
              () => _fetchSessionDetailResultFromApi(sid),
            )
            .then((result) {
              _updateSessionDetailSuccessCache(sid, result);
              _updateSessionDetailRetryGuard(sid, result);
              return result;
            })
            .whenComplete(() {
              _sessionDetailInflight.remove(sid);
            });
    _sessionDetailInflight[sid] = request;
    return request;
  }

  Future<T> _enqueueSessionDetailRequest<T>(Future<T> Function() action) {
    final queued = _sessionDetailQueue.then((_) async {
      try {
        return await action();
      } finally {
        await Future<void>.delayed(_sessionDetailRequestInterval);
      }
    });
    _service._sessionDetailQueue = queued.then<void>(
      (_) {},
      onError: (_, __) {},
    );
    return queued;
  }

  SessionDetailResult? _readSessionDetailSuccessCache(String sessionId) {
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final entry = _sessionDetailSuccessCache[sessionId];
    if (entry == null) return null;
    if (nowMs >= entry.expiresAtMs) {
      _sessionDetailSuccessCache.remove(sessionId);
      return null;
    }
    return entry.result;
  }

  void _updateSessionDetailSuccessCache(
    String sessionId,
    SessionDetailResult result,
  ) {
    if (result.code != 0 || result.data == null) return;
    _sessionDetailSuccessCache[sessionId] = _SessionDetailSuccessCache(
      result: result,
      expiresAtMs:
          DateTime.now().millisecondsSinceEpoch +
          _sessionDetailCacheTtl.inMilliseconds,
    );
  }

  SessionDetailResult? _readSessionDetailRetryGuard(String sessionId) {
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final guard = _sessionDetailRetryGuards[sessionId];
    if (guard == null) {
      return null;
    }
    if (nowMs >= guard.retryAfterMs) {
      _sessionDetailRetryGuards.remove(sessionId);
      return null;
    }
    return guard.result;
  }

  void _updateSessionDetailRetryGuard(
    String sessionId,
    SessionDetailResult result,
  ) {
    if (result.code == 0 && result.data != null) {
      _sessionDetailRetryGuards.remove(sessionId);
      return;
    }

    final backoff = _resolveSessionDetailBackoff(result);
    if (backoff <= Duration.zero) {
      _sessionDetailRetryGuards.remove(sessionId);
      return;
    }

    _sessionDetailRetryGuards[sessionId] = _SessionDetailRetryGuard(
      result: result,
      retryAfterMs:
          DateTime.now().millisecondsSinceEpoch + backoff.inMilliseconds,
    );
  }

  Duration _resolveSessionDetailBackoff(SessionDetailResult result) {
    if (result.httpStatus == 429 || result.code == 10005) {
      return _sessionDetailRateLimitBackoff;
    }
    if (result.networkError) {
      return _sessionDetailNetworkBackoff;
    }
    if (result.code != 0) {
      return _sessionDetailFailureBackoff;
    }
    return Duration.zero;
  }

  Future<SessionDetailResult> _fetchSessionDetailResultFromApi(
    String sessionId,
  ) async {
    try {
      final resp = await _dio.get(
        '/sessions/detail',
        queryParameters: {'session_id': sessionId},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            return SessionDetailResult(
              data: Map<String, dynamic>.from(data),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionDetailResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }
        final msg = body['msg']?.toString() ?? _unknownError;
        debugPrint('Fetch session detail failed: $msg');
        return SessionDetailResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      return SessionDetailResult(
        code: 50001,
        httpStatus: resp.statusCode ?? 0,
        message: _unknownError,
      );
    } on DioException catch (e) {
      int code = 0;
      String errMsg = e.message ?? _unknownError;
      if (e.response != null && e.response?.data is Map) {
        final data = e.response?.data as Map;
        code = _toInt(data['code']);
        errMsg = data['msg']?.toString() ?? errMsg;
      }
      final httpStatus = e.response?.statusCode ?? 0;
      final networkError = e.response == null;
      if (code == 0 && !networkError) {
        if (httpStatus == 403) {
          code = 4003;
        } else if (httpStatus == 404) {
          code = 4004;
        } else {
          code = 50001;
        }
      }
      debugPrint('Fetch session detail error: $errMsg');
      return SessionDetailResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      debugPrint('Fetch session detail unexpected error: $e');
      return SessionDetailResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }
}
