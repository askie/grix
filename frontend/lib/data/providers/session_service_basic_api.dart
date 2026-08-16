part of 'session_service.dart';

class _SessionServiceBasicApi {
  _SessionServiceBasicApi(this._service);

  final SessionService _service;

  Dio get _dio => _service._dio;
  String get _unknownError => _service._unknownError;
  int _toInt(dynamic v) => _service._toInt(v);
  bool _toBool(dynamic v) => _service._toBool(v);
  int _normalizeTimestamp(int ts) => _service._normalizeTimestamp(ts);

  int _normalizeMessageCreatedAt(dynamic raw) {
    if (raw == null) return 0;
    if (raw is int) return _normalizeTimestamp(raw);
    if (raw is num) return _normalizeTimestamp(raw.toInt());

    final text = raw.toString().trim();
    if (text.isEmpty) return 0;

    final parsedInt = int.tryParse(text);
    if (parsedInt != null) {
      return _normalizeTimestamp(parsedInt);
    }
    final parsedTime = DateTime.tryParse(text);
    if (parsedTime != null) {
      return parsedTime.toUtc().millisecondsSinceEpoch;
    }
    return 0;
  }

  Future<String?> createSession(String peerId, int peerType) async {
    try {
      final resp = await _dio.post(
        '/sessions/create',
        data: {'peer_id': peerId, 'peer_type': peerType},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return resp.data['data']['session_id'] as String;
      }
      final msg = resp.data['msg'] ?? _unknownError;
      debugPrint('Create session failed: $msg');
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Create session error: $errMsg');
    } catch (e) {
      debugPrint('Create session unexpected error: $e');
    }
    return null;
  }

  Future<String?> openLatestSession(String peerId, int peerType) async {
    try {
      final resp = await _dio.post(
        '/sessions/open_latest',
        data: {'peer_id': peerId, 'peer_type': peerType},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return resp.data['data']['session_id'] as String;
      }
      final msg = resp.data['msg'] ?? _unknownError;
      debugPrint('Open latest session failed: $msg');
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Open latest session error: $errMsg');
    } catch (e) {
      debugPrint('Open latest session unexpected error: $e');
    }
    return null;
  }

  Future<String?> renameSession(String sessionId, String title) async {
    final result = await renameSessionResult(sessionId, title);
    return result.code == 0 ? result.title : null;
  }

  Future<SessionRenameResult> renameSessionResult(
    String sessionId,
    String title,
  ) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return SessionRenameResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }

    try {
      final resp = await _dio.post(
        '/sessions/rename',
        data: {'session_id': sid, 'title': title},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            return SessionRenameResult(
              title: data['title']?.toString() ?? '',
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionRenameResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }
        final msg = body['msg']?.toString() ?? _unknownError;
        debugPrint('Rename session failed: $msg');
        return SessionRenameResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      return SessionRenameResult(
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
      if (code != 4004 && httpStatus != 404) {
        debugPrint('Rename session error: $errMsg');
      }
      return SessionRenameResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      debugPrint('Rename session unexpected error: $e');
      return SessionRenameResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<SessionMemberNicknameResult> setGroupNicknameResult(
    String sessionId,
    String nickname,
  ) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return SessionMemberNicknameResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }

    try {
      final resp = await _dio.post(
        '/sessions/members/nickname',
        data: {'session_id': sid, 'nickname': nickname},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            return SessionMemberNicknameResult(
              groupNickname: data['group_nickname']?.toString() ?? '',
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionMemberNicknameResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }
        final msg = body['msg']?.toString() ?? _unknownError;
        debugPrint('Set group nickname failed: $msg');
        return SessionMemberNicknameResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      return SessionMemberNicknameResult(
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
      debugPrint('Set group nickname error: $errMsg');
      return SessionMemberNicknameResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      debugPrint('Set group nickname unexpected error: $e');
      return SessionMemberNicknameResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<SessionPinResult> setSessionPinnedResult(
    String sessionId, {
    required bool isPinned,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return SessionPinResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }

    try {
      final resp = await _dio.post(
        '/sessions/pin',
        data: {'session_id': sid, 'is_pinned': isPinned},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            final pinnedAt = _normalizeTimestamp(_toInt(data['pinned_at']));
            return SessionPinResult(
              sessionId: data['session_id']?.toString().trim() ?? sid,
              isPinned: _toBool(data['is_pinned']),
              pinnedAt: pinnedAt,
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionPinResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }

        final msg = body['msg']?.toString() ?? _unknownError;
        return SessionPinResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      return SessionPinResult(
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
      return SessionPinResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      return SessionPinResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<SessionMuteResult> setSessionMutedResult(
    String sessionId, {
    required bool isMuted,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return SessionMuteResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }

    try {
      final resp = await _dio.post(
        '/sessions/mute',
        data: {'session_id': sid, 'is_muted': isMuted},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            return SessionMuteResult(
              sessionId: data['session_id']?.toString().trim() ?? sid,
              isMuted: _toBool(data['is_muted']),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionMuteResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }

        final msg = body['msg']?.toString() ?? _unknownError;
        return SessionMuteResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      return SessionMuteResult(
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
      return SessionMuteResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      return SessionMuteResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<String?> createGroupSession({
    required String name,
    List<String> memberIds = const [],
    List<int> memberTypes = const [],
  }) async {
    final groupName = name.trim();
    if (groupName.isEmpty) return null;

    try {
      final resp = await _dio.post(
        '/sessions/create_group',
        data: {
          'name': groupName,
          'member_ids': memberIds,
          'member_types': memberTypes,
        },
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return resp.data['data']['session_id'] as String;
      }
      final msg = resp.data['msg'] ?? _unknownError;
      debugPrint('Create group session failed: $msg');
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Create group session error: $errMsg');
    } catch (e) {
      debugPrint('Create group session unexpected error: $e');
    }
    return null;
  }

  /// Delete (revoke) a message
  Future<bool> deleteMessage({
    required String sessionId,
    required String msgId,
  }) async {
    final sid = sessionId.trim();
    final mid = msgId.trim();
    if (sid.isEmpty || mid.isEmpty) return false;

    try {
      final resp = await _dio.post(
        '/messages/delete',
        data: {'session_id': sid, 'msg_id': mid},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return true;
      }
      final msg = resp.data['msg'] ?? _unknownError;
      debugPrint('Delete message failed: $msg');
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Delete message error: $errMsg');
    } catch (e) {
      debugPrint('Delete message unexpected error: $e');
    }
    return false;
  }

  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return SessionMessageHistoryResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }

    final normalizedLimit = limit <= 0 ? 20 : (limit > 100 ? 100 : limit);
    final rawBeforeMsgId = beforeMsgId?.trim() ?? '';
    // 消息号是 19 位雪花号，Web 端（编译为 JS）整数只有 53 位精度，转 int 会丢尾部
    // 精度，导致 before_id 游标错位、历史分页拉错/漏消息。直接以字符串传给后端
    // （后端按 strconv.ParseInt 精确解析），绝不转 int。
    final beforeIdParam =
        (rawBeforeMsgId.isEmpty || rawBeforeMsgId == '0') ? '0' : rawBeforeMsgId;

    try {
      final resp = await _dio.get(
        '/messages/history',
        queryParameters: {
          'session_id': sid,
          'before_id': beforeIdParam,
          'limit': normalizedLimit,
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code != 0) {
          final msg = body['msg']?.toString() ?? _unknownError;
          return SessionMessageHistoryResult(
            code: code == 0 ? 50001 : code,
            httpStatus: resp.statusCode ?? 200,
            message: msg,
          );
        }
        final data = body['data'];
        if (data is! Map) {
          return SessionMessageHistoryResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }
        final rawMessages = data['messages'];
        final normalizedMessages = <Map<String, dynamic>>[];
        var nextBeforeMsgId = '';
        if (rawMessages is List) {
          for (final item in rawMessages) {
            if (item is! Map) {
              continue;
            }
            final msg = Map<String, dynamic>.from(item);
            final rawMsgId = msg['msg_id']?.toString().trim() ?? '';
            if (rawMsgId.isNotEmpty) {
              // Server returns descending pages; the last non-empty msg_id is
              // the next page cursor even if this row is filtered out locally.
              nextBeforeMsgId = rawMsgId;
            }
            if (_toBool(msg['is_revoked'])) {
              continue;
            }
            final msgId = msg['msg_id']?.toString().trim() ?? '';
            if (msgId.isEmpty) {
              continue;
            }
            final createdAt = _normalizeMessageCreatedAt(msg['created_at']);
            if (createdAt <= 0) {
              continue;
            }
            final senderTypeRaw = _toInt(msg['sender_type']);
            final msgTypeRaw = _toInt(msg['msg_type']);
            normalizedMessages.add({
              'msg_id': msgId,
              'session_id': msg['session_id']?.toString().trim() ?? sid,
              'sender_id': msg['sender_id']?.toString().trim() ?? '',
              'sender_type': senderTypeRaw > 0 ? senderTypeRaw : 1,
              'msg_type': msgTypeRaw > 0 ? msgTypeRaw : 1,
              'content': msg['content']?.toString() ?? '',
              'extra': msg['extra'],
              'quoted_message_id': msg['quoted_message_id']?.toString(),
              'created_at': createdAt,
              'visible_to': msg['visible_to'],
            });
          }
        }
        return SessionMessageHistoryResult(
          messages: normalizedMessages,
          hasMore: data['has_more'] == true,
          nextBeforeMsgId: nextBeforeMsgId,
          code: 0,
          httpStatus: resp.statusCode ?? 200,
        );
      }
      return SessionMessageHistoryResult(
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
      return SessionMessageHistoryResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      return SessionMessageHistoryResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<WidgetSessionModerationResult> closeVisitorSession(
    String sessionId,
  ) async {
    return _moderateVisitorSession(sessionId, ban: false);
  }

  Future<WidgetSessionModerationResult> banVisitorSession(
    String sessionId,
  ) async {
    return _moderateVisitorSession(sessionId, ban: true);
  }

  Future<WidgetSessionModerationResult> _moderateVisitorSession(
    String sessionId, {
    required bool ban,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return WidgetSessionModerationResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }
    final path = ban ? '/widget/sessions/ban' : '/widget/sessions/close';
    try {
      final resp = await _dio.post(path, data: {'session_id': sid});
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          return WidgetSessionModerationResult(
            code: 0,
            httpStatus: resp.statusCode ?? 200,
          );
        }
        return WidgetSessionModerationResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: body['msg']?.toString() ?? _unknownError,
        );
      }
      return WidgetSessionModerationResult(
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
      return WidgetSessionModerationResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      return WidgetSessionModerationResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<WidgetSiteListResult> fetchWidgetSites({
    int status = 0,
    int limit = 50,
    int offset = 0,
  }) async {
    try {
      final resp = await _dio.get(
        '/widget/sites/list',
        queryParameters: {'status': status, 'limit': limit, 'offset': offset},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            final rawItems = data['items'];
            final items = <WidgetSiteModel>[
              if (rawItems is List)
                for (final item in rawItems)
                  if (item is Map)
                    WidgetSiteModel.fromJson(Map<String, dynamic>.from(item)),
            ];
            return WidgetSiteListResult(
              items: items,
              total: _toInt(data['total']),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
        }
        return WidgetSiteListResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: body['msg']?.toString() ?? _unknownError,
        );
      }
      return WidgetSiteListResult(
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
      return WidgetSiteListResult(
        code: code == 0 ? 50001 : code,
        httpStatus: e.response?.statusCode ?? 0,
        message: errMsg,
        networkError: e.response == null,
      );
    } catch (e) {
      return WidgetSiteListResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<WidgetSiteCreateResult> createWidgetSite({
    required String siteName,
    required List<String> allowedOrigins,
    WidgetDisplayConfig? displayConfig,
  }) async {
    try {
      final resp = await _dio.post(
        '/widget/sites/create',
        data: {
          'site_name': siteName,
          'allowed_origins': allowedOrigins,
          if (displayConfig != null) 'display_config': displayConfig.toJson(),
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            final site = data['site'];
            return WidgetSiteCreateResult(
              site: site is Map
                  ? WidgetSiteModel.fromJson(Map<String, dynamic>.from(site))
                  : null,
              siteSecret: (data['site_secret'] ?? '').toString(),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
        }
        return WidgetSiteCreateResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: body['msg']?.toString() ?? _unknownError,
        );
      }
      return WidgetSiteCreateResult(
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
      return WidgetSiteCreateResult(
        code: code == 0 ? 50001 : code,
        httpStatus: e.response?.statusCode ?? 0,
        message: errMsg,
        networkError: e.response == null,
      );
    } catch (e) {
      return WidgetSiteCreateResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<WidgetSiteDetailResult> fetchWidgetSiteDetail(String siteId) async {
    final id = siteId.trim();
    if (id.isEmpty) {
      return const WidgetSiteDetailResult(
        code: 10003,
        message: 'settings_widget_sites_id_required'.tr,
      );
    }
    try {
      final resp = await _dio.get('/widget/sites/detail', queryParameters: {'id': id});
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            final rawSite = data['site'];
            return WidgetSiteDetailResult(
              site: rawSite is Map
                  ? WidgetSiteModel.fromJson(Map<String, dynamic>.from(rawSite))
                  : null,
              loaderUrl: (data['loader_url'] ?? '').toString(),
              embedCode: (data['embed_code'] ?? '').toString(),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
        }
        return WidgetSiteDetailResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: body['msg']?.toString() ?? _unknownError,
        );
      }
      return WidgetSiteDetailResult(
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
      return WidgetSiteDetailResult(
        code: code == 0 ? 50001 : code,
        httpStatus: e.response?.statusCode ?? 0,
        message: errMsg,
        networkError: e.response == null,
      );
    } catch (e) {
      return WidgetSiteDetailResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<WidgetSessionModerationResult> updateWidgetSite({
    required String id,
    required String siteName,
    required List<String> allowedOrigins,
    required int status,
    WidgetDisplayConfig? displayConfig,
  }) async {
    final siteId = id.trim();
    if (siteId.isEmpty) {
      return const WidgetSessionModerationResult(
        code: 10003,
        message: 'settings_widget_sites_id_required'.tr,
      );
    }
    try {
      final resp = await _dio.post(
        '/widget/sites/update',
        data: {
          'id': siteId,
          'site_name': siteName,
          'allowed_origins': allowedOrigins,
          'status': status,
          if (displayConfig != null) 'display_config': displayConfig.toJson(),
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          return WidgetSessionModerationResult(
            code: 0,
            httpStatus: resp.statusCode ?? 200,
          );
        }
        return WidgetSessionModerationResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: body['msg']?.toString() ?? _unknownError,
        );
      }
      return WidgetSessionModerationResult(
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
      return WidgetSessionModerationResult(
        code: code == 0 ? 50001 : code,
        httpStatus: e.response?.statusCode ?? 0,
        message: errMsg,
        networkError: e.response == null,
      );
    } catch (e) {
      return WidgetSessionModerationResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<WidgetSessionModerationResult> deleteWidgetSite(String siteId) async {
    final id = siteId.trim();
    if (id.isEmpty) {
      return const WidgetSessionModerationResult(
        code: 10003,
        message: 'settings_widget_sites_id_required'.tr,
      );
    }
    try {
      final resp = await _dio.post('/widget/sites/delete', data: {'id': id});
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          return WidgetSessionModerationResult(
            code: 0,
            httpStatus: resp.statusCode ?? 200,
          );
        }
        return WidgetSessionModerationResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: body['msg']?.toString() ?? _unknownError,
        );
      }
      return WidgetSessionModerationResult(
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
      return WidgetSessionModerationResult(
        code: code == 0 ? 50001 : code,
        httpStatus: e.response?.statusCode ?? 0,
        message: errMsg,
        networkError: e.response == null,
      );
    } catch (e) {
      return WidgetSessionModerationResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<WidgetSiteRotateSecretResult> rotateWidgetSiteSecret(
    String siteId,
  ) async {
    final id = siteId.trim();
    if (id.isEmpty) {
      return const WidgetSiteRotateSecretResult(
        code: 10003,
        message: 'settings_widget_sites_id_required'.tr,
      );
    }
    try {
      final resp = await _dio.post('/widget/sites/rotate_secret', data: {'id': id});
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            return WidgetSiteRotateSecretResult(
              siteId: (data['site_id'] ?? '').toString(),
              siteKey: (data['site_key'] ?? '').toString(),
              siteSecret: (data['site_secret'] ?? '').toString(),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
        }
        return WidgetSiteRotateSecretResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: body['msg']?.toString() ?? _unknownError,
        );
      }
      return WidgetSiteRotateSecretResult(
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
      return WidgetSiteRotateSecretResult(
        code: code == 0 ? 50001 : code,
        httpStatus: e.response?.statusCode ?? 0,
        message: errMsg,
        networkError: e.response == null,
      );
    } catch (e) {
      return WidgetSiteRotateSecretResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }
}
