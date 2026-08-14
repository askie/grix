import 'chat_agent_status_card_data.dart';
import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

class ChatAgentOpenSessionCardData extends ChatMessageCardData {
  const ChatAgentOpenSessionCardData({
    this.cardInstanceId = '',
    this.summaryText = '',
    this.detailText = '',
    this.initialCwd = '',
    this.submittedPath = '',
    this.submissionStatus,
  }) : super(type: ChatMessageCardType.agentOpenSession);

  final String cardInstanceId;
  final String summaryText;
  final String detailText;
  final String initialCwd;
  final String submittedPath;
  final ChatAgentStatusCardData? submissionStatus;

  String get displayCardInstanceId => cardInstanceId.trim();
  String get displaySummaryText => summaryText.trim();
  String get displayDetailText => detailText.trim();
  String get displayInitialCwd => initialCwd.trim();
  String get displaySubmittedPath => submittedPath.trim();

  ChatAgentOpenSessionCardData copyWithSubmission({
    required String nextSubmittedPath,
    required ChatAgentStatusCardData? nextSubmissionStatus,
  }) {
    return ChatAgentOpenSessionCardData(
      cardInstanceId: cardInstanceId,
      summaryText: summaryText,
      detailText: detailText,
      initialCwd: initialCwd,
      submittedPath: nextSubmittedPath,
      submissionStatus: nextSubmissionStatus,
    );
  }

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'card_instance_id': cardInstanceId,
      'summary_text': summaryText,
      'detail_text': detailText,
      'initial_cwd': initialCwd,
      'submitted_path': submittedPath,
    };
  }
}
