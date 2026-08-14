// agent WS 连接安全相关的前端数据模型：连接（登录）历史 + IP 规则（黑名单）。
// 字段对应后端 model.AgentConnectionLog / model.AgentIPRule 的 JSON 输出。

/// 一条 agent 连接（登录）历史记录。
class AgentConnectionLogEntry {
  const AgentConnectionLogEntry({
    required this.id,
    required this.clientType,
    required this.clientIP,
    required this.ipLocation,
    required this.isPrimary,
    required this.geoChanged,
    required this.allowlistMiss,
    required this.disconnectReason,
    this.connectedAt,
    this.disconnectedAt,
  });

  final String id;
  final String clientType;
  final String clientIP;
  final String ipLocation;
  final bool isPrimary;
  final bool geoChanged;
  final bool allowlistMiss;
  final String disconnectReason;
  final DateTime? connectedAt;
  final DateTime? disconnectedAt;

  /// 无断开时间即视为当前仍在线。
  bool get isOnline => disconnectedAt == null;

  factory AgentConnectionLogEntry.fromJson(Map<String, dynamic> json) {
    return AgentConnectionLogEntry(
      id: json['id']?.toString().trim() ?? '',
      clientType: json['client_type']?.toString().trim() ?? '',
      clientIP: json['client_ip']?.toString().trim() ?? '',
      ipLocation: json['ip_location']?.toString().trim() ?? '',
      isPrimary: json['is_primary'] == true,
      geoChanged: json['geo_changed'] == true,
      allowlistMiss: json['allowlist_miss'] == true,
      disconnectReason: json['disconnect_reason']?.toString().trim() ?? '',
      connectedAt: _parseTime(json['connected_at']),
      disconnectedAt: _parseTime(json['disconnected_at']),
    );
  }
}

/// 一条 agent IP 规则（当前前端只用到黑名单 ban）。
class AgentIPRuleEntry {
  const AgentIPRuleEntry({
    required this.id,
    required this.ruleType,
    required this.ipCidr,
    required this.remark,
    this.createdAt,
  });

  final String id;
  final String ruleType;
  final String ipCidr;
  final String remark;
  final DateTime? createdAt;

  bool get isBan => ruleType == 'ban';
  bool get isAllow => ruleType == 'allow';

  factory AgentIPRuleEntry.fromJson(Map<String, dynamic> json) {
    return AgentIPRuleEntry(
      id: json['id']?.toString().trim() ?? '',
      ruleType: json['rule_type']?.toString().trim() ?? '',
      ipCidr: json['ip_cidr']?.toString().trim() ?? '',
      remark: json['remark']?.toString().trim() ?? '',
      createdAt: _parseTime(json['created_at']),
    );
  }
}

DateTime? _parseTime(dynamic raw) {
  if (raw == null) {
    return null;
  }
  final text = raw.toString().trim();
  if (text.isEmpty) {
    return null;
  }
  return DateTime.tryParse(text)?.toLocal();
}
