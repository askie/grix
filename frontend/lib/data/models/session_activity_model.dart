class SessionActivityModel {
  final String sessionId;
  final String kind;
  final bool active;
  final String actorId;
  final String actorType;
  final String executorId;
  final String executorType;
  final String source;
  final String refMsgId;
  final String refEventId;
  final String statusText;
  final int updatedAt;
  final int expiresAt;

  const SessionActivityModel({
    required this.sessionId,
    required this.kind,
    required this.active,
    required this.actorId,
    required this.actorType,
    required this.executorId,
    required this.executorType,
    required this.source,
    required this.refMsgId,
    required this.refEventId,
    required this.statusText,
    required this.updatedAt,
    required this.expiresAt,
  });

  factory SessionActivityModel.fromJson(Map<String, dynamic> json) {
    return SessionActivityModel(
      sessionId: json['session_id']?.toString().trim() ?? '',
      kind: json['kind']?.toString().trim() ?? '',
      active: json['active'] == true,
      actorId: _normalizeId(json['actor_id']),
      actorType: json['actor_type']?.toString().trim() ?? '',
      executorId: _normalizeId(json['executor_id']),
      executorType: json['executor_type']?.toString().trim() ?? '',
      source: json['source']?.toString().trim() ?? '',
      refMsgId: _normalizeId(json['ref_msg_id']),
      refEventId: _normalizeId(json['ref_event_id']),
      statusText: json['status_text']?.toString().trim() ?? '',
      updatedAt: _readInt(json['updated_at']),
      expiresAt: _readInt(json['expires_at']),
    );
  }

  bool get isExpired {
    if (expiresAt <= 0) return false;
    return expiresAt <= DateTime.now().millisecondsSinceEpoch;
  }

  Map<String, dynamic> toJson() {
    return {
      'session_id': sessionId,
      'kind': kind,
      'active': active,
      'actor_id': actorId,
      'actor_type': actorType,
      'executor_id': executorId,
      'executor_type': executorType,
      'source': source,
      'ref_msg_id': refMsgId,
      'ref_event_id': refEventId,
      'status_text': statusText,
      'updated_at': updatedAt,
      'expires_at': expiresAt,
    };
  }

  static String _normalizeId(dynamic value) {
    if (value == null) return '';
    if (value is String) return value.trim();
    if (value is int) return value.toString();
    if (value is num) {
      final asInt = value.toInt();
      if (value == asInt) {
        return asInt.toString();
      }
    }
    return value.toString().trim();
  }

  static int _readInt(dynamic value) {
    if (value == null) return 0;
    if (value is int) return value;
    if (value is num) return value.toInt();
    if (value is String) {
      return int.tryParse(value.trim()) ?? 0;
    }
    return 0;
  }
}
