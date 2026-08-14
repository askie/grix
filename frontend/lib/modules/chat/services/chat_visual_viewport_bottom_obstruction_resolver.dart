import 'dart:math' as math;

class ChatVisualViewportBottomObstructionResolver {
  const ChatVisualViewportBottomObstructionResolver._();

  static double resolve({
    required double layoutViewportHeight,
    required double visualViewportHeight,
    required double visualViewportOffsetTop,
  }) {
    if (layoutViewportHeight <= 0 || visualViewportHeight <= 0) {
      return 0;
    }

    return math.max(
      0,
      layoutViewportHeight - visualViewportHeight - visualViewportOffsetTop,
    );
  }
}
