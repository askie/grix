import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

class ChatAgentStatusCardData extends ChatMessageCardData {
  const ChatAgentStatusCardData({
    required this.category,
    required this.status,
    required this.summary,
    this.detailText = '',
    this.referenceId = '',
    this.cardInstanceId = '',
  }) : super(type: ChatMessageCardType.agentStatus);

  final String category;
  final String status;
  final String summary;
  final String detailText;
  final String referenceId;
  final String cardInstanceId;

  String get displayCategory => category.trim();
  String get displayStatus => status.trim();
  String get displaySummary => summary.trim();
  String get displayDetailText => detailText.trim();
  String get displayReferenceId => referenceId.trim();
  String get displayCardInstanceId => cardInstanceId.trim();

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'category': category,
      'status': status,
      'summary': summary,
      'detail_text': detailText,
      'reference_id': referenceId,
      'card_instance_id': cardInstanceId,
    };
  }
}
