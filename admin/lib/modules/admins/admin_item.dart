/// 管理员列表项，对应后端 AdminListItem。
class AdminItem {
  AdminItem({
    required this.id,
    required this.username,
    required this.nickname,
    required this.role,
    required this.roleId,
    required this.roleName,
    required this.status,
    required this.lastLoginAt,
    required this.createdAt,
  });

  final String id;
  final String username;
  final String nickname;
  final int role; // 1=超级管理员 2=自定义角色
  final String? roleId;
  final String roleName;
  final int status; // 1=启用 2=禁用
  final DateTime? lastLoginAt;
  final DateTime? createdAt;

  bool get isActive => status == 1;
  bool get isSuperAdmin => role == 1;
  String get displayName => nickname.isNotEmpty ? nickname : username;
  String get roleDisplay => isSuperAdmin ? '超级管理员' : roleName;

  factory AdminItem.fromJson(Map<String, dynamic> j) {
    DateTime? t(dynamic v) =>
        v == null ? null : DateTime.tryParse(v.toString());
    return AdminItem(
      id: (j['id'] ?? '').toString(),
      username: (j['username'] ?? '').toString(),
      nickname: (j['nickname'] ?? '').toString(),
      role: (j['role'] as num?)?.toInt() ?? 1,
      roleId: j['role_id']?.toString(),
      roleName: (j['role_name'] ?? '').toString(),
      status: (j['status'] as num?)?.toInt() ?? 0,
      lastLoginAt: t(j['last_login_at']),
      createdAt: t(j['created_at']),
    );
  }
}

/// 管理员列表结果（含当前登录管理员 ID）。
class AdminListResult {
  AdminListResult({required this.items, required this.currentAdminId});

  final List<AdminItem> items;
  final String currentAdminId;
}
