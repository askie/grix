import '../../../../data/models/message_model.dart';
import '../models/chat_exec_approval_card_data.dart';
import '../models/chat_exec_status_card_data.dart';
import '../models/chat_message_card_data.dart';
import 'chat_message_card_codec.dart';

class ChatExecCardProjection {
  const ChatExecCardProjection({
    required this.overridesByIndex,
    required this.hiddenIndexes,
  });

  final Map<int, ChatMessageCardData> overridesByIndex;
  final Set<int> hiddenIndexes;

  static const empty = ChatExecCardProjection(
    overridesByIndex: <int, ChatMessageCardData>{},
    hiddenIndexes: <int>{},
  );
}

class ChatExecCardProjector {
  const ChatExecCardProjector._();

  static ChatExecCardProjection project(
    List<MessageModel> messages, {
    List<ChatMessageCardData?>? decodedCards,
  }) {
    if (messages.isEmpty) {
      return ChatExecCardProjection.empty;
    }

    final resolvedDecodedCards =
        decodedCards ??
        List<ChatMessageCardData?>.generate(messages.length, (index) {
          final message = messages[index];
          return ChatMessageCardCodec.decodeFromMessage(
            content: message.content,
          );
        });

    final pendingIndexByApprovalId = <String, int>{};
    for (var index = 0; index < resolvedDecodedCards.length; index++) {
      final card = resolvedDecodedCards[index];
      if (card is! ChatExecApprovalCardData) {
        continue;
      }
      final approvalId = card.approvalId.trim();
      if (approvalId.isEmpty) {
        continue;
      }
      pendingIndexByApprovalId.putIfAbsent(approvalId, () => index);
    }

    final latestResolutionByPendingIndex = <int, ChatExecStatusCardData>{};
    final latestExecutionByPendingIndex = <int, ChatExecStatusCardData>{};
    final hiddenIndexes = <int>{};
    for (var index = 0; index < resolvedDecodedCards.length; index++) {
      final card = resolvedDecodedCards[index];
      if (card is! ChatExecStatusCardData) {
        continue;
      }
      final approvalId = card.approvalId.trim();
      if (approvalId.isEmpty) {
        continue;
      }
      final pendingIndex = pendingIndexByApprovalId[approvalId];
      if (pendingIndex == null || pendingIndex >= index) {
        continue;
      }
      if (_isResolutionStatus(card.status)) {
        latestResolutionByPendingIndex[pendingIndex] = card;
      } else {
        latestExecutionByPendingIndex[pendingIndex] = card;
      }
      hiddenIndexes.add(index);
    }

    if (latestResolutionByPendingIndex.isEmpty &&
        latestExecutionByPendingIndex.isEmpty) {
      return ChatExecCardProjection.empty;
    }

    final overridesByIndex = <int, ChatMessageCardData>{};
    final pendingIndexes = {
      ...latestResolutionByPendingIndex.keys,
      ...latestExecutionByPendingIndex.keys,
    };
    for (final pendingIndex in pendingIndexes) {
      final pendingCard = resolvedDecodedCards[pendingIndex];
      if (pendingCard is! ChatExecApprovalCardData) {
        continue;
      }
      overridesByIndex[pendingIndex] = pendingCard.copyWithStatuses(
        nextResolutionStatus: latestResolutionByPendingIndex[pendingIndex],
        nextExecutionStatus: latestExecutionByPendingIndex[pendingIndex],
      );
    }

    return ChatExecCardProjection(
      overridesByIndex: overridesByIndex,
      hiddenIndexes: hiddenIndexes,
    );
  }

  static bool _isResolutionStatus(String status) {
    return status == 'approval-expired' ||
        status == 'approval-forwarded' ||
        status == 'approval-unavailable' ||
        status == 'resolved-allow-once' ||
        status == 'resolved-allow-always' ||
        status == 'resolved-allow-rule' ||
        status == 'resolved-deny';
  }
}
