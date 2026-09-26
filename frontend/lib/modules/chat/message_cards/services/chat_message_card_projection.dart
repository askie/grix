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

  /// Rendered-bubble unit lengths over [messages], matching exactly what the
  /// chat list paints: internal directives and projector-hidden rows are
  /// zero-height and merge into the adjacent visible unit (a collapsed
  /// tool-execution group therefore stays one atomic unit and trim
  /// boundaries never split it). Returns one entry per visible bubble; the
  /// sum of all entries always equals `messages.length`.
  ///
  /// Pass a persistent [decodeCache] on hot paths (resident-cap trimming,
  /// first-screen auto-fill): only new or changed rows are decoded, the rest
  /// are cache hits.
  static List<int> visibleUnitLengths(
    List<MessageModel> messages, {
    String? currentUserId,
    ChatMessageCardDecodeCache? decodeCache,
  }) {
    if (messages.isEmpty) {
      return const <int>[];
    }
    final projection = project(
      messages,
      currentUserId: currentUserId,
      decodeCache: decodeCache,
    );
    final hiddenIndexes = projection.hiddenIndexes;
    final unitLengths = <int>[];
    var leadingHidden = 0;
    for (var index = 0; index < messages.length; index++) {
      if (hiddenIndexes.contains(index) ||
          ChatMessageCardCodec.isInternalDirectiveMessage(
            messages[index].content,
          )) {
        // Hidden rows merge into the preceding visible unit so a collapsed
        // group (leader + hidden members) stays one atomic unit. Hidden
        // rows before the first visible bubble attach forward instead.
        if (unitLengths.isEmpty) {
          leadingHidden++;
        } else {
          unitLengths[unitLengths.length - 1]++;
        }
        continue;
      }
      unitLengths.add(1);
    }
    if (leadingHidden > 0) {
      if (unitLengths.isEmpty) {
        // A fully zero-height window still needs one accounting unit so
        // prefix/suffix math keeps the rows together.
        unitLengths.add(leadingHidden);
      } else {
        unitLengths[0] += leadingHidden;
      }
    }
    return unitLengths;
  }

  /// Number of rendered bubbles [messages] collapse into.
  static int visibleBubbleCount(
    List<MessageModel> messages, {
    String? currentUserId,
    ChatMessageCardDecodeCache? decodeCache,
  }) {
    return visibleUnitLengths(
      messages,
      currentUserId: currentUserId,
      decodeCache: decodeCache,
    ).length;
  }

  /// Raw length of the oldest [maxUnits] whole visible units. Units are
  /// atomic: a unit is either kept entirely or dropped entirely. Returns the
  /// total raw length when the window has at most [maxUnits] units.
  static int prefixRawLengthForUnits(List<int> unitLengths, int maxUnits) {
    var total = 0;
    for (final length in unitLengths) {
      total += length;
    }
    if (unitLengths.length <= maxUnits) {
      return total;
    }
    var sum = 0;
    for (var i = 0; i < maxUnits; i++) {
      sum += unitLengths[i];
    }
    return sum;
  }

  /// Leading raw rows to drop so only the newest [maxUnits] whole visible
  /// units remain (0 when the window has at most [maxUnits] units).
  static int suffixDropCountForUnits(List<int> unitLengths, int maxUnits) {
    if (unitLengths.length <= maxUnits) {
      return 0;
    }
    var total = 0;
    for (final length in unitLengths) {
      total += length;
    }
    var keep = 0;
    for (
      var i = unitLengths.length - maxUnits;
      i < unitLengths.length;
      i++
    ) {
      keep += unitLengths[i];
    }
    return total - keep;
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
