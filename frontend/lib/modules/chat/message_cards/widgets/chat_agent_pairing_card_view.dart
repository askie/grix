import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../../../app/themes/app_theme.dart';
import '../models/chat_agent_pairing_card_data.dart';

class ChatAgentPairingCardView extends StatefulWidget {
  const ChatAgentPairingCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
  });

  final ChatAgentPairingCardData card;
  final bool isMine;
  final double fontScale;

  @override
  State<ChatAgentPairingCardView> createState() =>
      _ChatAgentPairingCardViewState();
}

class _ChatAgentPairingCardViewState extends State<ChatAgentPairingCardView> {
  bool _copied = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final accentColor = widget.isMine
        ? theme.colorScheme.primary
        : theme.colorScheme.secondary;
    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.labelMedium?.copyWith(
            fontSize: 11 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ) ??
          TextStyle(
            fontSize: 11 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ),
    );
    final bodyStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 12 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.8),
            height: 1.45,
          ) ??
          TextStyle(
            fontSize: 12 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.8),
            height: 1.45,
          ),
    );
    final codeStyle =
        theme.textTheme.headlineSmall?.copyWith(
          fontSize: 24 * widget.fontScale,
          fontWeight: FontWeight.w800,
          letterSpacing: 1.6,
          color: theme.colorScheme.onSurface,
          fontFamily: 'monospace',
        ) ??
        TextStyle(
          fontSize: 24 * widget.fontScale,
          fontWeight: FontWeight.w800,
          letterSpacing: 1.6,
          color: theme.colorScheme.onSurface,
          fontFamily: 'monospace',
        );

    return Container(
      key: const Key('chat_message_card_agent_pairing'),
      constraints: const BoxConstraints(minWidth: 240, maxWidth: 360),
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
            children: [
              Container(
                width: 34,
                height: 34,
                decoration: BoxDecoration(
                  color: accentColor.withValues(alpha: 0.14),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Icon(Icons.link_rounded, size: 18, color: accentColor),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  'chat_message_card_agent_pairing_label'.tr,
                  style: titleStyle,
                ),
              ),
              TextButton.icon(
                key: const Key('chat_message_card_agent_pairing_copy'),
                onPressed: _handleCopy,
                icon: Icon(
                  _copied ? Icons.check_rounded : Icons.copy_rounded,
                  size: 16,
                ),
                label: Text(
                  _copied
                      ? 'chat_message_card_agent_pairing_copied'.tr
                      : 'chat_message_card_agent_pairing_copy'.tr,
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text('chat_message_card_agent_pairing_code'.tr, style: titleStyle),
          const SizedBox(height: 6),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 14),
            decoration: BoxDecoration(
              color: theme.colorScheme.surface,
              borderRadius: BorderRadius.circular(10),
              border: Border.all(
                color: theme.colorScheme.outline.withValues(alpha: 0.12),
              ),
            ),
            child: Text(
              widget.card.displayPairingCode,
              style: codeStyle,
            ),
          ),
          if (widget.card.displayInstructionText.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(widget.card.displayInstructionText, style: bodyStyle),
          ],
          if (widget.card.displayCommandHint.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(
              '${'chat_message_card_agent_pairing_command'.tr}: ${widget.card.displayCommandHint}',
              style: bodyStyle,
            ),
          ],
        ],
      ),
    );
  }

  Future<void> _handleCopy() async {
    await Clipboard.setData(
      ClipboardData(text: widget.card.displayPairingCode),
    );
    if (!mounted) {
      return;
    }
    setState(() {
      _copied = true;
    });
  }
}
