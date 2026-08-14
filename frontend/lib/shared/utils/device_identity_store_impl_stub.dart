import 'package:shared_preferences/shared_preferences.dart';

const String _deviceIdKey = 'device_identity_id';

Future<DeviceIdentityStoreImpl> createDeviceIdentityStoreImpl() async {
  final prefs = await SharedPreferences.getInstance();
  return DeviceIdentityStoreImpl(prefs);
}

class DeviceIdentityStoreImpl {
  DeviceIdentityStoreImpl(this._prefs);

  final SharedPreferences _prefs;

  Future<String?> getDeviceId() async => _prefs.getString(_deviceIdKey);

  Future<void> setDeviceId(String value) async {
    await _prefs.setString(_deviceIdKey, value);
  }
}
