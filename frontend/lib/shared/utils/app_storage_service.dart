import '../../data/providers/local_db.dart';
import 'region_pref_store.dart';
import 'user_image_cache_manager.dart';

const String _keyRegion = 'app_region';
const String _keyApiEndpoint = 'app_api_endpoint';
const String _keyWsEndpoint = 'app_ws_endpoint';

class AppStorageService {
  static Future<LocalStorageSummary> getActiveUserSummary() {
    return LocalDb.getStorageSummary();
  }

  static Future<void> clearActiveUserLocalData() async {
    await LocalDb.clearActiveUserData();
  }

  static Future<void> clearActiveUserImageCache() async {
    final userId = LocalDb.activeUserId;
    if (userId == null || userId.isEmpty) {
      return;
    }
    await UserImageCacheManager.clearUserCache(userId);
  }

  static Future<void> clearActiveUserStorage() async {
    await clearActiveUserImageCache();
    await clearActiveUserLocalData();
  }

  // ── 区域端点持久化 ──────────────────────────────────────────────

  /// 持久化区域对应的 API / WS 端点。
  ///
  /// 注意：不写区域标识（_keyRegion）。区域是用户在登录页手动选择的结果，
  /// 由 [saveRegion] 单独持久化；登录响应里携带的 region 不得覆盖它，否则会出现
  /// 「网页版同源请求实际打到当前域名所属区域、后端回的 region 把用户手选区域改掉」的问题。
  static Future<void> saveRegionEndpoints({
    required String apiEndpoint,
    required String wsEndpoint,
  }) async {
    try {
      await RegionPrefStore.setString(_keyApiEndpoint, apiEndpoint);
      await RegionPrefStore.setString(_keyWsEndpoint, wsEndpoint);
    } catch (_) {}
  }

  /// 清除持久化的区域端点缓存（API / WS），但保留用户选择的区域标识。
  /// 退出登录时调用：账号相关的端点缓存需清掉，避免脏数据；区域是用户的
  /// 主动选择，应跨账号、跨登录态保留，下次启动 resolveInitialRegion 继续沿用，
  /// 不会跳回按系统语言推断的默认区域。端点可由区域常量重新推导，清掉无副作用。
  static Future<void> clearRegionEndpoints() async {
    try {
      await RegionPrefStore.remove(_keyApiEndpoint);
      await RegionPrefStore.remove(_keyWsEndpoint);
    } catch (_) {}
  }

  /// 仅持久化区域标识（不含端点）。用于登录前用户在区域开关上的手动选择，
  /// 下次启动 resolveInitialRegion 会沿用该选择。
  static Future<void> saveRegion(String region) async {
    try {
      await RegionPrefStore.setString(_keyRegion, region);
    } catch (_) {}
  }

  /// 读取持久化的区域标识，未设置时返回 null。
  static Future<String?> loadRegion() async {
    try {
      return await RegionPrefStore.getString(_keyRegion);
    } catch (_) {
      return null;
    }
  }

  /// 读取持久化的 API 端点，未设置时返回 null。
  static Future<String?> loadApiEndpoint() async {
    try {
      return await RegionPrefStore.getString(_keyApiEndpoint);
    } catch (_) {
      return null;
    }
  }

  /// 读取持久化的 WS 端点，未设置时返回 null。
  static Future<String?> loadWsEndpoint() async {
    try {
      return await RegionPrefStore.getString(_keyWsEndpoint);
    } catch (_) {
      return null;
    }
  }
}
