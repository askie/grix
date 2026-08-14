import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

class ChatConversationCardData extends ChatMessageCardData {
  const ChatConversationCardData({
    required this.sessionId,
    required this.sessionType,
    required this.title,
    this.peerId = '',
    this.peerNickname = '',
    this.avatarUrl = '',
  }) : super(type: ChatMessageCardType.conversation);

  final String sessionId;
  final String sessionType;
  final String title;
  final String peerId;
  final String peerNickname;
  final String avatarUrl;

  String get normalizedSessionType {
    return sessionType.trim() == 'group' ? 'group' : 'private';
  }

  String get displayTitle {
    final normalizedTitle = title.trim();
    if (normalizedTitle.isNotEmpty) {
      return normalizedTitle;
    }
    return sessionId.trim();
  }

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'session_id': sessionId,
      'session_type': sessionType,
      'title': title,
      'peer_id': peerId,
      'peer_nickname': peerNickname,
      'avatar_url': avatarUrl,
    };
  }
}
