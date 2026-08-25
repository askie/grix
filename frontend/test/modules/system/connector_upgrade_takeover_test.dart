import 'dart:convert';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

/// 升级事务停滞接管：daemon 在线但 pending 停在 guardian 阶段不动时，看门狗
/// （只管离线）永远不会介入，桌面端必须自己把机器带上能收口事务的新版本。
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.respond);

  ResponseBody Function(RequestOptions options) respond;

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return respond(options);
  }
}

ResponseBody _healthz({Map<String, dynamic>? upgrade}) =>
    ResponseBody.fromString(
      jsonEncode({
        'status': 'ok',
        'uptime': 1,
        'pid': 4242,
        'version': '4.2.5',
        'agents': <dynamic>[],
        if (upgrade != null) 'upgrade': upgrade,
      }),
      200,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );

Future<ProcessResult> _noopRun(
  String executable,
  List<String> arguments,
) async => ProcessResult(0, 0, '', '');

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
    Get.addTranslations(AppTranslations().keys);
    Get.locale = const Locale('zh', 'CN');
  });

  tearDown(() {
    debugDefaultTargetPlatformOverride = null;
    Get.reset();
  });

  final t0 = DateTime(2026, 8, 25, 9);

  (GrixConnectorService, List<String>) build(
    _FakeAdapter adapter,
    DateTime Function() clock,
  ) {
    final cmds = <String>[];
    final service = GrixConnectorService()
      ..httpAdapter = adapter
      ..processRun = _noopRun
      ..startProbeDelay = Duration.zero
      ..loadLastGoodVersion = (() async => null)
      ..saveLastGoodVersion = ((_) async {})
      ..clock = clock;
    service.installShell =
        (
          cmd, {
          required String clientType,
          required int timeoutSeconds,
          required bool skipVerify,
        }) async {
          cmds.add(cmd);
          return true;
        };
    return (service, cmds);
  }

  Future<void> settle(GrixConnectorService service) async {
    await service.stalledUpgradeTakeoverForTest;
  }

  test('同一 phase 停滞超过 30 分钟：重装最新版并 restart', () async {
    var now = t0;
    final adapter = _FakeAdapter(
      (_) => _healthz(upgrade: {'in_progress': true, 'phase': 'handoff_ready'}),
    );
    final (service, cmds) = build(adapter, () => now);

    await service.checkHealth();
    await settle(service);
    expect(cmds, isEmpty);

    now = t0.add(const Duration(minutes: 29));
    await service.checkHealth();
    await settle(service);
    expect(cmds, isEmpty, reason: '窗口内不能抢跑，guardian 可能还在干活');

    now = t0.add(const Duration(minutes: 31));
    await service.checkHealth();
    await settle(service);
    expect(cmds, [
      'npm install -g grix-connector@latest',
      'grix-connector restart',
    ]);

    // 同一会话只接管一次，healthz 仍报在途也不再反复装包
    now = t0.add(const Duration(minutes: 90));
    await service.checkHealth();
    await settle(service);
    expect(cmds, hasLength(2));
  });

  test('phase 在推进就重新计时，不算停滞', () async {
    var now = t0;
    var phase = 'handoff_ready';
    final adapter = _FakeAdapter(
      (_) => _healthz(upgrade: {'in_progress': true, 'phase': phase}),
    );
    final (service, cmds) = build(adapter, () => now);

    await service.checkHealth();
    now = t0.add(const Duration(minutes: 20));
    phase = 'activating';
    await service.checkHealth();
    now = t0.add(const Duration(minutes: 35));
    await service.checkHealth();
    await settle(service);

    expect(cmds, isEmpty);
  });

  test('事务收场后计时清零，新事务从头计', () async {
    var now = t0;
    Map<String, dynamic>? upgrade = {'in_progress': true, 'phase': 'verifying'};
    final adapter = _FakeAdapter((_) => _healthz(upgrade: upgrade));
    final (service, cmds) = build(adapter, () => now);

    await service.checkHealth();
    now = t0.add(const Duration(minutes: 20));
    upgrade = null;
    await service.checkHealth();
    now = t0.add(const Duration(minutes: 25));
    upgrade = {'in_progress': true, 'phase': 'verifying'};
    await service.checkHealth();
    now = t0.add(const Duration(minutes: 40));
    await service.checkHealth();
    await settle(service);

    expect(cmds, isEmpty);
  });

  test('npm 装包失败就不 restart，留给下一次 App 会话', () async {
    var now = t0;
    final adapter = _FakeAdapter(
      (_) => _healthz(upgrade: {'in_progress': true, 'phase': 'activating'}),
    );
    final (service, cmds) = build(adapter, () => now);
    service.installShell =
        (
          cmd, {
          required String clientType,
          required int timeoutSeconds,
          required bool skipVerify,
        }) async {
          cmds.add(cmd);
          service.installLog.value = 'npm ERR! EACCES: permission denied';
          return false;
        };

    await service.checkHealth();
    now = t0.add(const Duration(minutes: 31));
    await service.checkHealth();
    await settle(service);

    expect(cmds, ['npm install -g grix-connector@latest']);
  });
}
