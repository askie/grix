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
    this.hiddenLeaderByIndex = const <int, int>{},
  });

  final Map<int, ChatMessageCardData> overridesByIndex;
  final Set<int> hiddenIndexes;

  /// Hidden raw index -> the raw index of the leader card it folds into.
  /// Used by window trimming to keep a leader and its hidden followers in
  /// the same trim unit even when they are not adjacent.
  final Map<int, int> hiddenLeaderByIndex;

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
    final hiddenLeaderByIndex = <int, int>{
      ...execProjection.hiddenLeaderByIndex,
      ...agentInteractionProjection.hiddenLeaderByIndex,
      ...toolExecutionGroupProjection.hiddenLeaderByIndex,
    };

    return ChatMessageCardProjection(
      overridesByIndex: overridesByIndex,
      hiddenIndexes: hiddenIndexes,
      hiddenLeaderByIndex: hiddenLeaderByIndex,
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
    return accountVisibility(
      messages,
      currentUserId: currentUserId,
      decodeCache: decodeCache,
    ).unitLengths;
  }

  /// Full visibility accounting: contiguous unit lengths plus the hidden
  /// follower -> leader links, so trim helpers can keep a leader and its
  /// hidden followers on the same side of a cut even when they are not
  /// adjacent (e.g. an exec status card separated from its approval card by
  /// other visible messages).
  static ChatWindowVisibilityAccounting accountVisibility(
    List<MessageModel> messages, {
    String? currentUserId,
    ChatMessageCardDecodeCache? decodeCache,
  }) {
    if (messages.isEmpty) {
      return const ChatWindowVisibilityAccounting(
        unitLengths: <int>[],
        hiddenLeaderByIndex: <int, int>{},
      );
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
    return ChatWindowVisibilityAccounting(
      unitLengths: unitLengths,
      hiddenLeaderByIndex: projection.hiddenLeaderByIndex,
    );
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
  /// atomic: a unit is either kept entirely or dropped entirely. The cut is
  /// then extended forward so a kept leader never loses its hidden followers
  /// (which would flip it back to an unresolved display state). Returns the
  /// total raw length when the window has at most [maxUnits] units.
  static int prefixRawLengthForUnits(
    ChatWindowVisibilityAccounting accounting,
    int maxUnits,
  ) {
    final unitLengths = accounting.unitLengths;
    final unitEnds = _unitEnds(unitLengths);
    final total = unitEnds.isEmpty ? 0 : unitEnds.last;
    if (unitLengths.length <= maxUnits || maxUnits <= 0) {
      return total;
    }
    var keepUnits = maxUnits;
    var stable = false;
    while (!stable) {
      stable = true;
      final keepEnd = unitEnds[keepUnits - 1];
      for (final entry in accounting.hiddenLeaderByIndex.entries) {
        if (entry.key >= keepEnd && entry.value < keepEnd) {
          final hostUnit = _unitIndexOfRaw(unitEnds, entry.key);
          if (hostUnit >= keepUnits) {
            keepUnits = hostUnit + 1;
            stable = false;
          }
        }
      }
    }
    return unitEnds[keepUnits - 1];
  }

  /// Leading raw rows to drop so only the newest [maxUnits] whole visible
  /// units remain (0 when the window has at most [maxUnits] units). The cut
  /// is pulled back so a kept hidden follower never loses its leader (which
  /// would make it reappear as a standalone card).
  static int suffixDropCountForUnits(
    ChatWindowVisibilityAccounting accounting,
    int maxUnits,
  ) {
    final unitLengths = accounting.unitLengths;
    if (unitLengths.length <= maxUnits || maxUnits <= 0) {
      return 0;
    }
    final unitEnds = _unitEnds(unitLengths);
    var dropUnits = unitLengths.length - maxUnits;
    var stable = false;
    while (!stable && dropUnits > 0) {
      stable = true;
      final dropEnd = unitEnds[dropUnits - 1];
      for (final entry in accounting.hiddenLeaderByIndex.entries) {
        if (entry.key >= dropEnd && entry.value < dropEnd) {
          final leaderUnit = _unitIndexOfRaw(unitEnds, entry.value);
          if (leaderUnit < dropUnits) {
            dropUnits = leaderUnit;
            stable = false;
          }
        }
      }
    }
    return dropUnits <= 0 ? 0 : unitEnds[dropUnits - 1];
  }

  /// Exclusive raw end offset of each unit.
  static List<int> _unitEnds(List<int> unitLengths) {
    final ends = List<int>.filled(unitLengths.length, 0, growable: false);
    var acc = 0;
    for (var i = 0; i < unitLengths.length; i++) {
      acc += unitLengths[i];
      ends[i] = acc;
    }
    return ends;
  }

  /// Unit containing [rawIndex] (binary search over exclusive unit ends).
  static int _unitIndexOfRaw(List<int> unitEnds, int rawIndex) {
    var low = 0;
    var high = unitEnds.length;
    while (low < high) {
      final mid = (low + high) >> 1;
      if (unitEnds[mid] > rawIndex) {
        high = mid;
      } else {
        low = mid + 1;
      }
    }
    return low;
  }
}

/// Visible-window accounting snapshot: contiguous rendered-bubble unit
/// lengths (one per visible bubble, summing to the raw row count) plus the
/// hidden follower -> leader links needed for fate-aligned trim cuts.
class ChatWindowVisibilityAccounting {
  const ChatWindowVisibilityAccounting({
    required this.unitLengths,
    required this.hiddenLeaderByIndex,
  });

  final List<int> unitLengths;
  final Map<int, int> hiddenLeaderByIndex;
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
