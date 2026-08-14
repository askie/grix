import 'package:web/web.dart' as web;

Future<AuthSessionStoreImpl> createAuthSessionStoreImpl() async {
  return AuthSessionStoreImpl(web.window.localStorage);
}

class AuthSessionStoreImpl {
  AuthSessionStoreImpl(this._storage);

  final web.Storage _storage;

  Future<String?> getString(String key) async => _storage.getItem(key);

  Future<int?> getInt(String key) async {
    final raw = _storage.getItem(key);
    if (raw == null) {
      return null;
    }
    return int.tryParse(raw.trim());
  }

  Future<bool?> getBool(String key) async {
    final raw = _storage.getItem(key);
    if (raw == null) {
      return null;
    }
    switch (raw.trim().toLowerCase()) {
      case 'true':
        return true;
      case 'false':
        return false;
      default:
        return null;
    }
  }

  Future<void> setString(String key, String value) async {
    _storage.setItem(key, value);
  }

  Future<void> setInt(String key, int value) async {
    _storage.setItem(key, value.toString());
  }

  Future<void> setBool(String key, bool value) async {
    _storage.setItem(key, value.toString());
  }

  Future<void> remove(String key) async {
    _storage.removeItem(key);
  }

  Future<void> clearLegacyGlobalAuthData(Iterable<String> keys) async {}
}
