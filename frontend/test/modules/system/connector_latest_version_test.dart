import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

/// "有没有新版"必须和"connector 会不会升"同源：升级由 connector 按后端的灰度规则执行，
/// npm registry 上出了新包不代表这台机器被圈进灰度。判断源一旦跑偏，灰度期内没被圈中的
/// 机器会一直亮着升级按钮，点了 connector 却什么都不做。
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.respond);

  /// path -> json body
  final Map<String, dynamic> Function(RequestOptions options) respond;
  final requestedPaths = <String>[];

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requestedPaths.add(options.uri.toString());
    return ResponseBody.fromString(
      jsonEncode(respond(options)),
      200,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }
}

void main() {
  tearDown(Get.reset);

  test('connector 说灰度没放行：不提示更新，也不去问 npm', () async {
    final adapter = _FakeAdapter((_) => {'available': false});
    final service = GrixConnectorService()
      ..isRunning.value = true
      ..installedVersion.value = '3.4.0'
      ..httpAdapter = adapter;

    await service.checkLatestVersion();

    expect(service.hasUpdate, isFalse);
    expect(
      adapter.requestedPaths.any((p) => p.contains('registry.npmjs.org')),
      isFalse,
      reason: 'connector 已经给出答案，不该再拿 npm latest 覆盖它',
    );
  });

  test('connector 说放行 3.6.0：按它给的版本提示更新', () async {
    final adapter = _FakeAdapter(
      (_) => {
        'available': true,
        'release': {'version': '3.6.0'},
      },
    );
    final service = GrixConnectorService()
      ..isRunning.value = true
      ..installedVersion.value = '3.4.0'
      ..httpAdapter = adapter;

    await service.checkLatestVersion();

    expect(service.latestVersion.value, '3.6.0');
    expect(service.hasUpdate, isTrue);
  });

  test('daemon 没在跑：问不到 connector，回落到 npm registry', () async {
    final adapter = _FakeAdapter((_) => {'version': '3.6.0'});
    final service = GrixConnectorService()
      ..isRunning.value = false
      ..installedVersion.value = '3.4.0'
      ..httpAdapter = adapter;

    await service.checkLatestVersion();

    expect(service.latestVersion.value, '3.6.0');
    expect(
      adapter.requestedPaths.single.contains('registry.npmjs.org'),
      isTrue,
    );
  });
}
