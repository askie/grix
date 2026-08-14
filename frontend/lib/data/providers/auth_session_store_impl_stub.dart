import 'package:shared_preferences/shared_preferences.dart';

import '../../app/profile/desktop_runtime.dart';
import '../../app/profile/profile_local_store.dart';

Future<AuthSessionStoreImpl> createAuthSessionStoreImpl() async {
  // 桌面端：账号态入实例 profile 的专属文件，多实例登录不同账号互不覆盖。
  // 测试环境（FLUTTER_TEST）回落 SharedPreferences：widget 测试的
  // FakeAsync 区里 path_provider 平台通道永不返回会挂死。
  if (isDesktopRuntime) {
    return AuthSessionStoreImpl.profile(await ProfileLocalStore.instance());
  }
  return AuthSessionStoreImpl.prefs(await SharedPreferences.getInstance());
}

class AuthSessionStoreImpl {
  AuthSessionStoreImpl.prefs(SharedPreferences prefs)
    : _prefs = prefs,
      _profileStore = null;

  AuthSessionStoreImpl.profile(ProfileLocalStore store)
    : _prefs = null,
      _profileStore = store;

  final SharedPreferences? _prefs;
  final ProfileLocalStore? _profileStore;

  Future<String?> getString(String key) async => _profileStore != null
      ? _profileStore.getString(key)
      : _prefs!.getString(key);

  Future<int?> getInt(String key) async =>
      _profileStore != null ? _profileStore.getInt(key) : _prefs!.getInt(key);

  Future<bool?> getBool(String key) async =>
      _profileStore != null ? _profileStore.getBool(key) : _prefs!.getBool(key);

  Future<void> setString(String key, String value) async {
    final store = _profileStore;
    if (store != null) {
      await store.set(key, value);
      return;
    }
    await _prefs!.setString(key, value);
  }

  Future<void> setInt(String key, int value) async {
    final store = _profileStore;
    if (store != null) {
      await store.set(key, value);
      return;
    }
    await _prefs!.setInt(key, value);
  }

  Future<void> setBool(String key, bool value) async {
    final store = _profileStore;
    if (store != null) {
      await store.set(key, value);
      return;
    }
    await _prefs!.setBool(key, value);
  }

  Future<void> remove(String key) async {
    final store = _profileStore;
    if (store != null) {
      await store.remove(key);
      return;
    }
    await _prefs!.remove(key);
  }

  Future<void> clearLegacyGlobalAuthData(Iterable<String> keys) async {}
}
