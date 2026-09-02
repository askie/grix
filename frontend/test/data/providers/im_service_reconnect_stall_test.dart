import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

/// 回归场景：桌面端休眠唤醒后，底层建连/清理动作悬死或抛错，导致
/// "正在连接中"标记永不复位——自动重连链路死亡、横幅上的"重试"按钮
/// 点击后毫无反应（请求根本不会发出）。以下用可控通道复现这些底层
/// 行为，验证状态机在任何一种情况下都能继续发起新的连接尝试。

class _FakeAuthService extends AuthService {
  String? tokenValue = 'test_access_token';

  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => tokenValue;

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) => true;

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async => TokenRefreshStatus.ready;
}

class _FakeWebSocketSink implements WebSocketSink {
  _FakeWebSocketSink({required this.onClose});

  final Future<void> Function() onClose;

  @override
  Future<void> close([int? closeCode, String? closeReason]) => onClose();

  @override
  void add(dynamic data) {}

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeWebSocketChannel implements WebSocketChannel {
  _FakeWebSocketChannel({
    required this.ready,
    required Stream<dynamic> stream,
    required WebSocketSink sink,
  }) : _stream = stream,
       _sink = sink;

  @override
  final Future<void> ready;

  final Stream<dynamic> _stream;
  final WebSocketSink _sink;

  @override
  Stream<dynamic> get stream => _stream;

  @override
  WebSocketSink get sink => _sink;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

/// ready 永不完成、订阅取消与 sink 关闭全部悬死——模拟唤醒后底层
/// connect 黑洞化的最坏情况。
WebSocketChannel hangingConnectChannel() {
  final controller = StreamController<dynamic>(
    onCancel: () => Completer<void>().future,
  );
  return _FakeWebSocketChannel(
    ready: Completer<void>().future,
    stream: controller.stream,
    sink: _FakeWebSocketSink(onClose: () => Completer<void>().future),
  );
}

/// ready 立刻失败，但清理动作（cancel/close）悬死。
WebSocketChannel failingConnectHangingCleanupChannel() {
  final controller = StreamController<dynamic>(
    onCancel: () => Completer<void>().future,
  );
  return _FakeWebSocketChannel(
    ready: Future<void>.error(StateError('connect failed')),
    stream: controller.stream,
    sink: _FakeWebSocketSink(onClose: () => Completer<void>().future),
  );
}

/// ready 立刻失败，且清理动作以异步错误收场。
WebSocketChannel failingConnectThrowingCleanupChannel() {
  final controller = StreamController<dynamic>(
    onCancel: () => Future<void>.error(StateError('cancel failed')),
  );
  return _FakeWebSocketChannel(
    ready: Future<void>.error(StateError('connect failed')),
    stream: controller.stream,
    sink: _FakeWebSocketSink(
      onClose: () => Future<void>.error(StateError('close failed')),
    ),
  );
}

/// ready 立刻成功、通道整体可用——用于覆盖"连上之后"的鉴权分支。
WebSocketChannel healthyConnectChannel() {
  final controller = StreamController<dynamic>();
  return _FakeWebSocketChannel(
    ready: Future<void>.value(),
    stream: controller.stream,
    sink: _FakeWebSocketSink(onClose: () async {}),
  );
}

Future<void> expectEventually(
  bool Function() condition, {
  Duration timeout = const Duration(seconds: 4),
  String? reason,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    if (condition()) return;
    await Future<void>.delayed(const Duration(milliseconds: 25));
  }
  expect(condition(), isTrue, reason: reason);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late _FakeAuthService authService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    authService = _FakeAuthService();
    Get.put<AuthService>(authService);
  });

  tearDown(() {
    ImService.channelConnectorForTest = null;
    ImService.connectAttemptHardTimeout = const Duration(seconds: 20);
    Get.reset();
  });

  test('手动重试必须作废悬死中的建连尝试并立刻发起新连接', () async {
    var connectCalls = 0;
    ImService.channelConnectorForTest = (uri) {
      connectCalls++;
      return hangingConnectChannel();
    };

    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(connectCalls, 1);

    // 在途尝试悬死在 ready 上（未超时）。手动重试必须绕开它开新连接，
    // 而不是被"正在连接中"守卫静默吞掉。
    service.syncNow();
    await expectEventually(
      () => connectCalls >= 2,
      timeout: const Duration(seconds: 1),
      reason: '手动重试被在途悬死尝试吞掉，没有发起新连接',
    );

    service.disconnect();
  });

  test('建连失败且清理动作悬死时，自动重连链路必须继续存活', () async {
    var connectCalls = 0;
    ImService.channelConnectorForTest = (uri) {
      connectCalls++;
      return failingConnectHangingCleanupChannel();
    };

    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');

    // 第一次失败后应按退避（1 秒档）自动发起第二次，而不是被悬死的
    // cancel/close 卡住 _isConnecting 导致链路死亡。
    await expectEventually(() => connectCalls >= 2, reason: '清理动作悬死导致自动重连链路死亡');
    expect(service.connectionStage, ImConnectionStage.reconnecting);

    service.disconnect();
  });

  test('清理动作抛异步错误时，状态复位不被打断且不外溢未处理异常', () async {
    var connectCalls = 0;
    ImService.channelConnectorForTest = (uri) {
      connectCalls++;
      return failingConnectThrowingCleanupChannel();
    };

    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');

    await expectEventually(
      () => connectCalls >= 2,
      reason: '清理动作抛错跳过了状态复位，自动重连链路死亡',
    );

    service.disconnect();
  });

  test('连上后发现无 token：必须立刻回收连接进入重连循环，而不是挂着干等', () async {
    authService.tokenValue = null;
    var connectCalls = 0;
    ImService.channelConnectorForTest = (uri) {
      connectCalls++;
      return healthyConnectChannel();
    };

    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');

    // 老逻辑在这里直接 return：连接挂在"鉴权中"，只能指望 90 秒心跳或
    // 服务端踢人。新逻辑必须立刻断开并按退避发起下一轮建连。
    await expectEventually(
      () => connectCalls >= 2,
      reason: '无 token 的连接被挂起干等，没有进入重连循环',
    );

    service.disconnect();
  });

  test('看门狗兜底：未知路径悬死超过硬超时后强制回收并续上重连', () async {
    ImService.connectAttemptHardTimeout = const Duration(milliseconds: 200);
    var connectCalls = 0;
    ImService.channelConnectorForTest = (uri) {
      connectCalls++;
      return hangingConnectChannel();
    };

    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(connectCalls, 1);

    // 无任何外部触发（不点重试、无网络事件），仅靠看门狗把悬死尝试
    // 作废，重连链路应自行续上第二次尝试。
    await expectEventually(() => connectCalls >= 2, reason: '看门狗未能回收悬死的建连尝试');
    expect(service.connectionStage, ImConnectionStage.reconnecting);

    service.disconnect();
  });
}
