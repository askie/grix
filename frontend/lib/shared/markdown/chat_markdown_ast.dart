enum ChatMarkdownNodeType {
  document,
  heading,
  paragraph,
  thematicBreak,
  blockquote,
  list,
  listItem,
  taskItem,
  codeBlock,
  table,
  tableHead,
  tableBody,
  tableRow,
  tableCell,
  mathBlock,
  mermaidBlock,
  htmlBlockText,
  footnoteDef,
  text,
  softBreak,
  hardBreak,
  emphasis,
  strong,
  strike,
  inlineCode,
  link,
  image,
  video,
  audio,
  mathInline,
  autolink,
  footnoteRef,
  escapedText,
  unknown,
}

class ChatMarkdownSourceRange {
  const ChatMarkdownSourceRange({required this.start, required this.end});

  final int start;
  final int end;
}

class ChatMarkdownNode {
  const ChatMarkdownNode({
    required this.type,
    this.children = const <ChatMarkdownNode>[],
    this.attrs = const <String, Object?>{},
    this.sourceRange,
    this.normalized = true,
    this.fallbackReason,
  });

  final ChatMarkdownNodeType type;
  final List<ChatMarkdownNode> children;
  final Map<String, Object?> attrs;
  final ChatMarkdownSourceRange? sourceRange;
  final bool normalized;
  final String? fallbackReason;
}

class ChatMarkdownDocument extends ChatMarkdownNode {
  const ChatMarkdownDocument({required super.children})
    : super(type: ChatMarkdownNodeType.document);
}
