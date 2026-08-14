import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

class ChatAgentPairingCardData extends ChatMessageCardData {
  const ChatAgentPairingCardData({
    required this.pairingCode,
    this.instructionText = '',
    this.commandHint = '/grix access pair <code>',
  }) : super(type: ChatMessageCardType.agentPairing);

  final String pairingCode;
  final String instructionText;
  final String commandHint;

  String get displayPairingCode {
    return pairingCode.trim();
  }

  String get displayInstructionText {
    return instructionText.trim();
  }

  String get displayCommandHint {
    return commandHint.trim();
  }

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'pairing_code': pairingCode,
      'instruction_text': instructionText,
      'command_hint': commandHint,
    };
  }
}
