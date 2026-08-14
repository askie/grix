import '../../../../data/models/message_model.dart';
import '../../models/chat_message_identity.dart';
import '../models/chat_message_card_data.dart';
import 'chat_agent_interaction_card_projection.dart';
import 'chat_exec_card_projection.dart';
import 'chat_message_card_codec.dart';
import 'chat_tool_execution_group_card_projection.dart';

class ChatMessageCardProjection {
  const ChatMessageCardProjection({
    required this.overridesByIndex,
    required this.hiddenIndexes,
  });

  final Map<int, ChatMessageCardData> overridesByIndex;
  final Set<int> hiddenIndexes;

  static const empty = ChatMessageCardProjection(
    overridesByIndex: <int, ChatMessageCardData>{},
    hiddenIndexes: <int>{},
  );
}

class ChatMessageCardProjector {
  const ChatMessageCardProjector._();

  static ChatMessageCardProjection project(
    List<MessageModel> messages, {
    String? currentUserId,
    ChatMessageCardDecodeCache? decodeCache,
  }) {
    if (messages.isEmpty) {
      return ChatMessageCardProjection.empty;
    }

    final decodedCards = decodeCache != null
        ? decodeCache.resolveDecodedCards(messages)
        : List<ChatMessageCardData?>.generate(messages.length, (index) {
            final message = messages[index];
            return ChatMessageCardCodec.decodeFromMessage(
              content: message.content,
            );
          });

    final execProjection = ChatExecCardProjector.project(
      messages,
      decodedCards: decodedCards,
    );
    final agentInteractionProjection =
        ChatAgentInteractionCardProjector.project(
          messages,
          decodedCards: decodedCards,
        );
    final toolExecutionGroupProjection =
        ChatToolExecutionGroupProjector.project(
          messages,
          currentUserId: currentUserId,
          decodedCards: decodedCards,
        );

    if (execProjection.hiddenIndexes.isEmpty &&
        execProjection.overridesByIndex.isEmpty &&
        agentInteractionProjection.hiddenIndexes.isEmpty &&
        agentInteractionProjection.overridesByIndex.isEmpty &&
        toolExecutionGroupProjection.hiddenIndexes.isEmpty &&
        toolExecutionGroupProjection.overridesByIndex.isEmpty) {
      return ChatMessageCardProjection.empty;
    }

    final overridesByIndex = <int, ChatMessageCardData>{};
    overridesByIndex.addAll(execProjection.overridesByIndex);
    overridesByIndex.addAll(agentInteractionProjection.overridesByIndex);
    overridesByIndex.addAll(toolExecutionGroupProjection.overridesByIndex);
    final hiddenIndexes = <int>{
      ...execProjection.hiddenIndexes,
      ...agentInteractionProjection.hiddenIndexes,
      ...toolExecutionGroupProjection.hiddenIndexes,
    };

    return ChatMessageCardProjection(
      overridesByIndex: overridesByIndex,
      hiddenIndexes: hiddenIndexes,
    );
  }
}

class ChatMessageCardDecodeCache {
  final Map<String, _ChatMessageCardDecodeCacheEntry> _entries =
      <String, _ChatMessageCardDecodeCacheEntry>{};

  List<ChatMessageCardData?> resolveDecodedCards(List<MessageModel> messages) {
    if (messages.isEmpty) {
      _entries.clear();
      return const <ChatMessageCardData?>[];
    }

    final activeKeys = <String>{};
    final decodedCards = List<ChatMessageCardData?>.filled(
      messages.length,
      null,
      growable: false,
    );
    for (var index = 0; index < messages.length; index++) {
      final message = messages[index];
      final cacheKey = ChatMessageIdentity.selectionKey(message);
      activeKeys.add(cacheKey);
      final content = message.content;
      final cached = _entries[cacheKey];
      if (cached != null && cached.content == content) {
        decodedCards[index] = cached.card;
        continue;
      }
      final card = ChatMessageCardCodec.decodeFromMessage(content: content);
      _entries[cacheKey] = _ChatMessageCardDecodeCacheEntry(
        content: content,
        card: card,
      );
      decodedCards[index] = card;
    }
    _entries.removeWhere((key, _) => !activeKeys.contains(key));
    return decodedCards;
  }

  void clear() {
    _entries.clear();
  }
}

class _ChatMessageCardDecodeCacheEntry {
  const _ChatMessageCardDecodeCacheEntry({required this.content, this.card});

  final String content;
  final ChatMessageCardData? card;
}
