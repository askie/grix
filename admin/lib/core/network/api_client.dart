import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

import '../config/app_config.dart';
import '../storage/token_store.dart';
import 'api_exception.dart';

/// 统一的 API 客户端。
///
/// 职责：
///   - 注入 Bearer Token；
///   - 拆解后端统一响应信封 `{code, msg, data}`，成功返回 data，失败抛 [ApiException]；
///   - 处理网络层与 HTTP 错误，统一转换为 [ApiException]；
///   - 401 时回调 [onUnauthorized]（由上层接管跳转登录）。
class ApiClient {
  ApiClient._() {
    _dio = Dio(
      BaseOptions(
        baseUrl: AppConfig.apiRoot,
        connectTimeout: const Duration(seconds: 15),
        receiveTimeout: const Duration(seconds: 30),
        // 不让 Dio 因非 2xx 抛错，统一在拦截器里按信封处理。
        validateStatus: (_) => true,
      ),
    );

    _dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          final token = TokenStore.current;
          if (token != null && token.isNotEmpty) {
            options.headers['Authorization'] = 'Bearer $token';
          }
          handler.next(options);
        },
      ),
    );
  }

  static final ApiClient instance = ApiClient._();

  late final Dio _dio;

  /// 401 未授权回调，由上层（如根导航）设置以跳转登录页。
  void Function()? onUnauthorized;

  /// 切换区域时动态更新 baseUrl，使后续请求打到新的后端。
  void updateBaseUrl(String newBaseUrl) {
    _dio.options.baseUrl = newBaseUrl;
  }

  /// 仅供测试：替换底层网络适配器以注入伪响应。
  @visibleForTesting
  set httpClientAdapter(HttpClientAdapter adapter) =>
      _dio.httpClientAdapter = adapter;

  Future<dynamic> get(String path, {Map<String, dynamic>? query}) {
    return _request(() => _dio.get(path, queryParameters: query));
  }

  Future<dynamic> post(
    String path, {
    Object? data,
    Map<String, dynamic>? query,
  }) {
    return _request(() => _dio.post(path, data: data, queryParameters: query));
  }

  Future<dynamic> put(String path, {Object? data}) {
    return _request(() => _dio.put(path, data: data));
  }

  Future<dynamic> delete(String path, {Object? data}) {
    return _request(() => _dio.delete(path, data: data));
  }

  /// 执行请求并统一处理信封与错误。返回 data 字段内容。
  Future<dynamic> _request(Future<Response> Function() run) async {
    Response resp;
    try {
      resp = await run();
    } on DioException catch (e) {
      throw ApiException(_networkMessage(e));
    }

    final status = resp.statusCode ?? 0;
    final body = resp.data;

    // 期望信封为 Map：{code, msg, data}
    if (body is Map) {
      final code = (body['code'] as num?)?.toInt() ?? -1;
      final msg = (body['msg'] ?? '').toString();
      if (status == 401) {
        _handleUnauthorized();
        throw ApiException(
          msg.isEmpty ? '登录已失效，请重新登录' : msg,
          code: code,
          statusCode: status,
        );
      }
      if (code == 0) {
        return body['data'];
      }
      throw ApiException(
        msg.isEmpty ? '请求失败' : msg,
        code: code,
        statusCode: status,
      );
    }

    if (status == 401) {
      _handleUnauthorized();
      throw ApiException('登录已失效，请重新登录', statusCode: status);
    }
    if (status >= 200 && status < 300) {
      throw ApiException('接口返回格式异常，请确认后端服务已更新', statusCode: status);
    }
    throw ApiException('请求失败（HTTP $status）', statusCode: status);
  }

  void _handleUnauthorized() {
    final cb = onUnauthorized;
    if (cb != null) {
      cb();
    }
  }

  String _networkMessage(DioException e) {
    switch (e.type) {
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.receiveTimeout:
      case DioExceptionType.sendTimeout:
        return '网络超时，请稍后重试';
      case DioExceptionType.connectionError:
        return '无法连接服务器，请检查网络';
      default:
        return '网络异常：${e.message ?? '未知错误'}';
    }
  }
}
