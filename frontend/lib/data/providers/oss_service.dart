import 'package:dio/dio.dart';
import 'package:get/get.dart';
import 'package:flutter/foundation.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

class OssService extends GetxService {
  final _dio = Dio(
    BaseOptions(
      baseUrl: AppRuntimeEndpoints.apiBaseUrl,
      connectTimeout: const Duration(seconds: 10),
      receiveTimeout: const Duration(seconds: 30),
    ),
  );

  @override
  void onInit() {
    super.onInit();
    if (Get.isRegistered<AuthService>()) {
      Get.find<AuthService>().attachAuthInterceptor(_dio);
    }
  }

  /// 请求后端的 presign 接口获取上传 URL 和访问 URL
  Future<Map<String, String>?> getPresignedUrl(
    String filename,
    String contentType,
  ) async {
    try {
      final res = await _dio.post(
        '/oss/presign',
        data: {'filename': filename, 'content_type': contentType},
      );

      if (res.statusCode == 200 && res.data['code'] == 0) {
        final data = res.data['data'] as Map<String, dynamic>;
        final uploadUrl = data['upload_url']?.toString();
        // 如果后端不返回 media_access_url，前端可以自己拼或者等发送消息时后端处理。假设后端返回或者我们只需要 uploadUrl 就够用。这里为了兼容旧逻辑暂时读取 media_access_url 可能为空。
        final accessUrl =
            data['media_access_url']?.toString() ??
            data['object_key']?.toString();

        if (uploadUrl != null && uploadUrl.isNotEmpty) {
          return {
            'uploadUrl': uploadUrl,
            'accessUrl': accessUrl ?? '',
            'objectKey': data['object_key']?.toString() ?? '',
          };
        }
      } else {
        debugPrint('❌ getPresignedUrl failed: ${res.data['msg']}');
      }
    } catch (e) {
      debugPrint('❌ getPresignedUrl error: $e');
    }
    return null;
  }

  /// 将文件字节流 PUT 上传到 OSS
  Future<bool> uploadToOss(
    String uploadUrl,
    Uint8List fileBytes, {
    String? contentType,
  }) async {
    try {
      final uploadDio = Dio(
        BaseOptions(
          connectTimeout: const Duration(seconds: 10),
          sendTimeout: const Duration(minutes: 2),
          receiveTimeout: const Duration(minutes: 2),
        ),
      );
      final options = Options(
        headers: {
          Headers.contentLengthHeader: fileBytes.length.toString(),
          if (contentType != null) Headers.contentTypeHeader: contentType,
        },
      );

      final res = await uploadDio.put(
        uploadUrl,
        data: fileBytes,
        options: options,
        onSendProgress: (count, total) {
          if (total > 0) {
            // debugPrint('Upload progress: ${(count / total * 100).toStringAsFixed(2)}%');
          }
        },
      );

      if (res.statusCode == 200 ||
          res.statusCode == 201 ||
          res.statusCode == 204) {
        return true;
      } else {
        debugPrint(
          '❌ uploadToOss failed: HTTP ${res.statusCode} ${res.statusMessage} body=${res.data}',
        );
      }
    } on DioException catch (e) {
      debugPrint(
        '❌ uploadToOss dio error: type=${e.type} status=${e.response?.statusCode} message=${e.message} body=${e.response?.data}',
      );
    } catch (e) {
      debugPrint('❌ uploadToOss error: $e');
    }
    return false;
  }

  Future<bool> uploadStreamToOss(
    String uploadUrl,
    Stream<List<int>> stream, {
    required int contentLength,
    String? contentType,
  }) async {
    try {
      final uploadDio = Dio(
        BaseOptions(
          connectTimeout: const Duration(seconds: 10),
          sendTimeout: const Duration(minutes: 8),
          receiveTimeout: const Duration(minutes: 8),
        ),
      );
      final options = Options(
        headers: {
          Headers.contentLengthHeader: contentLength.toString(),
          if (contentType != null) Headers.contentTypeHeader: contentType,
        },
      );

      final res = await uploadDio.put(
        uploadUrl,
        data: stream,
        options: options,
      );

      if (res.statusCode == 200 ||
          res.statusCode == 201 ||
          res.statusCode == 204) {
        return true;
      } else {
        debugPrint(
          '❌ uploadStreamToOss failed: HTTP ${res.statusCode} ${res.statusMessage} body=${res.data}',
        );
      }
    } on DioException catch (e) {
      debugPrint(
        '❌ uploadStreamToOss dio error: type=${e.type} status=${e.response?.statusCode} message=${e.message} body=${e.response?.data}',
      );
    } catch (e) {
      debugPrint('❌ uploadStreamToOss error: $e');
    }
    return false;
  }

  Future<bool> deleteObjects(List<String> objectKeys) async {
    final normalized = objectKeys
        .map((item) => item.trim())
        .where((item) => item.isNotEmpty)
        .toList(growable: false);
    if (normalized.isEmpty) {
      return true;
    }

    try {
      final res = await _dio.post(
        '/oss/delete',
        data: {'object_keys': normalized},
      );
      return res.statusCode == 200 && res.data['code'] == 0;
    } catch (e) {
      debugPrint('❌ deleteObjects error: $e');
      return false;
    }
  }
}
