import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

class ChatEggInstallStatusCardData extends ChatMessageCardData {
  const ChatEggInstallStatusCardData({
    required this.installId,
    required this.status,
    required this.summary,
    this.step = '',
    this.detailText = '',
    this.targetAgentId = '',
    this.errorCode = '',
    this.errorMsg = '',
  }) : super(type: ChatMessageCardType.eggInstallStatus);

  final String installId;
  final String status;
  final String summary;
  final String step;
  final String detailText;
  final String targetAgentId;
  final String errorCode;
  final String errorMsg;

  String get displayInstallId => installId.trim();

  String get displayStatus => status.trim();

  String get displaySummary => summary.trim();

  String get displayStep => step.trim();

  String get displayDetailText => detailText.trim();

  String get displayTargetAgentId => targetAgentId.trim();

  String get displayErrorCode => errorCode.trim();

  String get displayErrorMsg => errorMsg.trim();

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'install_id': installId,
      'status': status,
      'summary': summary,
      'step': step,
      'detail_text': detailText,
      'target_agent_id': targetAgentId,
      'error_code': errorCode,
      'error_msg': errorMsg,
    };
  }
}
