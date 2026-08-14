import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';
import 'chat_tool_execution_card_data.dart';

class ChatToolExecutionGroupCardData extends ChatMessageCardData {
  const ChatToolExecutionGroupCardData({
    required this.children,
    required this.displayCard,
  }) : super(type: ChatMessageCardType.toolExecutionGroup);

  final List<ChatToolExecutionCardData> children;

  final ChatToolExecutionCardData displayCard;

  int get count => children.length;

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'children': children.map((c) => c.toPayload()).toList(),
    };
  }
}
