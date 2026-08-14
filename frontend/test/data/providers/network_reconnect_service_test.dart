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
  bool suspendedForBackground = false;

  @override
  bool get isSuspendedForAppBackground => suspendedForBackground;

  @override
  void syncNow() {
    syncCalls++;
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

      await monitor.controller.close();
      service.onClose();
    },
  );
}
