import 'chat_markdown_ast.dart';

enum ChatMarkdownFeature {
  heading,
  thematicBreak,
  blockquote,
  list,
  taskList,
  codeBlock,
  table,
  math,
  mermaid,
  link,
  image,
  video,
  audio,
  inlineFormatting,
  footnote,
  html,
}

class ChatMarkdownSemanticSummary {
  const ChatMarkdownSemanticSummary({
    this.features = const <ChatMarkdownFeature>{},
  });

  final Set<ChatMarkdownFeature> features;

  bool get requiresRichRendering => features.isNotEmpty;
  bool get hasMath => features.contains(ChatMarkdownFeature.math);
  bool get hasImages => features.contains(ChatMarkdownFeature.image);
  bool get hasVideos => features.contains(ChatMarkdownFeature.video);
  bool get hasAudios => features.contains(ChatMarkdownFeature.audio);
  bool get hasCodeBlocks => features.contains(ChatMarkdownFeature.codeBlock);
  bool get hasMermaidBlocks => features.contains(ChatMarkdownFeature.mermaid);
  bool get hasTables => features.contains(ChatMarkdownFeature.table);
  bool get hasLinks => features.contains(ChatMarkdownFeature.link);
  bool get hasTaskLists => features.contains(ChatMarkdownFeature.taskList);
  bool get hasFootnotes => features.contains(ChatMarkdownFeature.footnote);
  bool get hasInlineFormatting =>
      features.contains(ChatMarkdownFeature.inlineFormatting);

  bool hasFeature(ChatMarkdownFeature feature) => features.contains(feature);
}

class ChatMarkdownSemanticAnalyzer {
  const ChatMarkdownSemanticAnalyzer();

  ChatMarkdownSemanticSummary analyze(ChatMarkdownDocument document) {
    final features = <ChatMarkdownFeature>{};
    _visit(document, features);
    return ChatMarkdownSemanticSummary(features: Set.unmodifiable(features));
  }

  void _visit(
    ChatMarkdownNode node,
    Set<ChatMarkdownFeature> features,
  ) {
    final feature = _mapFeature(node.type);
    if (feature != null) {
      features.add(feature);
    }
    for (final child in node.children) {
      _visit(child, features);
    }
  }

  ChatMarkdownFeature? _mapFeature(ChatMarkdownNodeType type) {
    switch (type) {
      case ChatMarkdownNodeType.heading:
        return ChatMarkdownFeature.heading;
      case ChatMarkdownNodeType.thematicBreak:
        return ChatMarkdownFeature.thematicBreak;
      case ChatMarkdownNodeType.blockquote:
        return ChatMarkdownFeature.blockquote;
      case ChatMarkdownNodeType.list:
      case ChatMarkdownNodeType.listItem:
        return ChatMarkdownFeature.list;
      case ChatMarkdownNodeType.taskItem:
        return ChatMarkdownFeature.taskList;
      case ChatMarkdownNodeType.codeBlock:
        return ChatMarkdownFeature.codeBlock;
      case ChatMarkdownNodeType.table:
      case ChatMarkdownNodeType.tableHead:
      case ChatMarkdownNodeType.tableBody:
      case ChatMarkdownNodeType.tableRow:
      case ChatMarkdownNodeType.tableCell:
        return ChatMarkdownFeature.table;
      case ChatMarkdownNodeType.mathBlock:
      case ChatMarkdownNodeType.mathInline:
        return ChatMarkdownFeature.math;
      case ChatMarkdownNodeType.mermaidBlock:
        return ChatMarkdownFeature.mermaid;
      case ChatMarkdownNodeType.link:
      case ChatMarkdownNodeType.autolink:
        return ChatMarkdownFeature.link;
      case ChatMarkdownNodeType.image:
        return ChatMarkdownFeature.image;
      case ChatMarkdownNodeType.video:
        return ChatMarkdownFeature.video;
      case ChatMarkdownNodeType.audio:
        return ChatMarkdownFeature.audio;
      case ChatMarkdownNodeType.emphasis:
      case ChatMarkdownNodeType.strong:
      case ChatMarkdownNodeType.strike:
      case ChatMarkdownNodeType.inlineCode:
      case ChatMarkdownNodeType.hardBreak:
        return ChatMarkdownFeature.inlineFormatting;
      case ChatMarkdownNodeType.footnoteDef:
      case ChatMarkdownNodeType.footnoteRef:
        return ChatMarkdownFeature.footnote;
      case ChatMarkdownNodeType.htmlBlockText:
        return ChatMarkdownFeature.html;
      case ChatMarkdownNodeType.document:
      case ChatMarkdownNodeType.paragraph:
      case ChatMarkdownNodeType.text:
      case ChatMarkdownNodeType.softBreak:
      case ChatMarkdownNodeType.escapedText:
      case ChatMarkdownNodeType.unknown:
        return null;
    }
  }
}
