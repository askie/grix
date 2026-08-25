import '../models/chat_forward_message_item.dart';

class ChatForwardContentBuilder {
  const ChatForwardContentBuilder._();

  static String buildConversationCardContent({
    required String cardContent,
    String accompanyingMessage = '',
  }) {
    final normalizedMessage = accompanyingMessage.trim();
    if (normalizedMessage.isEmpty) {
      return cardContent;
    }
    return '$normalizedMessage\n\n$cardContent';
  }

  static String buildMergedContent({
    required List<ChatForwardMessageItem> messages,
    required String title,
    required String senderLabel,
    required String timeLabel,
    required String emptyContentPlaceholder,
    String sessionIdLabel = 'Session ID',
    String messageIdLabel = 'Message ID',
  }) {
    if (messages.isEmpty) {
      return '';
    }

    final buffer = StringBuffer()..writeln('[$title]');
    for (var i = 0; i < messages.length; i++) {
      final message = messages[i];
      final senderName =
          message.senderName.trim().isEmpty ? '-' : message.senderName.trim();
      final content =
          message.content.isEmpty ? emptyContentPlaceholder : message.content;

      buffer.writeln();
      buffer.writeln('${i + 1}. $senderLabel: $senderName');
      buffer.writeln('$timeLabel: ${_formatTime(message.createdAt)}');
      final sessionId = message.sessionId.trim();
      if (sessionId.isNotEmpty) {
        buffer.writeln('$sessionIdLabel: $sessionId');
      }
      final messageId = message.messageId.trim();
      if (messageId.isNotEmpty) {
        buffer.writeln('$messageIdLabel: $messageId');
      }
      buffer.writeln(content);

      if (i != messages.length - 1) {
        buffer.writeln();
        buffer.writeln('---');
      }
    }

    return buffer.toString().trimRight();
  }

  static String _formatTime(int createdAtMs) {
    final dt = DateTime.fromMillisecondsSinceEpoch(createdAtMs).toLocal();
    return '${dt.year.toString().padLeft(4, '0')}-${dt.month.toString().padLeft(2, '0')}-${dt.day.toString().padLeft(2, '0')} ${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
  }
}
