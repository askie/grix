import '../../../data/models/message_model.dart';
import '../message_cards/services/chat_message_card_projection.dart';

class ChatMessageListSnapshot {
  const ChatMessageListSnapshot({
    required this.messages,
    required this.cardProjection,
    required this.previousVisibleBubbleIndexes,
    required this.visibleMessageIndexes,
    required this.visiblePositionByKey,
    required this.messageIndexByKey,
    required this.messageByLookupId,
    required this.peerReplyAfterFlags,
  });

  final List<MessageModel> messages;
  final ChatMessageCardProjection cardProjection;
  final List<int> previousVisibleBubbleIndexes;

  /// Raw message indexes that render a real list item, in order. The list
  /// delegate iterates this instead of the raw window so collapsed
  /// tool-execution rows and internal directives never materialize as
  /// zero-height children.
  final List<int> visibleMessageIndexes;

  /// Selection key -> position within [visibleMessageIndexes], for
  /// `findChildIndexCallback` key-based child lookup.
  final Map<String, int> visiblePositionByKey;

  final Map<String, int> messageIndexByKey;
  final Map<String, MessageModel> messageByLookupId;
  final List<bool> peerReplyAfterFlags;

  static const empty = ChatMessageListSnapshot(
    messages: <MessageModel>[],
    cardProjection: ChatMessageCardProjection.empty,
    previousVisibleBubbleIndexes: <int>[],
    visibleMessageIndexes: <int>[],
    visiblePositionByKey: <String, int>{},
    messageIndexByKey: <String, int>{},
    messageByLookupId: <String, MessageModel>{},
    peerReplyAfterFlags: <bool>[],
  );
}
