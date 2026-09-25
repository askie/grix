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

  /// Decode-free check mirroring the grouping predicate above, for hot window
  /// accounting paths (resident-cap trimming, first-screen auto-fill) where
  /// decoding every row would be too expensive. A false positive only makes
  /// the accounting keep a few extra rows — never drops visible content.
  static bool isToolExecutionCardContent(String content) {
    return content.contains('grix://card/tool_execution?') ||
        content.contains('grix://card/tool_execution)');
  }

  /// Collapse-aware rendered-bubble lengths: a maximal run of >=2 consecutive
  /// same-sender tool-execution cards renders as a single group bubble, so it
  /// contributes one unit whose raw length is the run length; every other
  /// message contributes one unit of raw length 1.
  static List<int> visibleUnitLengths(List<MessageModel> messages) {
    final lengths = <int>[];
    var index = 0;
    while (index < messages.length) {
      final message = messages[index];
      if (!isToolExecutionCardContent(message.content)) {
        lengths.add(1);
        index++;
        continue;
      }
      final senderId = message.senderId.trim();
      var end = index + 1;
      while (end < messages.length &&
          messages[end].senderId.trim() == senderId &&
          isToolExecutionCardContent(messages[end].content)) {
        end++;
      }
      final runLength = end - index;
      if (runLength >= 2) {
        lengths.add(runLength);
        index = end;
      } else {
        lengths.add(1);
        index++;
      }
    }
    return lengths;
  }

  /// Number of rendered bubbles [messages] collapse into.
  static int visibleBubbleCount(List<MessageModel> messages) {
    return visibleUnitLengths(messages).length;
  }

  /// Raw length of the oldest [maxUnits] whole visible units. A collapsed
  /// group is never split: it is either kept entirely or dropped entirely.
  static int prefixRawLengthForUnits(
    List<MessageModel> messages,
    int maxUnits,
  ) {
    final lengths = visibleUnitLengths(messages);
    if (lengths.length <= maxUnits) return messages.length;
    var sum = 0;
    for (var i = 0; i < maxUnits; i++) {
      sum += lengths[i];
    }
    return sum;
  }

  /// Start index of the newest [maxUnits] whole visible units (0 when the
  /// window has at most [maxUnits] units). A collapsed group is never split.
  static int suffixRawStartForUnits(List<MessageModel> messages, int maxUnits) {
    final lengths = visibleUnitLengths(messages);
    if (lengths.length <= maxUnits) return 0;
    var sum = 0;
    for (var i = lengths.length - maxUnits; i < lengths.length; i++) {
      sum += lengths[i];
    }
    return messages.length - sum;
  }
}
