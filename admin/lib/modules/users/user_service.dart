import '../../core/network/api_client.dart';
import '../../core/network/page_result.dart';
import 'admin_user_item.dart';

/// 用户管理 API。
class UserService {
  /// 列表：支持关键词、状态（active/banned/空=全部）、在线筛选与分页。
  static Future<PageResult<AdminUserItem>> list({
    String? query,
    String? status,
    bool onlineOnly = false,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get(
      '/users',
      query: {
        if (query != null && query.isNotEmpty) 'q': query,
        if (status != null && status.isNotEmpty) 'status': status,
        if (onlineOnly) 'online': true,
        'page': page,
        'page_size': pageSize,
      },
    );
    return PageResult.fromData(data, AdminUserItem.fromJson);
  }

  /// 批量按 ID 查询用户（ID→昵称目录）。任何已登录管理员可用；
  /// 无 users 权限时后端会抹掉邮箱/手机号，仅保留展示字段。
  static Future<List<AdminUserItem>> lookup(List<String> ids) async {
    if (ids.isEmpty) return const [];
    final data = await ApiClient.instance.get(
      '/users/lookup',
      query: {'ids': ids.join(',')},
    );
    return (data['items'] as List? ?? const [])
        .whereType<Map<String, dynamic>>()
        .map(AdminUserItem.fromJson)
        .toList();
  }

  static Future<void> ban(String userId, String reason) {
    return ApiClient.instance.post(
      '/users/$userId/ban',
      data: {'reason': reason},
    );
  }

  static Future<void> unban(String userId) {
    return ApiClient.instance.post('/users/$userId/unban');
  }

  static Future<void> unmuteModeration(String userId) {
    return ApiClient.instance.post('/users/$userId/unmute-moderation');
  }

  static Future<void> unlockLogin(String userId) {
    return ApiClient.instance.post('/users/$userId/unlock-login');
  }

  /// 强制解绑用户手机号：清空 users.phone_e164/phone_country
  /// 并删除 user_identities 里 phone_sms_* 记录。
  static Future<void> unbindPhone(String userId) {
    return ApiClient.instance.post('/users/$userId/unbind-phone');
  }
}
