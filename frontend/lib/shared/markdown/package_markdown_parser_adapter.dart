import 'package:markdown/markdown.dart' as md;

import 'chat_markdown_ast.dart';
import 'chat_markdown_code_language.dart';
import 'chat_markdown_html_codec.dart';
import 'chat_markdown_parser_adapter.dart';

class PackageMarkdownParserAdapter implements ChatMarkdownParserAdapter {
  PackageMarkdownParserAdapter({
    md.ExtensionSet? extensionSet,
    this.blockSyntaxes = const <md.BlockSyntax>[],
    this.inlineSyntaxes = const <md.InlineSyntax>[],
  }) : extensionSet = extensionSet ?? md.ExtensionSet.gitHubFlavored;

  final md.ExtensionSet extensionSet;
  final List<md.BlockSyntax> blockSyntaxes;
  final List<md.InlineSyntax> inlineSyntaxes;

  @override
  ChatMarkdownDocument parse(String markdown) {
    final document = md.Document(
      extensionSet: extensionSet,
      blockSyntaxes: blockSyntaxes,
      inlineSyntaxes: inlineSyntaxes,
    );
    final nodes = document.parseLines(markdown.split('\n'));
    return ChatMarkdownDocument(
      children: nodes.map(_mapNode).toList(growable: false),
    );
  }

  ChatMarkdownNode _mapNode(md.Node node) {
    if (node is md.Text) {
      return ChatMarkdownNode(
        type: ChatMarkdownNodeType.text,
        attrs: <String, Object?>{'text': _decodeText(node.text)},
      );
    }

    if (node is! md.Element) {
      return const ChatMarkdownNode(type: ChatMarkdownNodeType.unknown);
    }

    if (node.tag == 'pre') {
      md.Element? codeElement;
      for (final child in node.children ?? const <md.Node>[]) {
        if (child is md.Element && child.tag == 'code') {
          codeElement = child;
          break;
        }
      }

      final languageClass = codeElement?.attributes['class'];
      final language = resolveCodeFenceLanguageFromClass(languageClass);
      final codeText = _decodeText(
        codeElement?.textContent ?? node.textContent,
      );
      final type = language == 'mermaid'
          ? ChatMarkdownNodeType.mermaidBlock
          : ChatMarkdownNodeType.codeBlock;
      return ChatMarkdownNode(
        type: type,
        attrs: <String, Object?>{
          'language': language.isEmpty ? null : language,
          'text': codeText,
        },
      );
    }

    final type = _mapElementType(node);
    final mappedChildren = _mapChildren(node, type);
    final attrs = <String, Object?>{};

    switch (type) {
      case ChatMarkdownNodeType.heading:
        attrs['level'] = int.tryParse(node.tag.substring(1));
        break;
      case ChatMarkdownNodeType.list:
        attrs['ordered'] = node.tag == 'ol';
        attrs['start'] = int.tryParse(node.attributes['start'] ?? '');
        attrs['containsTaskItems'] = _hasClass(node, 'contains-task-list');
        break;
      case ChatMarkdownNodeType.link:
      case ChatMarkdownNodeType.autolink:
        attrs['href'] = node.attributes['href'];
        attrs['title'] = _decodeNullableText(node.attributes['title']);
        break;
      case ChatMarkdownNodeType.taskItem:
        attrs['checked'] = _isTaskItemChecked(node);
        break;
      case ChatMarkdownNodeType.image:
        attrs['src'] = node.attributes['src'];
        attrs['alt'] = _decodeNullableText(node.attributes['alt']);
        attrs['title'] = _decodeNullableText(node.attributes['title']);
        break;
      case ChatMarkdownNodeType.video:
        attrs['src'] = node.attributes['src'];
        attrs['width'] = node.attributes['width'];
        attrs['poster'] = node.attributes['poster'];
        break;
      case ChatMarkdownNodeType.audio:
        attrs['src'] = node.attributes['src'];
        attrs['title'] = _decodeNullableText(node.attributes['title']);
        break;
      case ChatMarkdownNodeType.tableCell:
        attrs['align'] = node.attributes['align'];
        break;
      case ChatMarkdownNodeType.mathBlock:
      case ChatMarkdownNodeType.mathInline:
        attrs['tex'] = node.attributes['tex'] ?? _decodeText(node.textContent);
        break;
      case ChatMarkdownNodeType.mermaidBlock:
        attrs['text'] = _decodeText(node.textContent);
        break;
      case ChatMarkdownNodeType.footnoteDef:
        attrs['id'] = node.attributes['id'];
        break;
      case ChatMarkdownNodeType.footnoteRef:
        final anchor = _firstChildElement(node, 'a');
        attrs['href'] = anchor?.attributes['href'];
        attrs['id'] = anchor?.attributes['id'];
        attrs['label'] = _decodeText(node.textContent);
        break;
      case ChatMarkdownNodeType.inlineCode:
        attrs['text'] = _decodeText(node.textContent);
        break;
      case ChatMarkdownNodeType.htmlBlockText:
      case ChatMarkdownNodeType.unknown:
        attrs['tag'] = node.tag;
        break;
      case ChatMarkdownNodeType.document:
      case ChatMarkdownNodeType.paragraph:
      case ChatMarkdownNodeType.thematicBreak:
      case ChatMarkdownNodeType.blockquote:
      case ChatMarkdownNodeType.listItem:
      case ChatMarkdownNodeType.codeBlock:
      case ChatMarkdownNodeType.table:
      case ChatMarkdownNodeType.tableHead:
      case ChatMarkdownNodeType.tableBody:
      case ChatMarkdownNodeType.tableRow:
      case ChatMarkdownNodeType.text:
      case ChatMarkdownNodeType.softBreak:
      case ChatMarkdownNodeType.hardBreak:
      case ChatMarkdownNodeType.emphasis:
      case ChatMarkdownNodeType.strong:
      case ChatMarkdownNodeType.strike:
      case ChatMarkdownNodeType.escapedText:
        break;
    }

    return ChatMarkdownNode(type: type, children: mappedChildren, attrs: attrs);
  }

  List<ChatMarkdownNode> _mapChildren(
    md.Element node,
    ChatMarkdownNodeType type,
  ) {
    final children = <ChatMarkdownNode>[];
    for (final child in node.children ?? const <md.Node>[]) {
      if (type == ChatMarkdownNodeType.taskItem &&
          child is md.Element &&
          child.tag == 'input') {
        continue;
      }
      children.add(_mapNode(child));
    }
    return List.unmodifiable(children);
  }

  ChatMarkdownNodeType _mapElementType(md.Element node) {
    switch (node.tag) {
      case 'p':
        return ChatMarkdownNodeType.paragraph;
      case 'blockquote':
        return ChatMarkdownNodeType.blockquote;
      case 'ul':
      case 'ol':
        return ChatMarkdownNodeType.list;
      case 'li':
        if (_isFootnoteDefinition(node)) {
          return ChatMarkdownNodeType.footnoteDef;
        }
        if (_isTaskItem(node)) {
          return ChatMarkdownNodeType.taskItem;
        }
        return ChatMarkdownNodeType.listItem;
      case 'sup':
        if (_hasClass(node, 'footnote-ref')) {
          return ChatMarkdownNodeType.footnoteRef;
        }
        return ChatMarkdownNodeType.unknown;
      case 'section':
        if (_hasClass(node, 'footnotes')) {
          return ChatMarkdownNodeType.document;
        }
        return ChatMarkdownNodeType.unknown;
      case 'code':
        return ChatMarkdownNodeType.inlineCode;
      case 'em':
        return ChatMarkdownNodeType.emphasis;
      case 'strong':
        return ChatMarkdownNodeType.strong;
      case 'del':
        return ChatMarkdownNodeType.strike;
      case 'a':
        return _isAutolink(node)
            ? ChatMarkdownNodeType.autolink
            : ChatMarkdownNodeType.link;
      case 'img':
        return ChatMarkdownNodeType.image;
      case 'chat-video':
        return ChatMarkdownNodeType.video;
      case 'chat-audio':
        return ChatMarkdownNodeType.audio;
      case 'br':
        return ChatMarkdownNodeType.hardBreak;
      case 'hr':
        return ChatMarkdownNodeType.thematicBreak;
      case 'table':
        return ChatMarkdownNodeType.table;
      case 'thead':
        return ChatMarkdownNodeType.tableHead;
      case 'tbody':
        return ChatMarkdownNodeType.tableBody;
      case 'tr':
        return ChatMarkdownNodeType.tableRow;
      case 'th':
      case 'td':
        return ChatMarkdownNodeType.tableCell;
      case 'latex-block':
        return ChatMarkdownNodeType.mathBlock;
      case 'latex-inline':
        return ChatMarkdownNodeType.mathInline;
      case 'mermaid-block':
        return ChatMarkdownNodeType.mermaidBlock;
      default:
        if (RegExp(r'^h[1-6]$').hasMatch(node.tag)) {
          return ChatMarkdownNodeType.heading;
        }
        return ChatMarkdownNodeType.unknown;
    }
  }

  bool _isTaskItem(md.Element node) => _hasClass(node, 'task-list-item');

  bool _isTaskItemChecked(md.Element node) {
    final checkbox = _firstChildElement(node, 'input');
    return checkbox?.attributes.containsKey('checked') ?? false;
  }

  bool _isFootnoteDefinition(md.Element node) =>
      node.attributes['id']?.startsWith('fn-') ?? false;

  bool _isAutolink(md.Element node) {
    final href = node.attributes['href'];
    final children = node.children ?? const <md.Node>[];
    if (href == null || node.attributes.containsKey('title')) {
      return false;
    }
    if (children.length != 1) {
      return false;
    }
    final child = children.first;
    return child is md.Text && child.text == href;
  }

  bool _hasClass(md.Element node, String className) {
    final value = node.attributes['class'];
    if (value == null || value.isEmpty) {
      return false;
    }
    return value.split(' ').contains(className);
  }

  md.Element? _firstChildElement(md.Element node, String tag) {
    for (final child in node.children ?? const <md.Node>[]) {
      if (child is md.Element && child.tag == tag) {
        return child;
      }
    }
    return null;
  }

  String _decodeText(String text) => ChatMarkdownHtmlCodec.decode(text);

  String? _decodeNullableText(String? text) =>
      ChatMarkdownHtmlCodec.decodeNullable(text);
}
