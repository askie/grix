import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_math_fork/flutter_math.dart';

import '../../modules/chat/message_cards/models/chat_message_card_action.dart';
import '../../modules/chat/message_cards/models/chat_message_card_data.dart';
import '../../modules/chat/message_cards/services/chat_message_card_codec.dart';
import '../../modules/chat/message_cards/widgets/chat_message_card_view.dart';
import '../../modules/chat/services/chat_managed_input.dart';
import '../markdown/chat_markdown_ast.dart';
import '../markdown/chat_markdown_uri_policy.dart';
import '../utils/app_external_links.dart';
import 'chat_markdown_audio_view.dart';
import 'chat_markdown_image_view.dart';
import 'chat_markdown_style_sheet.dart';
import 'chat_markdown_video_view.dart';

class ChatMarkdownInlineRenderer {
  const ChatMarkdownInlineRenderer({
    required this.styleSheet,
    this.onMessageCardAction,
    this.onMessageCardTap,
    this.sourceMessageId = '',
    this.managedInputBinding,
    this.isExecApprovalPending,
    this.pickRemoteDirectory,
  });

  final ChatMarkdownStyleSheet styleSheet;
  final ChatMessageCardActionHandler? onMessageCardAction;
  final ValueChanged<ChatMessageCardData>? onMessageCardTap;
  final String sourceMessageId;
  final ChatManagedInputBinding? managedInputBinding;
  final bool Function(String approvalId)? isExecApprovalPending;
  final Future<String?> Function()? pickRemoteDirectory;

  List<InlineSpan> buildSpans(
    List<ChatMarkdownNode> nodes, {
    TextStyle? baseStyle,
  }) {
    final spans = <InlineSpan>[];
    final effectiveBaseStyle = baseStyle ?? styleSheet.paragraphStyle;
    for (var i = 0; i < nodes.length; i++) {
      final node = nodes[i];
      if (node.type == ChatMarkdownNodeType.text && i > 0) {
        final prev = nodes[i - 1];
        if (prev.type == ChatMarkdownNodeType.softBreak ||
            prev.type == ChatMarkdownNodeType.hardBreak) {
          final text = node.attrs['text']?.toString() ?? '';
          final trimmed = text.replaceFirst(RegExp(r'^[ \t]+'), '');
          if (trimmed.isNotEmpty) {
            spans.add(TextSpan(text: trimmed, style: effectiveBaseStyle));
          }
          continue;
        }
      }
      spans.addAll(_buildNode(node, effectiveBaseStyle));
    }
    return spans;
  }

  String plainText(List<ChatMarkdownNode> nodes) {
    final buffer = StringBuffer();
    for (final node in nodes) {
      buffer.write(_plainTextForNode(node));
    }
    return buffer.toString();
  }

  List<InlineSpan> _buildNode(ChatMarkdownNode node, TextStyle baseStyle) {
    switch (node.type) {
      case ChatMarkdownNodeType.text:
        return [
          TextSpan(
            text: node.attrs['text']?.toString() ?? '',
            style: baseStyle,
          ),
        ];
      case ChatMarkdownNodeType.softBreak:
      case ChatMarkdownNodeType.hardBreak:
        return [TextSpan(text: '\n', style: baseStyle)];
      case ChatMarkdownNodeType.emphasis:
        return [
          TextSpan(
            style: baseStyle.copyWith(fontStyle: FontStyle.italic),
            children: buildSpans(node.children, baseStyle: baseStyle),
          ),
        ];
      case ChatMarkdownNodeType.strong:
        return [
          TextSpan(
            style: baseStyle.copyWith(fontWeight: FontWeight.w700),
            children: buildSpans(node.children, baseStyle: baseStyle),
          ),
        ];
      case ChatMarkdownNodeType.strike:
        return [
          TextSpan(
            style: baseStyle.copyWith(decoration: TextDecoration.lineThrough),
            children: buildSpans(node.children, baseStyle: baseStyle),
          ),
        ];
      case ChatMarkdownNodeType.inlineCode:
        return [
          TextSpan(
            text: node.attrs['text']?.toString() ?? plainText(node.children),
            style: baseStyle.merge(styleSheet.inlineCodeStyle),
          ),
        ];
      case ChatMarkdownNodeType.link:
      case ChatMarkdownNodeType.autolink:
        // [CARD PROTOCOL] Entry point 3 of 4 — inline renderer card detection.
        // Strict parse: if decodeGrixUriCard returns null, render as plain text.
        // No workarounds. If the link is not parsed correctly, fix the backend.
        final label = plainText(node.children);
        final href = node.attrs['href']?.toString() ?? '';
        final isGrixCardHref = href.trimLeft().toLowerCase().startsWith(
          'grix://card/',
        );
        final card = ChatMessageCardCodec.decodeGrixUriCard(href);
        if (card != null) {
          return [
            WidgetSpan(
              child: ChatMessageCardView(
                card: card,
                sourceMessageId: sourceMessageId,
                isMine: false,
                fontScale: 1.0,
                onTap: onMessageCardTap == null
                    ? null
                    : () => onMessageCardTap!(card),
                onAction: onMessageCardAction,
                managedInputBinding: managedInputBinding,
                isExecApprovalPending: isExecApprovalPending,
                pickRemoteDirectory: pickRemoteDirectory,
              ),
            ),
          ];
        }
        if (isGrixCardHref) {
          return [
            TextSpan(text: label.isNotEmpty ? label : href, style: baseStyle),
          ];
        }
        if (href.startsWith('#')) {
          return [
            TextSpan(
              text: label.isNotEmpty ? label : href,
              style: baseStyle.merge(styleSheet.linkStyle),
            ),
          ];
        }
        final resolvedUri = ChatMarkdownUriPolicy.resolveSafeLinkUri(href);
        final resolvedLabel = label.isNotEmpty ? label : href;
        if (resolvedUri == null) {
          return [
            TextSpan(text: resolvedLabel, style: baseStyle.merge(styleSheet.linkStyle)),
          ];
        }
        return [
          TextSpan(
            text: resolvedLabel,
            style: baseStyle.merge(styleSheet.linkStyle),
            recognizer: TapGestureRecognizer()
              ..onTap = () => AppExternalLinks.open(resolvedUri.toString()),
            mouseCursor: SystemMouseCursors.click,
          ),
        ];
      case ChatMarkdownNodeType.image:
        final src = node.attrs['src']?.toString() ?? '';
        final alt = node.attrs['alt']?.toString() ?? '';
        if (src.isEmpty) {
          return [TextSpan(text: alt, style: baseStyle)];
        }
        return [
          WidgetSpan(
            alignment: PlaceholderAlignment.middle,
            child: ChatMarkdownImageView(src: src, alt: alt, inline: true),
          ),
        ];
      case ChatMarkdownNodeType.video:
        final videoSrc = node.attrs['src']?.toString() ?? '';
        if (videoSrc.isEmpty) {
          return [TextSpan(text: videoSrc, style: baseStyle)];
        }
        return [
          WidgetSpan(
            alignment: PlaceholderAlignment.middle,
            child: ChatMarkdownVideoView(
              src: videoSrc,
              width: double.tryParse(node.attrs['width']?.toString() ?? ''),
              poster: node.attrs['poster']?.toString(),
              inline: true,
            ),
          ),
        ];
      case ChatMarkdownNodeType.audio:
        final audioSrc = node.attrs['src']?.toString() ?? '';
        if (audioSrc.isEmpty) {
          return [TextSpan(text: audioSrc, style: baseStyle)];
        }
        return [
          WidgetSpan(
            alignment: PlaceholderAlignment.middle,
            child: ChatMarkdownAudioView(
              src: audioSrc,
              title: node.attrs['title']?.toString(),
              inline: true,
            ),
          ),
        ];
      case ChatMarkdownNodeType.mathInline:
        final tex = node.attrs['tex']?.toString() ?? plainText(node.children);
        return [
          WidgetSpan(
            alignment: PlaceholderAlignment.middle,
            child: Math.tex(
              tex,
              textStyle: styleSheet.inlineMathStyle,
              mathStyle: MathStyle.text,
              onErrorFallback: (error) => Text(
                '\$$tex\$',
                style: baseStyle.merge(styleSheet.inlineCodeStyle),
              ),
            ),
          ),
        ];
      case ChatMarkdownNodeType.footnoteRef:
        final label = node.attrs['label']?.toString() ?? '';
        return [
          TextSpan(
            text: '[${label.trim()}]',
            style: baseStyle.merge(styleSheet.footnoteLabelStyle),
          ),
        ];
      case ChatMarkdownNodeType.paragraph:
      case ChatMarkdownNodeType.document:
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
      case ChatMarkdownNodeType.heading:
      case ChatMarkdownNodeType.thematicBreak:
      case ChatMarkdownNodeType.htmlBlockText:
      case ChatMarkdownNodeType.escapedText:
      case ChatMarkdownNodeType.unknown:
        return buildSpans(node.children, baseStyle: baseStyle);
    }
  }

  String _plainTextForNode(ChatMarkdownNode node) {
    switch (node.type) {
      case ChatMarkdownNodeType.text:
        return node.attrs['text']?.toString() ?? '';
      case ChatMarkdownNodeType.inlineCode:
        return node.attrs['text']?.toString() ?? '';
      case ChatMarkdownNodeType.link:
      case ChatMarkdownNodeType.autolink:
      case ChatMarkdownNodeType.paragraph:
      case ChatMarkdownNodeType.document:
      case ChatMarkdownNodeType.blockquote:
      case ChatMarkdownNodeType.list:
      case ChatMarkdownNodeType.listItem:
      case ChatMarkdownNodeType.taskItem:
      case ChatMarkdownNodeType.footnoteDef:
      case ChatMarkdownNodeType.heading:
      case ChatMarkdownNodeType.emphasis:
      case ChatMarkdownNodeType.strong:
      case ChatMarkdownNodeType.strike:
        return plainText(node.children);
      case ChatMarkdownNodeType.softBreak:
      case ChatMarkdownNodeType.hardBreak:
        return '\n';
      case ChatMarkdownNodeType.mathInline:
      case ChatMarkdownNodeType.mathBlock:
        return node.attrs['tex']?.toString() ?? '';
      case ChatMarkdownNodeType.footnoteRef:
        return node.attrs['label']?.toString() ?? '';
      case ChatMarkdownNodeType.image:
        return node.attrs['alt']?.toString() ?? '';
      case ChatMarkdownNodeType.video:
        return '';
      case ChatMarkdownNodeType.audio:
        return node.attrs['title']?.toString() ?? '';
      case ChatMarkdownNodeType.codeBlock:
      case ChatMarkdownNodeType.mermaidBlock:
        return node.attrs['text']?.toString() ?? '';
      case ChatMarkdownNodeType.thematicBreak:
        return '---';
      case ChatMarkdownNodeType.table:
      case ChatMarkdownNodeType.tableHead:
      case ChatMarkdownNodeType.tableBody:
      case ChatMarkdownNodeType.tableRow:
      case ChatMarkdownNodeType.tableCell:
      case ChatMarkdownNodeType.htmlBlockText:
      case ChatMarkdownNodeType.escapedText:
      case ChatMarkdownNodeType.unknown:
        return plainText(node.children);
    }
  }
}
