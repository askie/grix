import '../message_cards/services/chat_message_card_codec.dart';

class ChatVoiceCommandResponseFilter {
  ChatVoiceCommandResponseFilter._();

  static bool isSpeakablePlainText(String content) {
    final normalized = content.trim();
    if (normalized.isEmpty) return false;
    return ChatMessageCardCodec.decodeFromMessage(content: normalized) == null;
  }
}
