import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/modules/share_ingest/services/share_ingest_service.dart';

class _LoggedInAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  RxBool get isLoggedInRx => true.obs;
}

void main() {
  const channel = MethodChannel('grix/share_ingest');

  test('overlapping polls do not run consumePending in parallel', () async {
    var inFlight = 0;
    var maxInFlight = 0;
    var consumeCalls = 0;
    final holdFirstPoll = Completer<void>();

    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      if (call.method == 'consumePending') {
        consumeCalls++;
        inFlight++;
        maxInFlight = inFlight > maxInFlight ? inFlight : maxInFlight;
        if (consumeCalls == 1) {
          await holdFirstPoll.future;
        }
        inFlight--;
        return <Object?>[];
      }
      return null;
    });

    Get.reset();
    Get.put<AuthService>(_LoggedInAuthService());

    final service = ShareIngestService();

    final first = service.consumePendingOnLaunch();
    await Future<void>.delayed(const Duration(milliseconds: 10));
    await service.consumePendingOnLaunch();

    expect(maxInFlight, lessThanOrEqualTo(1));
    expect(consumeCalls, 1);

    holdFirstPoll.complete();
    await first;

    await service.consumePendingOnLaunch();
    expect(consumeCalls, 2);

    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
    Get.reset();
  });
}
