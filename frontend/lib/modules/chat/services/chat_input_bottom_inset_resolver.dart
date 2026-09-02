import 'dart:math' as math;

import 'package:flutter/foundation.dart';

@immutable
class ChatInputBottomInsetResolution {
  const ChatInputBottomInsetResolution({
    required this.keyboardInset,
    required this.restingBottomInset,
    required this.inputBottomInset,
  });

  final double keyboardInset;
  final double restingBottomInset;
  final double inputBottomInset;
}

class ChatInputBottomInsetResolver {
  const ChatInputBottomInsetResolver._();

  static ChatInputBottomInsetResolution resolve({
    required double viewPaddingBottom,
    required double systemGestureInsetBottom,
    required double liveKeyboardInsetBottom,
    required double retainedKeyboardInsetBottom,
    required double platformViewportObstructionBottom,
    required double minBottomSpacing,
  }) {
    final keyboardInset = math.max(
      platformViewportObstructionBottom,
      math.max(liveKeyboardInsetBottom, retainedKeyboardInsetBottom),
    );
    final restingBottomInset = math.max(
      minBottomSpacing,
      math.max(viewPaddingBottom, systemGestureInsetBottom),
    );

    return ChatInputBottomInsetResolution(
      keyboardInset: keyboardInset,
      restingBottomInset: restingBottomInset,
      inputBottomInset: keyboardInset > 0
          ? minBottomSpacing
          : restingBottomInset,
    );
  }
}
