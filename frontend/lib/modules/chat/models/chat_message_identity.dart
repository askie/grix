import '../../../data/models/message_model.dart';

class ChatMessageIdentity {
  const ChatMessageIdentity._();

  static String selectionKey(MessageModel message) {
    final clientMsgId = message.clientMsgId?.trim() ?? '';
    if (clientMsgId.isNotEmpty) {
      return 'c:$clientMsgId';
    }

    final msgId = message.msgId.trim();
    if (msgId.isNotEmpty) {
      return 'm:$msgId';
    }

    final snippet = message.content.length > 24
        ? message.content.substring(0, 24)
        : message.content;
    return 'f:${message.senderType}:${message.senderId}:${message.createdAt}:$snippet';
  }
}
