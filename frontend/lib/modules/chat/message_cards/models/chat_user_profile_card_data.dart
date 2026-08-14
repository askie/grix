import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

class ChatUserProfileCardData extends ChatMessageCardData {
  const ChatUserProfileCardData({
    required this.userId,
    required this.nickname,
    required this.avatarUrl,
    this.peerType = 1,
  }) : super(type: ChatMessageCardType.userProfile);

  final String userId;
  final String nickname;
  final String avatarUrl;
  final int peerType;

  int get normalizedPeerType => peerType == 2 ? 2 : 1;

  bool get isAgent => normalizedPeerType == 2;

  String get displayName {
    final normalizedNickname = nickname.trim();
    if (normalizedNickname.isNotEmpty) {
      return normalizedNickname;
    }
    return userId.trim();
  }

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'user_id': userId,
      'peer_type': normalizedPeerType,
      'nickname': nickname,
      'avatar_url': avatarUrl,
    };
  }
}
