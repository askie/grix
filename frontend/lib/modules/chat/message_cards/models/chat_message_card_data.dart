import 'chat_message_card_type.dart';

abstract class ChatMessageCardData {
  const ChatMessageCardData({required this.type});

  final ChatMessageCardType type;

  Map<String, dynamic> toPayload();
}
