import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

class ChatToolExecutionCardData extends ChatMessageCardData {
  const ChatToolExecutionCardData({
    required this.summaryText,
    this.detailText = '',
  }) : super(type: ChatMessageCardType.toolExecution);

  final String summaryText;
  final String detailText;

  String get displaySummaryText => summaryText.trim();

  String get displayDetailText => detailText.trim();

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'summary_text': summaryText,
      'detail_text': detailText,
    };
  }
}
