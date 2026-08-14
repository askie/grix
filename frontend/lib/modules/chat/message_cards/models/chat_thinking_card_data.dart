import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

class ChatThinkingCardData extends ChatMessageCardData {
  const ChatThinkingCardData({
    required this.content,
  }) : super(type: ChatMessageCardType.thinking);

  final String content;

  String get displayContent => content.trim();

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'content': content,
    };
  }
}
