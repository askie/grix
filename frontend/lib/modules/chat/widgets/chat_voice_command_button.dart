import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

class ChatVoiceCommandButton extends StatefulWidget {
  const ChatVoiceCommandButton({
    super.key,
    required this.isListening,
    required this.isAwaitingResponse,
    required this.transcriptPreview,
    required this.onStart,
    required this.onStopAndSubmit,
  });

  /// Shared with the send control so tapping send is not treated as
  /// tap-outside. Send already flushes recognition before dispatching.
  static const Object composerTapGroupId = 'chat_voice_command_composer';

  final RxBool isListening;
  final RxBool isAwaitingResponse;
  final RxString transcriptPreview;
  final Future<void> Function() onStart;
  final Future<void> Function() onStopAndSubmit;

  @override
  State<ChatVoiceCommandButton> createState() => _ChatVoiceCommandButtonState();
}

class _ChatVoiceCommandButtonState extends State<ChatVoiceCommandButton>
    with SingleTickerProviderStateMixin {
  late final AnimationController _breath;
  Worker? _listeningWorker;

  @override
  void initState() {
    super.initState();
    _breath = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1100),
    );
    _listeningWorker = ever<bool>(widget.isListening, _syncBreath);
    _syncBreath(widget.isListening.value);
  }

  void _syncBreath(bool listening) {
    if (listening) {
      _breath.repeat(reverse: true);
    } else {
      _breath
        ..stop()
        ..value = 0;
    }
  }

  @override
  void dispose() {
    _listeningWorker?.dispose();
    _breath.dispose();
    super.dispose();
  }

  void _toggleListening(bool listening) {
    if (listening) {
      unawaited(widget.onStopAndSubmit());
    } else {
      unawaited(widget.onStart());
    }
  }

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final theme = Theme.of(context);
      final listening = widget.isListening.value;
      final awaiting = widget.isAwaitingResponse.value;
      final preview = widget.transcriptPreview.value.trim();
      return TapRegion(
        groupId: ChatVoiceCommandButton.composerTapGroupId,
        enabled: listening,
        onTapOutside: (_) => unawaited(widget.onStopAndSubmit()),
        child: Tooltip(
          message: awaiting
              ? 'chat_voice_command_awaiting'.tr
              : listening
              ? (preview.isEmpty
                    ? 'chat_voice_command_release_to_fill'.tr
                    : preview)
              : 'chat_voice_command_hold_to_talk'.tr,
          child: GestureDetector(
            behavior: HitTestBehavior.opaque,
            onTap: () => _toggleListening(listening),
            child: AnimatedBuilder(
              animation: _breath,
              builder: (context, child) {
                final t = listening
                    ? Curves.easeInOut.transform(_breath.value)
                    : 0.0;
                return SizedBox(
                  key: const Key('chat_voice_command_button'),
                  width: 24,
                  height: 24,
                  child: awaiting
                      ? Center(
                          child: SizedBox(
                            width: 12,
                            height: 12,
                            child: CircularProgressIndicator(
                              strokeWidth: 1.6,
                              color: theme.colorScheme.onSurface.withValues(
                                alpha: 0.45,
                              ),
                            ),
                          ),
                        )
                      : Stack(
                          alignment: Alignment.center,
                          clipBehavior: Clip.none,
                          children: [
                            if (listening)
                              Opacity(
                                key: const Key('chat_voice_command_breath'),
                                opacity: 0.22 + 0.5 * t,
                                child: DecoratedBox(
                                  decoration: BoxDecoration(
                                    shape: BoxShape.circle,
                                    color: theme.colorScheme.error.withValues(
                                      alpha: 0.85,
                                    ),
                                  ),
                                  child: const SizedBox(width: 18, height: 18),
                                ),
                              ),
                            Icon(
                              listening
                                  ? Icons.mic_rounded
                                  : Icons.mic_none_rounded,
                              color: listening
                                  ? Color.lerp(
                                      theme.colorScheme.error.withValues(
                                        alpha: 0.55,
                                      ),
                                      theme.colorScheme.error,
                                      t,
                                    )
                                  : theme.colorScheme.secondary.withValues(
                                      alpha: 0.52,
                                    ),
                              size: 16,
                            ),
                          ],
                        ),
                );
              },
            ),
          ),
        ),
      );
    });
  }
}
