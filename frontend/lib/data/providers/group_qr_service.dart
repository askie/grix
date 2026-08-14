import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

enum GroupQrErrorType { invalidCode, unauthorized, network, server, unknown }

class GroupQrCodeInfo {
  const GroupQrCodeInfo({required this.code, required this.shareUrl});

  final String code;
  final String shareUrl;

  factory GroupQrCodeInfo.fromJson(Map<String, dynamic> json) {
    return GroupQrCodeInfo(
      code: json['code']?.toString().trim() ?? '',
      shareUrl: json['share_url']?.toString().trim() ?? '',
    );
  }
}

class GroupQrResolveResult {
  const GroupQrResolveResult({
    required this.code,
    required this.sessionId,
    required this.groupName,
    required this.ownerId,
    required this.ownerNickname,
    required this.memberCount,
    required this.isMember,
  });

  final String code;
  final String sessionId;
  final String groupName;
  final String ownerId;
  final String ownerNickname;
  final int memberCount;
  final bool isMember;

  factory GroupQrResolveResult.fromJson(Map<String, dynamic> json) {
    return GroupQrResolveResult(
      code: json['code']?.toString().trim() ?? '',
      sessionId: json['session_id']?.toString().trim() ?? '',
      groupName: json['group_name']?.toString().trim() ?? '',
      ownerId: json['owner_id']?.toString().trim() ?? '',
      ownerNickname: json['owner_nickname']?.toString().trim() ?? '',
      memberCount: _toInt(json['member_count']),
      isMember: _toBool(json['is_member']),
    );
  }
}

class GroupQrJoinResult {
  const GroupQrJoinResult({
    required this.sessionId,
    required this.groupName,
    required this.joined,
  });

  final String sessionId;
  final String groupName;
  final bool joined;

  factory GroupQrJoinResult.fromJson(Map<String, dynamic> json) {
    return GroupQrJoinResult(
      sessionId: json['session_id']?.toString().trim() ?? '',
      groupName: json['group_name']?.toString().trim() ?? '',
      joined: _toBool(json['joined']),
    );
  }
}

class GroupQrResolveResponse {
  const GroupQrResolveResponse._({
    this.data,
    this.errorType,
    this.message = '',
    this.code = 0,
    this.httpStatus = 200,
  });

  final GroupQrResolveResult? data;
  final GroupQrErrorType? errorType;
  final String message;
  final int code;
  final int httpStatus;

  bool get ok => data != null;

  factory GroupQrResolveResponse.success({
    required GroupQrResolveResult data,
    int httpStatus = 200,
  }) {
    return GroupQrResolveResponse._(data: data, httpStatus: httpStatus);
  }

  factory GroupQrResolveResponse.failure({
    required GroupQrErrorType errorType,
    required String message,
    int code = 50001,
    int httpStatus = 0,
  }) {
    return GroupQrResolveResponse._(
      errorType: errorType,
      message: message,
      code: code,
      httpStatus: httpStatus,
    );
  }
}

class GroupQrJoinResponse {
  const GroupQrJoinResponse._({
    this.data,
    this.errorType,
    this.message = '',
    this.code = 0,
    this.httpStatus = 200,
  });

  final GroupQrJoinResult? data;
  final GroupQrErrorType? errorType;
  final String message;
  final int code;
  final int httpStatus;

  bool get ok => data != null;

  factory GroupQrJoinResponse.success({
    required GroupQrJoinResult data,
    int httpStatus = 200,
  }) {
    return GroupQrJoinResponse._(data: data, httpStatus: httpStatus);
  }

  factory GroupQrJoinResponse.failure({
    required GroupQrErrorType errorType,
    required String message,
    int code = 50001,
    int httpStatus = 0,
  }) {
    return GroupQrJoinResponse._(
      errorType: errorType,
      message: message,
      code: code,
      httpStatus: httpStatus,
    );
  }
}

class GroupQrService extends GetxService {
  late final Dio _dio;

  Future<GroupQrService> init() async {
    final authService = Get.find<AuthService>();
    _dio = Dio(
      BaseOptions(
        baseUrl: AppRuntimeEndpoints.apiBaseUrl,
        connectTimeout: const Duration(seconds: 10),
        receiveTimeout: const Duration(seconds: 10),
      ),
    );
    authService.attachAuthInterceptor(_dio);
    return this;
  }

  Future<GroupQrCodeInfo?> fetchGroupQrCode(String sessionId) async {
    final normalizedSessionID = sessionId.trim();
    if (normalizedSessionID.isEmpty) {
      return null;
    }

    try {
      final resp = await _dio.get(
        '/sessions/group/qr',
        queryParameters: {'session_id': normalizedSessionID},
      );
      final data = _extractData(resp.data);
      if (data == null) {
        return null;
      }
      return GroupQrCodeInfo.fromJson(data);
    } catch (e) {
      debugPrint('GroupQrService.fetchGroupQrCode error: $e');
      return null;
    }
  }

  Future<GroupQrResolveResponse> resolveCodeDetailed(String code) async {
    final normalized = code.trim();
    if (normalized.isEmpty) {
      return GroupQrResolveResponse.failure(
        errorType: GroupQrErrorType.invalidCode,
        message: 'conversations_scan_invalid_qr'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final resp = await _dio.get('/sessions/group/qr/resolve/$normalized');
      return _decodeResolveResponse(
        resp.data,
        httpStatus: resp.statusCode ?? 200,
      );
    } on DioException catch (e) {
      return _decodeDioErrorResolve(e);
    } catch (e) {
      debugPrint('GroupQrService.resolveCodeDetailed error: $e');
      return GroupQrResolveResponse.failure(
        errorType: GroupQrErrorType.unknown,
        message: 'common_unknown_error'.tr,
      );
    }
  }

  Future<GroupQrJoinResponse> joinByCodeDetailed(String code) async {
    final normalized = code.trim();
    if (normalized.isEmpty) {
      return GroupQrJoinResponse.failure(
        errorType: GroupQrErrorType.invalidCode,
        message: 'conversations_scan_invalid_qr'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final resp = await _dio.post(
        '/sessions/group/join_by_qr',
        data: {'code': normalized},
      );
      return _decodeJoinResponse(resp.data, httpStatus: resp.statusCode ?? 200);
    } on DioException catch (e) {
      return _decodeDioErrorJoin(e);
    } catch (e) {
      debugPrint('GroupQrService.joinByCodeDetailed error: $e');
      return GroupQrJoinResponse.failure(
        errorType: GroupQrErrorType.unknown,
        message: 'common_unknown_error'.tr,
      );
    }
  }

  GroupQrResolveResponse _decodeResolveResponse(
    dynamic raw, {
    required int httpStatus,
  }) {
    if (raw is! Map) {
      return GroupQrResolveResponse.failure(
        errorType: GroupQrErrorType.unknown,
        message: 'common_unknown_error'.tr,
        httpStatus: httpStatus,
      );
    }

    final body = Map<String, dynamic>.from(raw);
    final code = _toInt(body['code'], fallback: 50001);
    if (code != 0) {
      return GroupQrResolveResponse.failure(
        errorType: _mapErrorType(code: code, httpStatus: httpStatus),
        message: _mapErrorMessage(body, code: code, httpStatus: httpStatus),
        code: code,
        httpStatus: httpStatus,
      );
    }

    final dataRaw = body['data'];
    if (dataRaw is! Map) {
      return GroupQrResolveResponse.failure(
        errorType: GroupQrErrorType.unknown,
        message: 'common_unknown_error'.tr,
        httpStatus: httpStatus,
      );
    }

    final data = GroupQrResolveResult.fromJson(
      Map<String, dynamic>.from(dataRaw),
    );
    return GroupQrResolveResponse.success(data: data, httpStatus: httpStatus);
  }

  GroupQrJoinResponse _decodeJoinResponse(
    dynamic raw, {
    required int httpStatus,
  }) {
    if (raw is! Map) {
      return GroupQrJoinResponse.failure(
        errorType: GroupQrErrorType.unknown,
        message: 'common_unknown_error'.tr,
        httpStatus: httpStatus,
      );
    }

    final body = Map<String, dynamic>.from(raw);
    final code = _toInt(body['code'], fallback: 50001);
    if (code != 0) {
      return GroupQrJoinResponse.failure(
        errorType: _mapErrorType(code: code, httpStatus: httpStatus),
        message: _mapErrorMessage(body, code: code, httpStatus: httpStatus),
        code: code,
        httpStatus: httpStatus,
      );
    }

    final dataRaw = body['data'];
    if (dataRaw is! Map) {
      return GroupQrJoinResponse.failure(
        errorType: GroupQrErrorType.unknown,
        message: 'common_unknown_error'.tr,
        httpStatus: httpStatus,
      );
    }

    final data = GroupQrJoinResult.fromJson(Map<String, dynamic>.from(dataRaw));
    return GroupQrJoinResponse.success(data: data, httpStatus: httpStatus);
  }

  GroupQrResolveResponse _decodeDioErrorResolve(DioException e) {
    final body = e.response?.data;
    if (body is Map) {
      final decoded = _decodeResolveResponse(
        body,
        httpStatus: e.response?.statusCode ?? 0,
      );
      if (!decoded.ok) {
        return decoded;
      }
    }
    return GroupQrResolveResponse.failure(
      errorType: _mapNetworkOrServerError(e),
      message: _fallbackDioMessage(e),
      httpStatus: e.response?.statusCode ?? 0,
    );
  }

  GroupQrJoinResponse _decodeDioErrorJoin(DioException e) {
    final body = e.response?.data;
    if (body is Map) {
      final decoded = _decodeJoinResponse(
        body,
        httpStatus: e.response?.statusCode ?? 0,
      );
      if (!decoded.ok) {
        return decoded;
      }
    }
    return GroupQrJoinResponse.failure(
      errorType: _mapNetworkOrServerError(e),
      message: _fallbackDioMessage(e),
      httpStatus: e.response?.statusCode ?? 0,
    );
  }

  GroupQrErrorType _mapErrorType({required int code, required int httpStatus}) {
    if (code == 10004) {
      return GroupQrErrorType.invalidCode;
    }
    if (code == 10001 || httpStatus == 401 || httpStatus == 403) {
      return GroupQrErrorType.unauthorized;
    }
    return GroupQrErrorType.server;
  }

  String _mapErrorMessage(
    Map<String, dynamic> body, {
    required int code,
    required int httpStatus,
  }) {
    if (code == 10004) {
      return 'conversations_scan_invalid_qr'.tr;
    }
    if (code == 10001 || httpStatus == 401 || httpStatus == 403) {
      return _readMessage(body, fallback: 'auth_error_unauthorized'.tr);
    }
    return _readMessage(body, fallback: 'common_unknown_error'.tr);
  }

  GroupQrErrorType _mapNetworkOrServerError(DioException e) {
    if (_isNetworkError(e)) {
      return GroupQrErrorType.network;
    }
    final statusCode = e.response?.statusCode ?? 0;
    if (statusCode == 401 || statusCode == 403) {
      return GroupQrErrorType.unauthorized;
    }
    return GroupQrErrorType.server;
  }

  String _fallbackDioMessage(DioException e) {
    final statusCode = e.response?.statusCode ?? 0;
    if (statusCode == 401 || statusCode == 403) {
      return 'auth_error_unauthorized'.tr;
    }
    if (_isNetworkError(e)) {
      return 'common_unknown_error'.tr;
    }
    return 'common_unknown_error'.tr;
  }

  bool _isNetworkError(DioException e) {
    return e.type == DioExceptionType.connectionError ||
        e.type == DioExceptionType.connectionTimeout ||
        e.type == DioExceptionType.sendTimeout ||
        e.type == DioExceptionType.receiveTimeout;
  }

  String _readMessage(Map<String, dynamic> body, {required String fallback}) {
    final message = body['msg']?.toString().trim() ?? '';
    if (message.isEmpty) return fallback;
    return message;
  }

  Map<String, dynamic>? _extractData(dynamic raw) {
    if (raw is! Map) {
      return null;
    }
    final body = Map<String, dynamic>.from(raw);
    final code = _toInt(body['code'], fallback: 50001);
    if (code != 0) {
      return null;
    }
    final data = body['data'];
    if (data is! Map) {
      return null;
    }
    return Map<String, dynamic>.from(data);
  }
}

int _toInt(dynamic value, {int fallback = 0}) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '') ?? fallback;
}

bool _toBool(dynamic value) {
  if (value is bool) return value;
  if (value is num) return value != 0;
  final normalized = value?.toString().trim().toLowerCase() ?? '';
  return normalized == '1' || normalized == 'true' || normalized == 'yes';
}
