import '../../core/network/api_client.dart';
import 'admin_item.dart';

/// 管理员管理 API。
class AdminService {
  static Future<AdminListResult> list() async {
    final data = await ApiClient.instance.get('/admins');
    final map = (data as Map).cast<String, dynamic>();
    final raw = (map['items'] as List?) ?? const [];
    return AdminListResult(
      items: raw
          .map((e) => AdminItem.fromJson((e as Map).cast<String, dynamic>()))
          .toList(),
      currentAdminId: (map['current_admin_id'] ?? '').toString(),
    );
  }

  /// 创建管理员。role=1 超管，role=2 自定义角色（需传 roleId）。
  static Future<void> create(
    String username,
    String nickname,
    String password, {
    int role = 1,
    String? roleId,
  }) {
    return ApiClient.instance.post('/admins', data: {
      'username': username,
      'nickname': nickname,
      'password': password,
      'role': role,
      if (roleId != null) 'role_id': roleId,
    });
  }

  static Future<void> enable(String id) =>
      ApiClient.instance.post('/admins/$id/enable');

  static Future<void> disable(String id) =>
      ApiClient.instance.post('/admins/$id/disable');

  static Future<void> remove(String id) =>
      ApiClient.instance.delete('/admins/$id');
}
