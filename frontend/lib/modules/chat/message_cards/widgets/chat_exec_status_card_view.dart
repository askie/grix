import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../../app/themes/app_theme.dart';
import '../models/chat_exec_status_card_data.dart';
import '../services/chat_agent_card_text_localizer.dart';

class ChatExecStatusCardView extends StatelessWidget {
  const ChatExecStatusCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
  });

  final ChatExecStatusCardData card;
  final bool isMine;
  final double fontScale;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final viewportWidth = MediaQuery.sizeOf(context).width;
    final accentColor = _resolveAccentColor(theme);
    final summaryText = ChatAgentCardTextLocalizer.localize(card.displaySummary);
    final warningText = ChatAgentCardTextLocalizer.localize(
      card.displayWarningText,
    );
    final detailText = ChatAgentCardTextLocalizer.localize(
      card.displayDetailText,
    );
    final commandText = card.displayCommand;

    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.labelMedium?.copyWith(
            fontSize: 11 * fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ) ??
          TextStyle(
            fontSize: 11 * fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ),
    );
    final summaryStyle = AppTheme.applyTextFont(
      theme.textTheme.bodyMedium?.copyWith(
            fontSize: 13 * fontScale,
            color: theme.colorScheme.onSurface,
            height: 1.45,
            fontWeight: FontWeight.w600,
          ) ??
          TextStyle(
            fontSize: 13 * fontScale,
            color: theme.colorScheme.onSurface,
            height: 1.45,
            fontWeight: FontWeight.w600,
          ),
    );
    final detailStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 11 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.76),
            height: 1.45,
          ) ??
          TextStyle(
            fontSize: 11 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.76),
            height: 1.45,
          ),
    );
    final plainDetailStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 11 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.78),
            height: 1.45,
          ) ??
          TextStyle(
            fontSize: 11 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.78),
            height: 1.45,
          ),
    );
    final codeStyle =
        theme.textTheme.bodyMedium?.copyWith(
          fontSize: 12 * fontScale,
          color: theme.colorScheme.onSurface,
          height: 1.45,
          fontFamily: 'monospace',
          fontFamilyFallback: AppTheme.textFontFallbackOrNull,
        ) ??
        TextStyle(
          fontSize: 12 * fontScale,
          color: theme.colorScheme.onSurface,
          height: 1.45,
          fontFamily: 'monospace',
          fontFamilyFallback: AppTheme.textFontFallbackOrNull,
        );
    final warningStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 11 * fontScale,
            color: theme.colorScheme.error,
            fontWeight: FontWeight.w600,
            height: 1.4,
          ) ??
          TextStyle(
            fontSize: 11 * fontScale,
            color: theme.colorScheme.error,
            fontWeight: FontWeight.w600,
            height: 1.4,
          ),
    );

    return Container(
      key: const Key('chat_message_card_exec_status'),
      constraints: BoxConstraints(
        minWidth: 240,
        maxWidth: viewportWidth * 0.8,
      ),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: accentColor.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: accentColor.withValues(alpha: 0.18)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 34,
                height: 34,
                decoration: BoxDecoration(
                  color: accentColor.withValues(alpha: 0.14),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Icon(_resolveStatusIcon(), size: 18, color: accentColor),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      'chat_message_card_exec_status_label'.tr,
                      style: titleStyle,
                    ),
                    const SizedBox(height: 6),
                    _buildStatusBadge(context, accentColor, titleStyle),
                  ],
                ),
              ),
            ],
          ),
          if (warningText.isNotEmpty) ...[
            const SizedBox(height: 10),
            _buildInfoBox(
              context,
              title: 'chat_message_card_exec_status_warning'.tr,
              body: warningText,
              bodyStyle: warningStyle,
              borderColor: theme.colorScheme.error.withValues(alpha: 0.18),
              backgroundColor: theme.colorScheme.error.withValues(alpha: 0.08),
            ),
          ],
          if (summaryText.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(summaryText, style: summaryStyle),
          ],
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 6,
            children: _buildMetaChips(context, detailStyle),
          ),
          if (commandText.isNotEmpty) ...[
            const SizedBox(height: 10),
            _buildInfoBox(
              context,
              title: 'chat_message_card_exec_status_command'.tr,
              body: commandText,
              bodyStyle: codeStyle,
            ),
          ],
          if (detailText.isNotEmpty) ...[
            const SizedBox(height: 10),
            _buildInfoBox(
              context,
              title: 'chat_message_card_exec_status_details'.tr,
              body: detailText,
              bodyStyle: _shouldUseCodeStyleForDetails()
                  ? codeStyle
                  : plainDetailStyle,
            ),
          ],
        ],
      ),
    );
  }

  Color _resolveAccentColor(ThemeData theme) {
    switch (card.displayStatus) {
      case 'approval-expired':
        return theme.colorScheme.error;
      case 'approval-forwarded':
        return theme.colorScheme.primary;
      case 'approval-unavailable':
        return AppTheme.statusWarningColor(theme.brightness);
      case 'resolved-allow-once':
      case 'resolved-allow-always':
      case 'resolved-allow-rule':
        return AppTheme.statusSuccessColor(theme.brightness);
      case 'resolved-deny':
        return theme.colorScheme.error;
      case 'running':
        return AppTheme.statusWarningColor(theme.brightness);
      case 'finished':
        return AppTheme.statusSuccessColor(theme.brightness);
      case 'denied':
        return theme.colorScheme.error;
      default:
        return isMine ? theme.colorScheme.primary : theme.colorScheme.secondary;
    }
  }

  Widget _buildStatusBadge(
    BuildContext context,
    Color accentColor,
    TextStyle titleStyle,
  ) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      decoration: BoxDecoration(
        color: accentColor.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(_resolveStatusLabel(), style: titleStyle),
    );
  }

  IconData _resolveStatusIcon() {
    switch (card.displayStatus) {
      case 'approval-expired':
        return Icons.timer_off_outlined;
      case 'approval-forwarded':
        return Icons.forward_to_inbox_rounded;
      case 'approval-unavailable':
        return Icons.portable_wifi_off_rounded;
      case 'running':
        return Icons.autorenew_rounded;
      case 'finished':
        return Icons.task_alt_rounded;
      case 'denied':
        return Icons.cancel_outlined;
      default:
        return Icons.info_outline_rounded;
    }
  }

  bool _shouldUseCodeStyleForDetails() {
    return card.displayStatus == 'finished';
  }

  List<Widget> _buildMetaChips(BuildContext context, TextStyle style) {
    final chips = <Widget>[];
    final approvalId = card.displayApprovalId;
    final approvalCommandId = card.displayApprovalCommandId;
    final host = card.displayHost;
    final nodeId = card.displayNodeId;
    final sessionId = card.displaySessionId;
    final reason = card.displayReason;
    final decision = card.displayDecision;
    final resolvedById = card.displayResolvedById;
    final exitLabel = card.displayExitLabel;
    final channelLabel = card.displayChannelLabel;

    if (approvalId.isNotEmpty) {
      chips.add(
        _buildMetaChip(
          context,
          label:
              '${'chat_message_card_exec_status_approval_id'.tr}: $approvalId',
          style: style,
        ),
      );
    }
    if (host.isNotEmpty) {
      chips.add(
        _buildMetaChip(
          context,
          label: '${'chat_message_card_exec_approval_host'.tr}: $host',
          style: style,
        ),
      );
    }
    if (approvalCommandId.isNotEmpty && approvalCommandId != approvalId) {
      chips.add(
        _buildMetaChip(
          context,
          label:
              '${'chat_message_card_exec_status_command'.tr} ID: $approvalCommandId',
          style: style,
        ),
      );
    }
    if (nodeId.isNotEmpty) {
      chips.add(
        _buildMetaChip(
          context,
          label: '${'chat_message_card_exec_approval_node'.tr}: $nodeId',
          style: style,
        ),
      );
    }
    if (sessionId.isNotEmpty) {
      chips.add(
        _buildMetaChip(
          context,
          label: '${'chat_message_card_exec_status_session'.tr}: $sessionId',
          style: style,
        ),
      );
    }
    if (exitLabel.isNotEmpty) {
      chips.add(
        _buildMetaChip(
          context,
          label: '${'chat_message_card_exec_status_exit'.tr}: $exitLabel',
          style: style,
        ),
      );
    }
    if (reason.isNotEmpty) {
      chips.add(
        _buildMetaChip(
          context,
          label: '${'chat_message_card_exec_status_reason'.tr}: $reason',
          style: style,
        ),
      );
    }
    if (decision.isNotEmpty) {
      chips.add(
        _buildMetaChip(
          context,
          label:
              '${'chat_message_card_exec_status_decision'.tr}: ${_resolveDecisionLabel(decision)}',
          style: style,
        ),
      );
    }
    if (resolvedById.isNotEmpty) {
      chips.add(
        _buildMetaChip(
          context,
          label:
              '${'chat_message_card_exec_status_resolved_by'.tr}: $resolvedById',
          style: style,
        ),
      );
    }
    if (channelLabel.isNotEmpty) {
      chips.add(
        _buildMetaChip(
          context,
          label: '${'chat_message_card_exec_status_channel'.tr}: $channelLabel',
          style: style,
        ),
      );
    }
    return chips;
  }

  Widget _buildInfoBox(
    BuildContext context, {
    required String title,
    required String body,
    required TextStyle bodyStyle,
    Color? backgroundColor,
    Color? borderColor,
  }) {
    final theme = Theme.of(context);
    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.labelSmall?.copyWith(
            fontSize: 10 * fontScale,
            fontWeight: FontWeight.w700,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.72),
          ) ??
          TextStyle(
            fontSize: 10 * fontScale,
            fontWeight: FontWeight.w700,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.72),
          ),
    );
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: backgroundColor ?? theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(
          color:
              borderColor ?? theme.colorScheme.outline.withValues(alpha: 0.12),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(title, style: titleStyle),
          const SizedBox(height: 6),
          SelectionArea(child: Text(body, style: bodyStyle)),
        ],
      ),
    );
  }

  Widget _buildMetaChip(
    BuildContext context, {
    required String label,
    required TextStyle style,
  }) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface.withValues(alpha: 0.92),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(
          color: theme.colorScheme.outline.withValues(alpha: 0.1),
        ),
      ),
      child: Text(label, style: style),
    );
  }

  String _resolveStatusLabel() {
    switch (card.displayStatus) {
      case 'approval-expired':
        return 'chat_message_card_exec_status_expired'.tr;
      case 'approval-forwarded':
        return 'chat_message_card_exec_status_forwarded'.tr;
      case 'approval-unavailable':
        return 'chat_message_card_exec_status_unavailable'.tr;
      case 'resolved-allow-once':
        return 'chat_message_card_exec_status_resolved_allow_once'.tr;
      case 'resolved-allow-always':
        return 'chat_message_card_exec_status_resolved_allow_always'.tr;
      case 'resolved-allow-rule':
        return 'chat_message_card_exec_status_resolved_allow_rule'.tr;
      case 'resolved-deny':
        return 'chat_message_card_exec_status_resolved_deny'.tr;
      case 'running':
        return 'chat_message_card_exec_status_running'.tr;
      case 'finished':
        return 'chat_message_card_exec_status_finished'.tr;
      case 'denied':
        return 'chat_message_card_exec_status_denied'.tr;
      default:
        return card.displayStatus;
    }
  }

  String _resolveDecisionLabel(String decision) {
    switch (decision) {
      case 'allow':
        return 'chat_message_card_exec_approval_allow'.tr;
      case 'allow-once':
        return 'chat_message_card_exec_status_resolved_allow_once'.tr;
      case 'allow-always':
        return 'chat_message_card_exec_status_resolved_allow_always'.tr;
      case 'allow-rule':
        return 'chat_message_card_exec_status_resolved_allow_rule'.tr;
      case 'deny':
        return 'chat_message_card_exec_status_resolved_deny'.tr;
      default:
        return decision;
    }
  }
}
