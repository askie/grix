import 'package:flutter/material.dart';

import '../../modules/chat/message_cards/models/chat_message_card_action.dart';
import '../../modules/chat/message_cards/models/chat_message_card_data.dart';
import '../../modules/chat/services/chat_managed_input.dart';
import '../markdown/chat_markdown_ast.dart';
import 'chat_markdown_audio_view.dart';
import 'chat_markdown_code_block_view.dart';
import 'chat_markdown_image_view.dart';
import 'chat_markdown_inline_renderer.dart';
import 'chat_markdown_math_block_view.dart';
import 'chat_markdown_mermaid_view.dart';
import 'chat_markdown_style_sheet.dart';
import 'chat_markdown_table_view.dart';
import 'chat_markdown_video_view.dart';

class ChatMarkdownBlockRenderer {
  const ChatMarkdownBlockRenderer({
    required this.styleSheet,
    this.onMessageCardAction,
    this.onMessageCardTap,
    this.sourceMessageId = '',
    this.managedInputBinding,
    this.isExecApprovalPending,
    this.pickRemoteDirectory,
    this.onAgentFilePathTap,
  });

  final ChatMarkdownStyleSheet styleSheet;
  final ChatMessageCardActionHandler? onMessageCardAction;
  final ValueChanged<ChatMessageCardData>? onMessageCardTap;
  final String sourceMessageId;
  final ChatManagedInputBinding? managedInputBinding;
  final bool Function(String approvalId)? isExecApprovalPending;
  final Future<String?> Function()? pickRemoteDirectory;
  final ValueChanged<String>? onAgentFilePathTap;

  List<Widget> buildBlocks(List<ChatMarkdownNode> nodes, {int listDepth = 0}) {
    final widgets = <Widget>[];
    for (final node in nodes) {
      final block = buildBlock(node, listDepth: listDepth);
      if (block == null) {
        continue;
      }
      if (widgets.isNotEmpty) {
        widgets.add(const SizedBox(height: 10));
      }
      widgets.add(block);
    }
    return widgets;
  }

  Widget? buildBlock(ChatMarkdownNode node, {int listDepth = 0}) {
    // Render fallback nodes as plain text paragraphs
    if (node.fallbackReason != null) {
      final text = _extractFallbackText(node);
      if (text.isNotEmpty) {
        return Text(text, style: styleSheet.paragraphStyle);
      }
      return null;
    }

    final inlineRenderer = ChatMarkdownInlineRenderer(
      styleSheet: styleSheet,
      onMessageCardAction: onMessageCardAction,
      onMessageCardTap: onMessageCardTap,
      sourceMessageId: sourceMessageId,
      managedInputBinding: managedInputBinding,
      isExecApprovalPending: isExecApprovalPending,
      pickRemoteDirectory: pickRemoteDirectory,
      onAgentFilePathTap: onAgentFilePathTap,
    );

    switch (node.type) {
      case ChatMarkdownNodeType.paragraph:
        if (_isStandaloneImageParagraph(node)) {
          final imageNode = node.children.first;
          return _buildImage(imageNode);
        }
        if (_isStandaloneVideoParagraph(node)) {
          return _buildVideo(node.children.first);
        }
        if (_isStandaloneAudioParagraph(node)) {
          return _buildAudio(node.children.first);
        }
        return _buildMixedContent(node.children, listDepth: listDepth);
      case ChatMarkdownNodeType.heading:
        final level = (node.attrs['level'] as int?) ?? 1;
        final style = styleSheet.headingStyle(level);
        return _buildRichText(
          style: style,
          children: inlineRenderer.buildSpans(node.children, baseStyle: style),
        );
      case ChatMarkdownNodeType.thematicBreak:
        return Divider(color: styleSheet.dividerColor, height: 1, thickness: 1);
      case ChatMarkdownNodeType.blockquote:
        return Container(
          padding: styleSheet.blockquotePadding,
          decoration: BoxDecoration(
            border: Border(
              left: BorderSide(
                color: styleSheet.blockquoteBorderColor,
                width: 3,
              ),
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: buildBlocks(node.children, listDepth: listDepth),
          ),
        );
      case ChatMarkdownNodeType.list:
        final listWidget = Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: _buildListItems(node, listDepth: listDepth),
        );
        if (listDepth > 0) {
          return Padding(
            padding: EdgeInsets.only(
              left: listDepth * styleSheet.nestedListIndent,
            ),
            child: listWidget,
          );
        }
        return listWidget;
      case ChatMarkdownNodeType.codeBlock:
        return ChatMarkdownCodeBlockView(
          code: node.attrs['text']?.toString() ?? '',
          language: node.attrs['language']?.toString(),
          styleSheet: styleSheet,
        );
      case ChatMarkdownNodeType.table:
        return ChatMarkdownTableView(
          tableNode: node,
          styleSheet: styleSheet,
          onAgentFilePathTap: onAgentFilePathTap,
        );
      case ChatMarkdownNodeType.mathBlock:
        return ChatMarkdownMathBlockView(
          tex: node.attrs['tex']?.toString() ?? '',
          styleSheet: styleSheet,
        );
      case ChatMarkdownNodeType.mermaidBlock:
        return ChatMarkdownMermaidView(
          source: node.attrs['text']?.toString() ?? '',
          textStyle: styleSheet.preTextStyle,
          decoration: styleSheet.preDecoration,
          padding: styleSheet.prePadding,
          margin: styleSheet.preMargin,
          backgroundColor: styleSheet.preBackgroundColor,
        );
      case ChatMarkdownNodeType.footnoteDef:
        return _buildFootnoteDefinition(node, listDepth: listDepth);
      case ChatMarkdownNodeType.image:
        return _buildImage(node);
      case ChatMarkdownNodeType.video:
        return _buildVideo(node);
      case ChatMarkdownNodeType.audio:
        return _buildAudio(node);
      case ChatMarkdownNodeType.listItem:
      case ChatMarkdownNodeType.taskItem:
        return _buildListItem(
          node,
          marker: node.type == ChatMarkdownNodeType.taskItem ? null : '•',
          listDepth: listDepth,
        );
      case ChatMarkdownNodeType.document:
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: buildBlocks(node.children, listDepth: listDepth),
        );
      case ChatMarkdownNodeType.text:
      case ChatMarkdownNodeType.softBreak:
      case ChatMarkdownNodeType.hardBreak:
      case ChatMarkdownNodeType.emphasis:
      case ChatMarkdownNodeType.strong:
      case ChatMarkdownNodeType.strike:
      case ChatMarkdownNodeType.inlineCode:
      case ChatMarkdownNodeType.link:
      case ChatMarkdownNodeType.mathInline:
      case ChatMarkdownNodeType.autolink:
      case ChatMarkdownNodeType.footnoteRef:
        return _buildRichText(
          style: styleSheet.paragraphStyle,
          children: inlineRenderer.buildSpans([
            node,
          ], baseStyle: styleSheet.paragraphStyle),
        );
      case ChatMarkdownNodeType.tableHead:
      case ChatMarkdownNodeType.tableBody:
      case ChatMarkdownNodeType.tableRow:
      case ChatMarkdownNodeType.tableCell:
      case ChatMarkdownNodeType.htmlBlockText:
      case ChatMarkdownNodeType.escapedText:
      case ChatMarkdownNodeType.unknown:
        return null;
    }
  }

  String _extractFallbackText(ChatMarkdownNode node) {
    final text = node.attrs['text']?.toString();
    if (text != null && text.isNotEmpty) {
      return text;
    }
    final buffer = StringBuffer();
    for (final child in node.children) {
      final childText = child.attrs['text']?.toString();
      if (childText != null) {
        buffer.write(childText);
      } else {
        buffer.write(_extractFallbackText(child));
      }
    }
    return buffer.toString();
  }

  List<Widget> _buildListItems(ChatMarkdownNode listNode, {int listDepth = 0}) {
    final ordered = listNode.attrs['ordered'] == true;
    final start = (listNode.attrs['start'] as int?) ?? 1;
    final widgets = <Widget>[];
    var index = 0;

    for (final child in listNode.children) {
      final marker = ordered ? '${start + index}.' : '•';
      final item = _buildListItem(child, marker: marker, listDepth: listDepth);
      if (widgets.isNotEmpty) {
        widgets.add(const SizedBox(height: 4));
      }
      widgets.add(item);
      index += 1;
    }

    return widgets;
  }

  Widget _buildListItem(ChatMarkdownNode node, {required String? marker, int listDepth = 0}) {
    final body = _buildMixedContent(node.children, listDepth: listDepth);
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: EdgeInsets.zero,
          child: node.type == ChatMarkdownNodeType.taskItem
              ? Icon(
                  (node.attrs['checked'] == true)
                      ? Icons.check_box_rounded
                      : Icons.check_box_outline_blank_rounded,
                  size: 18,
                  color: styleSheet.listMarkerStyle.color,
                )
              : Text(marker ?? '•', style: styleSheet.listMarkerStyle),
        ),
        Expanded(child: body),
      ],
    );
  }

  Widget _buildMixedContent(List<ChatMarkdownNode> children, {int listDepth = 0}) {
    if (children.isEmpty) {
      return const SizedBox.shrink();
    }

    if (_areInlineNodes(children)) {
      return _buildInlineText(children);
    }

    // Mixed inline + block: group consecutive inline nodes into single
    // RichText, render block nodes individually.
    final widgets = <Widget>[];
    final inlineBuffer = <ChatMarkdownNode>[];

    void flushInlineBuffer() {
      if (inlineBuffer.isEmpty) {
        return;
      }
      widgets.add(_buildInlineText(inlineBuffer.toList(growable: false)));
      inlineBuffer.clear();
    }

    for (final child in children) {
      if (_isInlineNode(child)) {
        inlineBuffer.add(child);
      } else {
        flushInlineBuffer();
        final block = buildBlock(child, listDepth: listDepth + 1);
        if (block != null) {
          if (widgets.isNotEmpty) {
            widgets.add(const SizedBox(height: 4));
          }
          widgets.add(block);
        }
      }
    }
    flushInlineBuffer();

    if (widgets.length == 1) {
      return widgets.first;
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: widgets,
    );
  }

  Widget _buildInlineText(List<ChatMarkdownNode> nodes) {
    final inlineRenderer = ChatMarkdownInlineRenderer(
      styleSheet: styleSheet,
      onMessageCardAction: onMessageCardAction,
      onMessageCardTap: onMessageCardTap,
      sourceMessageId: sourceMessageId,
      managedInputBinding: managedInputBinding,
      isExecApprovalPending: isExecApprovalPending,
      pickRemoteDirectory: pickRemoteDirectory,
      onAgentFilePathTap: onAgentFilePathTap,
    );
    return _buildRichText(
      style: styleSheet.paragraphStyle,
      children: inlineRenderer.buildSpans(
        nodes,
        baseStyle: styleSheet.paragraphStyle,
      ),
    );
  }

  bool _isInlineNode(ChatMarkdownNode node) {
    switch (node.type) {
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
        return true;
      case ChatMarkdownNodeType.paragraph:
      case ChatMarkdownNodeType.document:
      case ChatMarkdownNodeType.heading:
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
      case ChatMarkdownNodeType.htmlBlockText:
      case ChatMarkdownNodeType.escapedText:
      case ChatMarkdownNodeType.unknown:
        return false;
    }
  }

  Widget _buildFootnoteDefinition(ChatMarkdownNode node, {int listDepth = 0}) {
    final id = node.attrs['id']?.toString() ?? '';
    final label = id.startsWith('fn-') ? id.substring(3) : id;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 20,
          child: Text('$label.', style: styleSheet.footnoteLabelStyle),
        ),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: buildBlocks(node.children, listDepth: listDepth),
          ),
        ),
      ],
    );
  }

  bool _areInlineNodes(List<ChatMarkdownNode> nodes) {
    for (final node in nodes) {
      switch (node.type) {
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
          continue;
        case ChatMarkdownNodeType.paragraph:
        case ChatMarkdownNodeType.document:
        case ChatMarkdownNodeType.heading:
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
        case ChatMarkdownNodeType.htmlBlockText:
        case ChatMarkdownNodeType.escapedText:
        case ChatMarkdownNodeType.unknown:
          return false;
      }
    }
    return true;
  }

  bool _isStandaloneImageParagraph(ChatMarkdownNode node) {
    return node.type == ChatMarkdownNodeType.paragraph &&
        node.children.length == 1 &&
        node.children.first.type == ChatMarkdownNodeType.image;
  }

  Widget _buildImage(ChatMarkdownNode node) {
    return ChatMarkdownImageView(
      src: node.attrs['src']?.toString() ?? '',
      alt: node.attrs['alt']?.toString(),
    );
  }

  bool _isStandaloneVideoParagraph(ChatMarkdownNode node) {
    return node.type == ChatMarkdownNodeType.paragraph &&
        node.children.length == 1 &&
        node.children.first.type == ChatMarkdownNodeType.video;
  }

  Widget _buildVideo(ChatMarkdownNode node) {
    return ChatMarkdownVideoView(
      src: node.attrs['src']?.toString() ?? '',
      width: double.tryParse(node.attrs['width']?.toString() ?? ''),
      poster: node.attrs['poster']?.toString(),
    );
  }

  bool _isStandaloneAudioParagraph(ChatMarkdownNode node) {
    return node.type == ChatMarkdownNodeType.paragraph &&
        node.children.length == 1 &&
        node.children.first.type == ChatMarkdownNodeType.audio;
  }

  Widget _buildAudio(ChatMarkdownNode node) {
    return ChatMarkdownAudioView(
      src: node.attrs['src']?.toString() ?? '',
      title: node.attrs['title']?.toString(),
    );
  }

  Widget _buildRichText({
    required TextStyle style,
    required List<InlineSpan> children,
  }) {
    return Text.rich(TextSpan(style: style, children: children));
  }
}
