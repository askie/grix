import 'package:flutter/material.dart';

import '../../modules/chat/message_cards/models/chat_message_card_action.dart';
import '../../modules/chat/message_cards/models/chat_message_card_data.dart';
import '../../modules/chat/services/chat_managed_input.dart';
import '../markdown/chat_markdown_ast.dart';
import 'chat_markdown_block_renderer.dart';
import 'chat_markdown_style_sheet.dart';

class ChatMarkdownAstView extends StatelessWidget {
  const ChatMarkdownAstView({
    super.key,
    required this.document,
    required this.styleSheet,
    this.onMessageCardAction,
    this.onMessageCardTap,
    this.sourceMessageId = '',
    this.managedInputBinding,
    this.isExecApprovalPending,
    this.pickRemoteDirectory,
  });

  final ChatMarkdownDocument document;
  final ChatMarkdownStyleSheet styleSheet;
  final ChatMessageCardActionHandler? onMessageCardAction;
  final ValueChanged<ChatMessageCardData>? onMessageCardTap;
  final String sourceMessageId;
  final ChatManagedInputBinding? managedInputBinding;
  final bool Function(String approvalId)? isExecApprovalPending;
  final Future<String?> Function()? pickRemoteDirectory;

  static bool supportsDocument(ChatMarkdownDocument document) {
    return _supportsNode(document);
  }

  static bool _supportsNode(ChatMarkdownNode node) {
    // Nodes with fallbackReason are rendered as plain text paragraphs
    if (node.fallbackReason != null) {
      return true;
    }
    switch (node.type) {
      case ChatMarkdownNodeType.document:
      case ChatMarkdownNodeType.heading:
      case ChatMarkdownNodeType.paragraph:
      case ChatMarkdownNodeType.thematicBreak:
      case ChatMarkdownNodeType.blockquote:
      case ChatMarkdownNodeType.list:
      case ChatMarkdownNodeType.listItem:
      case ChatMarkdownNodeType.taskItem:
      case ChatMarkdownNodeType.codeBlock:
      case ChatMarkdownNodeType.table:
      case ChatMarkdownNodeType.tableHead:
      case ChatMarkdownNodeType.tableBody:
      case ChatMarkdownNodeType.tableRow:
      case ChatMarkdownNodeType.tableCell:
      case ChatMarkdownNodeType.mathBlock:
      case ChatMarkdownNodeType.mermaidBlock:
      case ChatMarkdownNodeType.footnoteDef:
      case ChatMarkdownNodeType.text:
      case ChatMarkdownNodeType.softBreak:
      case ChatMarkdownNodeType.hardBreak:
      case ChatMarkdownNodeType.emphasis:
      case ChatMarkdownNodeType.strong:
      case ChatMarkdownNodeType.strike:
      case ChatMarkdownNodeType.inlineCode:
      case ChatMarkdownNodeType.link:
      case ChatMarkdownNodeType.image:
      case ChatMarkdownNodeType.video:
      case ChatMarkdownNodeType.audio:
      case ChatMarkdownNodeType.mathInline:
      case ChatMarkdownNodeType.autolink:
      case ChatMarkdownNodeType.footnoteRef:
        return node.children.every(_supportsNode);
      case ChatMarkdownNodeType.htmlBlockText:
      case ChatMarkdownNodeType.escapedText:
      case ChatMarkdownNodeType.unknown:
        return false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final renderer = ChatMarkdownBlockRenderer(
      styleSheet: styleSheet,
      onMessageCardAction: onMessageCardAction,
      onMessageCardTap: onMessageCardTap,
      sourceMessageId: sourceMessageId,
      managedInputBinding: managedInputBinding,
      isExecApprovalPending: isExecApprovalPending,
      pickRemoteDirectory: pickRemoteDirectory,
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: renderer.buildBlocks(document.children),
    );
  }
}
