import 'package:dio/dio.dart';
import 'package:get/get.dart';

import '../../shared/utils/app_region_config.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

class QRLoginCreateData {
  const QRLoginCreateData({
    required this.qrSessionId,
    required this.qrText,
    required this.pollToken,
    required this.expiresIn,
    required this.pollIntervalMs,
  });

  final String qrSessionId;
  final String qrText;
  final String pollToken;
  final int expiresIn;
  final int pollIntervalMs;

  factory QRLoginCreateData.fromJson(Map<String, dynamic> json) {
    return QRLoginCreateData(
      qrSessionId: json['qr_session_id']?.toString().trim() ?? '',
      qrText: json['qr_text']?.toString().trim() ?? '',
      pollToken: json['poll_token']?.toString().trim() ?? '',
      expiresIn: _toInt(json['expires_in']),
      pollIntervalMs: _toInt(json['poll_interval_ms']),
    );
  }
}

class QRLoginStatusData {
  const QRLoginStatusData({
    required this.status,
    required this.expiresIn,
    this.scannerUserId,
    this.scannerNickname,
    this.scannerAvatarUrl,
  });

  final String status;
  final int expiresIn;
  final String? scannerUserId;
  final String? scannerNickname;
  final String? scannerAvatarUrl;

  factory QRLoginStatusData.fromJson(Map<String, dynamic> json) {
    final scannerRaw = json['scanner_user'];
    Map<String, dynamic>? scanner;
    if (scannerRaw is Map) {
      scanner = Map<String, dynamic>.from(scannerRaw);
    }
    return QRLoginStatusData(
      status: json['status']?.toString().trim() ?? '',
      expiresIn: _toInt(json['expires_in']),
      scannerUserId: scanner?['user_id']?.toString().trim(),
      scannerNickname: scanner?['nickname']?.toString().trim(),
      scannerAvatarUrl: scanner?['avatar_url']?.toString().trim(),
    );
  }
}

class QRLoginScanData {
  const QRLoginScanData({
    required this.qrSessionId,
    required this.status,
    required this.expiresIn,
  });

  final String qrSessionId;
  final String status;
  final int expiresIn;

  factory QRLoginScanData.fromJson(Map<String, dynamic> json) {
    return QRLoginScanData(
      qrSessionId: json['qr_session_id']?.toString().trim() ?? '',
      status: json['status']?.toString().trim() ?? '',
      expiresIn: _toInt(json['expires_in']),
    );
  }
}

class QrLoginService extends GetxService {
  late final Dio _dio;

  Future<QrLoginService> init() async {
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

  bool isQrLoginPayload(String raw) {
    final normalized = raw.trim();
    if (normalized.isEmpty) return false;
    final uri = Uri.tryParse(normalized);
    if (uri == null) return false;
    return uri.scheme.toLowerCase() == 'grix' &&
        uri.host.toLowerCase() == 'auth' &&
        uri.path == '/qr-login' &&
        (uri.queryParameters['sid']?.trim().isNotEmpty ?? false) &&
        (uri.queryParameters['qt']?.trim().isNotEmpty ?? false);
  }

  /// 提取二维码登录 payload 携带的区域标识（rg 参数）；旧版二维码不带则返回 null。
  AppRegion? qrPayloadRegion(String raw) {
    final rg =
        Uri.tryParse(raw.trim())?.queryParameters['rg']?.trim().toLowerCase() ??
        '';
    if (rg.isEmpty) return null;
    return rg == 'cn' ? AppRegion.cn : AppRegion.global;
  }

  Future<ServiceResult<QRLoginCreateData>> createSession({
    String deviceLabel = '',
  }) async {
    try {
      final response = await _dio.post(
        '/auth/qr/create',
        data: {'device_label': deviceLabel.trim()},
      );
      return _decodeDataResult(
        response.data,
        (json) => QRLoginCreateData.fromJson(json),
        fallbackMessage: 'login_qr_create_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<QRLoginCreateData>(
        e,
        fallbackMessage: 'login_qr_create_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<QRLoginCreateData>.failure(
        message: 'login_qr_create_failed'.tr,
      );
    }
  }

  Future<ServiceResult<QRLoginStatusData>> queryStatus({
    required String sessionId,
    required String pollToken,
  }) async {
    final sid = sessionId.trim();
    final token = pollToken.trim();
    if (sid.isEmpty || token.isEmpty) {
      return ServiceResult<QRLoginStatusData>.failure(
        message: 'login_qr_status_failed'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }
    try {
      final response = await _dio.get(
        '/auth/qr/status',
        queryParameters: {'session_id': sid, 'poll_token': token},
      );
      return _decodeDataResult(
        response.data,
        (json) => QRLoginStatusData.fromJson(json),
        fallbackMessage: 'login_qr_status_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<QRLoginStatusData>(
        e,
        fallbackMessage: 'login_qr_status_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<QRLoginStatusData>.failure(
        message: 'login_qr_status_failed'.tr,
      );
    }
  }

  Future<ServiceResult<QRLoginScanData>> scan({
    required String rawPayload,
  }) async {
    final normalized = rawPayload.trim();
    if (normalized.isEmpty) {
      return ServiceResult<QRLoginScanData>.failure(
        message: 'login_qr_scan_failed'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }
    try {
      final response = await _dio.post(
        '/auth/qr/scan',
        data: {'raw_payload': normalized},
      );
      return _decodeDataResult(
        response.data,
        (json) => QRLoginScanData.fromJson(json),
        fallbackMessage: 'login_qr_scan_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<QRLoginScanData>(
        e,
        fallbackMessage: 'login_qr_scan_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<QRLoginScanData>.failure(
        message: 'login_qr_scan_failed'.tr,
      );
    }
  }

  Future<ServiceResult<void>> confirm({
    required String qrSessionId,
    required bool approve,
  }) async {
    final sessionId = qrSessionId.trim();
    if (sessionId.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'login_qr_confirm_failed'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }
    try {
      final response = await _dio.post(
        '/auth/qr/confirm',
        data: {'qr_session_id': sessionId, 'approve': approve},
      );
      return _decodePlainResult(
        response.data,
        fallbackMessage: 'login_qr_confirm_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<void>(
        e,
        fallbackMessage: 'login_qr_confirm_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<void>.failure(message: 'login_qr_confirm_failed'.tr);
    }
  }

  ServiceResult<T> _decodeDataResult<T>(
    dynamic raw,
    T Function(Map<String, dynamic>) decoder, {
    required String fallbackMessage,
  }) {
    if (raw is! Map) {
      return ServiceResult<T>.failure(message: fallbackMessage);
    }
    final body = Map<String, dynamic>.from(raw);
    final code = _toInt(body['code'], fallback: 50001);
    if (code != 0) {
      return ServiceResult<T>.failure(
        message: _extractMessage(body, fallbackMessage),
        code: code,
        httpStatus: 200,
      );
    }
    final dataRaw = body['data'];
    if (dataRaw is! Map) {
      return ServiceResult<T>.failure(message: fallbackMessage);
    }
    final data = decoder(Map<String, dynamic>.from(dataRaw));
    return ServiceResult<T>.success(data: data);
  }

  ServiceResult<void> _decodePlainResult(
    dynamic raw, {
    required String fallbackMessage,
  }) {
    if (raw is! Map) {
      return ServiceResult<void>.failure(message: fallbackMessage);
    }
    final body = Map<String, dynamic>.from(raw);
    final code = _toInt(body['code'], fallback: 50001);
    if (code != 0) {
      return ServiceResult<void>.failure(
        message: _extractMessage(body, fallbackMessage),
        code: code,
        httpStatus: 200,
      );
    }
    return ServiceResult<void>.success();
  }

  ServiceResult<T> _dioFailure<T>(
    DioException e, {
    required String fallbackMessage,
  }) {
    final body = e.response?.data;
    if (body is Map) {
      final json = Map<String, dynamic>.from(body);
      return ServiceResult<T>.failure(
        message: _extractMessage(json, fallbackMessage),
        code: _toInt(json['code'], fallback: 50001),
        httpStatus: e.response?.statusCode ?? 0,
      );
    }
    return ServiceResult<T>.failure(
      message: fallbackMessage,
      httpStatus: e.response?.statusCode ?? 0,
    );
  }

  String _extractMessage(Map<String, dynamic> body, String fallback) {
    final msg = body['msg']?.toString().trim() ?? '';
    if (msg.isEmpty) return fallback;
    return msg;
  }
}

int _toInt(dynamic value, {int fallback = 0}) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '') ?? fallback;
}
