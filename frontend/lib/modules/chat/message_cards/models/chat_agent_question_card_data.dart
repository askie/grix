import 'chat_agent_status_card_data.dart';
import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

class ChatAgentQuestionPrompt {
  const ChatAgentQuestionPrompt({
    required this.index,
    required this.header,
    required this.prompt,
    this.fieldKey = '',
    this.options = const <String>[],
    this.multiSelect = false,
  });

  final int index;
  final String header;
  final String prompt;
  final String fieldKey;
  final List<String> options;
  final bool multiSelect;

  String get displayHeader {
    return header.trim();
  }

  String get displayPrompt {
    return prompt.trim();
  }

  String get displayFieldKey {
    return fieldKey.trim();
  }

  List<String> get displayOptions {
    return options
        .map((value) => value.trim())
        .where((value) => value.isNotEmpty)
        .toList(growable: false);
  }

  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'index': index,
      'header': header,
      'prompt': prompt,
      'field_key': fieldKey,
      'options': options,
      'multi_select': multiSelect,
    };
  }
}

class ChatAgentQuestionCardData extends ChatMessageCardData {
  const ChatAgentQuestionCardData({
    required this.requestId,
    required this.questions,
    this.mode = 'form',
    this.message = '',
    this.url = '',
    this.openUrlLabel = '',
    this.footerText = '',
    this.submittedAnswer = '',
    this.submittedAcceptText = '',
    this.submittedCancelText = '',
    this.submissionStatus,
    this.expiresAtMs = 0,
  }) : super(type: ChatMessageCardType.agentQuestion);

  final String requestId;
  final List<ChatAgentQuestionPrompt> questions;
  final String mode;
  final String message;
  final String url;
  final String openUrlLabel;
  final String footerText;
  final String submittedAnswer;
  final String submittedAcceptText;
  final String submittedCancelText;
  final ChatAgentStatusCardData? submissionStatus;

  /// 作答截止时间（epoch 毫秒），0 表示无限制。到期后前端禁用提交并提示改用文字回复。
  final int expiresAtMs;

  String get displayRequestId {
    return requestId.trim();
  }

  String get displayMode {
    final normalized = mode.trim().toLowerCase();
    return normalized.isEmpty ? 'form' : normalized;
  }

  bool get isUrlMode {
    return displayMode == 'url';
  }

  String get displayMessage {
    return message.trim();
  }

  String get displayUrl {
    return url.trim();
  }

  String get displayOpenUrlLabel {
    return openUrlLabel.trim();
  }

  String get displayFooterText {
    return footerText.trim();
  }

  String get displaySubmittedAnswer {
    return submittedAnswer.trim();
  }

  String get displaySubmittedAcceptText {
    return submittedAcceptText.trim();
  }

  String get displaySubmittedCancelText {
    return submittedCancelText.trim();
  }

  ChatAgentQuestionCardData copyWithSubmission({
    required String nextSubmittedAnswer,
    required String nextSubmittedAcceptText,
    required String nextSubmittedCancelText,
    required ChatAgentStatusCardData? nextSubmissionStatus,
  }) {
    return ChatAgentQuestionCardData(
      requestId: requestId,
      questions: questions,
      mode: mode,
      message: message,
      url: url,
      openUrlLabel: openUrlLabel,
      footerText: footerText,
      submittedAnswer: nextSubmittedAnswer,
      submittedAcceptText: nextSubmittedAcceptText,
      submittedCancelText: nextSubmittedCancelText,
      submissionStatus: nextSubmissionStatus,
      expiresAtMs: expiresAtMs,
    );
  }

  bool get hasExpiry {
    return expiresAtMs > 0;
  }

  bool get supportsQuickOptionReplies {
    if (isUrlMode) {
      return false;
    }
    if (questions.length != 1) {
      return false;
    }
    final question = questions.first;
    return !question.multiSelect && question.displayOptions.isNotEmpty;
  }

  List<String> get quickReplyOptions {
    if (!supportsQuickOptionReplies) {
      return const <String>[];
    }
    return questions.first.displayOptions;
  }

  bool get supportsStructuredReplies {
    if (isUrlMode) {
      return false;
    }
    return questions.isNotEmpty;
  }

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'request_id': requestId,
      'questions': questions.map((value) => value.toPayload()).toList(),
      'mode': mode,
      'message': message,
      'url': url,
      'open_url_label': openUrlLabel,
      'footer_text': footerText,
      'submitted_answer': submittedAnswer,
      'submitted_accept_text': submittedAcceptText,
      'submitted_cancel_text': submittedCancelText,
      'expires_at': expiresAtMs,
    };
  }
}
