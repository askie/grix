import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

/// A single conversation-level pinned message.
///
/// NOTE: the server has no session/thread-meta endpoint for this today (only
/// per-user session-list pinning exists — `session_members.is_pinned` —
/// which pins a whole conversation in the session list, not one message
/// inside it). Until a server endpoint exists this is device-local only: it
/// does not sync across the owner's other devices.
class ChatPinnedMessage {
  const ChatPinnedMessage({
    required this.sessionId,
    required this.msgId,
    required this.summary,
    required this.pinnedAt,
  });

  final String sessionId;
  final String msgId;
  final String summary;
  final int pinnedAt;

  ChatPinnedMessage copyWith({String? summary}) => ChatPinnedMessage(
    sessionId: sessionId,
    msgId: msgId,
    summary: summary ?? this.summary,
    pinnedAt: pinnedAt,
  );

  Map<String, dynamic> toJson() => {
    'session_id': sessionId,
    'msg_id': msgId,
    'summary': summary,
    'pinned_at': pinnedAt,
  };

  static ChatPinnedMessage? fromJson(Map<String, dynamic> json) {
    final sessionId = json['session_id']?.toString().trim() ?? '';
    final msgId = json['msg_id']?.toString().trim() ?? '';
    if (sessionId.isEmpty || msgId.isEmpty) {
      return null;
    }
    final rawPinnedAt = json['pinned_at'];
    final pinnedAt = rawPinnedAt is int
        ? rawPinnedAt
        : int.tryParse(rawPinnedAt?.toString() ?? '') ?? 0;
    return ChatPinnedMessage(
      sessionId: sessionId,
      msgId: msgId,
      summary: json['summary']?.toString() ?? '',
      pinnedAt: pinnedAt,
    );
  }
}

/// Device-local persistence for the one pinned message a conversation can
/// have, keyed by `chat_pinned_message_{userId}_{sessionId}` — same key
/// shape as [ChatDraftIndex]'s per-session draft keys.
class ChatPinnedMessageStore {
  ChatPinnedMessageStore._();

  static const String _keyPrefix = 'chat_pinned_message_';

  static String _key({required String userId, required String sessionId}) =>
      '$_keyPrefix${userId}_$sessionId';

  static Future<ChatPinnedMessage?> load({
    required String userId,
    required String sessionId,
  }) async {
    final uid = userId.trim();
    final sid = sessionId.trim();
    if (uid.isEmpty || sid.isEmpty) {
      return null;
    }
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString(_key(userId: uid, sessionId: sid));
      if (raw == null || raw.isEmpty) {
        return null;
      }
      final decoded = jsonDecode(raw);
      if (decoded is! Map) {
        return null;
      }
      return ChatPinnedMessage.fromJson(Map<String, dynamic>.from(decoded));
    } catch (_) {
      return null;
    }
  }

  static Future<void> save({
    required String userId,
    required ChatPinnedMessage pinned,
  }) async {
    final uid = userId.trim();
    final sid = pinned.sessionId.trim();
    if (uid.isEmpty || sid.isEmpty) {
      return;
    }
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(
        _key(userId: uid, sessionId: sid),
        jsonEncode(pinned.toJson()),
      );
    } catch (_) {
      // 持久层不可用时置顶仍在内存中生效，仅下次冷启动会丢失。
    }
  }

  static Future<void> clear({
    required String userId,
    required String sessionId,
  }) async {
    final uid = userId.trim();
    final sid = sessionId.trim();
    if (uid.isEmpty || sid.isEmpty) {
      return;
    }
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.remove(_key(userId: uid, sessionId: sid));
    } catch (_) {}
  }
}
