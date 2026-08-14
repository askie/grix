import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/core/network/api_client.dart';
import 'package:grix_admin/core/network/api_exception.dart';
import 'package:grix_admin/modules/users/user_service.dart';

/// 伪网络适配器：按请求返回预设响应，验证 ApiClient 信封拆解与服务解析。
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.handler);

  final ResponseBody Function(RequestOptions options) handler;

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async => handler(options);
}

ResponseBody _json(Map<String, dynamic> body, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

void main() {
  test('UserService.list 解析分页与查询参数', () async {
    late RequestOptions captured;
    ApiClient.instance.httpClientAdapter = _FakeAdapter((options) {
      captured = options;
      return _json({
        'code': 0,
        'msg': 'success',
        'data': {
          'items': [
            {'id': '1', 'username': 'alice', 'status': 1},
          ],
          'total': 1,
          'page': 1,
          'page_size': 20,
        },
      });
    });

    final result = await UserService.list(
      query: 'ali',
      status: 'active',
      onlineOnly: true,
    );

    expect(result.total, 1);
    expect(result.items.single.username, 'alice');
    expect(captured.path, '/users');
    expect(captured.queryParameters['q'], 'ali');
    expect(captured.queryParameters['status'], 'active');
    expect(captured.queryParameters['online'], true);
  });

  test('业务错误码抛 ApiException', () async {
    ApiClient.instance.httpClientAdapter = _FakeAdapter(
      (options) => _json({'code': 10004, 'msg': '出错了'}, status: 200),
    );

    expect(() => UserService.list(), throwsA(isA<ApiException>()));
  });
}
