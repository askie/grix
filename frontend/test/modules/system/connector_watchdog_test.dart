import 'dart:convert';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

/// 看门狗的故障处置面：崩溃循环必须带退避，挂死的 daemon 必须能被杀掉重拉。
/// 这些路径退化的表象都是"connector 永远好不了"或"每 10 秒 spawn 一次进程"。
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

ResponseBody _json(Map<String, dynamic> body, int status) =>
    ResponseBody.fromString(
      jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );

ResponseBody _healthzOk({int pid = 4242}) => _json({
      'status': 'ok',
      'uptime': 1,
      'pid': pid,
      'version': '3.20.0',
      'agents': <dynamic>[],
    }, 200);

/// 记录进程调用的假 runner。看门狗和 restartDaemon 的所有进程操作都走注入的
/// processRun，测试里绝不真的 spawn / 杀进程。
class _FakeProcessRunner {
  final calls = <List<String>>[];

  /// pid 探活结果：true = 进程还活着
  bool pidAlive = false;

  /// ps 报出的命令行（身份校验依据）
  String psCommandLine = '/usr/local/bin/node /usr/local/bin/grix-connector start';

  Future<ProcessResult> call(String executable, List<String> arguments) async {
    calls.add([executable, ...arguments]);
    if (executable == 'ps') {
      return ProcessResult(0, 0, psCommandLine, '');
    }
    if (executable == 'kill' && arguments.first == '-0') {
      return ProcessResult(0, pidAlive ? 0 : 1, '', '');
    }
    // kill、bash -lc 'grix-connector start' 等一律成功
    return ProcessResult(0, 0, '', '');
  }

  bool killed(int pid) =>
      calls.any((c) => c.length == 2 && c[0] == 'kill' && c[1] == '$pid');
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    // 测试宿主的 defaultTargetPlatform 是 android，会被 _keepAlive 的桌面闸门挡住
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
    Get.addTranslations(AppTranslations().keys);
    Get.locale = const Locale('zh', 'CN');
  });

  tearDown(() {
    debugDefaultTargetPlatformOverride = null;
    Get.reset();
  });

  final t0 = DateTime(2026, 8, 24, 12);

  GrixConnectorService buildService(
    _FakeAdapter adapter,
    _FakeProcessRunner runner,
    DateTime Function() clock,
  ) =>
      GrixConnectorService()
        ..httpAdapter = adapter
        ..processRun = runner.call
        ..startProbeDelay = Duration.zero
        ..clock = clock;

  group('崩溃循环退避', () {
    test('在线不足稳定窗口就掉线：计入退避，而不是每个周期立刻重拉', () async {
      var now = t0;
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => now);

      await service.checkHealth();
      expect(service.isRunning.value, isTrue);
      expect(service.lastKnownPidForTest, 4242);
      expect(service.consecutiveFailuresForTest, 0);

      // 10 秒后 daemon 崩了（远小于稳定窗口）
      now = t0.add(const Duration(seconds: 10));
      adapter.respond = (_) => throw DioException(
            requestOptions: RequestOptions(path: '/healthz'),
            type: DioExceptionType.connectionError,
          );
      await service.checkHealth();

      expect(service.isRunning.value, isFalse);
      expect(service.consecutiveFailuresForTest, 1);
      expect(
        service.nextRestartAtForTest,
        now.add(connectorRestartBackoff(1)),
        reason: '短命在线视作崩溃循环的一环，下一次拉起要等退避',
      );
      // pid 离线后仍要握着：挂死清理只能靠它
      expect(service.lastKnownPidForTest, 4242);
    });

    test('在线撑满稳定窗口后退避才清零', () async {
      var now = t0;
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => now);

      await service.checkHealth(); // 上线
      now = t0.add(const Duration(seconds: 10));
      adapter.respond = (_) => throw DioException(
            requestOptions: RequestOptions(path: '/healthz'),
            type: DioExceptionType.connectionError,
          );
      await service.checkHealth(); // 短命掉线，failures=1

      // 重新上线：稳定窗口未满，退避不清零
      now = t0.add(const Duration(seconds: 20));
      adapter.respond = (_) => _healthzOk();
      await service.checkHealth();
      expect(service.isRunning.value, isTrue);
      expect(service.consecutiveFailuresForTest, 1,
          reason: '刚上线还谈不上恢复，清零要等它站稳');

      // 站稳一分钟后才算恢复
      now = now.add(GrixConnectorService.stableOnlineWindow);
      await service.checkHealth();
      expect(service.consecutiveFailuresForTest, 0);
      expect(service.nextRestartAtForTest, isNull);
    });
  });

  group('挂死 daemon 清理', () {
    test('连续拉起无效后，看门狗杀掉旧 pid 再拉起', () async {
      var now = t0;
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => now)
        ..isInstalled.value = true;

      await service.checkHealth(); // 上线，记住 pid 4242

      // daemon 挂死：进程活着但 /healthz 再也探不通
      adapter.respond = (_) => throw DioException(
            requestOptions: RequestOptions(path: '/healthz'),
            type: DioExceptionType.connectionTimeout,
          );

      // 第一轮：短命掉线计入退避，拉起被退避门挡住
      now = t0.add(const Duration(seconds: 10));
      await service.checkHealth();
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(runner.killed(4242), isFalse);

      // 第二轮：过了退避门，先普通 start（无效，failures 涨到 2）
      now = t0.add(const Duration(seconds: 25));
      await service.checkHealth();
      await Future<void>.delayed(const Duration(milliseconds: 100));
      expect(runner.killed(4242), isFalse,
          reason: '失败次数未到阈值，先给普通 start 机会');
      expect(service.consecutiveFailuresForTest, 2);

      // 第三轮：达到阈值，升级为杀掉疑似挂死的旧进程再拉起
      now = t0.add(const Duration(seconds: 60));
      await service.checkHealth();
      // kill 流程里有一次 300ms 的真实探活等待，留足余量防抖
      await Future<void>.delayed(const Duration(milliseconds: 800));
      expect(runner.killed(4242), isTrue);
      expect(service.lastKnownPidForTest, 0, reason: '杀过的 pid 不能再杀第二次');
    });

    test('pid 已被复用成别的进程：只跳过清理，不误杀', () async {
      final runner = _FakeProcessRunner()..psCommandLine = '/usr/bin/some-other-tool';
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => t0)
        ..pid.value = 4242;

      final ok = await service.restartDaemon();

      expect(runner.killed(4242), isFalse);
      expect(ok, isTrue, reason: '跳过杀进程后仍要照常拉起');
      expect(
        runner.calls.any((c) => c.join(' ').contains('grix-connector start')),
        isTrue,
      );
    });
  });

  group('restartDaemon', () {
    test('杀掉当前 daemon 进程并重新拉起', () async {
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => t0)
        ..pid.value = 4242;

      final ok = await service.restartDaemon();

      expect(runner.killed(4242), isTrue);
      expect(ok, isTrue);
      expect(service.isRunning.value, isTrue);
    });
  });
}
