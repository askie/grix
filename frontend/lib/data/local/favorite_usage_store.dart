import 'dart:convert';

import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../providers/auth_service.dart';
import '../providers/user_favorite_path_service.dart';

/// 收藏路径最近使用时间的本地存储。
///
/// 用于在文件选择器的收藏列表中把最近用过的收藏排在前面。
/// 数据按当前用户隔离，未登录时使用兜底 key。
class FavoriteUsageStore {
  FavoriteUsageStore({String? userId}) : _userId = userId;

  /// 使用当前登录用户的 [AuthService.userId] 构造存储实例。
  ///
  /// AuthService 未注册时退回兜底 key：收藏排序是非关键的本地增强，
  /// 不应让文件选择器在缺少该依赖的环境下直接构建失败。
  FavoriteUsageStore.currentUser()
    : _userId = Get.isRegistered<AuthService>()
          ? Get.find<AuthService>().userId
          : null;

  final String? _userId;

  String get _prefsKey => _userId != null
      ? 'favorite_path_last_used_v1:$_userId'
      : 'favorite_path_last_used_v1';

  /// 加载每个收藏 id 对应的最近使用时间戳（epoch millis）。
  Future<Map<String, int>> load() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString(_prefsKey);
      if (raw == null || raw.isEmpty) return {};
      final decoded = jsonDecode(raw) as Map<String, dynamic>;
      return decoded.map((k, v) => MapEntry(k, (v as num).toInt()));
    } catch (_) {
      // 本地缓存损坏时返回空，避免影响功能。
      return {};
    }
  }

  /// 将指定收藏 id 的最近使用时间更新为当前时刻。
  Future<void> touchAll(Set<String> ids) async {
    if (ids.isEmpty) return;
    try {
      final prefs = await SharedPreferences.getInstance();
      final map = await load();
      final now = DateTime.now().millisecondsSinceEpoch;
      for (final id in ids) {
        map[id] = now;
      }
      await prefs.setString(_prefsKey, jsonEncode(map));
    } catch (_) {
      // 本地非关键数据，失败不阻断交互。
    }
  }

  /// 清理掉已不存在的收藏 id，避免僵尸数据累积。
  Future<void> prune(Set<String> validIds) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final map = await load();
      final before = map.length;
      map.removeWhere((id, _) => !validIds.contains(id));
      if (map.length != before) {
        await prefs.setString(_prefsKey, jsonEncode(map));
      }
    } catch (_) {
      // 本地非关键数据，失败不阻断交互。
    }
  }

  /// 按最近使用时间对收藏列表排序。
  ///
  /// 有记录的收藏按时间戳降序排在前面；无记录的保持服务器原始顺序。
  static List<FavoritePathItem> sortByLastUsed(
    List<FavoritePathItem> favorites,
    Map<String, int> lastUsedAt,
  ) {
    final entries = favorites.asMap().entries.toList();
    entries.sort((a, b) {
      final aTime = lastUsedAt[a.value.id];
      final bTime = lastUsedAt[b.value.id];
      if (aTime != null && bTime != null) {
        return bTime.compareTo(aTime);
      }
      if (aTime != null) return -1;
      if (bTime != null) return 1;
      return a.key.compareTo(b.key);
    });
    return entries.map((e) => e.value).toList();
  }
}
