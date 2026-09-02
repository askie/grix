import 'chat_markdown_ast.dart';
import 'chat_markdown_lexer.dart';
import 'chat_markdown_parser_adapter.dart';
import 'chat_markdown_segment.dart';
import 'chat_markdown_semantics.dart';

class ChatMarkdownSection {
  const ChatMarkdownSection({
    required this.text,
    required this.startOffset,
    required this.endOffset,
  });

  final String text;
  final int startOffset;
  final int endOffset;
}

class ChatMarkdownSectionSplitter {
  const ChatMarkdownSectionSplitter({this.lexer = const ChatMarkdownLexer()});

  final ChatMarkdownLexer lexer;

  List<ChatMarkdownSection> split(String text) {
    if (text.isEmpty) {
      return const <ChatMarkdownSection>[];
    }

    final segments = lexer.lex(text);
    final sections = <ChatMarkdownSection>[];
    var cursor = 0;

    // Split on double-newlines that are outside fenced code blocks
    final splitPoints = <int>[];
    for (var i = 0; i < text.length - 1; i++) {
      if (text[i] == '\n' &&
          text[i + 1] == '\n' &&
          !_isInsideFencedCode(segments, i)) {
        splitPoints.add(i);
      }
    }

    if (splitPoints.isEmpty) {
      return <ChatMarkdownSection>[
        ChatMarkdownSection(text: text, startOffset: 0, endOffset: text.length),
      ];
    }

    for (final point in splitPoints) {
      if (point > cursor) {
        final sectionText = text.substring(cursor, point).trim();
        if (sectionText.isNotEmpty) {
          sections.add(
            ChatMarkdownSection(
              text: sectionText,
              startOffset: cursor,
              endOffset: point,
            ),
          );
        }
      }
      cursor = point + 2; // skip the \n\n
    }

    // Remaining text
    if (cursor < text.length) {
      final sectionText = text.substring(cursor).trim();
      if (sectionText.isNotEmpty) {
        sections.add(
          ChatMarkdownSection(
            text: sectionText,
            startOffset: cursor,
            endOffset: text.length,
          ),
        );
      }
    }

    return sections;
  }

  bool _isInsideFencedCode(List<ChatMarkdownSegment> segments, int offset) {
    for (final segment in segments) {
      if (segment.type == ChatMarkdownSegmentType.fencedCode &&
          offset >= segment.start &&
          offset < segment.end) {
        return true;
      }
    }
    return false;
  }
}

class ChatMarkdownSectionRenderer {
  const ChatMarkdownSectionRenderer({
    required this.parser,
    required this.semanticAnalyzer,
    this.splitter = const ChatMarkdownSectionSplitter(),
  });

  final ChatMarkdownParserAdapter parser;
  final ChatMarkdownSemanticAnalyzer semanticAnalyzer;
  final ChatMarkdownSectionSplitter splitter;

  ChatMarkdownDocument? tryPartialParse(String text) {
    final sections = splitter.split(text);
    if (sections.isEmpty) {
      return null;
    }

    final allChildren = <ChatMarkdownNode>[];
    var anyRichSection = false;

    for (final section in sections) {
      try {
        final doc = parser.parse(section.text);
        final semantics = semanticAnalyzer.analyze(doc);
        if (semantics.requiresRichRendering) {
          anyRichSection = true;
        }
        allChildren.addAll(doc.children);
      } catch (_) {
        // Section failed to parse: insert as plain text fallback node
        allChildren.add(
          ChatMarkdownNode(
            type: ChatMarkdownNodeType.paragraph,
            children: <ChatMarkdownNode>[
              ChatMarkdownNode(
                type: ChatMarkdownNodeType.text,
                attrs: <String, Object?>{'text': section.text},
              ),
            ],
            fallbackReason: 'Section parse failed',
          ),
        );
      }
    }

    if (!anyRichSection) {
      return null;
    }

    return ChatMarkdownDocument(children: allChildren);
  }
}
