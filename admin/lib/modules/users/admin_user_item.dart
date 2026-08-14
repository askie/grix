/// 用户列表项，对应后端 adminservice.UserListItem。
class AdminUserItem {
  AdminUserItem({
    required this.id,
    required this.username,
    required this.email,
    required this.phoneE164,
    required this.phoneCountry,
    required this.nickname,
    required this.avatarUrl,
    required this.status,
    required this.loginLocked,
    required this.lockRemaining,
    required this.bannedReason,
    required this.moderationMuted,
    required this.moderationMuteSessionCount,
    required this.createdAt,
  });

  final String id;
  final String username;
  final String email;
  final String phoneE164;
  final String phoneCountry;
  final String nickname;
  final String avatarUrl;

  /// 1=正常 2=封禁
  final int status;
  final bool loginLocked;
  final String lockRemaining;
  final String bannedReason;
  final bool moderationMuted;
  final int moderationMuteSessionCount;
  final DateTime? createdAt;

  bool get isBanned => status == 2;
  String get displayName => nickname.isNotEmpty ? nickname : username;

  factory AdminUserItem.fromJson(Map<String, dynamic> json) {
    DateTime? parseTime(dynamic v) {
      if (v == null) return null;
      return DateTime.tryParse(v.toString());
    }

    return AdminUserItem(
      id: (json['id'] ?? '').toString(),
      username: (json['username'] ?? '').toString(),
      email: (json['email'] ?? '').toString(),
      phoneE164: (json['phone_e164'] ?? '').toString(),
      phoneCountry: (json['phone_country'] ?? '').toString(),
      nickname: (json['nickname'] ?? '').toString(),
      avatarUrl: (json['avatar_url'] ?? '').toString(),
      status: (json['status'] as num?)?.toInt() ?? 0,
      loginLocked: json['login_locked'] == true,
      lockRemaining: (json['lock_remaining'] ?? '').toString(),
      bannedReason: (json['banned_reason'] ?? '').toString(),
      moderationMuted: json['moderation_muted'] == true,
      moderationMuteSessionCount:
          (json['moderation_mute_session_count'] as num?)?.toInt() ?? 0,
      createdAt: parseTime(json['created_at']),
    );
  }
}
