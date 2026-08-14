import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_session_store.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'support/auth_session_browser_storage.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const authKeys = <String>[
    'access_token',
    'refresh_token',
    'access_expires_at_ms',
    'username_modified',
  ];

  setUp(() async {
    if (!kIsWeb) {
      SharedPreferences.setMockInitialValues({});
    }
    await clearBrowserLocalStorage(authKeys);
    await clearBrowserSessionStorage(authKeys);
  });

  test('stores and clears auth session fields', () async {
    final store = await AuthSessionStore.create();

    await store.setString('access_token', 'access_a');
    await store.setString('refresh_token', 'refresh_a');
    await store.setInt('access_expires_at_ms', 1234567890);
    await store.setBool('username_modified', true);

    expect(await store.getString('access_token'), 'access_a');
    expect(await store.getString('refresh_token'), 'refresh_a');
    expect(await store.getInt('access_expires_at_ms'), 1234567890);
    expect(await store.getBool('username_modified'), isTrue);

    await store.removeAll(const <String>[
      'access_token',
      'refresh_token',
      'access_expires_at_ms',
      'username_modified',
    ]);

    expect(await store.getString('access_token'), isNull);
    expect(await store.getString('refresh_token'), isNull);
    expect(await store.getInt('access_expires_at_ms'), isNull);
    expect(await store.getBool('username_modified'), isNull);
  });

  test('web auth session uses persistent localStorage', () async {
    if (!kIsWeb) {
      return;
    }

    final store = await AuthSessionStore.create();
    await store.setString('access_token', 'access_a');
    await store.setString('refresh_token', 'refresh_a');

    expect(await readBrowserLocalStorage('access_token'), 'access_a');
    expect(await readBrowserLocalStorage('refresh_token'), 'refresh_a');
    expect(await readBrowserSessionStorage('access_token'), isNull);
    expect(await readBrowserSessionStorage('refresh_token'), isNull);
  });

  test('web legacy cleanup does not erase current auth session', () async {
    if (!kIsWeb) {
      return;
    }

    final store = await AuthSessionStore.create();
    await store.setString('access_token', 'current_access');
    await store.setString('refresh_token', 'current_refresh');

    await store.clearLegacyGlobalAuthData(const <String>['access_token']);

    expect(await readBrowserLocalStorage('access_token'), 'current_access');
    expect(await readBrowserLocalStorage('refresh_token'), 'current_refresh');
  });
}
