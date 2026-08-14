import 'dart:convert';

import '../../shared/utils/strict_int_parser.dart';

class MessageModel {
  final String msgId;
  final String sessionId;
  final String senderId;
  final int senderType;
  final int msgType;
  final String content;
  final Map<String, dynamic> extra;
  final bool isDeleted;
  final bool isRevoked;
  final int createdAt;
  final String? clientMsgId;
  final String? status;
  final String? agentDeliveryStatus;
  final String? quotedMessageId;
  final List<String>? visibleTo;

  /// 瞬态:标记流式占位消息属于"思考过程"流(后端 stream_chunk.is_thinking 派生)。
  /// 仅用于流式期渲染判定,不参与持久化/序列化。
  final bool isThinking;

  MessageModel({
    required this.msgId,
    required this.sessionId,
    required this.senderId,
    this.senderType = 1,
    this.msgType = 1,
    this.content = '',
    this.extra = const {},
    this.isDeleted = false,
    this.isRevoked = false,
    required this.createdAt,
    this.clientMsgId,
    this.status,
    this.agentDeliveryStatus,
    this.quotedMessageId,
    this.visibleTo,
    this.isThinking = false,
  });

  factory MessageModel.fromJson(Map<String, dynamic> json) {
    final extra = _readExtra(json['extra']);

    return MessageModel(
      msgId: _normalizeId(json['msg_id']),
      sessionId: json['session_id']?.toString() ?? '',
      senderId: _normalizeId(json['sender_id']),
      senderType: _readInt(json, 'sender_type', defaultValue: 1),
      msgType: _readInt(json, 'msg_type', defaultValue: 1),
      content: _resolveMessageContent(json, extra),
      extra: extra,
      isDeleted: json['is_deleted'] == true,
      isRevoked: json['is_revoked'] == true,
      createdAt: _normalizeTimestamp(_readInt(json, 'created_at')),
      clientMsgId: _normalizeOptionalId(json['client_msg_id']),
      status: json['status']?.toString(),
      agentDeliveryStatus: json['agent_delivery_status']?.toString(),
      quotedMessageId: _normalizeOptionalId(json['quoted_message_id']),
      visibleTo: readVisibleTo(json['visible_to']),
    );
  }

  static String _resolveMessageContent(
    Map<String, dynamic> json,
    Map<String, dynamic> extra,
  ) {
    final direct = json['content']?.toString() ?? '';
    if (direct.trim().isNotEmpty) {
      return direct;
    }
    for (final key in const [
      'final_content',
      'content_preview',
      'summary',
      'text',
      'message',
    ]) {
      final candidate = json[key]?.toString() ?? '';
      if (candidate.trim().isNotEmpty) {
        return candidate;
      }
    }
    for (final key in const [
      'content',
      'final_content',
      'content_preview',
      'summary',
      'text',
      'message',
    ]) {
      final candidate = extra[key]?.toString() ?? '';
      if (candidate.trim().isNotEmpty) {
        return candidate;
      }
    }
    return direct;
  }

  Map<String, dynamic> toJson() {
    return {
      'msg_id': msgId,
      'session_id': sessionId,
      'sender_id': senderId,
      'sender_type': senderType,
      'msg_type': msgType,
      'content': content,
      'extra': extra,
      'is_deleted': isDeleted,
      'is_revoked': isRevoked,
      'created_at': createdAt,
      if (clientMsgId != null) 'client_msg_id': clientMsgId,
      if (status != null) 'status': status,
      if (agentDeliveryStatus != null)
        'agent_delivery_status': agentDeliveryStatus,
      if (quotedMessageId != null) 'quoted_message_id': quotedMessageId,
      if (visibleTo != null && visibleTo!.isNotEmpty) 'visible_to': visibleTo,
    };
  }

  MessageModel copyWith({
    String? msgId,
    String? sessionId,
    String? senderId,
    int? senderType,
    int? msgType,
    String? content,
    Map<String, dynamic>? extra,
    bool? isDeleted,
    bool? isRevoked,
    int? createdAt,
    String? clientMsgId,
    String? status,
    String? agentDeliveryStatus,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool? isThinking,
  }) {
    return MessageModel(
      msgId: msgId ?? this.msgId,
      sessionId: sessionId ?? this.sessionId,
      senderId: senderId ?? this.senderId,
      senderType: senderType ?? this.senderType,
      msgType: msgType ?? this.msgType,
      content: content ?? this.content,
      extra: extra ?? this.extra,
      isDeleted: isDeleted ?? this.isDeleted,
      isRevoked: isRevoked ?? this.isRevoked,
      createdAt: createdAt ?? this.createdAt,
      clientMsgId: clientMsgId ?? this.clientMsgId,
      status: status ?? this.status,
      agentDeliveryStatus: agentDeliveryStatus ?? this.agentDeliveryStatus,
      quotedMessageId: quotedMessageId ?? this.quotedMessageId,
      visibleTo: visibleTo ?? this.visibleTo,
      isThinking: isThinking ?? this.isThinking,
    );
  }

  /// call_segment(msgType=6) 消息的真实发言者 ID。
  /// 语音大脑场景下 sender_id 是文字 agent（权限用），extra.speaker_user_id 才是实际发声方（如豆包）。
  String get effectiveSenderId {
    if (msgType == 6) {
      final speakerId = extra['speaker_user_id'];
      if (speakerId is String && speakerId.isNotEmpty) return speakerId;
    }
    return senderId;
  }

  static int _normalizeTimestamp(int ts) {
    final n = ts;
    if (n > 0 && n < 10000000000) {
      return n * 1000;
    }
    return n;
  }

  static int _readInt(
    Map<String, dynamic> json,
    String key, {
    int? defaultValue,
  }) {
    final value = json[key];
    if (value == null) {
      if (defaultValue != null) return defaultValue;
      throw FormatException('MessageModel.$key is required and must be int');
    }
    return StrictIntParser.parse(value, fieldName: 'MessageModel.$key');
  }

  static String _normalizeId(dynamic value) {
    final normalized = _normalizeOptionalId(value);
    return normalized ?? '';
  }

  static String? _normalizeOptionalId(dynamic value) {
    if (value == null) return null;

    if (value is String) return value.trim();
    if (value is int) return value.toString();
    if (value is num) {
      final asInt = value.toInt();
      if (value == asInt) {
        return asInt.toString();
      }
      return value.toString();
    }

    final raw = value.toString().trim();
    if (raw.isEmpty) return '';
    return raw;
  }

  static Map<String, dynamic> _readExtra(dynamic value) {
    if (value is Map<String, dynamic>) {
      return Map<String, dynamic>.from(value);
    }
    if (value is Map) {
      return Map<String, dynamic>.from(value);
    }
    if (value is String) {
      final trimmed = value.trim();
      if (trimmed.isEmpty) {
        return const {};
      }
      try {
        final decoded = jsonDecode(trimmed);
        if (decoded is Map) {
          return Map<String, dynamic>.from(decoded);
        }
      } catch (_) {
        return const {};
      }
    }
    return const {};
  }

  /// 解析 wire/DB 侧的 visible_to 字段（List 或 JSON 字符串），供 fromJson
  /// 与流式占位消息构建复用，保持同一解析口径。
  static List<String>? readVisibleTo(dynamic value) {
    if (value == null) return null;
    if (value is String) {
      final decoded = jsonDecode(value);
      if (decoded is List) return readVisibleTo(decoded);
      return null;
    }
    if (value is List) {
      final ids = value
          .map((e) => e?.toString() ?? '')
          .where((e) => e.isNotEmpty)
          .toList();
      return ids.isEmpty ? null : ids;
    }
    return null;
  }
}
