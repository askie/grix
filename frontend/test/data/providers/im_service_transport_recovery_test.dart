import 'dart:async';
import 'dart:convert';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/network_reconnect_service.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get token => 'test_access_token';

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) => true;
}

class _RecordingSink implements WebSocketSink {
  final packets = <Map<String, dynamic>>[];
  int closeCalls = 0;
  bool hangOnClose = false;

  @override
  void add(dynamic data) {
    packets.add(jsonDecode(data as String) as Map<String, dynamic>);
  }

  @override
  Future<void> close([int? closeCode, String? closeReason]) {
    closeCalls++;
    return hangOnClose ? Completer<void>().future : Future<void>.value();
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeChannel implements WebSocketChannel {
  _FakeChannel({Future<void>? ready}) : ready = ready ?? Future<void>.value();

  final downstream = StreamController<dynamic>();

  @override
  final Future<void> ready;

  @override
  final _RecordingSink sink = _RecordingSink();

  @override
  Stream<dynamic> get stream => downstream.stream;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeConnectivityMonitor implements ConnectivityMonitor {
  final controller = StreamController<List<ConnectivityResult>>.broadcast();

  @override
  Future<List<ConnectivityResult>> checkConnectivity() async => const [
    ConnectivityResult.wifi,
  ];

  @override
  Stream<List<ConnectivityResult>> get onConnectivityChanged =>
      controller.stream;
}

Future<void> _eventually(bool Function() condition) async {
  final deadline = DateTime.now().add(const Duration(seconds: 1));
  while (!condition() && DateTime.now().isBefore(deadline)) {
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  expect(condition(), isTrue);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final originalClock = ImService.nowMsProvider;
  late ImService service;
  late List<_FakeChannel> channels;
  late int nowMs;
  late _FakeConnectivityMonitor monitor;
  late NetworkReconnectService networkService;
  Future<void>? nextReady;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    nowMs = 100000;
    ImService.nowMsProvider = () => nowMs;
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
    channels = [];
    nextReady = null;
    ImService.channelConnectorForTest = (_) {
      final channel = _FakeChannel(ready: nextReady);
      nextReady = null;
      channels.add(channel);
      return channel;
    };
    Get.put<AuthService>(_FakeAuthService());
    service = Get.put<ImService>(ImService());
    monitor = _FakeConnectivityMonitor();
    networkService = NetworkReconnectService(monitor: monitor);
    await networkService.init();
    service.connect('ws://127.0.0.1:1/ws');
    await _eventually(
      () =>
          channels.isNotEmpty &&
          channels.first.sink.packets.any((p) => p['cmd'] == 'auth'),
    );
    service.seedRealtimeStateForTest(
      wsUrl: 'ws://127.0.0.1:1/ws',
      connected: true,
      authenticated: true,
    );
    service.setActiveSyncModeForTest('v2');
  });

  tearDown(() async {
    networkService.onClose();
    await monitor.controller.close();
    service.onClose();
    for (final channel in channels) {
      await channel.downstream.close();
    }
    ImService.channelConnectorForTest = null;
    ImService.nowMsProvider = originalClock;
    debugDefaultTargetPlatformOverride = null;
    Get.reset();
  });

  test('wifi to ethernet recycles the old socket and connects once', () async {
    final oldChannel = channels.single;
    oldChannel.sink.hangOnClose = true;
    monitor.controller.add(const [ConnectivityResult.ethernet]);
    await _eventually(() => channels.length == 2);
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(oldChannel.sink.closeCalls, 1);
    expect(channels, hasLength(2));
  });

  test('two-minute heartbeat gap reconnects immediately only once', () async {
    final oldChannel = channels.single;
    oldChannel.sink.hangOnClose = true;
    nowMs += const Duration(minutes: 2).inMilliseconds;
    final elapsed = Stopwatch()..start();
    service.handleHeartbeatTickForTest();
    service.handleHeartbeatTickForTest();
    await _eventually(() => channels.length == 2);
    expect(elapsed.elapsed, lessThan(const Duration(seconds: 1)));
    expect(oldChannel.sink.closeCalls, 1);
    expect(oldChannel.sink.packets.where((p) => p['cmd'] == 'ping'), isEmpty);
    // The new timer starts at the new clock value; the gap must not leak.
    service.handleHeartbeatTickForTest();
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(channels, hasLength(2));
  });

  test('normal thirty-second heartbeat ticks keep the socket', () async {
    final channel = channels.single;
    for (var i = 0; i < 2; i++) {
      nowMs += const Duration(seconds: 30).inMilliseconds;
      service.handleHeartbeatTickForTest();
    }
    expect(channels, hasLength(1));
    expect(channel.sink.closeCalls, 0);
    expect(channel.sink.packets.where((p) => p['cmd'] == 'ping'), hasLength(2));
  });

  test('sleep detection requires a gap strictly above sixty seconds', () async {
    nowMs += const Duration(seconds: 60).inMilliseconds;
    service.handleHeartbeatTickForTest();
    expect(service.isConnected, isTrue);
    // Keep pong current to distinguish sleep detection from pong timeout.
    service.handleSocketPayloadForTest(jsonEncode({'cmd': 'pong'}));
    nowMs += const Duration(seconds: 60).inMilliseconds + 1;
    service.handleHeartbeatTickForTest();
    await _eventually(() => channels.length == 2);
  });

  for (final firstTrigger in ['network', 'heartbeat', 'foreground']) {
    test('$firstTrigger recovery coalesces with the other triggers', () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      final pendingReady = Completer<void>();
      nextReady = pendingReady.future;
      service.setRealtimeAppState('background');
      nowMs += const Duration(minutes: 2).inMilliseconds;
      switch (firstTrigger) {
        case 'network':
          networkService.handleConnectivityResultsForTest(const [
            ConnectivityResult.ethernet,
          ]);
        case 'heartbeat':
          service.handleHeartbeatTickForTest();
        case 'foreground':
          service.setRealtimeAppState('foreground');
      }
      // Overlap before the immediate reconnect timer has fired.
      service.setRealtimeAppState('foreground');
      service.handleHeartbeatTickForTest();
      networkService.handleConnectivityResultsForTest(const [
        ConnectivityResult.ethernet,
      ]);
      service.ensureConnected();
      service.ensureConnected();
      await _eventually(() => channels.length == 2);
      expect(service.isConnecting, isTrue);
      // Overlap again while the new socket is waiting for ready.
      networkService.handleConnectivityResultsForTest(const [
        ConnectivityResult.mobile,
      ]);
      service.reconnectRealtime(reason: 'concurrent recovery');
      service.handleHeartbeatTickForTest();
      service.ensureConnected();
      pendingReady.complete();
      await _eventually(() => service.isConnected);
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(channels, hasLength(2));
    });
  }

  test(
    'suspended background sockets do not recover on a delayed tick',
    () async {
      service.suspendForAppBackground();
      nowMs += const Duration(minutes: 2).inMilliseconds;
      service.handleHeartbeatTickForTest();
      service.reconnectRealtime(reason: 'background recovery');
      networkService.handleConnectivityResultsForTest(const [
        ConnectivityResult.ethernet,
      ]);
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(service.isSuspendedForAppBackground, isTrue);
      expect(channels, hasLength(1));
    },
  );
}
