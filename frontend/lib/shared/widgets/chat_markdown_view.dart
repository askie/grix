import 'package:flutter/material.dart';

import '../../modules/chat/message_cards/models/chat_message_card_action.dart';
import '../../modules/chat/message_cards/models/chat_message_card_data.dart';
import '../../modules/chat/services/chat_managed_input.dart';
import '../markdown/chat_markdown_ast.dart';
import '../markdown/chat_markdown_semantics.dart';
import 'chat_markdown_ast_view.dart';
import 'chat_markdown_fallback_view.dart';
import 'chat_markdown_plain_text_view.dart';
import 'chat_markdown_render_strategy.dart';
import 'chat_selection_area.dart';
import 'chat_markdown_style_sheet.dart';

class ChatMarkdownView extends StatelessWidget {
  const ChatMarkdownView({
    super.key,
    required this.data,
    required this.textColor,
    required this.isMine,
    this.fontScale = 1.0,
    this.document,
    this.semantics,
    this.renderStrategy = const ChatMarkdownRenderStrategy(),
    this.onMessageCardAction,
    this.onMessageCardTap,
    this.sourceMessageId = '',
    this.managedInputBinding,
    this.isExecApprovalPending,
    this.pickRemoteDirectory,
    this.selectionEnabled = true,
    this.onSelectionCleared,
  });

  final String data;
  final Color textColor;
  final bool isMine;
  final double fontScale;
  final ChatMarkdownDocument? document;
  final ChatMarkdownSemanticSummary? semantics;
  final ChatMarkdownRenderStrategy renderStrategy;
  final ChatMessageCardActionHandler? onMessageCardAction;
  final ValueChanged<ChatMessageCardData>? onMessageCardTap;
  final String sourceMessageId;
  final ChatManagedInputBinding? managedInputBinding;
  final bool Function(String approvalId)? isExecApprovalPending;
  final Future<String?> Function()? pickRemoteDirectory;
  final bool selectionEnabled;
  final VoidCallback? onSelectionCleared;

  @override
  Widget build(BuildContext context) {
    final styleSheet = ChatMarkdownStyleSheet.fromTheme(
      theme: Theme.of(context),
      textColor: textColor,
      isMine: isMine,
      fontScale: fontScale,
    );
    final mode = renderStrategy.select(
      document: document,
      semantics: semantics,
    );

    switch (mode) {
      case ChatMarkdownRenderMode.nativeAst:
        return ChatSelectionArea(
          enabled: selectionEnabled,
          onSelectionCleared: onSelectionCleared,
          child: ChatMarkdownAstView(
            document: document!,
            styleSheet: styleSheet,
            onMessageCardAction: onMessageCardAction,
            onMessageCardTap: onMessageCardTap,
            sourceMessageId: sourceMessageId,
            managedInputBinding: managedInputBinding,
            isExecApprovalPending: isExecApprovalPending,
            pickRemoteDirectory: pickRemoteDirectory,
          ),
        );
      case ChatMarkdownRenderMode.fallbackPlainText:
        return ChatMarkdownPlainTextView(
          data: data,
          styleSheet: styleSheet,
          selectionEnabled: selectionEnabled,
          onSelectionCleared: onSelectionCleared,
        );
      case ChatMarkdownRenderMode.fallbackMarkdownWidget:
        return ChatMarkdownFallbackView(
          data: data,
          styleSheet: styleSheet,
          onMessageCardAction: onMessageCardAction,
          onMessageCardTap: onMessageCardTap,
          sourceMessageId: sourceMessageId,
          managedInputBinding: managedInputBinding,
          isExecApprovalPending: isExecApprovalPending,
          pickRemoteDirectory: pickRemoteDirectory,
          selectionEnabled: selectionEnabled,
          onSelectionCleared: onSelectionCleared,
        );
    }
  }
}
