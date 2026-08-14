import 'package:flutter/material.dart';

import 'chat_mermaid_model.dart';

/// Shared sizing tokens for node content in Mermaid diagrams.
///
/// Keep layout calculation and widget padding aligned by reading values
/// exclusively from this file.
class ChatMermaidNodeStyleTokens {
  const ChatMermaidNodeStyleTokens._();

  static const double flowchartVerticalPadding = 8;
  static const double flowchartHorizontalPadding = 10;
  static const double flowchartRoundedHorizontalPadding = 12;
  static const double flowchartDiamondHorizontalPadding = 14;

  static const double stateVerticalPadding = 8;
  static const double stateHorizontalPadding = 10;

  static EdgeInsets flowchartPaddingForShape(ChatMermaidNodeShape shape) {
    switch (shape) {
      case ChatMermaidNodeShape.diamond:
        return const EdgeInsets.symmetric(
          horizontal: flowchartDiamondHorizontalPadding,
          vertical: flowchartVerticalPadding,
        );
      case ChatMermaidNodeShape.rounded:
      case ChatMermaidNodeShape.stadium:
        return const EdgeInsets.symmetric(
          horizontal: flowchartRoundedHorizontalPadding,
          vertical: flowchartVerticalPadding,
        );
      default:
        return const EdgeInsets.symmetric(
          horizontal: flowchartHorizontalPadding,
          vertical: flowchartVerticalPadding,
        );
    }
  }

  static const EdgeInsets stateNodePadding = EdgeInsets.symmetric(
    horizontal: stateHorizontalPadding,
    vertical: stateVerticalPadding,
  );
}
