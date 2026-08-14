import 'dart:convert';

/// 角色模型，对应后端 AdminRole。
class RoleItem {
  RoleItem({
    required this.id,
    required this.name,
    required this.description,
    required this.permissions,
    required this.createdAt,
  });

  final String id;
  final String name;
  final String description;
  final List<String> permissions;
  final DateTime? createdAt;

  factory RoleItem.fromJson(Map<String, dynamic> j) {
    return RoleItem(
      id: (j['id'] ?? '').toString(),
      name: (j['name'] ?? '').toString(),
      description: (j['description'] ?? '').toString(),
      permissions: _parsePerms(j['permissions']),
      createdAt: j['created_at'] == null
          ? null
          : DateTime.tryParse(j['created_at'].toString()),
    );
  }
}

List<String> _parsePerms(dynamic raw) {
  if (raw is List) return raw.map((e) => e.toString()).toList();
  if (raw is String) {
    try {
      final decoded = jsonDecode(raw);
      if (decoded is List) return decoded.map((e) => e.toString()).toList();
    } catch (_) {}
  }
  return [];
}
