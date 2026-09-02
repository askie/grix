import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/services/link_safety_service.dart';

/// 用 dio 自带的 Interceptor 做轻量 mock：根据请求路径返回预设响应或抛错。
/// 不引第三方 mock 包，保持依赖洁净。
class _MockHandler extends Interceptor {
  _MockHandler(this.handler);

  /// 由测试设置：收到请求时返回 Response 或抛 DioException
  final dynamic Function(RequestOptions options) handler;

  @override
  void onRequest(RequestOptions options, RequestInterceptorHandler h) {
    try {
      final result = handler(options);
      if (result is Response) {
        h.resolve(result);
      } else if (result is DioException) {
        h.reject(result);
      } else {
        h.next(options);
      }
    } catch (e) {
      h.reject(DioException(requestOptions: options, error: e));
    }
  }
}

Dio _newDio(dynamic Function(RequestOptions o) handler) {
  final d = Dio(BaseOptions(baseUrl: 'http://x'));
  d.interceptors.add(_MockHandler(handler));
  return d;
}

Response<dynamic> _ok(RequestOptions o, Map<String, dynamic> dataField) {
  return Response<dynamic>(
    requestOptions: o,
    statusCode: 200,
    data: {'code': 0, 'msg': 'ok', 'data': dataField},
  );
}

void main() {
  group('LinkSafetyService.check', () {
    test('clean -> 返回 clean', () async {
      final svc = LinkSafetyService(
        dio: _newDio(
          (o) => _ok(o, {
            'results': [
              {'url': 'http://x.com', 'verdict': 'clean'},
            ],
          }),
        ),
      );
      final v = await svc.check('http://x.com');
      expect(v.level, LinkVerdictLevel.clean);
    });

    test('malicious -> 返回 malicious 并保留 reason / canonical_host', () async {
      final svc = LinkSafetyService(
        dio: _newDio(
          (o) => _ok(o, {
            'results': [
              {
                'url': 'http://evil.com',
                'verdict': 'malicious',
                'canonical_host': 'evil.com',
                'reason': 'domain',
              },
            ],
          }),
        ),
      );
      final v = await svc.check('http://evil.com');
      expect(v.level, LinkVerdictLevel.malicious);
      expect(v.canonicalHost, 'evil.com');
      expect(v.reason, 'domain');
    });

    test('suspicious -> 返回 suspicious', () async {
      final svc = LinkSafetyService(
        dio: _newDio(
          (o) => _ok(o, {
            'results': [
              {'url': 'http://x.com', 'verdict': 'suspicious'},
            ],
          }),
        ),
      );
      final v = await svc.check('http://x.com');
      expect(v.level, LinkVerdictLevel.suspicious);
    });

    test('网络失败 -> 按 suspicious 兜底', () async {
      final svc = LinkSafetyService(
        dio: _newDio((o) {
          return DioException(
            requestOptions: o,
            type: DioExceptionType.connectionTimeout,
          );
        }),
      );
      final v = await svc.check('http://x.com');
      expect(v.level, LinkVerdictLevel.suspicious);
      expect(v.reason, 'network');
    });

    test('401 -> 按 clean 放行（避免登录态问题误挡用户）', () async {
      final svc = LinkSafetyService(
        dio: _newDio((o) {
          return DioException(
            requestOptions: o,
            type: DioExceptionType.badResponse,
            response: Response<dynamic>(requestOptions: o, statusCode: 401),
          );
        }),
      );
      final v = await svc.check('http://x.com');
      expect(v.level, LinkVerdictLevel.clean);
    });

    test('5xx -> 按 suspicious 兜底', () async {
      final svc = LinkSafetyService(
        dio: _newDio((o) {
          return DioException(
            requestOptions: o,
            type: DioExceptionType.badResponse,
            response: Response<dynamic>(requestOptions: o, statusCode: 503),
          );
        }),
      );
      final v = await svc.check('http://x.com');
      expect(v.level, LinkVerdictLevel.suspicious);
    });

    test('LRU 命中 -> 同一 URL 二次点击不再调网络', () async {
      var calls = 0;
      final svc = LinkSafetyService(
        dio: _newDio((o) {
          calls++;
          return _ok(o, {
            'results': [
              {'url': 'http://x.com', 'verdict': 'malicious'},
            ],
          });
        }),
      );
      final v1 = await svc.check('http://x.com');
      final v2 = await svc.check('http://x.com');
      expect(v1.level, LinkVerdictLevel.malicious);
      expect(v2.level, LinkVerdictLevel.malicious);
      expect(calls, 1, reason: '同 URL 二次点击应只发一次请求');
    });

    test('空 URL -> 直接 clean，不发请求', () async {
      var calls = 0;
      final svc = LinkSafetyService(
        dio: _newDio((o) {
          calls++;
          return _ok(o, {'results': []});
        }),
      );
      final v = await svc.check('   ');
      expect(v.level, LinkVerdictLevel.clean);
      expect(calls, 0);
    });
  });
}
