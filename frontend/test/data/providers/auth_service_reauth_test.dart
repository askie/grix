import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeImService extends ImService {
  int resetCalls = 0;
  int loadSessionsCalls = 0;

  @override
  Future<void> resetForAccountSwitch() async {
    resetCalls++;
  }

  @override
  Future<void> loadSessionsForCurrentUser() async {
    loadSessionsCalls++;
  }
}

void main() {
  late AuthService service;
  late _FakeImService imService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({
      'access_token': 'stored_access',
      'refresh_token': 'stored_refresh',
      'access_expires_at_ms': 9999999999999,
      'user_id': '1001',
      'username': 'tester',
      'nickname': 'Tester',
    });
    imService = _FakeImService();
    Get.put<ImService>(imService);
    service = AuthService();
  });

  tearDown(() {
    Get.reset();
  });

  test('same-user auth grant does not reset runtime services', () async {
    await service.init();

    final applied = await service.applyAuthPayloadForTest({
      'access_token': 'next_access',
      'refresh_token': 'next_refresh',
      'expires_in': 7200,
      'user': {'id': '1001', 'username': 'tester', 'nickname': 'Tester'},
    });

    expect(applied, isTrue);
    expect(imService.resetCalls, 0);
    expect(imService.loadSessionsCalls, 1);
    expect(service.isLoggedIn, isTrue);
    expect(service.token, 'next_access');
  });

  test('different-user auth grant resets runtime services', () async {
    await service.init();

    final applied = await service.applyAuthPayloadForTest({
      'access_token': 'other_access',
      'refresh_token': 'other_refresh',
      'expires_in': 7200,
      'user': {'id': '2002', 'username': 'other', 'nickname': 'Other'},
    });

    expect(applied, isTrue);
    expect(imService.resetCalls, 1);
    expect(imService.loadSessionsCalls, 1);
    expect(service.isLoggedIn, isTrue);
    expect(service.userId, '2002');
  });

  test('login state becomes visible only after token is ready', () async {
    await service.init();
    await service.logout(notifyServer: false);

    String? observedToken;
    Worker? loginWorker;
    loginWorker = ever<bool>(service.isLoggedInRx, (loggedIn) {
      if (!loggedIn || observedToken != null) {
        return;
      }
      observedToken = service.token;
    });

    final applied = await service.applyAuthPayloadForTest({
      'access_token': 'race_free_access',
      'refresh_token': 'race_free_refresh',
      'expires_in': 7200,
      'user': {'id': '1001', 'username': 'tester', 'nickname': 'Tester'},
    });

    loginWorker.dispose();
    expect(applied, isTrue);
    expect(observedToken, 'race_free_access');
  });

  test('matching active session skips duplicate login request', () async {
    await service.init();

    final result = await service.login('tester', 'ignored-password');

    expect(result.ok, isTrue);
    expect(result.httpStatus, 200);
    expect(service.isLoggedIn, isTrue);
    expect(service.userId, '1001');
    expect(service.token, 'stored_access');
  });

  test(
    'matching active session also skips duplicate email login request',
    () async {
      SharedPreferences.setMockInitialValues({
        'access_token': 'stored_access',
        'refresh_token': 'stored_refresh',
        'access_expires_at_ms': 9999999999999,
        'user_id': '1001',
        'username': 'tester',
        'email': 'tester@example.com',
        'nickname': 'Tester',
      });
      service = AuthService();
      Get.replace<ImService>(imService);

      await service.init();

      final result = await service.login(
        'tester@example.com',
        'ignored-password',
      );

      expect(result.ok, isTrue);
      expect(result.httpStatus, 200);
      expect(service.isLoggedIn, isTrue);
      expect(service.userId, '1001');
      expect(service.token, 'stored_access');
    },
  );
}
