import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../../app/themes/app_theme.dart';
import '../../../../shared/widgets/app_dialog_style.dart';
import '../models/chat_exec_approval_card_data.dart';
import '../models/chat_message_card_action.dart';
import '../models/chat_exec_status_card_data.dart';

class ChatExecApprovalCardView extends StatefulWidget {
  const ChatExecApprovalCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
    this.isPending = false,
    this.onDecisionTap,
  });

  final ChatExecApprovalCardData card;
  final bool isMine;
  final double fontScale;
  final bool isPending;
  final Future<ChatMessageCardActionResult> Function(String decision)?
  onDecisionTap;

  @override
  State<ChatExecApprovalCardView> createState() =>
      _ChatExecApprovalCardViewState();
}

class _ChatExecApprovalCardViewState extends State<ChatExecApprovalCardView> {
  Timer? _expiryTimer;
  int? _remainingSeconds;
  bool _isSubmitting = false;
  String _submitError = '';

  ChatExecApprovalCardData get card => widget.card;

  @override
  void initState() {
    super.initState();
    _syncLocalState();
  }

  @override
  void didUpdateWidget(covariant ChatExecApprovalCardView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (_shouldResyncLocalState(oldWidget.card, widget.card)) {
      _syncLocalState();
      return;
    }
    if (oldWidget.isPending != widget.isPending) {
      setState(() {
        _isSubmitting = widget.isPending;
        if (widget.isPending) {
          _submitError = '';
        }
      });
    }
  }

  @override
  void dispose() {
    _expiryTimer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final viewportWidth = MediaQuery.sizeOf(context).width;
    final accentColor = widget.isMine
        ? theme.colorScheme.primary
        : theme.colorScheme.secondary;
    final warningText = card.displayWarningText;
    final hostText = card.displayHost;
    final nodeText = card.displayNodeId;
    final cwdText = card.displayCwd;
    final resolutionStatus = card.resolutionStatus;
    final executionStatus = card.executionStatus;
    final primaryStatus = executionStatus ?? resolutionStatus;
    final isExpired = _isExpired;
    final isTerminal = resolutionStatus != null || executionStatus != null;
    final isSubmitting =
        isTerminal ? false : (_isSubmitting || widget.isPending);
    final isActionDisabled =
        widget.onDecisionTap == null || isSubmitting || isExpired;

    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.labelMedium?.copyWith(
            fontSize: 11 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.88),
            letterSpacing: 0.2,
          ) ??
          TextStyle(
            fontSize: 11 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.88),
            letterSpacing: 0.2,
          ),
    );
    final commandStyle =
        (theme.textTheme.bodyMedium?.copyWith(
          fontSize: 12 * widget.fontScale,
          color: theme.colorScheme.onSurface,
          height: 1.45,
          fontFamily: 'monospace',
          fontFamilyFallback: AppTheme.textFontFallbackOrNull,
        ) ??
        TextStyle(
          fontSize: 12 * widget.fontScale,
          color: theme.colorScheme.onSurface,
          height: 1.45,
          fontFamily: 'monospace',
          fontFamilyFallback: AppTheme.textFontFallbackOrNull,
        ));
    final metaStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 11 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.72),
          ) ??
          TextStyle(
            fontSize: 11 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.72),
          ),
    );
    final warningStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 11 * widget.fontScale,
            color: theme.colorScheme.error,
            fontWeight: FontWeight.w600,
            height: 1.4,
          ) ??
          TextStyle(
            fontSize: 11 * widget.fontScale,
            color: theme.colorScheme.error,
            fontWeight: FontWeight.w600,
            height: 1.4,
          ),
    );
    final hintStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 11 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.68),
            height: 1.4,
          ) ??
          TextStyle(
            fontSize: 11 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.68),
            height: 1.4,
          ),
    );

    return Container(
      key: const Key('chat_message_card_exec_approval'),
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
                child: Icon(
                  Icons.shield_outlined,
                  size: 18,
                  color: accentColor,
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      'chat_message_card_exec_approval_label'.tr,
                      style: titleStyle,
                    ),
                    const SizedBox(height: 4),
                    Text(
                      'chat_message_card_exec_approval_hint'.tr,
                      style: hintStyle,
                    ),
                    if (isSubmitting) ...[
                      const SizedBox(height: 6),
                      _buildStatusBadge(
                        'chat_message_card_exec_approval_submitting'.tr,
                        Colors.orange.shade800,
                        titleStyle,
                      ),
                    ] else if (isExpired) ...[
                      const SizedBox(height: 6),
                      _buildStatusBadge(
                        'chat_message_card_exec_approval_expired'.tr,
                        theme.colorScheme.error,
                        titleStyle,
                      ),
                    ] else if (primaryStatus != null) ...[
                      const SizedBox(height: 6),
                      _buildStatusBadge(
                        _resolveStatusLabel(primaryStatus.displayStatus),
                        _resolveStatusColor(theme, primaryStatus.displayStatus),
                        titleStyle,
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
          if (warningText.isNotEmpty) ...[
            const SizedBox(height: 10),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
              decoration: BoxDecoration(
                color: theme.colorScheme.error.withValues(alpha: 0.08),
                borderRadius: BorderRadius.circular(10),
                border: Border.all(
                  color: theme.colorScheme.error.withValues(alpha: 0.18),
                ),
              ),
              child: Text(warningText, style: warningStyle),
            ),
          ],
          const SizedBox(height: 10),
          Text('chat_message_card_exec_approval_command'.tr, style: titleStyle),
          const SizedBox(height: 6),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(10),
            decoration: BoxDecoration(
              color: theme.colorScheme.surface,
              borderRadius: BorderRadius.circular(10),
              border: Border.all(
                color: theme.colorScheme.outline.withValues(alpha: 0.12),
              ),
            ),
            child: SelectionArea(
              child: Text(card.displayCommand, style: commandStyle),
            ),
          ),
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 6,
            children: [
              _buildMetaChip(
                label:
                    '${'chat_message_card_exec_approval_host'.tr}: $hostText',
                style: metaStyle,
              ),
              if (nodeText.isNotEmpty)
                _buildMetaChip(
                  label:
                      '${'chat_message_card_exec_approval_node'.tr}: $nodeText',
                  style: metaStyle,
                ),
              if (cwdText.isNotEmpty)
                _buildMetaChip(
                  label:
                      '${'chat_message_card_exec_approval_cwd'.tr}: $cwdText',
                  style: metaStyle,
                ),
              if (_remainingSeconds != null)
                _buildMetaChip(
                  label:
                      '${'chat_message_card_exec_approval_expires_in'.tr}: ${_remainingSeconds}s',
                  style: metaStyle,
                ),
            ],
          ),
          const SizedBox(height: 10),
          if (resolutionStatus != null ||
              executionStatus != null ||
              isExpired) ...[
            const SizedBox(height: 12),
            if (resolutionStatus != null)
              _buildResolvedStatusSection(
                context,
                resolvedStatus: resolutionStatus,
                titleStyle: titleStyle,
                hintStyle: hintStyle,
                warningStyle: warningStyle,
              ),
            if (isExpired &&
                resolutionStatus == null &&
                executionStatus == null)
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: theme.colorScheme.error.withValues(alpha: 0.08),
                  borderRadius: BorderRadius.circular(10),
                  border: Border.all(
                    color: theme.colorScheme.error.withValues(alpha: 0.18),
                  ),
                ),
                child: Text(
                  'chat_message_card_exec_approval_expired_hint'.tr,
                  style: warningStyle,
                ),
              ),
            if (executionStatus != null) ...[
              if (resolutionStatus != null) const SizedBox(height: 10),
              _buildExecutionStatusSection(
                context,
                executionStatus: executionStatus,
                titleStyle: titleStyle,
                hintStyle: hintStyle,
              ),
            ],
          ] else ...[
            Text(
              'chat_message_card_exec_approval_recommended'.tr,
              style: hintStyle,
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: _buildDecisionButtons(
                context,
                isActionDisabled: isActionDisabled,
                isSubmitting: isSubmitting,
              ),
            ),
            if (_submitError.isNotEmpty) ...[
              const SizedBox(height: 10),
              Text(
                _submitError,
                key: const Key('chat_message_card_exec_approval_submit_error'),
                style: warningStyle,
              ),
            ],
          ],
        ],
      ),
    );
  }

  Widget _buildResolvedStatusSection(
    BuildContext context, {
    required ChatExecStatusCardData resolvedStatus,
    required TextStyle titleStyle,
    required TextStyle hintStyle,
    required TextStyle warningStyle,
  }) {
    final theme = Theme.of(context);
    final statusColor = _resolveStatusColor(
      theme,
      resolvedStatus.displayStatus,
    );
    final summary = resolvedStatus.displaySummary;
    final detailText = resolvedStatus.displayDetailText;
    final warningText = resolvedStatus.displayWarningText;
    final commandText = resolvedStatus.displayCommand;
    final decisionText = _resolveDecisionLabel(resolvedStatus.displayDecision);
    final resolvedByText = _resolveResolvedByLabel(resolvedStatus);

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: statusColor.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: statusColor.withValues(alpha: 0.18)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text('chat_message_card_exec_approval_result'.tr, style: titleStyle),
          if (decisionText.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(decisionText, style: hintStyle),
          ],
          if (resolvedByText.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text(resolvedByText, style: hintStyle),
          ],
          if (summary.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(summary, style: hintStyle),
          ],
          if (warningText.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(warningText, style: warningStyle),
          ],
          if (commandText.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              '${'chat_message_card_exec_status_command'.tr}: $commandText',
              style: hintStyle,
            ),
          ],
          if (detailText.isNotEmpty) ...[
            const SizedBox(height: 8),
            SelectionArea(child: Text(detailText, style: hintStyle)),
          ],
        ],
      ),
    );
  }

  Widget _buildExecutionStatusSection(
    BuildContext context, {
    required ChatExecStatusCardData executionStatus,
    required TextStyle titleStyle,
    required TextStyle hintStyle,
  }) {
    final theme = Theme.of(context);
    final statusColor = _resolveStatusColor(
      theme,
      executionStatus.displayStatus,
    );
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: statusColor.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: statusColor.withValues(alpha: 0.18)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text('chat_message_card_exec_status_label'.tr, style: titleStyle),
          const SizedBox(height: 6),
          Text(executionStatus.displaySummary, style: hintStyle),
          if (executionStatus.displayDetailText.isNotEmpty) ...[
            const SizedBox(height: 8),
            SelectionArea(
              child: Text(executionStatus.displayDetailText, style: hintStyle),
            ),
          ],
        ],
      ),
    );
  }

  Future<void> _handleDecisionTap(BuildContext context, String decision) async {
    final callback = widget.onDecisionTap;
    if (callback == null || (_isSubmitting || widget.isPending) || _isExpired) {
      return;
    }
    if (!_requiresConfirmation(decision)) {
      await _submitDecision(decision);
      return;
    }

    final isDeny = decision == 'deny';
    final confirmed = await showAppConfirmDialog(
      context: context,
      title: isDeny
          ? 'chat_message_card_exec_approval_confirm_deny_title'.tr
          : 'chat_message_card_exec_approval_confirm_allow_always_title'.tr,
      message: isDeny
          ? 'chat_message_card_exec_approval_confirm_deny_message'.tr
          : 'chat_message_card_exec_approval_confirm_allow_always_message'.tr,
      confirmText: 'chat_message_card_exec_approval_confirm_continue'.tr,
      cancelText: 'chat_message_card_exec_approval_confirm_cancel'.tr,
      isDestructive: isDeny,
    );
    if (confirmed) {
      await _submitDecision(decision);
    }
  }

  List<Widget> _buildDecisionButtons(
    BuildContext context, {
    required bool isActionDisabled,
    required bool isSubmitting,
  }) {
    final theme = Theme.of(context);
    final buttons = <Widget>[];
    for (final decision in card.allowedDecisions) {
      if (decision == 'allow') {
        buttons.add(
          FilledButton.icon(
            key: const Key('chat_message_card_exec_approval_allow'),
            onPressed: isActionDisabled
                ? null
                : () => _handleDecisionTap(context, decision),
            icon: isSubmitting
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.check_circle_outline_rounded, size: 18),
            label: Text('chat_message_card_exec_approval_allow'.tr),
          ),
        );
        continue;
      }
      if (decision == 'allow-once') {
        buttons.add(
          FilledButton.icon(
            key: const Key('chat_message_card_exec_approval_allow_once'),
            onPressed: isActionDisabled
                ? null
                : () => _handleDecisionTap(context, decision),
            icon: isSubmitting
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.check_circle_outline_rounded, size: 18),
            label: Text('chat_message_card_exec_approval_allow_once'.tr),
          ),
        );
        continue;
      }
      if (decision == 'allow-always') {
        buttons.add(
          OutlinedButton.icon(
            key: const Key('chat_message_card_exec_approval_allow_always'),
            onPressed: isActionDisabled
                ? null
                : () => _handleDecisionTap(context, decision),
            icon: const Icon(Icons.all_inclusive_rounded, size: 18),
            label: Text('chat_message_card_exec_approval_allow_always'.tr),
          ),
        );
        continue;
      }
      if (decision == 'deny') {
        buttons.add(
          OutlinedButton.icon(
            key: const Key('chat_message_card_exec_approval_deny'),
            style: OutlinedButton.styleFrom(
              foregroundColor: theme.colorScheme.error,
            ),
            onPressed: isActionDisabled
                ? null
                : () => _handleDecisionTap(context, decision),
            icon: const Icon(Icons.block_rounded, size: 18),
            label: Text('chat_message_card_exec_approval_deny'.tr),
          ),
        );
        continue;
      }
      final ruleIndex = _extractRuleIndex(decision);
      if (ruleIndex == null) {
        continue;
      }
      buttons.add(
        OutlinedButton.icon(
          key: Key('chat_message_card_exec_approval_allow_rule_$ruleIndex'),
          onPressed: isActionDisabled
              ? null
              : () => _handleDecisionTap(context, decision),
          icon: const Icon(Icons.rule_folder_outlined, size: 18),
          label: Text(
            'chat_message_card_exec_approval_allow_rule'.trParams({
              'index': '$ruleIndex',
            }),
          ),
        ),
      );
    }
    return buttons;
  }

  Future<void> _submitDecision(String decision) async {
    setState(() {
      _isSubmitting = true;
      _submitError = '';
    });
    if (widget.onDecisionTap == null) {
      setState(() => _isSubmitting = false);
      return;
    }
    final result = await widget.onDecisionTap!(decision);
    if (!mounted) {
      return;
    }
    switch (result.status) {
      case ChatMessageCardActionStatus.submitted:
        return;
      case ChatMessageCardActionStatus.ignored:
        setState(() {
          _submitError = '';
        });
        return;
      case ChatMessageCardActionStatus.failed:
        setState(() {
          _isSubmitting = false;
          _submitError = result.message.isNotEmpty
              ? result.message
              : 'chat_message_card_exec_approval_submit_failed'.tr;
        });
        return;
    }
  }

  void _syncLocalState() {
    _expiryTimer?.cancel();
    _submitError = '';
    _isSubmitting = widget.isPending;
    final expiresInSeconds = card.expiresInSeconds;
    final expiresAtMs = card.expiresAtMs;
    final isTerminal =
        card.resolutionStatus != null || card.executionStatus != null;

    if (isTerminal) {
      _isSubmitting = false;
    }

    if (expiresAtMs != null) {
      _remainingSeconds =
          ((expiresAtMs - DateTime.now().millisecondsSinceEpoch) / 1000).ceil();
      if (_remainingSeconds! < 0) {
        _remainingSeconds = 0;
      }
    } else {
      _remainingSeconds = expiresInSeconds;
    }

    if (isTerminal || (_remainingSeconds ?? 0) <= 0) {
      if (_remainingSeconds != null && _remainingSeconds! < 0) {
        _remainingSeconds = 0;
      }
      return;
    }

    _expiryTimer = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (!mounted) {
        timer.cancel();
        return;
      }

      int nextSeconds;
      if (expiresAtMs != null) {
        nextSeconds =
            ((expiresAtMs - DateTime.now().millisecondsSinceEpoch) / 1000)
                .ceil();
      } else {
        nextSeconds = (_remainingSeconds ?? 0) - 1;
      }

      if (nextSeconds <= 0) {
        setState(() {
          _remainingSeconds = 0;
          _isSubmitting = false;
        });
        timer.cancel();
        return;
      }

      setState(() {
        _remainingSeconds = nextSeconds;
      });
    });
  }

  bool _shouldResyncLocalState(
    ChatExecApprovalCardData previous,
    ChatExecApprovalCardData next,
  ) {
    return !_isSameApprovalCardSnapshot(previous, next);
  }

  bool _isSameApprovalCardSnapshot(
    ChatExecApprovalCardData left,
    ChatExecApprovalCardData right,
  ) {
    return left.approvalId == right.approvalId &&
        left.approvalSlug == right.approvalSlug &&
        left.approvalCommandId == right.approvalCommandId &&
        left.command == right.command &&
        left.host == right.host &&
        left.nodeId == right.nodeId &&
        left.cwd == right.cwd &&
        left.warningText == right.warningText &&
        left.expiresInSeconds == right.expiresInSeconds &&
        left.expiresAtMs == right.expiresAtMs &&
        _sameStringList(left.allowedDecisions, right.allowedDecisions) &&
        _statusSnapshot(left.resolutionStatus) ==
            _statusSnapshot(right.resolutionStatus) &&
        _statusSnapshot(left.executionStatus) ==
            _statusSnapshot(right.executionStatus);
  }

  bool _sameStringList(List<String> left, List<String> right) {
    if (identical(left, right)) {
      return true;
    }
    if (left.length != right.length) {
      return false;
    }
    for (var index = 0; index < left.length; index++) {
      if (left[index] != right[index]) {
        return false;
      }
    }
    return true;
  }

  String _statusSnapshot(ChatExecStatusCardData? status) {
    if (status == null) {
      return '';
    }
    return [
      status.status,
      status.summary,
      status.detailText,
      status.approvalId,
      status.approvalCommandId,
      status.host,
      status.nodeId,
      status.sessionId,
      status.reason,
      status.decision,
      status.resolvedById,
      status.command,
      status.exitLabel,
      status.channelLabel,
      status.warningText,
    ].join('\u0001');
  }

  bool get _isExpired {
    final resolutionStatus = card.resolutionStatus?.displayStatus ?? '';
    if (resolutionStatus == 'approval-expired' ||
        resolutionStatus == 'approval-unavailable') {
      return true;
    }
    return card.resolutionStatus == null &&
        card.executionStatus == null &&
        (_remainingSeconds ?? 1) <= 0;
  }

  bool _requiresConfirmation(String decision) {
    return decision == 'allow-always' || decision == 'deny';
  }

  int? _extractRuleIndex(String decision) {
    final match = RegExp(r'^allow-rule:(\d+)$').firstMatch(decision.trim());
    if (match == null) {
      return null;
    }
    return int.tryParse(match.group(1) ?? '');
  }

  Widget _buildStatusBadge(
    String label,
    Color statusColor,
    TextStyle titleStyle,
  ) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      decoration: BoxDecoration(
        color: statusColor.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(label, style: titleStyle.copyWith(color: statusColor)),
    );
  }

  Color _resolveStatusColor(ThemeData theme, String status) {
    switch (status) {
      case 'approval-expired':
        return theme.colorScheme.error;
      case 'approval-forwarded':
        return Colors.blue.shade700;
      case 'approval-unavailable':
      case 'resolved-deny':
      case 'denied':
        return theme.colorScheme.error;
      case 'resolved-allow-once':
      case 'resolved-allow-always':
      case 'resolved-allow-rule':
      case 'finished':
        return Colors.green.shade700;
      case 'running':
        return Colors.orange.shade800;
      default:
        return theme.colorScheme.primary;
    }
  }

  String _resolveStatusLabel(String status) {
    switch (status) {
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
        return status;
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
        return '';
    }
  }

  String _resolveResolvedByLabel(ChatExecStatusCardData status) {
    final actorId = status.displayResolvedById;
    if (actorId.isEmpty) {
      return '';
    }
    return '${'chat_message_card_exec_status_resolved_by'.tr}: $actorId';
  }

  Widget _buildMetaChip({required String label, required TextStyle style}) {
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
}
