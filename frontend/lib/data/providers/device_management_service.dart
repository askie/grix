import 'package:dio/dio.dart';
import 'package:get/get.dart' hide Response;

import '../../shared/utils/app_runtime_endpoints.dart';
import '../models/login_device_session_model.dart';
import 'auth_service.dart';

class DeviceManagementService {
  DeviceManagementService({Dio? dio})
    : _dio =
          dio ??
          Dio(
            BaseOptions(
              baseUrl: AppRuntimeEndpoints.apiBaseUrl,
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 10),
            ),
          ) {
    Get.find<AuthService>().attachAuthInterceptor(_dio);
  }

  final Dio _dio;

  Future<List<LoginDeviceSessionModel>> fetchSessions() async {
    try {
      final response = await _dio.get('/devices/sessions');
      final body = _asBody(response.data);
      if (response.statusCode != 200 ||
          body == null ||
          _toInt(body['code']) != 0) {
        throw _extractMessage(
          body,
          fallback: 'device_management_load_failed'.tr,
        );
      }

      final data = _asBody(body['data']);
      final rawItems = data?['items'];
      if (rawItems is! List) {
        return const <LoginDeviceSessionModel>[];
      }

      return rawItems
          .whereType<Map>()
          .map(
            (item) => LoginDeviceSessionModel.fromJson(
              Map<String, dynamic>.from(item),
            ),
          )
          .where((item) => item.sessionId.isNotEmpty)
          .toList(growable: false);
    } on DioException catch (e) {
      throw _extractMessage(
        _asBody(e.response?.data),
        fallback: 'device_management_load_failed'.tr,
      );
    }
  }

  Future<ServiceResult<void>> removeSession(String sessionId) async {
    final normalizedSessionId = sessionId.trim();
    if (normalizedSessionId.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'device_management_remove_failed'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final response = await _dio.delete(
        '/devices/sessions/${Uri.encodeComponent(normalizedSessionId)}',
      );
      final body = _asBody(response.data);
      if (response.statusCode != 200 ||
          body == null ||
          _toInt(body['code']) != 0) {
        return ServiceResult<void>.failure(
          message: _extractMessage(
            body,
            fallback: 'device_management_remove_failed'.tr,
          ),
          code: _toInt(body?['code']),
          httpStatus: response.statusCode ?? 0,
        );
      }
      return ServiceResult<void>.success(
        httpStatus: response.statusCode ?? 200,
      );
    } on DioException catch (e) {
      return ServiceResult<void>.failure(
        message: _extractMessage(
          _asBody(e.response?.data),
          fallback: 'device_management_remove_failed'.tr,
        ),
        code: _toInt(_asBody(e.response?.data)?['code']),
        httpStatus: e.response?.statusCode ?? 0,
      );
    }
  }

  Map<String, dynamic>? _asBody(dynamic source) {
    if (source is Map<String, dynamic>) {
      return source;
    }
    if (source is Map) {
      return Map<String, dynamic>.from(source);
    }
    return null;
  }

  String _extractMessage(
    Map<String, dynamic>? body, {
    required String fallback,
  }) {
    final message = body?['msg']?.toString().trim() ?? '';
    if (message.isEmpty) {
      return fallback;
    }
    return message;
  }

  int _toInt(dynamic value) {
    if (value is int) {
      return value;
    }
    if (value is num) {
      return value.toInt();
    }
    return int.tryParse(value?.toString() ?? '') ?? 0;
  }
}
