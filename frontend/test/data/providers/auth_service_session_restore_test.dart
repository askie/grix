import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  TokenRefreshStatus refreshStatus = TokenRefreshStatus.ready;
  int unauthorizedCalls = 0;

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async {
    return refreshStatus;
  }

  @override
  void handleUnauthorized({String? expectedAccessToken}) {
    unauthorizedCalls++;
    logout(notifyServer: false);
  }
}

void main() {
  late _FakeAuthService service;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({
      'access_token': 'stored_access',
      'refresh_token': 'stored_refresh',
      'access_expires_at_ms': 1,
      'user_id': '1001',
      'username': 'tester',
      'nickname': 'Tester',
    });
    service = _FakeAuthService();
  });

  tearDown(() {
    Get.reset();
  });

  test('init clears stored auth when refresh session is invalid', () async {
    service.refreshStatus = TokenRefreshStatus.invalidSession;

    await service.init();

    final prefs = await SharedPreferences.getInstance();
    expect(service.isLoggedIn, isFalse);
    expect(service.token, isNull);
    expect(prefs.getString('access_token'), isNull);
    expect(prefs.getString('user_id'), isNull);
  });

  test('init keeps stored auth when refresh fails temporarily', () async {
    service.refreshStatus = TokenRefreshStatus.temporaryFailure;

    await service.init();

    final prefs = await SharedPreferences.getInstance();
    expect(service.isLoggedIn, isTrue);
    expect(service.token, 'stored_access');
    expect(prefs.getString('access_token'), 'stored_access');
    expect(prefs.getString('user_id'), '1001');
  });

  test('scheduled refresh only clears auth for invalid session', () async {
    service.refreshStatus = TokenRefreshStatus.temporaryFailure;
    await service.init();

    await service.runScheduledRefreshAttemptForTest();
    await Future<void>.delayed(Duration.zero);
    expect(service.unauthorizedCalls, 0);
    expect(service.isLoggedIn, isTrue);

    service.refreshStatus = TokenRefreshStatus.invalidSession;
    await service.runScheduledRefreshAttemptForTest();
    await Future<void>.delayed(Duration.zero);
    expect(service.unauthorizedCalls, 1);
  });
}
