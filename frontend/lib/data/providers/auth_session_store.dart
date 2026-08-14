import 'auth_session_store_impl_stub.dart'
    if (dart.library.js_interop) 'auth_session_store_impl_web.dart' as impl;

class AuthSessionStore {
  AuthSessionStore._(this._impl);

  final impl.AuthSessionStoreImpl _impl;

  static Future<AuthSessionStore> create() async {
    return AuthSessionStore._(await impl.createAuthSessionStoreImpl());
  }

  Future<String?> getString(String key) => _impl.getString(key);

  Future<int?> getInt(String key) => _impl.getInt(key);

  Future<bool?> getBool(String key) => _impl.getBool(key);

  Future<void> setString(String key, String value) =>
      _impl.setString(key, value);

  Future<void> setInt(String key, int value) => _impl.setInt(key, value);

  Future<void> setBool(String key, bool value) => _impl.setBool(key, value);

  Future<void> remove(String key) => _impl.remove(key);

  Future<void> removeAll(Iterable<String> keys) async {
    for (final key in keys) {
      await _impl.remove(key);
    }
  }

  Future<void> clearLegacyGlobalAuthData(Iterable<String> keys) {
    return _impl.clearLegacyGlobalAuthData(keys);
  }
}
