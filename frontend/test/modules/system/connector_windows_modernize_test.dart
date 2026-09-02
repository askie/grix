import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

/// Windows 存量 3.x 连接器：服务端 min_version 挡住了它们的自升级，桌面端必须
/// 用新版 CLI 替它们跑一次「装 latest → restart」，否则永远停在 3.x。
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.version);
  String version;
  @override
  void close({bool force = false}) {}
  @override
  Future<ResponseBody> fetch(
    RequestOptions o,
    Stream<Uint8List>? s,
    Future<void>? c,
  ) async {
    if (o.path.endsWith('/healthz')) {
      return ResponseBody.fromString(
        jsonEncode({
          'status': 'ok',
          'uptime': 1,
          'pid': 1,
          'version': version,
          'agents': [],
        }),
        200,
        headers: {
          Headers.contentTypeHeader: [Headers.jsonContentType],
        },
      );
    }
    return ResponseBody.fromString(
      '{}',
      404,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() {
    Get.testMode = true;
    Get.locale = const Locale('zh', 'CN');
    Get.addTranslations(AppTranslations().keys);
  });

  GrixConnectorService build(
    _FakeAdapter adapter,
    List<String> shells, {
    bool windows = true,
  }) {
    final service = GrixConnectorService()
      ..httpAdapter = adapter
      ..isWindowsPlatform = (() => windows)
      ..installShell =
          ((
            cmd, {
            required clientType,
            required timeoutSeconds,
            required skipVerify,
          }) async {
            shells.add(cmd);
            if (cmd.contains('restart')) adapter.version = '4.2.3';
            return true;
          });
    return service;
  }

  test('Windows 3.x 在线后：装 latest 并用新 CLI restart，一次即可', () async {
    final shells = <String>[];
    final adapter = _FakeAdapter('3.34.0');
    final service = build(adapter, shells);
    await service.checkHealth();
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(shells, [
      'npm install -g grix-connector@latest',
      'grix-connector restart',
    ]);
    expect(service.installedVersion.value, '4.2.3');
    await service.checkHealth();
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(shells.length, 2, reason: '升上去后不再重复');
  });

  test('npm 装包失败：不 restart，本次运行不再重试', () async {
    final shells = <String>[];
    final adapter = _FakeAdapter('3.30.0');
    final service = build(adapter, shells)
      ..installShell =
          ((
            cmd, {
            required clientType,
            required timeoutSeconds,
            required skipVerify,
          }) async {
            shells.add(cmd);
            return false;
          });
    await service.checkHealth();
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(shells.where((c) => c.contains('restart')), isEmpty);
    expect(service.modernizeAttemptedForTest, isTrue);
    await service.checkHealth();
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(
      shells
          .where((c) => c.startsWith('npm install -g grix-connector@latest'))
          .length,
      lessThanOrEqualTo(2),
      reason: '最多官方源 + 镜像各一次',
    );
  });

  test('非 Windows 或已是 4.x：不动', () async {
    for (final (win, ver) in [
      (false, '3.34.0'),
      (true, '4.0.0'),
      (true, '4.2.3'),
    ]) {
      final shells = <String>[];
      final service = build(_FakeAdapter(ver), shells, windows: win);
      await service.checkHealth();
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(shells, isEmpty, reason: 'win=$win ver=$ver');
    }
  });
}
