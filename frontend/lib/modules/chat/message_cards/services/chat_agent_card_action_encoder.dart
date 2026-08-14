import 'dart:convert';

import '../../../../shared/utils/chat_bind_directory_message.dart';
import '../models/chat_agent_open_session_card_data.dart';
import '../models/chat_agent_question_card_data.dart';

class ChatAgentCardActionEncoder {
  const ChatAgentCardActionEncoder._();

  static const String _questionReplyType = 'agent_question_reply';
  static const String _openSessionHost = 'open';
  static const String _openSessionPath = 'session';

  static String buildQuestionQuickReplyAction(
    ChatAgentQuestionCardData card,
    String answer,
  ) {
    final normalizedAnswer = answer.trim();
    if (!card.supportsQuickOptionReplies || normalizedAnswer.isEmpty) {
      throw ArgumentError.value(
        answer,
        'answer',
        'is not a supported quick answer',
      );
    }
    final matched = card.quickReplyOptions.firstWhere(
      (option) => option == normalizedAnswer,
      orElse: () => '',
    );
    if (matched.isEmpty) {
      throw ArgumentError.value(
        answer,
        'answer',
        'is not a supported quick answer',
      );
    }
    final question = card.questions.first;
    if (question.displayFieldKey.isNotEmpty) {
      return _buildQuestionReplyAction(
        requestId: card.displayRequestId,
        response: <String, dynamic>{
          'type': 'map',
          'entries': <Map<String, dynamic>>[
            <String, dynamic>{
              'key': question.displayFieldKey,
              'value': matched,
            },
          ],
        },
      );
    }
    return _buildQuestionReplyAction(
      requestId: card.displayRequestId,
      response: <String, dynamic>{'type': 'single', 'value': matched},
    );
  }

  static String buildQuestionStructuredReplyAction(
    ChatAgentQuestionCardData card,
    Map<int, String> answersByIndex,
  ) {
    if (!card.supportsStructuredReplies) {
      throw StateError('question card has no questions');
    }
    final normalizedAnswers = <int, String>{};
    for (final question in card.questions) {
      final answer = answersByIndex[question.index]?.trim() ?? '';
      if (answer.isEmpty) {
        throw ArgumentError.value(
          answersByIndex,
          'answersByIndex',
          'question ${question.index} answer is required',
        );
      }
      normalizedAnswers[question.index] = answer;
    }
    if (card.questions.length == 1 &&
        card.questions.first.displayFieldKey.isEmpty) {
      return _buildQuestionReplyAction(
        requestId: card.displayRequestId,
        response: <String, dynamic>{
          'type': 'single',
          'value': normalizedAnswers[card.questions.first.index]!,
        },
      );
    }
    final entries = card.questions
        .map((question) {
          return <String, dynamic>{
            'key': question.displayFieldKey.isNotEmpty
                ? question.displayFieldKey
                : '${question.index}',
            'value': normalizedAnswers[question.index]!,
          };
        })
        .toList(growable: false);
    return _buildQuestionReplyAction(
      requestId: card.displayRequestId,
      response: <String, dynamic>{'type': 'map', 'entries': entries},
    );
  }

  static String buildQuestionUrlCompleteAction(ChatAgentQuestionCardData card) {
    if (!card.isUrlMode) {
      throw StateError('question card has no url completion action');
    }
    return _buildQuestionReplyAction(
      requestId: card.displayRequestId,
      action: 'accept',
    );
  }

  static String buildQuestionUrlCancelAction(ChatAgentQuestionCardData card) {
    if (!card.isUrlMode) {
      throw StateError('question card has no url cancel action');
    }
    return _buildQuestionReplyAction(
      requestId: card.displayRequestId,
      action: 'cancel',
    );
  }

  static String buildOpenSessionAction(
    ChatAgentOpenSessionCardData card,
    String cwd,
  ) {
    return buildOpenSessionUri(cwd, cardInstanceId: card.displayCardInstanceId);
  }

  /// 生成目录绑定消息 URI。`card_instance_id` 可选：
  /// 空白页快捷绑定组件没有卡片，仅带 cwd 也能被后端处理。
  static String buildOpenSessionUri(String cwd, {String cardInstanceId = ''}) {
    final normalizedCwd = cwd.trim();
    if (normalizedCwd.isEmpty) {
      throw ArgumentError.value(cwd, 'cwd', 'must not be empty');
    }
    final queryParameters = <String, String>{'cwd': normalizedCwd};
    final normalizedCardInstanceId = cardInstanceId.trim();
    if (normalizedCardInstanceId.isNotEmpty) {
      queryParameters['card_instance_id'] = normalizedCardInstanceId;
    }
    return Uri(
      scheme: 'grix',
      host: _openSessionHost,
      pathSegments: const <String>[_openSessionPath],
      queryParameters: queryParameters,
    ).toString();
  }

  /// 从目录绑定消息 URI 中解出 cwd；不是绑定 URI 或缺 cwd 时返回空串。
  static String tryParseOpenSessionCwd(String raw) {
    return ChatBindDirectoryMessage.tryParseCwd(raw);
  }

  static String _buildQuestionReplyAction({
    required String requestId,
    Map<String, dynamic>? response,
    String action = '',
  }) {
    final normalizedRequestId = requestId.trim();
    if (normalizedRequestId.isEmpty) {
      throw ArgumentError.value(requestId, 'requestId', 'must not be empty');
    }
    final normalizedAction = action.trim();
    if (normalizedAction.isEmpty && response == null) {
      throw ArgumentError('response or action required');
    }
    final payload = <String, dynamic>{'request_id': normalizedRequestId};
    if (normalizedAction.isNotEmpty) {
      payload['action'] = normalizedAction;
    }
    if (response != null) {
      payload['response'] = response;
    }
    return Uri(
      scheme: 'grix',
      host: 'card',
      pathSegments: const <String>[_questionReplyType],
      queryParameters: <String, String>{'d': jsonEncode(payload)},
    ).toString();
  }
}
