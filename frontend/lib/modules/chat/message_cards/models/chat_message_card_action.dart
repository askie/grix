import 'chat_message_card_data.dart';

enum ChatMessageCardActionStatus { submitted, ignored, failed }

class ChatMessageCardActionResult {
  const ChatMessageCardActionResult._(this.status, {this.message = ''});

  const ChatMessageCardActionResult.submitted()
    : this._(ChatMessageCardActionStatus.submitted);

  const ChatMessageCardActionResult.ignored()
    : this._(ChatMessageCardActionStatus.ignored);

  const ChatMessageCardActionResult.failed([String message = ''])
    : this._(ChatMessageCardActionStatus.failed, message: message);

  final ChatMessageCardActionStatus status;
  final String message;
}

typedef ChatMessageCardActionHandler =
    Future<ChatMessageCardActionResult> Function(ChatMessageCardAction action);

class ChatMessageCardAction {
  const ChatMessageCardAction({
    required this.card,
    required this.actionId,
    this.sourceMessageId = '',
  });

  final ChatMessageCardData card;
  final String actionId;
  final String sourceMessageId;
}
