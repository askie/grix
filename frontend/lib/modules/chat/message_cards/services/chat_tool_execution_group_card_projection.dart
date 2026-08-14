import '../../../../data/models/message_model.dart';
import '../models/chat_message_card_data.dart';
import '../models/chat_tool_execution_card_data.dart';
import '../models/chat_tool_execution_group_card_data.dart';
import 'chat_message_card_codec.dart';

class ChatToolExecutionGroupProjection {
  const ChatToolExecutionGroupProjection({
    required this.overridesByIndex,
    required this.hiddenIndexes,
  });

  final Map<int, ChatMessageCardData> overridesByIndex;
  final Set<int> hiddenIndexes;

  static const empty = ChatToolExecutionGroupProjection(
    overridesByIndex: <int, ChatMessageCardData>{},
    hiddenIndexes: <int>{},
  );
}

class ChatToolExecutionGroupProjector {
  const ChatToolExecutionGroupProjector._();

  static ChatToolExecutionGroupProjection project(
    List<MessageModel> messages, {
    String? currentUserId,
    List<ChatMessageCardData?>? decodedCards,
  }) {
    if (messages.isEmpty) {
      return ChatToolExecutionGroupProjection.empty;
    }

    final resolvedDecodedCards =
        decodedCards ??
        List<ChatMessageCardData?>.generate(messages.length, (index) {
          final message = messages[index];
          final card = ChatMessageCardCodec.decodeFromMessage(
            content: message.content,
          );
          return card;
        });
    final normalizedCurrentUserId = currentUserId?.trim() ?? '';
    final overridesByIndex = <int, ChatMessageCardData>{};
    final hiddenIndexes = <int>{};

    var index = 0;
    while (index < messages.length) {
      final startMessage = messages[index];
      final startCard = _decodeToolExecutionCard(resolvedDecodedCards[index]);
      if (startCard == null ||
          _isCurrentUserMessage(startMessage, normalizedCurrentUserId)) {
        index++;
        continue;
      }

      final startIndex = index;
      final senderId = startMessage.senderId.trim();
      final children = <ChatToolExecutionCardData>[startCard];
      final childIndexes = <int>[startIndex];
      index++;

      while (index < messages.length) {
        final message = messages[index];
        final card = _decodeToolExecutionCard(resolvedDecodedCards[index]);
        if (card == null ||
            _isCurrentUserMessage(message, normalizedCurrentUserId) ||
            message.senderId.trim() != senderId) {
          break;
        }
        children.add(card);
        childIndexes.add(index);
        index++;
      }

      if (children.length < 2) {
        continue;
      }

      overridesByIndex[startIndex] = ChatToolExecutionGroupCardData(
        children: List<ChatToolExecutionCardData>.unmodifiable(children),
        displayCard: children.last,
      );
      hiddenIndexes.addAll(childIndexes.skip(1));
    }

    if (overridesByIndex.isEmpty && hiddenIndexes.isEmpty) {
      return ChatToolExecutionGroupProjection.empty;
    }

    return ChatToolExecutionGroupProjection(
      overridesByIndex: overridesByIndex,
      hiddenIndexes: hiddenIndexes,
    );
  }

  static ChatToolExecutionCardData? _decodeToolExecutionCard(
    ChatMessageCardData? card,
  ) {
    return card is ChatToolExecutionCardData ? card : null;
  }

  static bool _isCurrentUserMessage(
    MessageModel message,
    String currentUserId,
  ) {
    return currentUserId.isNotEmpty && message.senderId.trim() == currentUserId;
  }
}
