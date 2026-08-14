class SessionAvatarMember {
  const SessionAvatarMember({
    required this.memberId,
    required this.memberType,
    required this.displayName,
    required this.avatarUrl,
  });

  final String memberId;
  final int memberType;
  final String displayName;
  final String avatarUrl;

  String get avatarSeed => '$memberType:$memberId';

  Map<String, dynamic> toJson() {
    return {
      'member_id': memberId,
      'member_type': memberType,
      'display_name': displayName,
      'avatar_url': avatarUrl,
    };
  }

  factory SessionAvatarMember.fromJson(Map<String, dynamic> json) {
    return SessionAvatarMember(
      memberId: (json['member_id'] ?? '').toString().trim(),
      memberType: _readInt(json['member_type']),
      displayName: (json['display_name'] ?? '').toString().trim(),
      avatarUrl: (json['avatar_url'] ?? '').toString().trim(),
    );
  }

  static int _readInt(dynamic value) {
    if (value is int) {
      return value;
    }
    if (value is num) {
      return value.toInt();
    }
    return int.tryParse(value?.toString().trim() ?? '') ?? 0;
  }

  @override
  bool operator ==(Object other) {
    if (identical(this, other)) return true;
    return other is SessionAvatarMember &&
        other.memberId == memberId &&
        other.memberType == memberType &&
        other.displayName == displayName &&
        other.avatarUrl == avatarUrl;
  }

  @override
  int get hashCode => Object.hash(memberId, memberType, displayName, avatarUrl);
}
