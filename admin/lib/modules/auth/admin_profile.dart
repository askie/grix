/// 当前登录管理员信息。
class AdminProfile {
  AdminProfile({
    required this.id,
    required this.username,
    required this.nickname,
    required this.role,
    required this.status,
    required this.permissions,
  });

  final String id;
  final String username;
  final String nickname;
  final int role; // 1=超级管理员 2=自定义角色
  final int status;
  final List<String> permissions; // 拥有的权限 key 列表

  bool get isSuperAdmin => role == 1;
  String get displayName => nickname.isNotEmpty ? nickname : username;

  /// 是否拥有指定权限。
  bool hasPermission(String key) => isSuperAdmin || permissions.contains(key);

  factory AdminProfile.fromJson(
    Map<String, dynamic> json, {
    List<String>? permissions,
  }) {
    return AdminProfile(
      id: (json['id'] ?? '').toString(),
      username: (json['username'] ?? '').toString(),
      nickname: (json['nickname'] ?? '').toString(),
      role: (json['role'] as num?)?.toInt() ?? 0,
      status: (json['status'] as num?)?.toInt() ?? 0,
      permissions: permissions ?? const [],
    );
  }
}
