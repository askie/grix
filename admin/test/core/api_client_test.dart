import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:dio/io.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/core/network/api_client.dart';
import 'package:grix_admin/core/network/api_exception.dart';

class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.response);

  final ResponseBody Function(RequestOptions options) response;

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return response(options);
  }
}

void main() {
  group('ApiClient', () {
    test(
      'rejects successful non-envelope response with readable error',
      () async {
        ApiClient.instance.httpClientAdapter = _FakeAdapter(
          (_) => ResponseBody.fromString(
            '<!DOCTYPE html><html></html>',
            200,
            headers: {
              Headers.contentTypeHeader: ['text/html; charset=utf-8'],
            },
          ),
        );

        expect(
          () => ApiClient.instance.get('/visitor-bans'),
          throwsA(
            isA<ApiException>()
                .having((e) => e.message, 'message', contains('接口返回格式异常'))
                .having((e) => e.statusCode, 'statusCode', 200),
          ),
        );
      },
    );

    test('unwraps successful JSON envelope', () async {
      ApiClient.instance.httpClientAdapter = _FakeAdapter(
        (_) => ResponseBody.fromBytes(
          utf8.encode(
            jsonEncode({
              'code': 0,
              'msg': 'success',
              'data': {'ok': true},
            }),
          ),
          200,
          headers: {
            Headers.contentTypeHeader: [Headers.jsonContentType],
          },
        ),
      );

      final data = await ApiClient.instance.get('/visitor-bans');

      expect(data, {'ok': true});
    });
  });
}
