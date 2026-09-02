import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

// 说明：logout 是否通知服务端走的是私有方法 _notifyServerLogout，私有成员
// 无法跨 library 覆盖，测试环境的 HTTP 均返回 400 且被 logout 静默忽略，
// 因此这里只断言可观察的本地行为（登录态、凭证、列表条目）。
class _FakeAuthService extends AuthService {
  TokenRefreshStatus refreshStatus = TokenRefreshStatus.ready;

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async {
    return refreshStatus;
  }
}

Map<String, dynamic> authPayload(String userId, {String suffix = ''}) {
  return {
    'access_token': 'access_$userId$suffix',
    'refresh_token': 'refresh_$userId$suffix',
    'expires_in': 3600,
    'user': {
      'id': userId,
      'username': 'user_$userId',
      'email': 'u$userId@example.com',
      'nickname': 'nick_$userId',
    },
  };
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAuthService service;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    service = _FakeAuthService();
    await service.init();
  });

  tearDown(() {
    Get.reset();
  });

  test('successful login snapshots account into saved list', () async {
    final applied = await service.applyAuthPayloadForTest(authPayload('1001'));
    expect(applied, isTrue);

    final accounts = await service.listSavedAccounts();
    expect(accounts, hasLength(1));
    expect(accounts.first.userId, '1001');
    expect(accounts.first.refreshToken, 'refresh_1001');
    expect(accounts.first.needsRelogin, isFalse);
  });

  test('switch to another saved account swaps active session', () async {
    await service.applyAuthPayloadForTest(authPayload('1001'));
    await service.applyAuthPayloadForTest(authPayload('2002'));
    expect(service.userId, '2002');

    final outcome = await service.switchToSavedAccount('1001');

    expect(outcome, AccountSwitchOutcome.success);
    expect(service.isLoggedIn, isTrue);
    expect(service.userId, '1001');
    expect(service.token, 'access_1001');
    expect(service.refreshToken, 'refresh_1001');

    final accounts = await service.listSavedAccounts();
    expect(accounts, hasLength(2));
    final other = accounts.firstWhere((a) => a.userId == '2002');
    expect(other.needsRelogin, isFalse);
    // 最近使用排序：刚切换的账号在最前。
    expect(accounts.first.userId, '1001');
  });

  test('switch to current account is a no-op success', () async {
    await service.applyAuthPayloadForTest(authPayload('1001'));

    final outcome = await service.switchToSavedAccount('1001');

    expect(outcome, AccountSwitchOutcome.success);
    expect(service.userId, '1001');
  });

  test('switch to unknown account fails', () async {
    await service.applyAuthPayloadForTest(authPayload('1001'));

    expect(
      await service.switchToSavedAccount('9999'),
      AccountSwitchOutcome.failed,
    );
    expect(service.userId, '1001');
  });

  test(
    'switch to expired entry requires relogin without touching session',
    () async {
      await service.applyAuthPayloadForTest(authPayload('1001'));
      await service.applyAuthPayloadForTest(authPayload('2002'));
      // 模拟 1001 曾被登出：凭证已清空。
      await service.clearSavedAccountCredentialsForTest('1001');

      final outcome = await service.switchToSavedAccount('1001');

      expect(outcome, AccountSwitchOutcome.needLogin);
      expect(service.userId, '2002');
      expect(service.isLoggedIn, isTrue);
    },
  );

  test(
    'switch with revoked credentials clears target and logs out locally',
    () async {
      await service.applyAuthPayloadForTest(authPayload('1001'));
      await service.applyAuthPayloadForTest(authPayload('2002'));
      service.refreshStatus = TokenRefreshStatus.invalidSession;

      final outcome = await service.switchToSavedAccount('1001');

      expect(outcome, AccountSwitchOutcome.needLogin);
      expect(service.isLoggedIn, isFalse);
      final target = (await service.listSavedAccounts()).firstWhere(
        (a) => a.userId == '1001',
      );
      expect(target.needsRelogin, isTrue);
    },
  );

  test('switch succeeds when refresh fails temporarily (offline)', () async {
    await service.applyAuthPayloadForTest(authPayload('1001'));
    await service.applyAuthPayloadForTest(authPayload('2002'));
    service.refreshStatus = TokenRefreshStatus.temporaryFailure;

    final outcome = await service.switchToSavedAccount('1001');

    expect(outcome, AccountSwitchOutcome.success);
    expect(service.userId, '1001');
    expect(service.isLoggedIn, isTrue);
  });

  test('logout keeps saved entry but clears its credentials', () async {
    await service.applyAuthPayloadForTest(authPayload('1001'));

    await service.logout();

    expect(service.isLoggedIn, isFalse);
    final accounts = await service.listSavedAccounts();
    expect(accounts, hasLength(1));
    expect(accounts.first.userId, '1001');
    expect(accounts.first.needsRelogin, isTrue);
    expect(accounts.first.nickname, 'nick_1001');
  });

  test('removeSavedAccount for other account only deletes the entry', () async {
    await service.applyAuthPayloadForTest(authPayload('1001'));
    await service.applyAuthPayloadForTest(authPayload('2002'));

    await service.removeSavedAccount('1001');

    expect(service.isLoggedIn, isTrue);
    expect(service.userId, '2002');
    final accounts = await service.listSavedAccounts();
    expect(accounts, hasLength(1));
    expect(accounts.first.userId, '2002');
  });

  test(
    'removeSavedAccount for current account logs out and deletes entry',
    () async {
      await service.applyAuthPayloadForTest(authPayload('1001'));

      await service.removeSavedAccount('1001');

      expect(service.isLoggedIn, isFalse);
      expect(await service.listSavedAccounts(), isEmpty);
    },
  );

  test(
    'suspendCurrentSessionLocally keeps credentials for instant re-switch',
    () async {
      await service.applyAuthPayloadForTest(authPayload('1001'));

      await service.suspendCurrentSessionLocally();

      expect(service.isLoggedIn, isFalse);
      expect(service.token, isNull);
      final accounts = await service.listSavedAccounts();
      expect(accounts, hasLength(1));
      expect(accounts.first.needsRelogin, isFalse);
      expect(accounts.first.refreshToken, 'refresh_1001');

      // 挂起后可直接切回。
      final outcome = await service.switchToSavedAccount('1001');
      expect(outcome, AccountSwitchOutcome.success);
      expect(service.userId, '1001');
    },
  );
}
