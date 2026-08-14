import '../../../data/models/message_model.dart';
import '../message_cards/services/chat_message_card_projection.dart';

class ChatMessageListSnapshot {
  const ChatMessageListSnapshot({
    required this.messages,
    required this.cardProjection,
    required this.previousVisibleBubbleIndexes,
    required this.messageIndexByKey,
    required this.messageByLookupId,
    required this.peerReplyAfterFlags,
  });

  final List<MessageModel> messages;
  final ChatMessageCardProjection cardProjection;
  final List<int> previousVisibleBubbleIndexes;
  final Map<String, int> messageIndexByKey;
  final Map<String, MessageModel> messageByLookupId;
  final List<bool> peerReplyAfterFlags;

  static const empty = ChatMessageListSnapshot(
    messages: <MessageModel>[],
    cardProjection: ChatMessageCardProjection.empty,
    previousVisibleBubbleIndexes: <int>[],
    messageIndexByKey: <String, int>{},
    messageByLookupId: <String, MessageModel>{},
    peerReplyAfterFlags: <bool>[],
  );
}
