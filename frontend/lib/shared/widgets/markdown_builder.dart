import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:markdown_widget/markdown_widget.dart';
import 'package:markdown/markdown.dart' as md;

import '../utils/app_external_links.dart';
import '../../modules/chat/message_cards/models/chat_message_card_action.dart';
import '../../modules/chat/message_cards/models/chat_message_card_data.dart';
import '../../modules/chat/message_cards/services/chat_message_card_codec.dart';
import '../../modules/chat/message_cards/widgets/chat_message_card_view.dart';
import '../../modules/chat/services/chat_managed_input.dart';
import '../markdown/chat_markdown_code_language.dart';
import '../markdown/chat_markdown_html_codec.dart';
import '../markdown/chat_markdown_uri_policy.dart';
import 'chat_markdown_code_block_view.dart';
import 'chat_markdown_image_view.dart';
import 'chat_markdown_mermaid_view.dart';
import 'chat_markdown_style_sheet.dart';

SpanNodeGeneratorWithTag customLinkGenerator({
  ChatMessageCardActionHandler? onMessageCardAction,
  ValueChanged<ChatMessageCardData>? onMessageCardTap,
  String sourceMessageId = '',
  ChatManagedInputBinding? managedInputBinding,
  bool Function(String approvalId)? isExecApprovalPending,
  Future<String?> Function()? pickRemoteDirectory,
  ValueChanged<String>? onAgentFilePathTap,
}) {
  return SpanNodeGeneratorWithTag(
    tag: 'a',
    generator: (e, config, visitor) => CustomLinkNode(
      e.attributes,
      config.a,
      onMessageCardAction: onMessageCardAction,
      onMessageCardTap: onMessageCardTap,
      sourceMessageId: sourceMessageId,
      managedInputBinding: managedInputBinding,
      isExecApprovalPending: isExecApprovalPending,
      pickRemoteDirectory: pickRemoteDirectory,
      onAgentFilePathTap: onAgentFilePathTap,
    ),
  );
}

SpanNodeGeneratorWithTag customImageGenerator() {
  return SpanNodeGeneratorWithTag(
    tag: 'img',
    generator: (e, config, visitor) => CustomImageNode(e.attributes),
  );
}

class CustomImageNode extends SpanNode {
  final Map<String, String> attributes;

  CustomImageNode(this.attributes);

  @override
  InlineSpan build() {
    final src = attributes['src'];
    if (src != null) {
      return WidgetSpan(
        child: ChatMarkdownImageView(src: src, alt: attributes['alt']),
      );
    }
    return const TextSpan(text: "");
  }
}

class CustomLinkNode extends ElementNode {
  CustomLinkNode(
    this.attributes,
    this.linkConfig, {
    this.onMessageCardAction,
    this.onMessageCardTap,
    this.sourceMessageId = '',
    this.managedInputBinding,
    this.isExecApprovalPending,
    this.pickRemoteDirectory,
    this.onAgentFilePathTap,
  });

  final Map<String, String> attributes;
  final LinkConfig linkConfig;
  final ChatMessageCardActionHandler? onMessageCardAction;
  final ValueChanged<ChatMessageCardData>? onMessageCardTap;
  final String sourceMessageId;
  final ChatManagedInputBinding? managedInputBinding;
  final bool Function(String approvalId)? isExecApprovalPending;
  final Future<String?> Function()? pickRemoteDirectory;
  final ValueChanged<String>? onAgentFilePathTap;

  @override
  InlineSpan build() {
    final href = attributes['href'] ?? '';
    final agentFilePath = ChatMarkdownUriPolicy.resolveAgentFilePath(href);
    if (agentFilePath != null && onAgentFilePathTap != null) {
      return TextSpan(
        style: parentStyle?.merge(linkConfig.style) ?? linkConfig.style,
        children: [
          for (final child in children)
            _toLinkInlineSpan(
              child.build(),
              () => onAgentFilePathTap!(agentFilePath),
            ),
          if (children.isNotEmpty) const TextSpan(text: ' '),
        ],
      );
    }
    final safeUri = ChatMarkdownUriPolicy.resolveSafeLinkUri(href);
    if (safeUri == null) {
      return TextSpan(
        style: parentStyle?.merge(linkConfig.style) ?? linkConfig.style,
        children: [for (final child in children) child.build()],
      );
    }
    // [CARD PROTOCOL] Entry point 4 of 4 — fallback renderer card detection.
    // Strict parse: if decodeGrixUriCard returns null, render as plain link.
    // No workarounds. If the link is not parsed correctly, fix the backend.
    if (safeUri.scheme == 'grix' && safeUri.host == 'card') {
      final card = ChatMessageCardCodec.decodeGrixUriCard(href);
      if (card != null) {
        return WidgetSpan(
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
        );
      }
      return TextSpan(
        style: parentStyle?.merge(linkConfig.style) ?? linkConfig.style,
        children: [for (final child in children) child.build()],
      );
    }

    return TextSpan(
      style: parentStyle?.merge(linkConfig.style) ?? linkConfig.style,
      children: [
        for (final child in children)
          _toLinkInlineSpan(child.build(), () => _launchLink(safeUri)),
        if (children.isNotEmpty) const TextSpan(text: ' '),
      ],
    );
  }

  Future<void> _launchLink(Uri uri) async {
    // 收口到统一外链入口：http/https 会先过链接黑名单校验（恶意拦死 / 可疑提示）。
    await AppExternalLinks.open(uri.toString());
  }
}

class CustomPreNode extends ElementNode {
  final md.Element element;
  final PreConfig preConfig;
  final ChatMarkdownStyleSheet styleSheet;

  CustomPreNode(this.element, this.preConfig, this.styleSheet);

  @override
  InlineSpan build() {
    final content = ChatMarkdownHtmlCodec.decode(element.textContent);
    final language = _resolveLanguage();

    if (language == 'mermaid') {
      return WidgetSpan(
        child: ChatMarkdownMermaidView(
          source: content,
          textStyle: style,
          decoration: preConfig.decoration,
          padding: preConfig.padding,
          margin: preConfig.margin,
          backgroundColor: _backgroundColor(),
        ),
      );
    }

    final widget = ChatMarkdownCodeBlockView(
      code: content,
      language: language,
      styleSheet: styleSheet,
    );

    return WidgetSpan(
      child: preConfig.wrapper?.call(widget, content, language) ?? widget,
    );
  }

  @override
  TextStyle get style => preConfig.textStyle;

  Color _backgroundColor() {
    final decoration = preConfig.decoration;
    if (decoration case BoxDecoration(:final color?)) {
      return color;
    }
    return Colors.white;
  }

  String _resolveLanguage() {
    try {
      final codeElement = element.children?.first;
      if (codeElement is md.Element &&
          codeElement.attributes.containsKey('class')) {
        return resolveCodeFenceLanguageFromClass(
          codeElement.attributes['class'],
        );
      }
    } catch (_) {
      // Fall through to text.
    }
    return normalizeCodeFenceLanguage(preConfig.language);
  }
}

InlineSpan _toLinkInlineSpan(InlineSpan span, VoidCallback onTap) {
  if (span is TextSpan) {
    return TextSpan(
      text: span.text,
      children: span.children?.map((e) => _toLinkInlineSpan(e, onTap)).toList(),
      style: span.style,
      recognizer: TapGestureRecognizer()..onTap = onTap,
      mouseCursor: SystemMouseCursors.click,
      onEnter: span.onEnter,
      onExit: span.onExit,
      semanticsLabel: span.semanticsLabel,
      locale: span.locale,
      spellOut: span.spellOut,
    );
  }
  if (span is WidgetSpan) {
    return WidgetSpan(
      child: InkWell(onTap: onTap, child: span.child),
      alignment: span.alignment,
      baseline: span.baseline,
      style: span.style,
    );
  }
  return span;
}
