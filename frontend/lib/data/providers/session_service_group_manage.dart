part of 'session_service.dart';

class _SessionServiceGroupManageApi {
  _SessionServiceGroupManageApi(this._service);

  final SessionService _service;

  Dio get _dio => _service._dio;
  String get _unknownError => _service._unknownError;
  int _toInt(dynamic v) => _service._toInt(v);
  bool _toBool(dynamic v) => _service._toBool(v);

  Future<Map<String, dynamic>?> addGroupMembers({
    required String sessionId,
    required List<String> memberIds,
    List<int> memberTypes = const [],
  }) async {
    final result = await addGroupMembersResult(
      sessionId: sessionId,
      memberIds: memberIds,
      memberTypes: memberTypes,
    );
    return result.code == 0 ? result.data : null;
  }

  Future<SessionAddMembersResult> addGroupMembersResult({
    required String sessionId,
    required List<String> memberIds,
    List<int> memberTypes = const [],
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty || memberIds.isEmpty) {
      return SessionAddMembersResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }

    try {
      final resp = await _dio.post(
        '/sessions/members/add',
        data: {
          'session_id': sid,
          'member_ids': memberIds,
          'member_types': memberTypes,
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            return SessionAddMembersResult(
              data: Map<String, dynamic>.from(data),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionAddMembersResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }
        final msg = body['msg']?.toString() ?? _unknownError;
        debugPrint('Add group members failed: $msg');
        return SessionAddMembersResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      final msg = body is Map
          ? body['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('Add group members failed: $msg');
      return SessionAddMembersResult(
        code: 50001,
        httpStatus: resp.statusCode ?? 0,
        message: msg,
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
      debugPrint('Add group members error: $errMsg');
      return SessionAddMembersResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      debugPrint('Add group members unexpected error: $e');
      return SessionAddMembersResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<SessionInviteSettingResult> updateGroupInviteSettingResult({
    required String sessionId,
    required bool allowMemberInvite,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return SessionInviteSettingResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }

    try {
      final resp = await _dio.post(
        '/sessions/members/invite_setting',
        data: {'session_id': sid, 'allow_member_invite': allowMemberInvite},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            return SessionInviteSettingResult(
              allowMemberInvite: _toBool(data['allow_member_invite']),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionInviteSettingResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }
        final msg = body['msg']?.toString() ?? _unknownError;
        debugPrint('Update group invite setting failed: $msg');
        return SessionInviteSettingResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      final msg = body is Map
          ? body['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('Update group invite setting failed: $msg');
      return SessionInviteSettingResult(
        code: 50001,
        httpStatus: resp.statusCode ?? 0,
        message: msg,
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
      debugPrint('Update group invite setting error: $errMsg');
      return SessionInviteSettingResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      debugPrint('Update group invite setting unexpected error: $e');
      return SessionInviteSettingResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<SessionAllMembersMutedResult> updateGroupAllMembersMutedResult({
    required String sessionId,
    required bool allMembersMuted,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return SessionAllMembersMutedResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }

    try {
      final resp = await _dio.post(
        '/sessions/speaking/all_muted',
        data: {'session_id': sid, 'all_members_muted': allMembersMuted},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            return SessionAllMembersMutedResult(
              allMembersMuted: _toBool(data['all_members_muted']),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionAllMembersMutedResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }
        final msg = body['msg']?.toString() ?? _unknownError;
        debugPrint('Update group speaking setting failed: $msg');
        return SessionAllMembersMutedResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      final msg = body is Map
          ? body['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('Update group speaking setting failed: $msg');
      return SessionAllMembersMutedResult(
        code: 50001,
        httpStatus: resp.statusCode ?? 0,
        message: msg,
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
      debugPrint('Update group speaking setting error: $errMsg');
      return SessionAllMembersMutedResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      debugPrint('Update group speaking setting unexpected error: $e');
      return SessionAllMembersMutedResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<SessionMemberSpeakingResult> updateGroupMemberSpeakingResult({
    required String sessionId,
    required String memberId,
    int memberType = 1,
    bool? isSpeakMuted,
    bool? canSpeakWhenAllMuted,
  }) async {
    final sid = sessionId.trim();
    final mid = memberId.trim();
    if (sid.isEmpty) {
      return SessionMemberSpeakingResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }
    if (mid.isEmpty) {
      return SessionMemberSpeakingResult(
        code: 10003,
        message: 'session_error_member_id_required'.tr,
      );
    }
    if (isSpeakMuted == null && canSpeakWhenAllMuted == null) {
      return SessionMemberSpeakingResult(
        code: 10003,
        message: 'session_error_speaking_setting_required'.tr,
      );
    }

    try {
      final body = <String, dynamic>{
        'session_id': sid,
        'member_id': mid,
        'member_type': memberType,
      };
      if (isSpeakMuted != null) {
        body['is_speak_muted'] = isSpeakMuted;
      }
      if (canSpeakWhenAllMuted != null) {
        body['can_speak_when_all_muted'] = canSpeakWhenAllMuted;
      }
      final resp = await _dio.post('/sessions/members/speaking', data: body);
      final raw = resp.data;
      if (resp.statusCode == 200 && raw is Map) {
        final code = _toInt(raw['code']);
        if (code == 0) {
          final data = raw['data'];
          if (data is Map) {
            return SessionMemberSpeakingResult(
              memberId: data['member_id']?.toString().trim() ?? '',
              memberType: _toInt(data['member_type']),
              isSpeakMuted: _toBool(data['is_speak_muted']),
              canSpeakWhenAllMuted: _toBool(data['can_speak_when_all_muted']),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionMemberSpeakingResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }
        final msg = raw['msg']?.toString() ?? _unknownError;
        debugPrint('Update member speaking failed: $msg');
        return SessionMemberSpeakingResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      final msg = raw is Map
          ? raw['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('Update member speaking failed: $msg');
      return SessionMemberSpeakingResult(
        code: 50001,
        httpStatus: resp.statusCode ?? 0,
        message: msg,
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
      debugPrint('Update member speaking error: $errMsg');
      return SessionMemberSpeakingResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      debugPrint('Update member speaking unexpected error: $e');
      return SessionMemberSpeakingResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<SessionMemberAgentReceiveResult> updateGroupMemberAgentReceiveResult({
    required String sessionId,
    required String memberId,
    required int agentReceiveMode,
    int? agentReceiveBacklogCount,
    int memberType = 1,
  }) async {
    final sid = sessionId.trim();
    final mid = memberId.trim();
    if (sid.isEmpty) {
      return SessionMemberAgentReceiveResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }
    if (mid.isEmpty) {
      return SessionMemberAgentReceiveResult(
        code: 10003,
        message: 'session_error_member_id_required'.tr,
      );
    }

    try {
      final body = <String, dynamic>{
        'session_id': sid,
        'member_id': mid,
        'member_type': memberType,
        'agent_receive_mode': agentReceiveMode,
      };
      if (agentReceiveBacklogCount != null) {
        body['agent_receive_backlog_count'] = agentReceiveBacklogCount;
      }
      final resp = await _dio.post(
        '/sessions/members/agent_receive',
        data: body,
      );
      final raw = resp.data;
      if (resp.statusCode == 200 && raw is Map) {
        final code = _toInt(raw['code']);
        if (code == 0) {
          final data = raw['data'];
          if (data is Map) {
            return SessionMemberAgentReceiveResult(
              memberId: (data['member_id'] ?? '').toString().trim(),
              memberType: _toInt(data['member_type']),
              agentReceiveMode: _toInt(data['agent_receive_mode']),
              agentReceiveBacklogCount: _toInt(
                data['agent_receive_backlog_count'],
              ),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionMemberAgentReceiveResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }
        final msg = raw['msg']?.toString() ?? _unknownError;
        debugPrint('Update group member agent receive failed: $msg');
        return SessionMemberAgentReceiveResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      final msg = raw is Map
          ? raw['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('Update group member agent receive failed: $msg');
      return SessionMemberAgentReceiveResult(
        code: 50001,
        httpStatus: resp.statusCode ?? 0,
        message: msg,
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
      debugPrint('Update group member agent receive error: $errMsg');
      return SessionMemberAgentReceiveResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      debugPrint('Update group member agent receive unexpected error: $e');
      return SessionMemberAgentReceiveResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<Map<String, dynamic>?> removeGroupMembers({
    required String sessionId,
    required List<String> memberIds,
    List<int> memberTypes = const [],
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty || memberIds.isEmpty) return null;

    try {
      final resp = await _dio.post(
        '/sessions/members/remove',
        data: {
          'session_id': sid,
          'member_ids': memberIds,
          'member_types': memberTypes,
        },
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final data = resp.data['data'];
        if (data is Map) {
          return Map<String, dynamic>.from(data);
        }
      }
      final msg = resp.data['msg'] ?? _unknownError;
      debugPrint('Remove group members failed: $msg');
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Remove group members error: $errMsg');
    } catch (e) {
      debugPrint('Remove group members unexpected error: $e');
    }
    return null;
  }

  Future<SessionLeaveResult> leaveGroupResult({
    required String sessionId,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return SessionLeaveResult(
        code: 10003,
        message: 'session_error_session_id_required'.tr,
      );
    }

    try {
      final resp = await _dio.post(
        '/sessions/leave',
        data: {'session_id': sid},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map) {
        final code = _toInt(body['code']);
        if (code == 0) {
          final data = body['data'];
          if (data is Map) {
            return SessionLeaveResult(
              sessionId: data['session_id']?.toString().trim() ?? sid,
              left: _toBool(data['left']),
              code: 0,
              httpStatus: resp.statusCode ?? 200,
            );
          }
          return SessionLeaveResult(
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
            message: 'session_error_invalid_response_data'.tr,
          );
        }
        final msg = body['msg']?.toString() ?? _unknownError;
        debugPrint('Leave group failed: $msg');
        return SessionLeaveResult(
          code: code == 0 ? 50001 : code,
          httpStatus: resp.statusCode ?? 200,
          message: msg,
        );
      }
      final msg = body is Map
          ? body['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('Leave group failed: $msg');
      return SessionLeaveResult(
        code: 50001,
        httpStatus: resp.statusCode ?? 0,
        message: msg,
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
      debugPrint('Leave group error: $errMsg');
      return SessionLeaveResult(
        code: code,
        httpStatus: httpStatus,
        message: errMsg,
        networkError: networkError,
      );
    } catch (e) {
      debugPrint('Leave group unexpected error: $e');
      return SessionLeaveResult(
        code: 50001,
        message: e.toString(),
        networkError: true,
      );
    }
  }

  Future<Map<String, dynamic>?> updateGroupMemberRole({
    required String sessionId,
    required String memberId,
    int memberType = 1,
    required int role,
  }) async {
    final sid = sessionId.trim();
    final mid = memberId.trim();
    if (sid.isEmpty || mid.isEmpty) return null;

    try {
      final resp = await _dio.post(
        '/sessions/members/role',
        data: {
          'session_id': sid,
          'member_id': mid,
          'member_type': memberType,
          'role': role,
        },
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final data = resp.data['data'];
        if (data is Map) {
          return Map<String, dynamic>.from(data);
        }
      }
      final msg = resp.data['msg'] ?? _unknownError;
      debugPrint('Update group member role failed: $msg');
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Update group member role error: $errMsg');
    } catch (e) {
      debugPrint('Update group member role unexpected error: $e');
    }
    return null;
  }

  Future<Map<String, dynamic>?> transferGroupOwner({
    required String sessionId,
    required String memberId,
  }) async {
    final sid = sessionId.trim();
    final mid = memberId.trim();
    if (sid.isEmpty || mid.isEmpty) return null;

    try {
      final resp = await _dio.post(
        '/sessions/owner/transfer',
        data: {'session_id': sid, 'member_id': mid},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final data = resp.data['data'];
        if (data is Map) {
          return Map<String, dynamic>.from(data);
        }
      }
      final msg = resp.data['msg'] ?? _unknownError;
      debugPrint('Transfer group owner failed: $msg');
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Transfer group owner error: $errMsg');
    } catch (e) {
      debugPrint('Transfer group owner unexpected error: $e');
    }
    return null;
  }

  Future<Map<String, dynamic>?> convertToGroup({
    required String sessionId,
    String name = '',
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return null;

    try {
      final resp = await _dio.post(
        '/sessions/convert_to_group',
        data: {'session_id': sid, 'name': name.trim()},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final data = resp.data['data'];
        if (data is Map) {
          return Map<String, dynamic>.from(data);
        }
      }
      final msg = resp.data['msg'] ?? _unknownError;
      debugPrint('Convert to group failed: $msg');
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Convert to group error: $errMsg');
    } catch (e) {
      debugPrint('Convert to group unexpected error: $e');
    }
    return null;
  }

  Future<Map<String, dynamic>?> dissolveGroup({
    required String sessionId,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return null;

    try {
      final resp = await _dio.post(
        '/sessions/dissolve',
        data: {'session_id': sid},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final data = resp.data['data'];
        if (data is Map) {
          return Map<String, dynamic>.from(data);
        }
      }
      final msg = resp.data['msg'] ?? _unknownError;
      debugPrint('Dissolve group failed: $msg');
    } on DioException catch (e) {
      String errMsg = e.message ?? _unknownError;
      if (e.response != null && e.response?.data is Map) {
        errMsg = e.response?.data['msg'] ?? errMsg;
      }
      debugPrint('Dissolve group error: $errMsg');
    } catch (e) {
      debugPrint('Dissolve group unexpected error: $e');
    }
    return null;
  }
}
