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

  /// `command -v grix-connector` 的探测结果：本机装没装
  bool connectorPresent = true;

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
    if (arguments.isNotEmpty &&
        arguments.last.contains('command -v grix-connector')) {
      return ProcessResult(
          0, connectorPresent ? 0 : 1, connectorPresent ? '/usr/local/bin/grix-connector' : '', '');
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
        // 持久化与装包走注入的假实现：测试里既没有 SharedPreferences 平台通道，
        // 也绝不真的跑 npm install
        ..loadLastGoodVersion = (() async => null)
        ..saveLastGoodVersion = ((_) async {})
        ..rollbackInstall = ((_) async => true)
        ..clock = clock;

  bool startAttempted(_FakeProcessRunner runner) =>
      runner.calls.any((c) => c.join(' ').contains('grix-connector start'));

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

  group('版本回退兜底', () {
    ResponseBody connErr(RequestOptions _) => throw DioException(
          requestOptions: RequestOptions(path: '/healthz'),
          type: DioExceptionType.connectionError,
        );

    test('稳定在线满窗口后，当前版本被记为已知可用并持久化', () async {
      var now = t0;
      final saved = <String>[];
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => now)
        ..saveLastGoodVersion = ((v) async => saved.add(v));

      await service.checkHealth(); // 上线，3.20.0
      expect(service.lastGoodVersion.value, isEmpty,
          reason: '刚上线还没被证明可用');

      now = t0.add(GrixConnectorService.stableOnlineWindow);
      await service.checkHealth();
      expect(service.lastGoodVersion.value, '3.20.0');
      expect(saved, ['3.20.0']);

      // 后续轮询不重复落盘
      now = now.add(const Duration(seconds: 10));
      await service.checkHealth();
      expect(saved, ['3.20.0']);
    });

    test('杀进程都救不回来时回退到已知可用版本，且一轮离线期只试一次', () async {
      var now = t0;
      final rollbackCalls = <String>[];
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => now)
        ..isInstalled.value = true
        ..lastGoodVersion.value = '3.19.0'
        ..rollbackInstall = ((v) async {
          rollbackCalls.add(v);
          return true;
        });

      await service.checkHealth(); // 上线，记住 pid 4242
      adapter.respond = connErr;

      // r1：短命掉线计入退避，拉起被退避门挡住（failures=1）
      now = t0.add(const Duration(seconds: 10));
      await service.checkHealth();
      // r2：普通 start，无效（failures=2）
      now = t0.add(const Duration(seconds: 25));
      await service.checkHealth();
      await Future<void>.delayed(const Duration(milliseconds: 100));
      // r3：杀挂死进程 + start，仍无效（failures=3）
      now = t0.add(const Duration(seconds: 50));
      await service.checkHealth();
      await Future<void>.delayed(const Duration(milliseconds: 800));
      expect(runner.killed(4242), isTrue);
      expect(rollbackCalls, isEmpty, reason: '未到回退阈值');
      // r4：failures=4
      now = t0.add(const Duration(seconds: 95));
      await service.checkHealth();
      await Future<void>.delayed(const Duration(milliseconds: 100));
      // r5：达到阈值，回退安装已知可用版本
      now = t0.add(const Duration(seconds: 180));
      await service.checkHealth();
      await Future<void>.delayed(const Duration(milliseconds: 100));
      expect(rollbackCalls, ['3.19.0']);
      // r6：本轮不再重复回退
      now = t0.add(const Duration(seconds: 345));
      await service.checkHealth();
      await Future<void>.delayed(const Duration(milliseconds: 100));
      expect(rollbackCalls, ['3.19.0']);
    });

    test('没有已知可用版本时不执行回退', () async {
      var now = t0;
      final rollbackCalls = <String>[];
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => now)
        ..isInstalled.value = true
        ..rollbackInstall = ((v) async {
          rollbackCalls.add(v);
          return true;
        });

      await service.checkHealth();
      adapter.respond = connErr;
      for (final sec in [10, 25, 50, 95, 180]) {
        now = t0.add(Duration(seconds: sec));
        await service.checkHealth();
        await Future<void>.delayed(const Duration(milliseconds: 800));
      }

      expect(service.consecutiveFailuresForTest, greaterThanOrEqualTo(4));
      expect(rollbackCalls, isEmpty);
    });
  });

  group('npm 镜像兜底', () {
    test('looksLikeNetworkFailure 识别网络症状', () {
      expect(
        GrixConnectorService.looksLikeNetworkFailure(
            'npm ERR! network ETIMEDOUT 1.2.3.4:443'),
        isTrue,
      );
      expect(
        GrixConnectorService.looksLikeNetworkFailure('EACCES: permission denied'),
        isFalse,
      );
    });

    test('官方源网络失败后自动用镜像重试一次', () async {
      final cmds = <String>[];
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => t0);
      service.installShell = (cmd,
          {required String clientType,
          required int timeoutSeconds,
          required bool skipVerify}) async {
        cmds.add(cmd);
        if (cmds.length == 1) {
          service.installLog.value = 'npm ERR! network request failed';
          return false;
        }
        return true;
      };

      final ok = await service.npmInstall('grix-connector');

      expect(ok, isTrue);
      expect(cmds, hasLength(2));
      expect(cmds.first, 'npm install -g grix-connector');
      expect(
        cmds.last,
        'npm install -g grix-connector '
        '--registry=${GrixConnectorService.npmMirrorRegistry}',
      );
    });

    test('registry 被墙静默挂死（超时无输出）同样触发镜像重试', () async {
      final cmds = <String>[];
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => t0);
      service.installShell = (cmd,
          {required String clientType,
          required int timeoutSeconds,
          required bool skipVerify}) async {
        cmds.add(cmd);
        if (cmds.length == 1) {
          service.installLog.value = ''; // 挂死被杀：没有任何网络关键字
          service.lastInstallTimedOut = true;
          return false;
        }
        return true;
      };

      final ok = await service.npmInstall('grix-connector@3.19.0');

      expect(ok, isTrue);
      expect(cmds.last, contains('--registry='));
    });

    test('本地性失败（权限、磁盘）不做无谓的换源重试', () async {
      final cmds = <String>[];
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((_) => _healthzOk());
      final service = buildService(adapter, runner, () => t0);
      service.installShell = (cmd,
          {required String clientType,
          required int timeoutSeconds,
          required bool skipVerify}) async {
        cmds.add(cmd);
        service.installLog.value = 'npm ERR! EACCES: permission denied';
        return false;
      };

      final ok = await service.npmInstall('grix-connector');

      expect(ok, isFalse);
      expect(cmds, hasLength(1));
    });

    test('查最新版本：官方 registry 不通时退到镜像', () async {
      final runner = _FakeProcessRunner();
      final adapter = _FakeAdapter((options) {
        if (options.uri.host == 'registry.npmjs.org') {
          throw DioException(
            requestOptions: options,
            type: DioExceptionType.connectionTimeout,
          );
        }
        if (options.uri.host == 'registry.npmmirror.com') {
          return _json({'version': '3.21.0'}, 200);
        }
        return _json({}, 404);
      });
      final service = buildService(adapter, runner, () => t0);
      // daemon 不在跑：走 registry 回落路径
      expect(service.isRunning.value, isFalse);

      await service.checkLatestVersion();

      expect(service.latestVersion.value, '3.21.0');
    });
  });

  group('安装态自愈', () {
    test('安装探测误判后，看门狗在离线周期里重检并继续拉起', () async {
      final runner = _FakeProcessRunner(); // connectorPresent 默认 true
      final adapter = _FakeAdapter((_) => throw DioException(
            requestOptions: RequestOptions(path: '/healthz'),
            type: DioExceptionType.connectionError,
          ));
      final service = buildService(adapter, runner, () => t0);
      expect(service.isInstalled.value, isFalse);

      await service.checkHealth();
      await Future<void>.delayed(const Duration(milliseconds: 100));

      expect(service.isInstalled.value, isTrue,
          reason: '误判成未装不能把看门狗永久锁死');
      expect(startAttempted(runner), isTrue);
    });

    test('确实没装：不拉起，按退避重检', () async {
      final runner = _FakeProcessRunner()..connectorPresent = false;
      final adapter = _FakeAdapter((_) => throw DioException(
            requestOptions: RequestOptions(path: '/healthz'),
            type: DioExceptionType.connectionError,
          ));
      final service = buildService(adapter, runner, () => t0);

      await service.checkHealth();
      await Future<void>.delayed(const Duration(milliseconds: 100));

      expect(service.isInstalled.value, isFalse);
      expect(service.consecutiveFailuresForTest, 1);
      expect(service.nextRestartAtForTest, isNotNull);
      expect(startAttempted(runner), isFalse);
    });
  });
}
