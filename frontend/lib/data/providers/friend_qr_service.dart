import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

enum FriendQrResolveErrorType {
  invalidCode,
  unauthorized,
  network,
  server,
  unknown,
}

class FriendQrResolveResponse {
  const FriendQrResolveResponse._({
    this.data,
    this.errorType,
    this.message = '',
    this.code = 0,
    this.httpStatus = 200,
  });

  final FriendQrResolveResult? data;
  final FriendQrResolveErrorType? errorType;
  final String message;
  final int code;
  final int httpStatus;

  bool get ok => data != null;

  factory FriendQrResolveResponse.success({
    required FriendQrResolveResult data,
    int httpStatus = 200,
  }) {
    return FriendQrResolveResponse._(
      data: data,
      httpStatus: httpStatus,
    );
  }

  factory FriendQrResolveResponse.failure({
    required FriendQrResolveErrorType errorType,
    required String message,
    int code = 50001,
    int httpStatus = 0,
  }) {
    return FriendQrResolveResponse._(
      errorType: errorType,
      message: message,
      code: code,
      httpStatus: httpStatus,
    );
  }
}

class FriendQrCodeInfo {
  const FriendQrCodeInfo({
    required this.code,
    required this.shareUrl,
  });

  final String code;
  final String shareUrl;

  factory FriendQrCodeInfo.fromJson(Map<String, dynamic> json) {
    return FriendQrCodeInfo(
      code: json['code']?.toString().trim() ?? '',
      shareUrl: json['share_url']?.toString().trim() ?? '',
    );
  }
}

class FriendQrResolveResult {
  const FriendQrResolveResult({
    required this.userId,
    required this.username,
    required this.nickname,
    required this.avatarUrl,
    required this.isSelf,
    required this.isFriend,
    required this.outgoingPending,
    required this.incomingPending,
    required this.friendRequestHint,
  });

  final String userId;
  final String username;
  final String nickname;
  final String avatarUrl;
  final bool isSelf;
  final bool isFriend;
  final bool outgoingPending;
  final bool incomingPending;
  final String friendRequestHint;

  factory FriendQrResolveResult.fromJson(Map<String, dynamic> json) {
    return FriendQrResolveResult(
      userId: json['user_id']?.toString().trim() ?? '',
      username: json['username']?.toString().trim() ?? '',
      nickname: json['nickname']?.toString().trim() ?? '',
      avatarUrl: json['avatar_url']?.toString().trim() ?? '',
      isSelf: _readBool(json['is_self']),
      isFriend: _readBool(json['is_friend']),
      outgoingPending: _readBool(json['outgoing_pending']),
      incomingPending: _readBool(json['incoming_pending']),
      friendRequestHint: json['friend_request_hint']?.toString().trim() ?? '',
    );
  }

  static bool _readBool(dynamic value) {
    if (value is bool) return value;
    if (value is num) return value != 0;
    final normalized = value?.toString().trim().toLowerCase() ?? '';
    return normalized == '1' || normalized == 'true' || normalized == 'yes';
  }
}

class FriendQrService extends GetxService {
  late final Dio _dio;

  Future<FriendQrService> init() async {
    final authService = Get.find<AuthService>();
    _dio = Dio(BaseOptions(
      baseUrl: AppRuntimeEndpoints.apiBaseUrl,
      connectTimeout: const Duration(seconds: 10),
      receiveTimeout: const Duration(seconds: 10),
    ));
    authService.attachAuthInterceptor(_dio);
    return this;
  }

  Future<FriendQrCodeInfo?> fetchMyQrCode() async {
    try {
      final resp = await _dio.get('/friends/qr');
      final data = _extractData(resp.data);
      if (data == null) return null;
      return FriendQrCodeInfo.fromJson(data);
    } catch (e) {
      debugPrint('fetchMyQrCode error: $e');
      return null;
    }
  }

  Future<FriendQrResolveResult?> resolveCode(String code) async {
    final result = await resolveCodeDetailed(code);
    return result.data;
  }

  Future<FriendQrResolveResponse> resolveCodeDetailed(String code) async {
    final normalized = code.trim();
    if (normalized.isEmpty) {
      return FriendQrResolveResponse.failure(
        errorType: FriendQrResolveErrorType.invalidCode,
        message: 'conversations_scan_invalid_qr'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final resp = await _dio.get('/friends/qr/resolve/$normalized');
      return _decodeResolveResponse(
        resp.data,
        httpStatus: resp.statusCode ?? 200,
      );
    } on DioException catch (e) {
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

      final statusCode = e.response?.statusCode ?? 0;
      if (statusCode == 401 || statusCode == 403) {
        return FriendQrResolveResponse.failure(
          errorType: FriendQrResolveErrorType.unauthorized,
          message: 'auth_error_unauthorized'.tr,
          code: 10001,
          httpStatus: statusCode,
        );
      }
      if (_isNetworkError(e)) {
        return FriendQrResolveResponse.failure(
          errorType: FriendQrResolveErrorType.network,
          message: 'common_unknown_error'.tr,
          httpStatus: statusCode,
        );
      }
      return FriendQrResolveResponse.failure(
        errorType: FriendQrResolveErrorType.server,
        message: 'common_unknown_error'.tr,
        httpStatus: statusCode,
      );
    } catch (e) {
      debugPrint('resolveCodeDetailed error: $e');
      return FriendQrResolveResponse.failure(
        errorType: FriendQrResolveErrorType.unknown,
        message: 'common_unknown_error'.tr,
      );
    }
  }

  FriendQrResolveResponse _decodeResolveResponse(
    dynamic raw, {
    required int httpStatus,
  }) {
    if (raw is! Map) {
      return FriendQrResolveResponse.failure(
        errorType: FriendQrResolveErrorType.unknown,
        message: 'common_unknown_error'.tr,
        httpStatus: httpStatus,
      );
    }

    final body = Map<String, dynamic>.from(raw);
    final code = _readInt(body['code'], fallback: 50001);
    if (code != 0) {
      if (code == 10004) {
        return FriendQrResolveResponse.failure(
          errorType: FriendQrResolveErrorType.invalidCode,
          message: 'conversations_scan_invalid_qr'.tr,
          code: code,
          httpStatus: httpStatus,
        );
      }
      if (code == 10001 || httpStatus == 401 || httpStatus == 403) {
        return FriendQrResolveResponse.failure(
          errorType: FriendQrResolveErrorType.unauthorized,
          message: _readMessage(body, fallback: 'auth_error_unauthorized'.tr),
          code: code,
          httpStatus: httpStatus,
        );
      }
      return FriendQrResolveResponse.failure(
        errorType: FriendQrResolveErrorType.server,
        message: _readMessage(body, fallback: 'common_unknown_error'.tr),
        code: code,
        httpStatus: httpStatus,
      );
    }

    final dataRaw = body['data'];
    if (dataRaw is! Map) {
      return FriendQrResolveResponse.failure(
        errorType: FriendQrResolveErrorType.unknown,
        message: 'common_unknown_error'.tr,
        httpStatus: httpStatus,
      );
    }
    final data =
        FriendQrResolveResult.fromJson(Map<String, dynamic>.from(dataRaw));
    return FriendQrResolveResponse.success(data: data, httpStatus: httpStatus);
  }

  bool _isNetworkError(DioException e) {
    return e.type == DioExceptionType.connectionError ||
        e.type == DioExceptionType.connectionTimeout ||
        e.type == DioExceptionType.sendTimeout ||
        e.type == DioExceptionType.receiveTimeout;
  }

  int _readInt(dynamic value, {int fallback = 0}) {
    if (value is int) return value;
    if (value is num) return value.toInt();
    return int.tryParse(value?.toString() ?? '') ?? fallback;
  }

  String _readMessage(Map<String, dynamic> body, {required String fallback}) {
    final message = body['msg']?.toString().trim() ?? '';
    if (message.isEmpty) return fallback;
    return message;
  }

  Map<String, dynamic>? _extractData(dynamic raw) {
    if (raw is! Map) return null;
    final map = Map<String, dynamic>.from(raw);
    if (map['code'] != 0) return null;
    final data = map['data'];
    if (data is! Map) return null;
    return Map<String, dynamic>.from(data);
  }
}
