import 'package:flutter/material.dart';

import 'chat_markdown_style_sheet.dart';
import 'chat_selection_area.dart';

class ChatMarkdownPlainTextView extends StatelessWidget {
  const ChatMarkdownPlainTextView({
    super.key,
    required this.data,
    required this.styleSheet,
    this.selectionEnabled = true,
    this.onSelectionCleared,
  });

  final String data;
  final ChatMarkdownStyleSheet styleSheet;
  final bool selectionEnabled;
  final VoidCallback? onSelectionCleared;

  @override
  Widget build(BuildContext context) {
    return ChatSelectionArea(
      enabled: selectionEnabled,
      onSelectionCleared: onSelectionCleared,
      child: Text(data, style: styleSheet.paragraphStyle),
    );
  }
}
