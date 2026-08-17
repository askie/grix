import 'dart:convert';

import '../../shared/models/session_avatar_member.dart';
import '../../shared/utils/strict_int_parser.dart';

class SessionModel {
  final String sessionId;
  final String title;
  final String type;
  final String peerId;
  final int peerType;
  final String peerNickname;
  final String peerUsername;
  final int updatedAt;
  final bool isPinned;
  final bool isMuted;
  final int pinnedAt;
  final bool friendIsPinned;
  final int friendPinnedAt;
  final bool friendIsMuted;
  final bool isVisitor;
  final int unreadCount;
  final String lastMessage;
  final int lastMessageTime;
  final List<SessionAvatarMember> cachedGroupAvatarMembers;
  int get activityAt =>
      updatedAt >= lastMessageTime ? updatedAt : lastMessageTime;

  SessionModel({
    required this.sessionId,
    this.title = '',
    this.type = 'private',
    this.peerId = '',
    this.peerType = 0,
    this.peerNickname = '',
    this.peerUsername = '',
    required this.updatedAt,
    this.isPinned = false,
    this.isMuted = false,
    this.pinnedAt = 0,
    this.friendIsPinned = false,
    this.friendPinnedAt = 0,
    this.friendIsMuted = false,
    this.isVisitor = false,
    this.unreadCount = 0,
    this.lastMessage = '',
    required this.lastMessageTime,
    this.cachedGroupAvatarMembers = const <SessionAvatarMember>[],
  });

  factory SessionModel.fromJson(Map<String, dynamic> json) {
    return SessionModel(
      sessionId: json['session_id']?.toString() ?? '',
      title: json['title']?.toString() ?? '',
      type: json['type']?.toString() ?? 'private',
      peerId: json['peer_id']?.toString() ?? '',
      peerType: _readInt(json, 'peer_type', defaultValue: 0),
      peerNickname: json['peer_nickname']?.toString() ?? '',
      peerUsername: json['peer_username']?.toString() ?? '',
      updatedAt: _readInt(json, 'updated_at', defaultValue: 0),
      isPinned: _readBool(json, 'is_pinned', defaultValue: false),
      isMuted: _readBool(json, 'is_muted', defaultValue: false),
      pinnedAt: _readInt(json, 'pinned_at', defaultValue: 0),
      friendIsPinned: _readBool(json, 'friend_is_pinned', defaultValue: false),
      friendPinnedAt: _readInt(json, 'friend_pinned_at', defaultValue: 0),
      friendIsMuted: _readBool(json, 'friend_is_muted', defaultValue: false),
      isVisitor: _readBool(json, 'is_visitor', defaultValue: false),
      unreadCount: _readInt(json, 'unread_count', defaultValue: 0),
      lastMessage: json['last_message']?.toString() ?? '',
      lastMessageTime: _readInt(json, 'last_message_time', defaultValue: 0),
      cachedGroupAvatarMembers: _readGroupAvatarMembers(
        json['group_avatar_members'],
      ),
    );
  }

  Map<String, dynamic> toJson() {
    final map = {
      'session_id': sessionId,
      'title': title,
      'type': type,
      'peer_id': peerId,
      'peer_type': peerType,
      'peer_nickname': peerNickname,
      'peer_username': peerUsername,
      'updated_at': updatedAt,
      'is_pinned': isPinned,
      'is_muted': isMuted,
      'pinned_at': pinnedAt,
      'friend_is_pinned': friendIsPinned,
      'friend_pinned_at': friendPinnedAt,
      'friend_is_muted': friendIsMuted,
      'unread_count': unreadCount,
      'last_message': lastMessage,
      'last_message_time': lastMessageTime,
    };
    if (cachedGroupAvatarMembers.isNotEmpty) {
      map['group_avatar_members'] = jsonEncode(
        cachedGroupAvatarMembers
            .take(9)
            .map((member) => member.toJson())
            .toList(),
      );
    }
    return map;
  }

  SessionModel copyWith({
    String? sessionId,
    String? title,
    String? type,
    String? peerId,
    int? peerType,
    String? peerNickname,
    String? peerUsername,
    int? updatedAt,
    bool? isPinned,
    bool? isMuted,
    int? pinnedAt,
    bool? friendIsPinned,
    int? friendPinnedAt,
    bool? friendIsMuted,
    bool? isVisitor,
    int? unreadCount,
    String? lastMessage,
    int? lastMessageTime,
    List<SessionAvatarMember>? cachedGroupAvatarMembers,
  }) {
    return SessionModel(
      sessionId: sessionId ?? this.sessionId,
      title: title ?? this.title,
      type: type ?? this.type,
      peerId: peerId ?? this.peerId,
      peerType: peerType ?? this.peerType,
      peerNickname: peerNickname ?? this.peerNickname,
      peerUsername: peerUsername ?? this.peerUsername,
      updatedAt: updatedAt ?? this.updatedAt,
      isPinned: isPinned ?? this.isPinned,
      isMuted: isMuted ?? this.isMuted,
      pinnedAt: pinnedAt ?? this.pinnedAt,
      friendIsPinned: friendIsPinned ?? this.friendIsPinned,
      friendPinnedAt: friendPinnedAt ?? this.friendPinnedAt,
      friendIsMuted: friendIsMuted ?? this.friendIsMuted,
      isVisitor: isVisitor ?? this.isVisitor,
      unreadCount: unreadCount ?? this.unreadCount,
      lastMessage: lastMessage ?? this.lastMessage,
      lastMessageTime: lastMessageTime ?? this.lastMessageTime,
      cachedGroupAvatarMembers:
          cachedGroupAvatarMembers ?? this.cachedGroupAvatarMembers,
    );
  }

  static int compareByPriority(SessionModel a, SessionModel b) {
    if (a.isPinned != b.isPinned) {
      return b.isPinned ? 1 : -1;
    }

    final activityCompare = b.activityAt.compareTo(a.activityAt);
    if (activityCompare != 0) return activityCompare;

    if (a.isPinned && b.isPinned) {
      final pinnedCompare = b.pinnedAt.compareTo(a.pinnedAt);
      if (pinnedCompare != 0) return pinnedCompare;
    }

    return a.sessionId.compareTo(b.sessionId);
  }

  static int _readInt(
    Map<String, dynamic> json,
    String key, {
    required int defaultValue,
  }) {
    final value = json[key];
    if (value == null) return defaultValue;
    return StrictIntParser.parse(value, fieldName: 'SessionModel.$key');
  }

  static bool _readBool(
    Map<String, dynamic> json,
    String key, {
    required bool defaultValue,
  }) {
    final value = json[key];
    if (value == null) return defaultValue;
    if (value is bool) return value;
    if (value is String) {
      final normalized = value.trim().toLowerCase();
      if (normalized == 'true' || normalized == '1') return true;
      if (normalized == 'false' || normalized == '0') return false;
    }
    final parsed = StrictIntParser.tryParse(value);
    if (parsed == 0) return false;
    if (parsed == 1) return true;
    throw FormatException(
      'SessionModel.$key expects bool/int(0|1), got ${value.runtimeType}',
    );
  }

  static List<SessionAvatarMember> _readGroupAvatarMembers(dynamic raw) {
    if (raw == null) return const <SessionAvatarMember>[];

    dynamic decoded = raw;
    if (raw is String) {
      final normalized = raw.trim();
      if (normalized.isEmpty) return const <SessionAvatarMember>[];
      try {
        decoded = jsonDecode(normalized);
      } catch (_) {
        return const <SessionAvatarMember>[];
      }
    }

    if (decoded is! List) return const <SessionAvatarMember>[];
    final members = <SessionAvatarMember>[];
    for (final item in decoded.take(9)) {
      if (item is! Map) continue;
      final member = SessionAvatarMember.fromJson(
        Map<String, dynamic>.from(item),
      );
      if (member.memberId.isEmpty) continue;
      members.add(member);
    }
    if (members.isEmpty) return const <SessionAvatarMember>[];
    return List<SessionAvatarMember>.unmodifiable(members);
  }

  static bool _avatarMemberListsEqual(
    List<SessionAvatarMember> a,
    List<SessionAvatarMember> b,
  ) {
    if (identical(a, b)) return true;
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (a[i] != b[i]) return false;
    }
    return true;
  }

  @override
  bool operator ==(Object other) {
    if (identical(this, other)) {
      return true;
    }
    return other is SessionModel &&
        other.sessionId == sessionId &&
        other.title == title &&
        other.type == type &&
        other.peerId == peerId &&
        other.peerType == peerType &&
        other.peerNickname == peerNickname &&
        other.peerUsername == peerUsername &&
        other.updatedAt == updatedAt &&
        other.isPinned == isPinned &&
        other.isMuted == isMuted &&
        other.pinnedAt == pinnedAt &&
        other.friendIsPinned == friendIsPinned &&
        other.friendPinnedAt == friendPinnedAt &&
        other.friendIsMuted == friendIsMuted &&
        other.isVisitor == isVisitor &&
        other.unreadCount == unreadCount &&
        other.lastMessage == lastMessage &&
        other.lastMessageTime == lastMessageTime &&
        _avatarMemberListsEqual(
          other.cachedGroupAvatarMembers,
          cachedGroupAvatarMembers,
        );
  }

  @override
  int get hashCode => Object.hash(
    sessionId,
    title,
    type,
    peerId,
    peerType,
    peerNickname,
    peerUsername,
    updatedAt,
    isPinned,
    isMuted,
    pinnedAt,
    friendIsPinned,
    friendPinnedAt,
    friendIsMuted,
    isVisitor,
    unreadCount,
    lastMessage,
    lastMessageTime,
    Object.hashAll(cachedGroupAvatarMembers),
  );
}
