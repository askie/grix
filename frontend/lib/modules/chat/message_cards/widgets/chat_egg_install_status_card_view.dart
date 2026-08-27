import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../../app/themes/app_theme.dart';
import '../models/chat_egg_install_status_card_data.dart';

class ChatEggInstallStatusCardView extends StatelessWidget {
  const ChatEggInstallStatusCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
  });

  final ChatEggInstallStatusCardData card;
  final bool isMine;
  final double fontScale;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final viewportWidth = MediaQuery.sizeOf(context).width;
    final accentColor = _resolveAccentColor(theme);
    final statusBadgeStyle = AppTheme.applyTextFont(
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
            color: theme.colorScheme.onSurface.withValues(alpha: 0.78),
            height: 1.45,
          ) ??
          TextStyle(
            fontSize: 11 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.78),
            height: 1.45,
          ),
    );
    final metaLabelStyle = AppTheme.applyTextFont(
      detailStyle.copyWith(
        color: accentColor.withValues(alpha: 0.9),
        fontWeight: FontWeight.w700,
      ),
    );
    final metaLines = _buildMetaLines(metaLabelStyle, detailStyle);

    return Container(
      key: const Key('chat_message_card_egg_install_status'),
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
          if (card.displayInstallId.isNotEmpty) ...[
            _buildMetaLine(
              label: 'chat_message_card_egg_install_status_install_id'.tr,
              value: card.displayInstallId,
              labelStyle: metaLabelStyle,
              valueStyle: detailStyle,
            ),
            const SizedBox(height: 10),
          ],
          Align(
            alignment: Alignment.centerLeft,
            child: Container(
              key: const Key('chat_message_card_egg_install_status_badge'),
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
              decoration: BoxDecoration(
                color: accentColor.withValues(alpha: 0.12),
                borderRadius: BorderRadius.circular(999),
              ),
              child: Text(_resolveStatusLabel(), style: statusBadgeStyle),
            ),
          ),
          if (card.displaySummary.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(card.displaySummary, style: summaryStyle),
          ],
          if (metaLines.isNotEmpty) ...[
            const SizedBox(height: 10),
            Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: metaLines,
            ),
          ],
          if (card.displayErrorMsg.isNotEmpty) ...[
            const SizedBox(height: 10),
            _buildInfoBox(
              context,
              title: 'chat_message_card_egg_install_status_error'.tr,
              body: card.displayErrorMsg,
              accentColor: theme.colorScheme.error,
              bodyStyle: detailStyle.copyWith(
                color: theme.colorScheme.error,
                fontWeight: FontWeight.w600,
              ),
            ),
          ],
          if (card.displayDetailText.isNotEmpty) ...[
            const SizedBox(height: 10),
            _buildInfoBox(
              context,
              title: 'chat_message_card_egg_install_status_details'.tr,
              body: card.displayDetailText,
              accentColor: accentColor,
              bodyStyle: detailStyle,
            ),
          ],
        ],
      ),
    );
  }

  Color _resolveAccentColor(ThemeData theme) {
    switch (card.displayStatus) {
      case 'running':
        return AppTheme.statusWarningColor(theme.brightness);
      case 'success':
        return AppTheme.statusSuccessColor(theme.brightness);
      case 'failed':
        return theme.colorScheme.error;
      default:
        return isMine ? theme.colorScheme.primary : theme.colorScheme.secondary;
    }
  }

  String _resolveStatusLabel() {
    switch (card.displayStatus) {
      case 'running':
        return 'chat_message_card_egg_install_status_running'.tr;
      case 'success':
        return 'chat_message_card_egg_install_status_success'.tr;
      case 'failed':
        return 'chat_message_card_egg_install_status_failed'.tr;
      default:
        return card.displayStatus;
    }
  }

  List<Widget> _buildMetaLines(TextStyle labelStyle, TextStyle valueStyle) {
    final lines = <Widget>[];

    void addLine(String label, String value) {
      final trimmedValue = value.trim();
      if (trimmedValue.isEmpty) {
        return;
      }
      if (lines.isNotEmpty) {
        lines.add(const SizedBox(height: 6));
      }
      lines.add(
        _buildMetaLine(
          label: label,
          value: trimmedValue,
          labelStyle: labelStyle,
          valueStyle: valueStyle,
        ),
      );
    }

    addLine(
      'chat_message_card_egg_install_status_step'.tr,
      _resolveStepLabel(),
    );
    addLine(
      'chat_message_card_egg_install_status_target_agent'.tr,
      card.displayTargetAgentId,
    );
    addLine(
      'chat_message_card_egg_install_status_error_code'.tr,
      card.displayErrorCode,
    );
    return lines;
  }

  Widget _buildMetaLine({
    required String label,
    required String value,
    required TextStyle labelStyle,
    required TextStyle valueStyle,
  }) {
    return Text.rich(
      TextSpan(
        children: [
          TextSpan(text: '$label: ', style: labelStyle),
          TextSpan(text: value, style: valueStyle),
        ],
      ),
    );
  }

  String _resolveStepLabel() {
    switch (card.displayStep) {
      case 'agent_created':
        return 'chat_message_card_egg_install_status_step_agent_created'.tr;
      case 'downloaded':
        return 'chat_message_card_egg_install_status_step_downloaded'.tr;
      case 'installed':
        return 'chat_message_card_egg_install_status_step_installed'.tr;
      case 'completed':
        return 'chat_message_card_egg_install_status_step_completed'.tr;
      case 'user_cancelled':
        return 'chat_message_card_egg_install_status_step_user_cancelled'.tr;
      case 'target_not_found':
        return 'chat_message_card_egg_install_status_step_target_not_found'.tr;
      case 'download_failed':
        return 'chat_message_card_egg_install_status_step_download_failed'.tr;
      default:
        return card.displayStep;
    }
  }

  Widget _buildInfoBox(
    BuildContext context, {
    required String title,
    required String body,
    required Color accentColor,
    required TextStyle bodyStyle,
  }) {
    final titleStyle = AppTheme.applyTextFont(
      Theme.of(context).textTheme.labelSmall?.copyWith(
            fontSize: 10 * fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ) ??
          TextStyle(
            fontSize: 10 * fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ),
    );
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: accentColor.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: accentColor.withValues(alpha: 0.16)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(title, style: titleStyle),
          const SizedBox(height: 6),
          Text(body, style: bodyStyle),
        ],
      ),
    );
  }
}
