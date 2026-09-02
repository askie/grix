import 'device_identity_store_impl_stub.dart'
    if (dart.library.js_interop) 'device_identity_store_impl_web.dart'
    as impl;

class DeviceIdentityStore {
  DeviceIdentityStore._(this._impl);

  final impl.DeviceIdentityStoreImpl _impl;

  static Future<DeviceIdentityStore> create() async {
    return DeviceIdentityStore._(await impl.createDeviceIdentityStoreImpl());
  }

  Future<String?> getDeviceId() => _impl.getDeviceId();

  Future<void> setDeviceId(String value) => _impl.setDeviceId(value);
}
