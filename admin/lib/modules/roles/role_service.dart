import '../../core/network/api_client.dart';
import 'role_item.dart';

/// 角色管理 API。
class RoleService {
  static Future<List<RoleItem>> list() async {
    final data = await ApiClient.instance.get('/roles');
    final raw = ((data as Map)['items'] as List?) ?? [];
    return raw
        .map((e) => RoleItem.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  static Future<RoleItem> create({
    required String name,
    required String description,
    required List<String> permissions,
  }) async {
    final data = await ApiClient.instance.post('/roles', data: {
      'name': name,
      'description': description,
      'permissions': permissions,
    });
    return RoleItem.fromJson(
        ((data as Map)['role'] as Map).cast<String, dynamic>());
  }

  static Future<RoleItem> update(String id, {
    required String name,
    required String description,
    required List<String> permissions,
  }) async {
    final data = await ApiClient.instance.put('/roles/$id', data: {
      'name': name,
      'description': description,
      'permissions': permissions,
    });
    return RoleItem.fromJson(
        ((data as Map)['role'] as Map).cast<String, dynamic>());
  }

  static Future<void> remove(String id) =>
      ApiClient.instance.delete('/roles/$id');
}
