import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';
import 'chat_exec_status_card_data.dart';

class ChatExecApprovalCardData extends ChatMessageCardData {
  const ChatExecApprovalCardData({
    required this.approvalId,
    required this.approvalSlug,
    required this.approvalCommandId,
    required this.command,
    required this.host,
    required this.allowedDecisions,
    this.decisionCommands = const <String, String>{},
    this.nodeId = '',
    this.cwd = '',
    this.warningText = '',
    this.expiresInSeconds,
    this.expiresAtMs,
    this.resolutionStatus,
    this.executionStatus,
  }) : super(type: ChatMessageCardType.execApproval);

  final String approvalId;
  final String approvalSlug;
  final String approvalCommandId;
  final String command;
  final String host;
  final String nodeId;
  final String cwd;
  final String warningText;
  final int? expiresInSeconds;
  final int? expiresAtMs;
  final List<String> allowedDecisions;
  final Map<String, String> decisionCommands;
  final ChatExecStatusCardData? resolutionStatus;
  final ChatExecStatusCardData? executionStatus;

  String get displayApprovalCommandId {
    final normalizedCommandId = approvalCommandId.trim();
    if (normalizedCommandId.isNotEmpty) {
      return normalizedCommandId;
    }
    final normalizedSlug = approvalSlug.trim();
    if (normalizedSlug.isNotEmpty) {
      return normalizedSlug;
    }
    return approvalId.trim();
  }

  String get displayCommand {
    return command.trim();
  }

  String get displayHost {
    return host.trim();
  }

  String get displayNodeId {
    return nodeId.trim();
  }

  String get displayCwd {
    return cwd.trim();
  }

  String get displayWarningText {
    return warningText.trim();
  }

  bool get isResolved {
    return resolutionStatus != null;
  }

  bool supportsDecision(String decision) {
    return allowedDecisions.contains(decision);
  }

  ChatExecApprovalCardData copyWithStatuses({
    ChatExecStatusCardData? nextResolutionStatus,
    ChatExecStatusCardData? nextExecutionStatus,
  }) {
    return ChatExecApprovalCardData(
      approvalId: approvalId,
      approvalSlug: approvalSlug,
      approvalCommandId: approvalCommandId,
      command: command,
      host: host,
      allowedDecisions: allowedDecisions,
      decisionCommands: decisionCommands,
      nodeId: nodeId,
      cwd: cwd,
      warningText: warningText,
      expiresInSeconds: expiresInSeconds,
      expiresAtMs: expiresAtMs,
      resolutionStatus: nextResolutionStatus ?? resolutionStatus,
      executionStatus: nextExecutionStatus ?? executionStatus,
    );
  }

  String buildApprovalResolutionDirective(
    String decision, {
    String reason = '',
  }) {
    final normalizedDecision = decision.trim();
    if (!supportsDecision(normalizedDecision)) {
      throw ArgumentError.value(
        decision,
        'decision',
        'is not allowed for this exec approval card',
      );
    }
    final approvalIdValue = approvalId.trim();
    final approvalCommandIdValue = displayApprovalCommandId;
    if (approvalIdValue.isEmpty || approvalCommandIdValue.isEmpty) {
      throw StateError('approval identifiers are empty');
    }
    final segments = <String>[
      'approval_id=${Uri.encodeComponent(approvalIdValue)}',
      'approval_command_id=${Uri.encodeComponent(approvalCommandIdValue)}',
      'decision=${Uri.encodeComponent(normalizedDecision)}',
    ];
    final normalizedReason = reason.trim();
    if (normalizedReason.isNotEmpty) {
      segments.add('reason=${Uri.encodeComponent(normalizedReason)}');
    }
    return '[[exec-approval-resolution|${segments.join('|')}]]';
  }

  ChatExecApprovalCardData copyWithResolvedStatus(
    ChatExecStatusCardData nextResolvedStatus,
  ) {
    return copyWithStatuses(nextResolutionStatus: nextResolvedStatus);
  }

  String buildSubmissionMessage(String decision) {
    final normalizedDecision = decision.trim();
    if (!supportsDecision(normalizedDecision)) {
      throw ArgumentError.value(
        decision,
        'decision',
        'is not allowed for this exec approval card',
      );
    }
    final command = decisionCommands[normalizedDecision]?.trim() ?? '';
    if (command.isNotEmpty) {
      return command;
    }
    return buildApprovalResolutionDirective(normalizedDecision);
  }

  String buildApprovalCommand(String decision) {
    final normalizedDecision = decision.trim();
    if (!supportsDecision(normalizedDecision)) {
      throw ArgumentError.value(
        decision,
        'decision',
        'is not allowed for this exec approval card',
      );
    }
    final commandId = displayApprovalCommandId;
    if (commandId.isEmpty) {
      throw StateError('approval command id is empty');
    }
    return '/approve $commandId $normalizedDecision';
  }

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{
      'approval_id': approvalId,
      'approval_slug': approvalSlug,
      'approval_command_id': approvalCommandId,
      'command': command,
      'host': host,
      'node_id': nodeId,
      'cwd': cwd,
      'warning_text': warningText,
      'expires_in_seconds': expiresInSeconds,
      'expires_at_ms': expiresAtMs,
      'allowed_decisions': allowedDecisions,
      'decision_commands': decisionCommands,
    };
  }
}
