import 'dart:convert';

import '../../../../data/models/message_model.dart';
import '../models/chat_agent_open_session_card_data.dart';
import '../models/chat_agent_question_card_data.dart';
import '../models/chat_agent_status_card_data.dart';
import '../models/chat_message_card_data.dart';
import 'chat_message_card_codec.dart';

class ChatAgentInteractionCardProjection {
  const ChatAgentInteractionCardProjection({
    required this.overridesByIndex,
    required this.hiddenIndexes,
  });

  final Map<int, ChatMessageCardData> overridesByIndex;
  final Set<int> hiddenIndexes;

  static const empty = ChatAgentInteractionCardProjection(
    overridesByIndex: <int, ChatMessageCardData>{},
    hiddenIndexes: <int>{},
  );
}

class _ParsedQuestionSubmission {
  const _ParsedQuestionSubmission({
    required this.requestId,
    this.action = '',
    this.singleAnswer = '',
    this.entriesByKey = const <String, String>{},
  });

  final String requestId;
  final String action;
  final String singleAnswer;
  final Map<String, String> entriesByKey;
}

class _ParsedOpenSessionSubmission {
  const _ParsedOpenSessionSubmission({
    required this.cwd,
    this.cardInstanceId = '',
  });

  final String cwd;
  final String cardInstanceId;
}

class ChatAgentInteractionCardProjector {
  const ChatAgentInteractionCardProjector._();

  static ChatAgentInteractionCardProjection project(
    List<MessageModel> messages, {
    List<ChatMessageCardData?>? decodedCards,
  }) {
    if (messages.isEmpty) {
      return ChatAgentInteractionCardProjection.empty;
    }

    final resolvedDecodedCards =
        decodedCards ??
        List<ChatMessageCardData?>.generate(messages.length, (index) {
          final message = messages[index];
          return ChatMessageCardCodec.decodeFromMessage(
            content: message.content,
          );
        });

    final openSessionProjection = _projectOpenSessionInteractions(
      messages,
      resolvedDecodedCards,
    );
    final questionProjection = _projectQuestionInteractions(
      messages,
      resolvedDecodedCards,
    );

    if (openSessionProjection.overridesByIndex.isEmpty &&
        openSessionProjection.hiddenIndexes.isEmpty &&
        questionProjection.overridesByIndex.isEmpty &&
        questionProjection.hiddenIndexes.isEmpty) {
      return ChatAgentInteractionCardProjection.empty;
    }

    return ChatAgentInteractionCardProjection(
      overridesByIndex: <int, ChatMessageCardData>{
        ...openSessionProjection.overridesByIndex,
        ...questionProjection.overridesByIndex,
      },
      hiddenIndexes: <int>{
        ...openSessionProjection.hiddenIndexes,
        ...questionProjection.hiddenIndexes,
      },
    );
  }

  static ChatAgentInteractionCardProjection _projectOpenSessionInteractions(
    List<MessageModel> messages,
    List<ChatMessageCardData?> decodedCards,
  ) {
    final submittedPathByOpenSessionIndex = <int, String>{};
    final latestStatusByOpenSessionIndex = <int, ChatAgentStatusCardData>{};
    final hiddenIndexes = <int>{};
    final latestOpenSessionIndexByCardInstanceId = <String, int>{};
    var latestOpenSessionIndex = -1;
    var activeOpenSessionIndex = -1;

    for (var index = 0; index < decodedCards.length; index++) {
      final card = decodedCards[index];
      if (card is ChatAgentOpenSessionCardData) {
        latestOpenSessionIndex = index;
        final cardInstanceId = card.displayCardInstanceId;
        if (cardInstanceId.isNotEmpty) {
          latestOpenSessionIndexByCardInstanceId[cardInstanceId] = index;
        }
        final submittedPath = card.displaySubmittedPath;
        if (submittedPath.isNotEmpty) {
          submittedPathByOpenSessionIndex[index] = submittedPath;
          activeOpenSessionIndex = index;
        } else {
          activeOpenSessionIndex = -1;
        }
        continue;
      }

      final submitted = _parseOpenSessionSubmission(messages[index]);
      if (submitted != null) {
        final targetIndex = _resolveOpenSessionTargetIndex(
          cardInstanceId: submitted.cardInstanceId,
          latestOpenSessionIndexByCardInstanceId:
              latestOpenSessionIndexByCardInstanceId,
          fallbackIndex: latestOpenSessionIndex,
        );
        if (targetIndex < 0) {
          continue;
        }
        submittedPathByOpenSessionIndex[targetIndex] = submitted.cwd;
        latestStatusByOpenSessionIndex.remove(targetIndex);
        activeOpenSessionIndex = targetIndex;
        continue;
      }

      if (card is! ChatAgentStatusCardData ||
          card.displayCategory != 'session') {
        continue;
      }
      final targetIndex = _resolveOpenSessionTargetIndex(
        cardInstanceId: card.displayCardInstanceId,
        latestOpenSessionIndexByCardInstanceId:
            latestOpenSessionIndexByCardInstanceId,
        fallbackIndex: activeOpenSessionIndex,
      );
      if (targetIndex < 0) {
        continue;
      }
      latestStatusByOpenSessionIndex[targetIndex] = card;
      hiddenIndexes.add(index);
    }

    if (submittedPathByOpenSessionIndex.isEmpty &&
        latestStatusByOpenSessionIndex.isEmpty &&
        hiddenIndexes.isEmpty) {
      return ChatAgentInteractionCardProjection.empty;
    }

    final overridesByIndex = <int, ChatMessageCardData>{};
    final targetIndexes = <int>{
      ...submittedPathByOpenSessionIndex.keys,
      ...latestStatusByOpenSessionIndex.keys,
    };
    for (final targetIndex in targetIndexes) {
      final pendingCard = decodedCards[targetIndex];
      if (pendingCard is! ChatAgentOpenSessionCardData) {
        continue;
      }
      overridesByIndex[targetIndex] = pendingCard.copyWithSubmission(
        nextSubmittedPath:
            submittedPathByOpenSessionIndex[targetIndex] ??
            pendingCard.displaySubmittedPath,
        nextSubmissionStatus: latestStatusByOpenSessionIndex[targetIndex],
      );
    }

    if (overridesByIndex.isEmpty && hiddenIndexes.isEmpty) {
      return ChatAgentInteractionCardProjection.empty;
    }

    return ChatAgentInteractionCardProjection(
      overridesByIndex: overridesByIndex,
      hiddenIndexes: hiddenIndexes,
    );
  }

  static ChatAgentInteractionCardProjection _projectQuestionInteractions(
    List<MessageModel> messages,
    List<ChatMessageCardData?> decodedCards,
  ) {
    final latestQuestionIndexByRequestId = <String, int>{};
    final latestSubmissionByQuestionIndex = <int, _ParsedQuestionSubmission>{};
    final latestStatusByQuestionIndex = <int, ChatAgentStatusCardData>{};
    final hiddenIndexes = <int>{};

    for (var index = 0; index < decodedCards.length; index++) {
      final card = decodedCards[index];
      if (card is ChatAgentQuestionCardData) {
        final requestId = card.displayRequestId;
        if (requestId.isNotEmpty) {
          latestQuestionIndexByRequestId[requestId] = index;
        }
        continue;
      }

      final parsedSubmission = _parseQuestionSubmission(messages[index]);
      if (parsedSubmission != null) {
        final targetIndex =
            latestQuestionIndexByRequestId[parsedSubmission.requestId];
        if (targetIndex == null) {
          continue;
        }
        latestSubmissionByQuestionIndex[targetIndex] = parsedSubmission;
        latestStatusByQuestionIndex.remove(targetIndex);
        continue;
      }

      if (card is! ChatAgentStatusCardData ||
          card.displayCategory != 'question') {
        continue;
      }
      final requestId = card.displayReferenceId;
      if (requestId.isEmpty) {
        continue;
      }
      final targetIndex = latestQuestionIndexByRequestId[requestId];
      if (targetIndex == null) {
        continue;
      }
      latestStatusByQuestionIndex[targetIndex] = card;
      hiddenIndexes.add(index);
    }

    if (latestSubmissionByQuestionIndex.isEmpty &&
        latestStatusByQuestionIndex.isEmpty &&
        hiddenIndexes.isEmpty) {
      return ChatAgentInteractionCardProjection.empty;
    }

    final overridesByIndex = <int, ChatMessageCardData>{};
    final targetIndexes = <int>{
      ...latestSubmissionByQuestionIndex.keys,
      ...latestStatusByQuestionIndex.keys,
    };
    for (final targetIndex in targetIndexes) {
      final questionCard = decodedCards[targetIndex];
      if (questionCard is! ChatAgentQuestionCardData) {
        continue;
      }
      final submission = latestSubmissionByQuestionIndex[targetIndex];
      final resolvedSubmission = _resolveQuestionSubmission(
        card: questionCard,
        submission: submission,
      );
      overridesByIndex[targetIndex] = questionCard.copyWithSubmission(
        nextSubmittedAnswer: resolvedSubmission.$1,
        nextSubmittedAcceptText: resolvedSubmission.$2,
        nextSubmittedCancelText: resolvedSubmission.$3,
        nextSubmissionStatus: latestStatusByQuestionIndex[targetIndex],
      );
    }

    if (overridesByIndex.isEmpty && hiddenIndexes.isEmpty) {
      return ChatAgentInteractionCardProjection.empty;
    }

    return ChatAgentInteractionCardProjection(
      overridesByIndex: overridesByIndex,
      hiddenIndexes: hiddenIndexes,
    );
  }

  static (String, String, String) _resolveQuestionSubmission({
    required ChatAgentQuestionCardData card,
    required _ParsedQuestionSubmission? submission,
  }) {
    if (submission == null) {
      return (
        card.submittedAnswer,
        card.submittedAcceptText,
        card.submittedCancelText,
      );
    }

    if (submission.action == 'accept') {
      return (
        '',
        card.displaySubmittedAcceptText.isNotEmpty
            ? card.displaySubmittedAcceptText
            : 'Done',
        '',
      );
    }
    if (submission.action == 'cancel') {
      return (
        '',
        '',
        card.displaySubmittedCancelText.isNotEmpty
            ? card.displaySubmittedCancelText
            : 'Cancelled',
      );
    }

    if (submission.singleAnswer.isNotEmpty) {
      return (submission.singleAnswer, '', '');
    }

    final answerLines = <String>[];
    for (final question in card.questions) {
      final entryKey = question.displayFieldKey.isNotEmpty
          ? question.displayFieldKey
          : '${question.index}';
      final value = submission.entriesByKey[entryKey]?.trim() ?? '';
      if (value.isEmpty) {
        continue;
      }
      if (card.questions.length == 1) {
        return (value, '', '');
      }
      answerLines.add('${question.displayHeader}: $value');
    }
    if (answerLines.isNotEmpty) {
      return (answerLines.join('\n'), '', '');
    }

    final fallbackValues = submission.entriesByKey.values
        .map((value) => value.trim())
        .where((value) => value.isNotEmpty)
        .toList(growable: false);
    return (fallbackValues.join('\n'), '', '');
  }

  static _ParsedQuestionSubmission? _parseQuestionSubmission(
    MessageModel message,
  ) {
    final uri = Uri.tryParse(message.content.trim());
    if (uri == null || uri.scheme != 'grix' || uri.host != 'card') {
      return null;
    }
    final segments = uri.pathSegments
        .where((segment) => segment.isNotEmpty)
        .toList(growable: false);
    if (segments.length != 1 || segments.first != 'agent_question_reply') {
      return null;
    }
    final rawPayload = uri.queryParameters['d']?.trim() ?? '';
    if (rawPayload.isEmpty) {
      return null;
    }
    dynamic decoded;
    try {
      decoded = jsonDecode(rawPayload);
    } catch (_) {
      return null;
    }
    if (decoded is! Map) {
      return null;
    }
    final payload = Map<String, dynamic>.from(decoded);
    final requestId = payload['request_id']?.toString().trim() ?? '';
    if (requestId.isEmpty) {
      return null;
    }

    final action = payload['action']?.toString().trim() ?? '';
    if (action == 'accept' || action == 'cancel') {
      return _ParsedQuestionSubmission(requestId: requestId, action: action);
    }

    final response = payload['response'];
    if (response is! Map) {
      return null;
    }
    final normalizedResponse = Map<String, dynamic>.from(response);
    final responseType = normalizedResponse['type']?.toString().trim() ?? '';
    if (responseType == 'single') {
      final value = normalizedResponse['value']?.toString().trim() ?? '';
      if (value.isEmpty) {
        return null;
      }
      return _ParsedQuestionSubmission(
        requestId: requestId,
        singleAnswer: value,
      );
    }
    if (responseType != 'map') {
      return null;
    }

    final rawEntries = normalizedResponse['entries'];
    if (rawEntries is! List) {
      return null;
    }
    final entriesByKey = <String, String>{};
    for (final rawEntry in rawEntries) {
      if (rawEntry is! Map) {
        continue;
      }
      final entry = Map<String, dynamic>.from(rawEntry);
      final key = entry['key']?.toString().trim() ?? '';
      final value = entry['value']?.toString().trim() ?? '';
      if (key.isEmpty || value.isEmpty) {
        continue;
      }
      entriesByKey[key] = value;
    }
    if (entriesByKey.isEmpty) {
      return null;
    }
    return _ParsedQuestionSubmission(
      requestId: requestId,
      entriesByKey: entriesByKey,
    );
  }

  static int _resolveOpenSessionTargetIndex({
    required String cardInstanceId,
    required Map<String, int> latestOpenSessionIndexByCardInstanceId,
    required int fallbackIndex,
  }) {
    final normalizedCardInstanceId = cardInstanceId.trim();
    if (normalizedCardInstanceId.isNotEmpty) {
      return latestOpenSessionIndexByCardInstanceId[normalizedCardInstanceId] ??
          -1;
    }
    return fallbackIndex;
  }

  static _ParsedOpenSessionSubmission? _parseOpenSessionSubmission(
    MessageModel message,
  ) {
    final uri = Uri.tryParse(message.content.trim());
    if (uri == null || uri.scheme != 'grix' || uri.host != 'open') {
      return null;
    }
    final segments = uri.pathSegments
        .where((segment) => segment.isNotEmpty)
        .toList(growable: false);
    if (segments.length != 1 || segments.first != 'session') {
      return null;
    }
    final cwd = uri.queryParameters['cwd']?.trim() ?? '';
    if (cwd.isEmpty) {
      return null;
    }
    return _ParsedOpenSessionSubmission(
      cwd: cwd,
      cardInstanceId: uri.queryParameters['card_instance_id']?.trim() ?? '',
    );
  }
}
