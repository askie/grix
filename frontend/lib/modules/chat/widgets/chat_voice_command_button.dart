import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/utils/toast_util.dart';

class ChatVoiceCommandButton extends StatefulWidget {
  const ChatVoiceCommandButton({
    super.key,
    required this.isListening,
    required this.isAwaitingResponse,
    required this.transcriptPreview,
    required this.onStart,
    required this.onStopAndSubmit,
    required this.onCancel,
  });

  final RxBool isListening;
  final RxBool isAwaitingResponse;
  final RxString transcriptPreview;
  final Future<void> Function() onStart;
  final Future<void> Function() onStopAndSubmit;
  final Future<void> Function() onCancel;

  @override
  State<ChatVoiceCommandButton> createState() => _ChatVoiceCommandButtonState();
}

class _ChatVoiceCommandButtonState extends State<ChatVoiceCommandButton> {
  final OverlayPortalController _holdOverlay = OverlayPortalController();

  void _showHoldOverlay() {
    if (!_holdOverlay.isShowing) {
      _holdOverlay.show();
    }
  }

  void _hideHoldOverlay() {
    if (_holdOverlay.isShowing) {
      _holdOverlay.hide();
    }
  }

  @override
  void dispose() {
    _hideHoldOverlay();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return OverlayPortal(
      controller: _holdOverlay,
      overlayChildBuilder: _buildHoldOverlay,
      child: Obx(() {
        final theme = Theme.of(context);
        final listening = widget.isListening.value;
        final awaiting = widget.isAwaitingResponse.value;
        return Padding(
          padding: const EdgeInsets.only(right: 4, bottom: 2),
          child: Tooltip(
            message: awaiting
                ? '正在等待语音命令结果'
                : listening
                ? '松开填入'
                : '按住说话',
            child: GestureDetector(
              onTap: awaiting ? null : () => CustomToast.show('请按住麦克风按钮说话'),
              onLongPressStart: awaiting
                  ? null
                  : (_) {
                      _showHoldOverlay();
                      unawaited(widget.onStart());
                    },
              onLongPressEnd: awaiting
                  ? null
                  : (_) {
                      _hideHoldOverlay();
                      unawaited(widget.onStopAndSubmit());
                    },
              onLongPressCancel: awaiting
                  ? null
                  : () {
                      _hideHoldOverlay();
                      unawaited(widget.onCancel());
                    },
              child: AnimatedContainer(
                key: const Key('chat_voice_command_button'),
                duration: const Duration(milliseconds: 120),
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: awaiting
                      ? theme.colorScheme.surfaceContainerHighest
                      : listening
                      ? theme.colorScheme.error
                      : theme.colorScheme.secondaryContainer,
                ),
                alignment: Alignment.center,
                child: awaiting
                    ? SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: theme.colorScheme.onSurface.withValues(
                            alpha: 0.55,
                          ),
                        ),
                      )
                    : Icon(
                        listening ? Icons.mic_rounded : Icons.mic_none_rounded,
                        color: listening
                            ? theme.colorScheme.onError
                            : theme.colorScheme.onSecondaryContainer,
                        size: 22,
                      ),
              ),
            ),
          ),
        );
      }),
    );
  }

  Widget _buildHoldOverlay(BuildContext context) {
    final theme = Theme.of(context);
    return IgnorePointer(
      child: SafeArea(
        child: Align(
          alignment: const Alignment(0, -0.12),
          child: Obx(() {
            final preview = widget.transcriptPreview.value.trim();
            return Material(
              key: const Key('chat_voice_command_hold_overlay'),
              color: theme.colorScheme.inverseSurface.withValues(alpha: 0.94),
              elevation: 6,
              borderRadius: BorderRadius.circular(18),
              child: ConstrainedBox(
                constraints: const BoxConstraints(minWidth: 160, maxWidth: 280),
                child: Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 24,
                    vertical: 20,
                  ),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        Icons.mic_rounded,
                        size: 36,
                        color: theme.colorScheme.error,
                      ),
                      const SizedBox(height: 10),
                      Text(
                        '松开填入',
                        style: theme.textTheme.titleSmall?.copyWith(
                          color: theme.colorScheme.onInverseSurface,
                        ),
                      ),
                      if (preview.isNotEmpty) ...[
                        const SizedBox(height: 8),
                        Text(
                          preview,
                          maxLines: 3,
                          overflow: TextOverflow.ellipsis,
                          textAlign: TextAlign.center,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onInverseSurface
                                .withValues(alpha: 0.78),
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
              ),
            );
          }),
        ),
      ),
    );
  }
}
