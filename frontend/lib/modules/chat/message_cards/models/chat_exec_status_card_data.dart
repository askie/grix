import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

class ChatExecStatusCardData extends ChatMessageCardData {
  const ChatExecStatusCardData({
    required this.status,
    required this.summary,
    this.detailText = '',
    this.approvalId = '',
    this.approvalCommandId = '',
    this.host = '',
    this.nodeId = '',
    this.sessionId = '',
    this.reason = '',
    this.decision = '',
    this.resolvedById = '',
    this.command = '',
    this.exitLabel = '',
    this.channelLabel = '',
    this.warningText = '',
  }) : super(type: ChatMessageCardType.execStatus);

  final String status;
  final String summary;
  final String detailText;
  final String approvalId;
  final String approvalCommandId;
  final String host;
  final String nodeId;
  final String sessionId;
  final String reason;
  final String decision;
  final String resolvedById;
  final String command;
  final String exitLabel;
  final String channelLabel;
  final String warningText;

  String get displayStatus {
    return status.trim();
  }

  String get displaySummary {
    return summary.trim();
  }

  String get displayDetailText {
    return detailText.trim();
  }

  String get displayApprovalId {
    return approvalId.trim();
  }

  String get displayApprovalCommandId {
    return approvalCommandId.trim();
  }

  String get displayHost {
    return host.trim();
  }

  String get displayNodeId {
    return nodeId.trim();
  }

  String get displaySessionId {
    return sessionId.trim();
  }

  String get displayReason {
    return reason.trim();
  }

  String get displayDecision {
    return decision.trim();
  }

  String get displayResolvedById {
    return resolvedById.trim();
  }

  String get displayCommand {
    return command.trim();
  }

  String get displayExitLabel {
    return exitLabel.trim();
  }

  String get displayChannelLabel {
    return channelLabel.trim();
  }

  String get displayWarningText {
    return warningText.trim();
  }

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'status': status,
      'summary': summary,
      'detail_text': detailText,
      'approval_id': approvalId,
      'approval_command_id': approvalCommandId,
      'host': host,
      'node_id': nodeId,
      'session_id': sessionId,
      'reason': reason,
      'decision': decision,
      'resolved_by_id': resolvedById,
      'command': command,
      'exit_label': exitLabel,
      'channel_label': channelLabel,
      'warning_text': warningText,
    };
  }
}
