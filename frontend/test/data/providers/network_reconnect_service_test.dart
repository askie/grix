import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/network_reconnect_service.dart';
import 'package:get/get.dart';

class _FakeConnectivityMonitor implements ConnectivityMonitor {
  _FakeConnectivityMonitor({required this.initialResults});

  final List<ConnectivityResult> initialResults;
  final StreamController<List<ConnectivityResult>> controller =
      StreamController<List<ConnectivityResult>>.broadcast();

  @override
  Future<List<ConnectivityResult>> checkConnectivity() async {
    return initialResults;
  }

  @override
  Stream<List<ConnectivityResult>> get onConnectivityChanged =>
      controller.stream;
}

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;
}

class _SpyImService extends ImService {
  int syncCalls = 0;
  int reconnectCalls = 0;
  bool connected = false;
  bool connecting = false;
  bool suspendedForBackground = false;

  @override
  bool get isConnected => connected;

  @override
  bool get isConnecting => connecting;

  @override
  bool get isSuspendedForAppBackground => suspendedForBackground;

  @override
  void syncNow() {
    syncCalls++;
  }

  @override
  void reconnectRealtime({required String reason}) {
    reconnectCalls++;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() {
    Get.reset();
  });

  test(
    'helper only recovers when connectivity becomes usable or changes route',
    () {
      expect(
        shouldRecoverRealtimeOnConnectivityChange(
          previous: const {ConnectivityResult.none},
          next: const {ConnectivityResult.wifi},
        ),
        isTrue,
      );
      expect(
        shouldRecoverRealtimeOnConnectivityChange(
          previous: const {ConnectivityResult.wifi},
          next: const {ConnectivityResult.mobile},
        ),
        isTrue,
      );
      expect(
        shouldRecoverRealtimeOnConnectivityChange(
          previous: const {ConnectivityResult.wifi},
          next: const {ConnectivityResult.none},
        ),
        isFalse,
      );
    },
  );

  test(
    'network reconnect service triggers sync on usable connectivity recovery',
    () async {
      final monitor = _FakeConnectivityMonitor(
        initialResults: const [ConnectivityResult.none],
      );
      final service = NetworkReconnectService(monitor: monitor);
      final imService = _SpyImService();
      Get.put<AuthService>(_FakeAuthService());
      Get.put<ImService>(imService);

      await service.init();
      monitor.controller.add(const [ConnectivityResult.wifi]);
      await Future<void>.delayed(const Duration(milliseconds: 20));

      expect(imService.syncCalls, 1);
      expect(service.lastConnectivityForTest, const {ConnectivityResult.wifi});

      await monitor.controller.close();
      service.onClose();
    },
  );

  test('network reconnect service ignores duplicate usable states', () async {
    final monitor = _FakeConnectivityMonitor(
      initialResults: const [ConnectivityResult.wifi],
    );
    final service = NetworkReconnectService(monitor: monitor);
    final imService = _SpyImService();
    Get.put<AuthService>(_FakeAuthService());
    Get.put<ImService>(imService);

    await service.init();
    monitor.controller.add(const [ConnectivityResult.wifi]);
    await Future<void>.delayed(const Duration(milliseconds: 20));

    expect(imService.syncCalls, 0);
    expect(imService.reconnectCalls, 0);

    await monitor.controller.close();
    service.onClose();
  });

  test(
    'network reconnect service skips recovery while app suspended',
    () async {
      final monitor = _FakeConnectivityMonitor(
        initialResults: const [ConnectivityResult.none],
      );
      final service = NetworkReconnectService(monitor: monitor);
      final imService = _SpyImService()..suspendedForBackground = true;
      Get.put<AuthService>(_FakeAuthService());
      Get.put<ImService>(imService);

      await service.init();
      monitor.controller.add(const [ConnectivityResult.wifi]);
      await Future<void>.delayed(const Duration(milliseconds: 20));

      expect(imService.syncCalls, 0);
      expect(imService.reconnectCalls, 0);

      await monitor.controller.close();
      service.onClose();
    },
  );

  for (final connected in [false, true]) {
    test('wifi to ethernet recovers with connected=$connected', () async {
      final monitor = _FakeConnectivityMonitor(
        initialResults: const [ConnectivityResult.wifi],
      );
      final service = NetworkReconnectService(monitor: monitor);
      final imService = _SpyImService()..connected = connected;
      Get.put<AuthService>(_FakeAuthService());
      Get.put<ImService>(imService);
      await service.init();

      monitor.controller.add(const [ConnectivityResult.ethernet]);
      await Future<void>.delayed(const Duration(milliseconds: 20));
      expect(imService.reconnectCalls, connected ? 1 : 0);
      expect(imService.syncCalls, connected ? 0 : 1);

      await monitor.controller.close();
      service.onClose();
    });
  }

  test('offline to online recycles a socket still marked connected', () async {
    final monitor = _FakeConnectivityMonitor(
      initialResults: const [ConnectivityResult.wifi],
    );
    final service = NetworkReconnectService(monitor: monitor);
    final imService = _SpyImService()..connected = true;
    Get.put<AuthService>(_FakeAuthService());
    Get.put<ImService>(imService);
    await service.init();

    service.handleConnectivityResultsForTest(const [ConnectivityResult.none]);
    expect(imService.reconnectCalls, 0);
    service.handleConnectivityResultsForTest(const [ConnectivityResult.wifi]);
    expect(imService.reconnectCalls, 1);
    expect(imService.syncCalls, 0);

    await monitor.controller.close();
    service.onClose();
  });

  test('network changes are throttled for two seconds', () async {
    final monitor = _FakeConnectivityMonitor(
      initialResults: const [ConnectivityResult.wifi],
    );
    final service = NetworkReconnectService(monitor: monitor);
    final imService = _SpyImService()..connected = true;
    Get.put<AuthService>(_FakeAuthService());
    Get.put<ImService>(imService);
    await service.init();

    service.handleConnectivityResultsForTest(const [
      ConnectivityResult.ethernet,
    ]);
    service.handleConnectivityResultsForTest(const [ConnectivityResult.wifi]);
    expect(imService.reconnectCalls, 1);
    await Future<void>.delayed(const Duration(milliseconds: 2100));
    service.handleConnectivityResultsForTest(const [ConnectivityResult.mobile]);
    expect(imService.reconnectCalls, 2);
    expect(imService.syncCalls, 0);

    await monitor.controller.close();
    service.onClose();
  });

  test('network recovery preserves an in-flight connection', () async {
    final monitor = _FakeConnectivityMonitor(
      initialResults: const [ConnectivityResult.wifi],
    );
    final service = NetworkReconnectService(monitor: monitor);
    final imService = _SpyImService()..connecting = true;
    Get.put<AuthService>(_FakeAuthService());
    Get.put<ImService>(imService);
    await service.init();

    service.handleConnectivityResultsForTest(const [
      ConnectivityResult.ethernet,
    ]);
    expect(imService.reconnectCalls, 0);
    expect(imService.syncCalls, 0);

    await monitor.controller.close();
    service.onClose();
  });
}
