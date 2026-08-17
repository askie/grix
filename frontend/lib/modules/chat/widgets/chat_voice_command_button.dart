import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/utils/toast_util.dart';

class ChatVoiceCommandButton extends StatelessWidget {
  const ChatVoiceCommandButton({
    super.key,
    required this.isListening,
    required this.isAwaitingResponse,
    required this.onStart,
    required this.onStopAndSubmit,
    required this.onCancel,
  });

  final RxBool isListening;
  final RxBool isAwaitingResponse;
  final Future<void> Function() onStart;
  final Future<void> Function() onStopAndSubmit;
  final Future<void> Function() onCancel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Obx(() {
      final listening = isListening.value;
      final awaiting = isAwaitingResponse.value;
      return Padding(
        padding: const EdgeInsets.only(right: 4, bottom: 2),
        child: Tooltip(
          message: awaiting
              ? '正在等待语音命令结果'
              : listening
              ? '松开发送'
              : '按住说话',
          child: GestureDetector(
            onTap: awaiting ? null : () => CustomToast.show('请按住麦克风按钮说话'),
            onLongPressStart: awaiting ? null : (_) => unawaited(onStart()),
            onLongPressEnd: awaiting
                ? null
                : (_) => unawaited(onStopAndSubmit()),
            onLongPressCancel: awaiting ? null : () => unawaited(onCancel()),
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
    });
  }
}
