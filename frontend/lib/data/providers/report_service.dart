import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

class ReportAssetPresignData {
  const ReportAssetPresignData({
    required this.assetKey,
    required this.uploadUrl,
    required this.expiresInSeconds,
  });

  final String assetKey;
  final String uploadUrl;
  final int expiresInSeconds;
}

class ReportCreateData {
  const ReportCreateData({required this.reportId, required this.status});

  final String reportId;
  final String status;
}

class ReportService {
  ReportService({Dio? dio, AuthService? authService})
    : _dio =
          dio ??
          Dio(
            BaseOptions(
              baseUrl: AppRuntimeEndpoints.apiBaseUrl,
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 20),
            ),
          ) {
    final resolvedAuthService =
        authService ??
        (Get.isRegistered<AuthService>() ? Get.find<AuthService>() : null);
    resolvedAuthService?.attachAuthInterceptor(_dio);
  }

  final Dio _dio;

  Future<ServiceResult<ReportAssetPresignData>> presignAsset({
    required String filename,
    required String contentType,
  }) async {
    final normalizedFilename = filename.trim();
    final normalizedContentType = contentType.trim();
    if (normalizedFilename.isEmpty || normalizedContentType.isEmpty) {
      return ServiceResult<ReportAssetPresignData>.failure(
        message: 'report_upload_failed'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final resp = await _dio.post(
        '/reports/assets/presign',
        data: {
          'filename': normalizedFilename,
          'content_type': normalizedContentType,
        },
      );
      final body = _asBody(resp.data);
      if (resp.statusCode == 200 && body != null && _responseCode(body) == 0) {
        final data = _asBody(body['data']);
        if (data == null) {
          return ServiceResult<ReportAssetPresignData>.failure(
            message: 'report_upload_failed'.tr,
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
          );
        }

        final assetKey = data['asset_key']?.toString().trim() ?? '';
        final uploadUrl = data['upload_url']?.toString().trim() ?? '';
        if (assetKey.isEmpty || uploadUrl.isEmpty) {
          return ServiceResult<ReportAssetPresignData>.failure(
            message: 'report_upload_failed'.tr,
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
          );
        }

        return ServiceResult<ReportAssetPresignData>.success(
          data: ReportAssetPresignData(
            assetKey: assetKey,
            uploadUrl: uploadUrl,
            expiresInSeconds: _toInt(data['expires_in_seconds']),
          ),
          httpStatus: resp.statusCode ?? 200,
        );
      }

      return ServiceResult<ReportAssetPresignData>.failure(
        message: _responseMessage(body) ?? 'report_upload_failed'.tr,
        code: _responseCode(body),
        httpStatus: resp.statusCode ?? 0,
      );
    } on DioException catch (e) {
      final result = _dioFailure<ReportAssetPresignData>(
        e,
        fallbackMessage: 'report_upload_failed'.tr,
      );
      _logError('presignAsset', result.message, error: e);
      return result;
    } catch (e) {
      _logError('presignAsset', '$e', error: e);
      return ServiceResult<ReportAssetPresignData>.failure(
        message: 'report_upload_failed'.tr,
      );
    }
  }

  Future<ServiceResult<ReportCreateData>> createReport({
    required String targetType,
    required String targetUserId,
    required String targetSessionId,
    required String sourceSessionId,
    required String reasonCode,
    required String description,
    required List<String> assetKeys,
  }) async {
    try {
      final resp = await _dio.post(
        '/reports',
        data: {
          'target_type': targetType.trim(),
          'target_user_id': targetUserId.trim(),
          'target_session_id': targetSessionId.trim(),
          'source_session_id': sourceSessionId.trim(),
          'reason_code': reasonCode.trim(),
          'description': description.trim(),
          'asset_keys': assetKeys,
        },
      );
      final body = _asBody(resp.data);
      if (resp.statusCode == 200 && body != null && _responseCode(body) == 0) {
        final data = _asBody(body['data']);
        if (data == null) {
          return ServiceResult<ReportCreateData>.failure(
            message: 'report_submit_failed'.tr,
            code: 50001,
            httpStatus: resp.statusCode ?? 200,
          );
        }

        return ServiceResult<ReportCreateData>.success(
          data: ReportCreateData(
            reportId: data['report_id']?.toString().trim() ?? '',
            status: data['status']?.toString().trim() ?? '',
          ),
          httpStatus: resp.statusCode ?? 200,
        );
      }

      return ServiceResult<ReportCreateData>.failure(
        message: _responseMessage(body) ?? 'report_submit_failed'.tr,
        code: _responseCode(body),
        httpStatus: resp.statusCode ?? 0,
      );
    } on DioException catch (e) {
      final result = _dioFailure<ReportCreateData>(
        e,
        fallbackMessage: 'report_submit_failed'.tr,
      );
      _logError('createReport', result.message, error: e);
      return result;
    } catch (e) {
      _logError('createReport', '$e', error: e);
      return ServiceResult<ReportCreateData>.failure(
        message: 'report_submit_failed'.tr,
      );
    }
  }

  Map<String, dynamic>? _asBody(dynamic raw) {
    if (raw is Map<String, dynamic>) {
      return raw;
    }
    if (raw is Map) {
      return Map<String, dynamic>.from(raw);
    }
    return null;
  }

  int _responseCode(Map<String, dynamic>? body) {
    if (body == null) {
      return 50001;
    }
    return _toInt(body['code'], fallback: 50001);
  }

  String? _responseMessage(Map<String, dynamic>? body) {
    final message = body?['msg']?.toString().trim() ?? '';
    return message.isEmpty ? null : message;
  }

  int _toInt(dynamic value, {int fallback = 0}) {
    if (value is int) {
      return value;
    }
    if (value is num) {
      return value.toInt();
    }
    return int.tryParse(value?.toString() ?? '') ?? fallback;
  }

  ServiceResult<T> _dioFailure<T>(
    DioException error, {
    required String fallbackMessage,
  }) {
    final responseBody = _asBody(error.response?.data);
    final message =
        _responseMessage(responseBody) ??
        error.message?.trim() ??
        fallbackMessage;
    return ServiceResult<T>.failure(
      message: message.isEmpty ? fallbackMessage : message,
      code: _responseCode(responseBody),
      httpStatus: error.response?.statusCode ?? 0,
    );
  }

  void _logError(String action, String message, {Object? error}) {
    debugPrint('ReportService.$action failed: $message');
    if (error != null) {
      debugPrint('ReportService.$action error: $error');
    }
  }
}
