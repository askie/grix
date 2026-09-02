import 'package:markdown/markdown.dart' as md;

/// Shared `<video>` tag recognition for Markdown.
///
/// Supports the wrapped form `<video src="...">...</video>` and the
/// self-closing form `<video src="..." />`. The `src` may live on the
/// `<video>` tag itself or on a nested `<source src="...">` child.
///
/// Both a [md.BlockSyntax] and a [md.InlineSyntax] are provided because a
/// standalone self-closing `<video />` line is otherwise swallowed by the
/// CommonMark HTML-block rule before inline parsing runs.
class ChatMarkdownVideoSyntax {
  const ChatMarkdownVideoSyntax._();

  static final RegExp tagPattern = RegExp(
    r'<video\b([^>]*?)\s*/?>(?:([\s\S]*?)</video\s*>)?',
    caseSensitive: false,
  );

  static final RegExp _attributePattern = RegExp(
    '''([a-zA-Z][a-zA-Z0-9-]*)\\s*=\\s*(?:"([^"]*)"|'([^']*)')''',
  );

  static final RegExp _sourcePattern = RegExp(
    r'<source\b([^>]*)>',
    caseSensitive: false,
  );

  /// Builds a `chat-video` element from a matched `<video>` tag.
  static md.Element buildElement({
    required String tagAttrs,
    required String innerContent,
  }) {
    final attributes = _parseAttributes(tagAttrs);
    final src = attributes['src'] ?? _firstSourceSrc(innerContent);

    final element = md.Element.empty('chat-video');
    if (src != null && src.isNotEmpty) {
      element.attributes['src'] = src;
    }
    final width = attributes['width'];
    if (width != null && width.isNotEmpty) {
      element.attributes['width'] = width;
    }
    final poster = attributes['poster'];
    if (poster != null && poster.isNotEmpty) {
      element.attributes['poster'] = poster;
    }
    return element;
  }

  static Map<String, String> _parseAttributes(String raw) {
    final result = <String, String>{};
    for (final match in _attributePattern.allMatches(raw)) {
      final name = match.group(1)!.toLowerCase();
      final value = match.group(2) ?? match.group(3) ?? '';
      result[name] = value;
    }
    return result;
  }

  static String? _firstSourceSrc(String innerContent) {
    final sourceTag = _sourcePattern.firstMatch(innerContent);
    if (sourceTag == null) {
      return null;
    }
    return _parseAttributes(sourceTag.group(1) ?? '')['src'];
  }
}

/// Matches `<video>` embedded within paragraph text.
class ChatMarkdownVideoInlineSyntax extends md.InlineSyntax {
  ChatMarkdownVideoInlineSyntax()
    : super(ChatMarkdownVideoSyntax.tagPattern.pattern, caseSensitive: false);

  @override
  bool onMatch(md.InlineParser parser, Match match) {
    parser.addNode(
      ChatMarkdownVideoSyntax.buildElement(
        tagAttrs: match.group(1) ?? '',
        innerContent: match.group(2) ?? '',
      ),
    );
    return true;
  }
}

/// Matches a `<video>` tag that starts its own block, including the
/// self-closing form that the HTML-block rule would otherwise consume.
class ChatMarkdownVideoBlockSyntax extends md.BlockSyntax {
  const ChatMarkdownVideoBlockSyntax();

  static final RegExp _startPattern = RegExp(
    r'^\s*<video\b',
    caseSensitive: false,
  );
  static final RegExp _closePattern = RegExp(
    r'</video\s*>',
    caseSensitive: false,
  );
  static final RegExp _selfClosePattern = RegExp(
    r'<video\b[^>]*/>',
    caseSensitive: false,
  );

  @override
  RegExp get pattern => _startPattern;

  @override
  md.Node? parse(md.BlockParser parser) {
    final buffer = StringBuffer();
    while (!parser.isDone) {
      final line = parser.current.content;
      // A blank line ends the block so an unclosed tag can't swallow the rest.
      if (line.trim().isEmpty && buffer.isNotEmpty) {
        break;
      }
      buffer.write(line);
      buffer.write('\n');
      parser.advance();
      if (_closePattern.hasMatch(line) || _selfClosePattern.hasMatch(line)) {
        break;
      }
    }

    final raw = buffer.toString();
    final match = ChatMarkdownVideoSyntax.tagPattern.firstMatch(raw);
    if (match == null) {
      return md.Element('p', [md.Text(raw.trim())]);
    }
    return ChatMarkdownVideoSyntax.buildElement(
      tagAttrs: match.group(1) ?? '',
      innerContent: match.group(2) ?? '',
    );
  }
}
