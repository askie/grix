import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../app/profile/desktop_runtime.dart';
import '../../app/profile/profile_local_store.dart';

Future<AuthSessionStoreImpl> createAuthSessionStoreImpl() async {
  // 桌面端：账号态入实例 profile 的专属文件，多实例登录不同账号互不覆盖。
  if (isDesktopRuntime) {
    return AuthSessionStoreImpl.profile(await ProfileLocalStore.instance());
  }
  // 测试环境（FLUTTER_TEST）回落 SharedPreferences：widget 测试的
  // FakeAsync 区里 path_provider / flutter_secure_storage 平台通道
  // 永不返回会挂死。
  if (Platform.environment.containsKey('FLUTTER_TEST')) {
    return AuthSessionStoreImpl.prefs(await SharedPreferences.getInstance());
  }
  // 移动端（iOS/Android）：登录态 token 与多账号快照必须进系统安全存储
  // （Keychain / Keystore 加密），不再明文落 SharedPreferences。
  return AuthSessionStoreImpl.secure(const FlutterSecureStorage());
}

class AuthSessionStoreImpl {
  AuthSessionStoreImpl.prefs(SharedPreferences prefs)
    : _prefs = prefs,
      _profileStore = null,
      _secure = null;

  AuthSessionStoreImpl.profile(ProfileLocalStore store)
    : _prefs = null,
      _profileStore = store,
      _secure = null;

  AuthSessionStoreImpl.secure(FlutterSecureStorage secure)
    : _prefs = null,
      _profileStore = null,
      _secure = secure;

  final SharedPreferences? _prefs;
  final ProfileLocalStore? _profileStore;
  final FlutterSecureStorage? _secure;

  Future<String?> getString(String key) async {
    final secure = _secure;
    if (secure != null) return secure.read(key: key);
    return _profileStore != null
        ? _profileStore.getString(key)
        : _prefs!.getString(key);
  }

  Future<int?> getInt(String key) async {
    final secure = _secure;
    if (secure != null) {
      final raw = await secure.read(key: key);
      return raw == null ? null : int.tryParse(raw.trim());
    }
    return _profileStore != null ? _profileStore.getInt(key) : _prefs!.getInt(key);
  }

  Future<bool?> getBool(String key) async {
    final secure = _secure;
    if (secure != null) {
      final raw = await secure.read(key: key);
      if (raw == null) return null;
      switch (raw.trim().toLowerCase()) {
        case 'true':
          return true;
        case 'false':
          return false;
        default:
          return null;
      }
    }
    return _profileStore != null
        ? _profileStore.getBool(key)
        : _prefs!.getBool(key);
  }

  Future<void> setString(String key, String value) async {
    final secure = _secure;
    if (secure != null) {
      await secure.write(key: key, value: value);
      return;
    }
    final store = _profileStore;
    if (store != null) {
      await store.set(key, value);
      return;
    }
    await _prefs!.setString(key, value);
  }

  Future<void> setInt(String key, int value) async {
    final secure = _secure;
    if (secure != null) {
      await secure.write(key: key, value: value.toString());
      return;
    }
    final store = _profileStore;
    if (store != null) {
      await store.set(key, value);
      return;
    }
    await _prefs!.setInt(key, value);
  }

  Future<void> setBool(String key, bool value) async {
    final secure = _secure;
    if (secure != null) {
      await secure.write(key: key, value: value.toString());
      return;
    }
    final store = _profileStore;
    if (store != null) {
      await store.set(key, value);
      return;
    }
    await _prefs!.setBool(key, value);
  }

  Future<void> remove(String key) async {
    final secure = _secure;
    if (secure != null) {
      await secure.delete(key: key);
      return;
    }
    final store = _profileStore;
    if (store != null) {
      await store.remove(key);
      return;
    }
    await _prefs!.remove(key);
  }

  /// 清理旧版本残留在全局 SharedPreferences 的登录态明文。
  ///
  /// 移动端安全存储实现下顺带做一次性迁移：明文值先搬进安全存储
  /// （已有安全值不覆盖），再删除明文副本，老用户升级后不必重新登录。
  Future<void> clearLegacyGlobalAuthData(Iterable<String> keys) async {
    final secure = _secure;
    if (secure == null) return;
    try {
      final prefs = await SharedPreferences.getInstance();
      for (final key in keys) {
        final legacy = prefs.get(key);
        if (legacy == null) continue;
        if (await secure.read(key: key) == null) {
          await secure.write(key: key, value: legacy.toString());
        }
        await prefs.remove(key);
      }
    } catch (e) {
      debugPrint('⚠️ Migrate legacy auth data to secure storage failed: $e');
    }
  }
}
