import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

/// 升级链路的失败面：connector 拒绝、接口抖动、升级失败回滚。
/// 这些路径全都不能退化成"点了没反应"或"看不出到底怎么了"。
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.respond);

  final ResponseBody Function(RequestOptions options) respond;
  final requested = <String>[];

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requested.add(options.uri.toString());
    return respond(options);
  }
}

ResponseBody _json(Map<String, dynamic> body, int status) =>
    ResponseBody.fromString(
      jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );

void main() {
  tearDown(Get.reset);

  GrixConnectorService runningService(_FakeAdapter adapter) =>
      GrixConnectorService()
        ..isRunning.value = true
        ..installedVersion.value = '3.5.0'
        ..latestVersion.value = '3.6.0'
        ..httpAdapter = adapter;

  test('connector 拒绝升级：如实报出它的状态码，不能说成"连接失败"', () async {
    // 409 = 已有一次升级在跑。连接器答了，就不是连不上。
    final service = runningService(
      _FakeAdapter((_) => _json({'error': 'upgrade in progress'}, 409)),
    );

    final outcome = await service.upgrade();

    expect(outcome, ConnectorUpgradeOutcome.failed);
    expect(service.lastError.value, contains('409'));
    expect(service.upgradeQueued, isFalse, reason: '没下发成功就不能进等待态');
  });

  test('下发成功：进等待态', () async {
    final service = runningService(
      _FakeAdapter((_) => _json({'ok': true}, 200)),
    );

    final outcome = await service.upgrade();

    expect(outcome, ConnectorUpgradeOutcome.queued);
    expect(service.upgradeQueuedVersion.value, '3.6.0');
    expect(service.upgradeQueued, isTrue);
  });

  test('升级失败回滚后：等待态必须有出口，否则按钮永远回不来', () async {
    final now = DateTime(2026, 7, 14, 12);
    final service = runningService(
      _FakeAdapter((_) => _json({'ok': true}, 200)),
    )..clock = () => now;

    await service.upgrade();
    expect(service.upgradeQueued, isTrue);

    // connector 装包失败、回滚了，仍旧跑在 3.5.0 并继续报 3.6.0 可用。
    // 等待态若没有时效，界面就永久停在"已通知后台升级"，用户再也点不了升级。
    service.clock = () => now.add(GrixConnectorService.upgradeQueueTtl * 2);

    expect(service.upgradeQueued, isFalse);
  });

  test('GET /api/upgrade 抖了一下：不能拿 npm latest 覆盖它（那会绕开灰度）', () async {
    final adapter = _FakeAdapter((options) {
      if (options.uri.host == 'registry.npmjs.org') {
        return _json({'version': '9.9.9'}, 200);
      }
      return _json({'error': 'busy'}, 500);
    });
    final service = GrixConnectorService()
      ..isRunning.value = true
      ..installedVersion.value = '3.5.0'
      ..httpAdapter = adapter;

    await service.checkLatestVersion();

    expect(service.latestVersion.value, isEmpty, reason: '这一轮问不出来就先不更新');
    expect(service.hasUpdate, isFalse);
    expect(
      adapter.requested.any((u) => u.contains('registry.npmjs.org')),
      isFalse,
      reason: 'connector 在跑，它的判断才作数——npm latest 会亮出这台机器根本升不了的版本',
    );
  });

  test('老 connector 没有这个接口（404）：才允许回落 npm', () async {
    final adapter = _FakeAdapter((options) {
      if (options.uri.host == 'registry.npmjs.org') {
        return _json({'version': '3.6.0'}, 200);
      }
      return _json({'error': 'not found'}, 404);
    });
    final service = GrixConnectorService()
      ..isRunning.value = true
      ..installedVersion.value = '3.5.0'
      ..httpAdapter = adapter;

    await service.checkLatestVersion();

    expect(service.latestVersion.value, '3.6.0');
    expect(service.hasUpdate, isTrue);
  });

  test('daemon 报不出版本且 connector 说没得升：不能把这台机器判成"一切正常"', () async {
    // /healthz 不带 version 的老 daemon —— 它正是需要被升级救回来的那种。
    // 若把可用版本抹成空串，hasUpdate 里"运行中报不出版本就提示更新"的逃生通道就废了。
    final adapter = _FakeAdapter((options) {
      if (options.uri.host == 'registry.npmjs.org') {
        return _json({'version': '3.6.0'}, 200);
      }
      return _json({'available': false}, 200);
    });
    final service = GrixConnectorService()
      ..isRunning.value = true
      ..installedVersion.value = ''
      ..httpAdapter = adapter;

    await service.checkLatestVersion();

    expect(service.latestVersion.value, isNotEmpty);
    expect(service.hasUpdate, isTrue, reason: '这台机器必须还留着升级入口');
  });
}
