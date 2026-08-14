class LoginDeviceSessionModel {
  const LoginDeviceSessionModel({
    required this.sessionId,
    required this.deviceId,
    required this.platform,
    required this.online,
    required this.current,
    required this.lastSeenAt,
    required this.createdAt,
  });

  final String sessionId;
  final String deviceId;
  final String platform;
  final bool online;
  final bool current;
  final DateTime? lastSeenAt;
  final DateTime? createdAt;

  factory LoginDeviceSessionModel.fromJson(Map<String, dynamic> json) {
    return LoginDeviceSessionModel(
      sessionId: json['session_id']?.toString().trim() ?? '',
      deviceId: json['device_id']?.toString().trim() ?? '',
      platform: json['platform']?.toString().trim() ?? '',
      online: _toBool(json['online']),
      current: _toBool(json['current']),
      lastSeenAt: _toDateTime(json['last_seen_at']),
      createdAt: _toDateTime(json['created_at']),
    );
  }

  static bool _toBool(dynamic source) {
    if (source is bool) return source;
    if (source is num) return source != 0;
    final raw = source?.toString().trim().toLowerCase() ?? '';
    return raw == '1' || raw == 'true' || raw == 'yes';
  }

  static DateTime? _toDateTime(dynamic source) {
    final raw = source?.toString().trim() ?? '';
    if (raw.isEmpty) {
      return null;
    }
    return DateTime.tryParse(raw)?.toLocal();
  }
}
