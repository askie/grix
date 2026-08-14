/// Widget 访客封禁列表项，对应后端 adminservice.VisitorBanListItem。
class VisitorBanItem {
  VisitorBanItem({
    required this.id,
    required this.siteId,
    required this.siteName,
    required this.siteKey,
    required this.ownerUserId,
    required this.ownerUsername,
    required this.ownerNickname,
    required this.visitorId,
    required this.visitorKey,
    required this.visitorName,
    required this.visitorEmail,
    required this.sessionId,
    required this.lastPageUrl,
    required this.lastInitIpPrefix,
    required this.status,
    required this.hasIpBan,
    required this.createdAt,
    required this.updatedAt,
    required this.lastActiveAt,
    required this.lastInitAt,
  });

  final String id;
  final String siteId;
  final String siteName;
  final String siteKey;
  final String ownerUserId;
  final String ownerUsername;
  final String ownerNickname;
  final String visitorId;
  final String visitorKey;
  final String visitorName;
  final String visitorEmail;
  final String sessionId;
  final String lastPageUrl;
  final String lastInitIpPrefix;
  final int status;
  final bool hasIpBan;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final DateTime? lastActiveAt;
  final DateTime? lastInitAt;

  bool get isBanned => status == 3;

  String get visitorDisplayName {
    if (visitorName.isNotEmpty) return visitorName;
    return '访客 $visitorId';
  }

  String get ownerDisplayName {
    if (ownerNickname.isNotEmpty) return ownerNickname;
    if (ownerUsername.isNotEmpty) return ownerUsername;
    return ownerUserId;
  }

  factory VisitorBanItem.fromJson(Map<String, dynamic> json) {
    DateTime? parseTime(dynamic value) {
      if (value == null) return null;
      return DateTime.tryParse(value.toString());
    }

    return VisitorBanItem(
      id: (json['id'] ?? '').toString(),
      siteId: (json['site_id'] ?? '').toString(),
      siteName: (json['site_name'] ?? '').toString(),
      siteKey: (json['site_key'] ?? '').toString(),
      ownerUserId: (json['owner_user_id'] ?? '').toString(),
      ownerUsername: (json['owner_username'] ?? '').toString(),
      ownerNickname: (json['owner_nickname'] ?? '').toString(),
      visitorId: (json['visitor_id'] ?? '').toString(),
      visitorKey: (json['visitor_key'] ?? '').toString(),
      visitorName: (json['visitor_name'] ?? '').toString(),
      visitorEmail: (json['visitor_email'] ?? '').toString(),
      sessionId: (json['session_id'] ?? '').toString(),
      lastPageUrl: (json['last_page_url'] ?? '').toString(),
      lastInitIpPrefix: (json['last_init_ip_prefix'] ?? '').toString(),
      status: (json['status'] as num?)?.toInt() ?? 0,
      hasIpBan: json['has_ip_ban'] == true,
      createdAt: parseTime(json['created_at']),
      updatedAt: parseTime(json['updated_at']),
      lastActiveAt: parseTime(json['last_active_at']),
      lastInitAt: parseTime(json['last_init_at']),
    );
  }
}
