import '../../../data/models/message_model.dart';
import '../message_cards/services/chat_message_card_projection.dart';
import '../models/chat_message_identity.dart';
import '../models/chat_message_list_snapshot.dart';

class ChatMessageListSnapshotBuilder {
  final ChatMessageCardDecodeCache _decodeCache = ChatMessageCardDecodeCache();

  ChatMessageListSnapshot build({
    required List<MessageModel> messages,
    required String? currentUserId,
    required bool Function(String content) isInternalDirectiveMessage,
  }) {
    if (messages.isEmpty) {
      _decodeCache.clear();
      return ChatMessageListSnapshot.empty;
    }

    final cardProjection = ChatMessageCardProjector.project(
      messages,
      currentUserId: currentUserId,
      decodeCache: _decodeCache,
    );
    final previousVisibleBubbleIndexes = List<int>.filled(
      messages.length,
      -1,
      growable: false,
    );
    var previousBubbleIndex = -1;
    for (var index = 0; index < messages.length; index++) {
      final message = messages[index];
      if (isInternalDirectiveMessage(message.content) ||
          cardProjection.hiddenIndexes.contains(index)) {
        continue;
      }
      if (message.msgType == 3) {
        previousBubbleIndex = -1;
        continue;
      }
      previousVisibleBubbleIndexes[index] = previousBubbleIndex;
      previousBubbleIndex = index;
    }

    final messageIndexByKey = <String, int>{};
    final messageByLookupId = <String, MessageModel>{};
    final peerReplyAfterFlags = List<bool>.filled(
      messages.length,
      false,
      growable: false,
    );
    var hasPeerReplyAfter = false;
    for (var i = messages.length - 1; i >= 0; i--) {
      final msg = messages[i];
      messageIndexByKey[ChatMessageIdentity.selectionKey(msg)] = i;
      final msgId = msg.msgId.trim();
      if (msgId.isNotEmpty) {
        messageByLookupId[msgId] = msg;
      }
      final clientMsgId = msg.clientMsgId?.trim() ?? '';
      if (clientMsgId.isNotEmpty) {
        messageByLookupId[clientMsgId] = msg;
      }
      peerReplyAfterFlags[i] = hasPeerReplyAfter;
      if (msg.senderType != 1) {
        hasPeerReplyAfter = true;
      }
    }

    return ChatMessageListSnapshot(
      messages: List<MessageModel>.unmodifiable(messages),
      cardProjection: cardProjection,
      previousVisibleBubbleIndexes: previousVisibleBubbleIndexes,
      messageIndexByKey: messageIndexByKey,
      messageByLookupId: messageByLookupId,
      peerReplyAfterFlags: peerReplyAfterFlags,
    );
  }
}
