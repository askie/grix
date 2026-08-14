import 'region_pref_store_impl_stub.dart'
    if (dart.library.js_interop) 'region_pref_store_impl_web.dart'
    as impl;

/// 区域/端点等账号态偏好的存储门面。
///
/// 桌面端落到实例 profile 的专属文件（多实例登录不同区域账号互不覆盖），
/// 移动端与网页版维持 SharedPreferences 现状。
class RegionPrefStore {
  static Future<String?> getString(String key) => impl.regionPrefGetString(key);

  static Future<void> setString(String key, String value) =>
      impl.regionPrefSetString(key, value);

  static Future<void> remove(String key) => impl.regionPrefRemove(key);
}
