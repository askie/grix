import 'package:web/web.dart' as web;

// Keep the same key shape as SharedPreferences on web so existing installs
// keep a stable device identity after we bypass the plugin for auth-critical
// reads.
const String _deviceIdKey = 'flutter.device_identity_id';

Future<DeviceIdentityStoreImpl> createDeviceIdentityStoreImpl() async {
  return DeviceIdentityStoreImpl(web.window.localStorage);
}

class DeviceIdentityStoreImpl {
  DeviceIdentityStoreImpl(this._storage);

  final web.Storage _storage;

  Future<String?> getDeviceId() async => _storage.getItem(_deviceIdKey);

  Future<void> setDeviceId(String value) async {
    _storage.setItem(_deviceIdKey, value);
  }
}
