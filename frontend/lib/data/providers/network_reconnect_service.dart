import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import 'auth_service.dart';
import 'im_service.dart';

abstract class ConnectivityMonitor {
  Future<List<ConnectivityResult>> checkConnectivity();

  Stream<List<ConnectivityResult>> get onConnectivityChanged;
}

class ConnectivityPlusMonitor implements ConnectivityMonitor {
  ConnectivityPlusMonitor([Connectivity? connectivity])
    : _connectivity = connectivity ?? Connectivity();

  final Connectivity _connectivity;

  @override
  Future<List<ConnectivityResult>> checkConnectivity() {
    return _connectivity.checkConnectivity();
  }

  @override
  Stream<List<ConnectivityResult>> get onConnectivityChanged =>
      _connectivity.onConnectivityChanged;
}

Set<ConnectivityResult> normalizeConnectivityResults(
  List<ConnectivityResult> results,
) {
  if (results.isEmpty) {
    return const {ConnectivityResult.none};
  }
  final normalized = results.toSet();
  if (normalized.length > 1) {
    normalized.remove(ConnectivityResult.none);
  }
  return normalized.isEmpty ? const {ConnectivityResult.none} : normalized;
}

bool hasUsableConnectivity(Set<ConnectivityResult> results) {
  return results.any((result) => result != ConnectivityResult.none);
}

bool shouldRecoverRealtimeOnConnectivityChange({
  required Set<ConnectivityResult> previous,
  required Set<ConnectivityResult> next,
}) {
  if (!hasUsableConnectivity(next)) {
    return false;
  }
  if (!hasUsableConnectivity(previous)) {
    return true;
  }
  return !setEquals(previous, next);
}

class NetworkReconnectService extends GetxService {
  NetworkReconnectService({ConnectivityMonitor? monitor})
    : _monitor = monitor ?? ConnectivityPlusMonitor();

  static const Duration _recoveryThrottle = Duration(seconds: 2);

  final ConnectivityMonitor _monitor;
  StreamSubscription<List<ConnectivityResult>>? _subscription;
  Set<ConnectivityResult> _lastConnectivity = const {ConnectivityResult.none};
  DateTime? _lastRecoveryAt;

  Future<NetworkReconnectService> init() async {
    _lastConnectivity = normalizeConnectivityResults(
      await _monitor.checkConnectivity(),
    );
    _subscription = _monitor.onConnectivityChanged.listen(
      _handleConnectivityResults,
    );
    return this;
  }

  @override
  void onClose() {
    _subscription?.cancel();
    super.onClose();
  }

  void _handleConnectivityResults(List<ConnectivityResult> results) {
    final previous = _lastConnectivity;
    final next = normalizeConnectivityResults(results);
    if (setEquals(previous, next)) {
      return;
    }
    _lastConnectivity = next;
    if (!shouldRecoverRealtimeOnConnectivityChange(
      previous: previous,
      next: next,
    )) {
      return;
    }
    final now = DateTime.now();
    final lastRecoveryAt = _lastRecoveryAt;
    if (lastRecoveryAt != null &&
        now.difference(lastRecoveryAt) < _recoveryThrottle) {
      return;
    }
    _lastRecoveryAt = now;
    _recoverRealtime();
  }

  void _recoverRealtime() {
    if (!Get.isRegistered<AuthService>() || !Get.isRegistered<ImService>()) {
      return;
    }
    final authService = Get.find<AuthService>();
    final imService = Get.find<ImService>();
    if (!authService.isLoggedIn) {
      return;
    }
    if (imService.isSuspendedForAppBackground) {
      return;
    }
    imService.syncNow();
  }

  @visibleForTesting
  void handleConnectivityResultsForTest(List<ConnectivityResult> results) {
    _handleConnectivityResults(results);
  }

  @visibleForTesting
  Set<ConnectivityResult> get lastConnectivityForTest => _lastConnectivity;
}
