import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/app_badge_service.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const channel = MethodChannel('pub.dhf.grix/app_badge');

  setUp(() {
    debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
    AppBadgeService.resetForTest();
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
    AppBadgeService.resetForTest();
    debugDefaultTargetPlatformOverride = null;
  });

  test('same unread count is deduplicated unless force sync is requested',
      () async {
    final calls = <MethodCall>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      calls.add(call);
      return null;
    });

    await AppBadgeService.syncUnreadBadge(3);
    await AppBadgeService.syncUnreadBadge(3);
    await AppBadgeService.syncUnreadBadge(3, force: true);

    expect(calls, hasLength(2));
    expect(calls[0].method, 'setAppBadge');
    expect(calls[0].arguments, <String, dynamic>{'count': 3});
    expect(calls[1].method, 'setAppBadge');
    expect(calls[1].arguments, <String, dynamic>{'count': 3});
  });
}
