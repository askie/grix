/// 内容审查事件，对应后端 ContentModerationEventListItem。
class ModerationEvent {
  ModerationEvent({
    required this.id,
    required this.sessionId,
    required this.msgId,
    required this.senderId,
    required this.senderUsername,
    required this.senderEmail,
    required this.senderNickname,
    required this.matchedKeywords,
    required this.matchedKeywordsText,
    required this.recallStatusText,
    required this.recallAttempts,
    required this.hitCount,
    required this.muteApplied,
    required this.currentlyMuted,
    required this.createdAt,
  });

  final String id;
  final String sessionId;
  final String msgId;
  final String senderId;
  final String senderUsername;
  final String senderEmail;
  final String senderNickname;
  final List<String> matchedKeywords;
  final String matchedKeywordsText;
  final String recallStatusText;
  final int recallAttempts;
  final int hitCount;
  final bool muteApplied;
  final bool currentlyMuted;
  final DateTime? createdAt;

  String get senderName =>
      senderPlaceholderName.isNotEmpty ? senderPlaceholderName : senderId;

  String get senderPlaceholderName {
    if (senderNickname.isNotEmpty) return senderNickname;
    if (senderUsername.isNotEmpty) return senderUsername;
    return '';
  }

  factory ModerationEvent.fromJson(Map<String, dynamic> j) {
    return ModerationEvent(
      id: (j['id'] ?? '').toString(),
      sessionId: (j['session_id'] ?? '').toString(),
      msgId: (j['msg_id'] ?? '').toString(),
      senderId: (j['sender_id'] ?? '').toString(),
      senderUsername: (j['sender_username'] ?? '').toString(),
      senderEmail: (j['sender_email'] ?? '').toString(),
      senderNickname: (j['sender_nickname'] ?? '').toString(),
      matchedKeywords: ((j['matched_keywords'] as List?) ?? const [])
          .map((e) => e.toString())
          .toList(),
      matchedKeywordsText: (j['matched_keywords_text'] ?? '').toString(),
      recallStatusText: (j['recall_status_text'] ?? '').toString(),
      recallAttempts: (j['recall_attempts'] as num?)?.toInt() ?? 0,
      hitCount: (j['hit_count'] as num?)?.toInt() ?? 0,
      muteApplied: j['mute_applied'] == true,
      currentlyMuted: j['currently_muted'] == true,
      createdAt: j['created_at'] == null
          ? null
          : DateTime.tryParse(j['created_at'].toString()),
    );
  }
}

/// 内容审查设置，对应后端 systemsetting.ContentModerationSettings。
class ModerationSettings {
  ModerationSettings({
    required this.enabled,
    required this.keywords,
    required this.humanMuteThreshold,
  });

  final bool enabled;
  final List<String> keywords;
  final int humanMuteThreshold;

  factory ModerationSettings.fromJson(Map<String, dynamic> j) {
    return ModerationSettings(
      enabled: j['enabled'] == true,
      keywords: ((j['keywords'] as List?) ?? const [])
          .map((e) => e.toString())
          .toList(),
      humanMuteThreshold: (j['human_mute_threshold'] as num?)?.toInt() ?? 3,
    );
  }

  Map<String, dynamic> toJson() => {
    'enabled': enabled,
    'keywords': keywords,
    'human_mute_threshold': humanMuteThreshold,
  };
}
