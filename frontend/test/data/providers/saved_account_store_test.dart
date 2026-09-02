import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_session_store.dart';
import 'package:grix/data/providers/saved_account_store.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late SavedAccountStore store;

  SavedAccount buildAccount(
    String userId, {
    String refreshToken = 'refresh',
    int lastActiveAtMs = 0,
  }) {
    return SavedAccount(
      userId: userId,
      username: 'user_$userId',
      nickname: 'nick_$userId',
      email: 'u$userId@example.com',
      accessToken: 'access_$userId',
      refreshToken: refreshToken,
      accessExpiresAtMs: 123,
      region: 'cn',
      apiEndpoint: 'https://api.example.com',
      wsEndpoint: 'wss://ws.example.com',
      lastActiveAtMs: lastActiveAtMs,
    );
  }

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    store = SavedAccountStore(await AuthSessionStore.create());
  });

  test('upsert then list returns saved account with full fields', () async {
    await store.upsert(buildAccount('1001'));

    final accounts = await store.list();
    expect(accounts, hasLength(1));
    final account = accounts.first;
    expect(account.userId, '1001');
    expect(account.username, 'user_1001');
    expect(account.nickname, 'nick_1001');
    expect(account.email, 'u1001@example.com');
    expect(account.accessToken, 'access_1001');
    expect(account.refreshToken, 'refresh');
    expect(account.accessExpiresAtMs, 123);
    expect(account.region, 'cn');
    expect(account.apiEndpoint, 'https://api.example.com');
    expect(account.wsEndpoint, 'wss://ws.example.com');
    expect(account.needsRelogin, isFalse);
  });

  test('upsert replaces existing entry for the same user', () async {
    await store.upsert(buildAccount('1001'));
    await store.upsert(
      buildAccount('1001').copyWith(accessToken: 'newer-access'),
    );

    final accounts = await store.list();
    expect(accounts, hasLength(1));
    expect(accounts.first.accessToken, 'newer-access');
  });

  test('list sorts by lastActiveAtMs descending', () async {
    await store.upsert(buildAccount('old', lastActiveAtMs: 100));
    await store.upsert(buildAccount('newest', lastActiveAtMs: 300));
    await store.upsert(buildAccount('middle', lastActiveAtMs: 200));

    final accounts = await store.list();
    expect(accounts.map((a) => a.userId).toList(), ['newest', 'middle', 'old']);
  });

  test('remove deletes only the target account', () async {
    await store.upsert(buildAccount('1001'));
    await store.upsert(buildAccount('1002'));

    await store.remove('1001');

    final accounts = await store.list();
    expect(accounts, hasLength(1));
    expect(accounts.first.userId, '1002');
  });

  test('clearCredentials keeps entry but marks needsRelogin', () async {
    await store.upsert(buildAccount('1001'));

    await store.clearCredentials('1001');

    final account = await store.find('1001');
    expect(account, isNotNull);
    expect(account!.accessToken, isEmpty);
    expect(account.refreshToken, isEmpty);
    expect(account.accessExpiresAtMs, 0);
    expect(account.needsRelogin, isTrue);
    // 资料字段保留，供列表展示与重登预识别。
    expect(account.nickname, 'nick_1001');
  });

  test('corrupted storage payload degrades to empty list', () async {
    SharedPreferences.setMockInitialValues({
      SavedAccountStore.storageKey: '{not-json',
    });
    store = SavedAccountStore(await AuthSessionStore.create());

    expect(await store.list(), isEmpty);
  });

  test('find returns null for unknown or blank user id', () async {
    await store.upsert(buildAccount('1001'));

    expect(await store.find('9999'), isNull);
    expect(await store.find('  '), isNull);
  });
}
