import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/app_badge_service.dart';
import 'package:grix/data/providers/im_service.dart';

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

  test(
    'badge sync waits for authoritative refresh before writing stale local unread',
    () async {
      final calls = <MethodCall>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            calls.add(call);
            return null;
          });

      final service = ImService();
      addTearDown(service.onClose);

      service.deferSystemUnreadBadgeSyncUntilAuthoritativeRefresh();
      service.sessions.value = [
        SessionModel(
          sessionId: 's1',
          updatedAt: 1,
          unreadCount: 4,
          lastMessageTime: 1,
        ),
      ];
      await Future<void>.delayed(Duration.zero);

      expect(calls, isEmpty);

      await service.syncSystemUnreadBadgeNow(force: true, authoritative: true);
      await Future<void>.delayed(Duration.zero);

      expect(calls, hasLength(1));
      expect(calls.single.arguments, <String, dynamic>{'count': 4});

      service.sessions.value = [
        SessionModel(
          sessionId: 's1',
          updatedAt: 2,
          unreadCount: 2,
          lastMessageTime: 2,
        ),
      ];
      await Future<void>.delayed(Duration.zero);

      expect(calls, hasLength(2));
      expect(calls.last.arguments, <String, dynamic>{'count': 2});
    },
  );

  test('clearUnread writes badge count 0 on iOS', () async {
    final calls = <MethodCall>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          return null;
        });

    final service = ImService();
    addTearDown(service.onClose);

    service.sessions.value = [
      SessionModel(
        sessionId: 's1',
        updatedAt: 1,
        unreadCount: 2,
        lastMessageTime: 1,
      ),
    ];
    await Future<void>.delayed(Duration.zero);
    calls.clear();

    service.clearUnread('s1');
    await Future<void>.delayed(Duration.zero);

    expect(calls, isNotEmpty);
    expect(calls.last.method, 'setAppBadge');
    expect(calls.last.arguments, <String, dynamic>{'count': 0});
  });

  test('clearUnread still writes badge count 0 while defer is active', () async {
    final calls = <MethodCall>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          return null;
        });

    final service = ImService();
    addTearDown(service.onClose);

    service.deferSystemUnreadBadgeSyncUntilAuthoritativeRefresh();
    service.sessions.value = [
      SessionModel(
        sessionId: 's1',
        updatedAt: 1,
        unreadCount: 2,
        lastMessageTime: 1,
      ),
    ];
    await Future<void>.delayed(Duration.zero);
    expect(calls, isEmpty);

    service.clearUnread('s1');
    await Future<void>.delayed(Duration.zero);

    expect(calls, hasLength(1));
    expect(calls.single.method, 'setAppBadge');
    expect(calls.single.arguments, <String, dynamic>{'count': 0});
  });

  test('markUnread writes badge count 1 on iOS', () async {
    final calls = <MethodCall>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          return null;
        });

    final service = ImService();
    addTearDown(service.onClose);

    service.sessions.value = [
      SessionModel(
        sessionId: 's1',
        updatedAt: 1,
        unreadCount: 0,
        lastMessageTime: 1,
      ),
    ];
    await Future<void>.delayed(Duration.zero);
    calls.clear();

    service.markUnread('s1');
    await Future<void>.delayed(Duration.zero);

    expect(calls, isNotEmpty);
    expect(calls.last.method, 'setAppBadge');
    expect(calls.last.arguments, <String, dynamic>{'count': 1});
  });

  test(
    'authoritative refresh can still override a local badge write',
    () async {
      final calls = <MethodCall>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            calls.add(call);
            return null;
          });

      final service = ImService();
      addTearDown(service.onClose);

      service.deferSystemUnreadBadgeSyncUntilAuthoritativeRefresh();
      service.sessions.value = [
        SessionModel(
          sessionId: 's1',
          updatedAt: 1,
          unreadCount: 2,
          lastMessageTime: 1,
        ),
      ];
      await Future<void>.delayed(Duration.zero);

      service.clearUnread('s1');
      await Future<void>.delayed(Duration.zero);
      expect(calls, hasLength(1));
      expect(calls.single.arguments, <String, dynamic>{'count': 0});

      // Server still reports unread — the authoritative refresh must win.
      service.sessions.value = [
        SessionModel(
          sessionId: 's1',
          updatedAt: 2,
          unreadCount: 3,
          lastMessageTime: 2,
        ),
      ];
      await service.syncSystemUnreadBadgeNow(force: true, authoritative: true);
      await Future<void>.delayed(Duration.zero);

      expect(calls, hasLength(2));
      expect(calls.last.arguments, <String, dynamic>{'count': 3});
    },
  );
}
