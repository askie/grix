import 'package:markdown/markdown.dart' as md;

class LatexEnvironmentBlockSyntax extends md.BlockSyntax {
  static const Set<String> _mathEnvironments = {
    'equation',
    'equation*',
    'align',
    'align*',
    'aligned',
    'gather',
    'gather*',
    'multline',
    'multline*',
    'flalign',
    'flalign*',
    'alignat',
    'alignat*',
    'split',
    'cases',
    'matrix',
    'pmatrix',
    'bmatrix',
    'Bmatrix',
    'vmatrix',
    'Vmatrix',
    'smallmatrix',
  };
  static final RegExp _openPattern = RegExp(
    r'^\s*\\begin\{([A-Za-z*]+)\}(.*)$',
  );

  const LatexEnvironmentBlockSyntax();

  @override
  RegExp get pattern => _openPattern;

  @override
  bool canParse(md.BlockParser parser) {
    final open = _parseOpen(parser.current.content);
    if (open == null || !_mathEnvironments.contains(open.environment)) {
      return false;
    }
    if (_containsCloseToken(open.trailingText, open.environment)) {
      return true;
    }
    return _findClosingOffset(parser: parser, environment: open.environment) !=
        null;
  }

  @override
  md.Node? parse(md.BlockParser parser) {
    final open = _parseOpen(parser.current.content);
    if (open == null || !_mathEnvironments.contains(open.environment)) {
      return null;
    }

    final currentLine = parser.current.content;
    if (_containsCloseToken(open.trailingText, open.environment)) {
      parser.advance();
      return _buildLatexBlock(currentLine.trim());
    }

    final closingOffset = _findClosingOffset(
      parser: parser,
      environment: open.environment,
    );
    if (closingOffset == null) {
      return null;
    }

    final buffer = StringBuffer();
    while (!parser.isDone) {
      final line = parser.current.content;
      if (buffer.isNotEmpty) {
        buffer.writeln();
      }
      buffer.write(line);

      final isClosingLine = _containsCloseToken(line, open.environment);
      parser.advance();
      if (isClosingLine) {
        break;
      }
    }

    final tex = buffer.toString().trim();
    if (tex.isEmpty) {
      return null;
    }
    return _buildLatexBlock(tex);
  }

  _LatexEnvironmentOpen? _parseOpen(String line) {
    final match = _openPattern.firstMatch(line);
    if (match == null) {
      return null;
    }
    final environment = match.group(1);
    if (environment == null || environment.isEmpty) {
      return null;
    }
    return _LatexEnvironmentOpen(
      environment: environment,
      trailingText: match.group(2) ?? '',
    );
  }

  int? _findClosingOffset({
    required md.BlockParser parser,
    required String environment,
  }) {
    var offset = 1;
    while (true) {
      final line = parser.peek(offset);
      if (line == null) {
        return null;
      }
      if (_containsCloseToken(line.content, environment)) {
        return offset;
      }
      offset += 1;
    }
  }

  bool _containsCloseToken(String line, String environment) {
    final escaped = RegExp.escape(environment);
    return RegExp(r'\\end\{' + escaped + r'\}').hasMatch(line);
  }

  md.Element _buildLatexBlock(String tex) {
    final element = md.Element('latex-block', [md.Text(tex)]);
    element.attributes['tex'] = tex;
    return element;
  }
}

class _LatexEnvironmentOpen {
  const _LatexEnvironmentOpen({
    required this.environment,
    required this.trailingText,
  });

  final String environment;
  final String trailingText;
}

class LatexBlockSyntax extends md.BlockSyntax {
  static final RegExp _anyOpenPattern = RegExp(r'^\s*(\$\$|\\\[)');
  static final RegExp _dollarOpenPattern = RegExp(r'^\s*\$\$\s*$');
  static final RegExp _dollarOpenWithContentPattern = RegExp(r'^\s*\$\$(.+)$');
  static final RegExp _dollarInlinePattern = RegExp(r'^\s*\$\$(.+?)\$\$\s*$');
  static final RegExp _dollarClosePattern = RegExp(r'^\s*\$\$\s*$');
  static final RegExp _dollarCloseWithSuffixPattern = RegExp(r'^(.*)\$\$\s*$');
  static final RegExp _bracketInlinePattern = RegExp(r'^\s*\\\[(.+?)\\\]\s*$');
  static final RegExp _bracketOpenPattern = RegExp(r'^\s*\\\[\s*$');
  static final RegExp _bracketClosePattern = RegExp(r'^\s*\\\]\s*$');

  const LatexBlockSyntax();

  @override
  RegExp get pattern => _anyOpenPattern;

  @override
  bool canParse(md.BlockParser parser) {
    if (_dollarInlinePattern.hasMatch(parser.current.content)) {
      return true;
    }
    if (_bracketInlinePattern.hasMatch(parser.current.content)) {
      return true;
    }
    if (_dollarOpenWithContentPattern.hasMatch(parser.current.content)) {
      return _findDollarCloseOffset(parser: parser) != null;
    }
    if (_dollarOpenPattern.hasMatch(parser.current.content)) {
      return _findClosingOffset(
            parser: parser,
            closingPattern: _dollarClosePattern,
          ) !=
          null;
    }
    if (_bracketOpenPattern.hasMatch(parser.current.content)) {
      return _findClosingOffset(
            parser: parser,
            closingPattern: _bracketClosePattern,
          ) !=
          null;
    }
    return false;
  }

  @override
  md.Node? parse(md.BlockParser parser) {
    final inlineMatch = _dollarInlinePattern.firstMatch(parser.current.content);
    if (inlineMatch != null) {
      final tex = inlineMatch.group(1)!.trim();
      parser.advance();
      final element = md.Element('latex-block', [md.Text(tex)]);
      element.attributes['tex'] = tex;
      return element;
    }

    final bracketInlineMatch = _bracketInlinePattern.firstMatch(
      parser.current.content,
    );
    if (bracketInlineMatch != null) {
      final tex = bracketInlineMatch.group(1)!.trim();
      parser.advance();
      final element = md.Element('latex-block', [md.Text(tex)]);
      element.attributes['tex'] = tex;
      return element;
    }

    final openWithContentMatch = _dollarOpenWithContentPattern.firstMatch(
      parser.current.content,
    );
    if (openWithContentMatch != null) {
      return _parseDollarWrappedMultiline(parser, openWithContentMatch);
    }

    final openLine = parser.current.content;
    final isDollar = _dollarOpenPattern.hasMatch(openLine);
    final isBracket = _bracketOpenPattern.hasMatch(openLine);
    if (!isDollar && !isBracket) {
      return null;
    }

    final closingPattern = isDollar
        ? _dollarClosePattern
        : _bracketClosePattern;
    final closingOffset = _findClosingOffset(
      parser: parser,
      closingPattern: closingPattern,
    );
    if (closingOffset == null) {
      return null;
    }

    parser.advance();
    final buffer = StringBuffer();

    while (!parser.isDone) {
      final line = parser.current.content;
      if (closingPattern.hasMatch(line)) {
        parser.advance();
        break;
      }
      if (buffer.isNotEmpty) buffer.writeln();
      buffer.write(line);
      parser.advance();
    }

    final tex = buffer.toString().trim();
    if (tex.isEmpty) return null;

    final element = md.Element('latex-block', [md.Text(tex)]);
    element.attributes['tex'] = tex;
    return element;
  }

  md.Node? _parseDollarWrappedMultiline(
    md.BlockParser parser,
    RegExpMatch openWithContentMatch,
  ) {
    final closingOffset = _findDollarCloseOffset(parser: parser);
    if (closingOffset == null) {
      return null;
    }

    final buffer = StringBuffer();
    final firstLineTail = openWithContentMatch.group(1)?.trimRight() ?? '';
    if (firstLineTail.isNotEmpty) {
      buffer.write(firstLineTail);
    }
    parser.advance();

    while (!parser.isDone) {
      final line = parser.current.content;
      final closeMatch = _dollarCloseWithSuffixPattern.firstMatch(line);
      if (closeMatch != null) {
        final beforeClose = (closeMatch.group(1) ?? '').trimRight();
        if (beforeClose.isNotEmpty) {
          if (buffer.isNotEmpty) {
            buffer.writeln();
          }
          buffer.write(beforeClose);
        }
        parser.advance();
        break;
      }
      if (buffer.isNotEmpty) {
        buffer.writeln();
      }
      buffer.write(line);
      parser.advance();
    }

    final tex = buffer.toString().trim();
    if (tex.isEmpty) {
      return null;
    }
    final element = md.Element('latex-block', [md.Text(tex)]);
    element.attributes['tex'] = tex;
    return element;
  }

  int? _findClosingOffset({
    required md.BlockParser parser,
    required RegExp closingPattern,
  }) {
    var offset = 1;
    while (true) {
      final line = parser.peek(offset);
      if (line == null) {
        return null;
      }
      if (closingPattern.hasMatch(line.content)) {
        return offset;
      }
      offset += 1;
    }
  }

  int? _findDollarCloseOffset({required md.BlockParser parser}) {
    var offset = 1;
    while (true) {
      final line = parser.peek(offset);
      if (line == null) {
        return null;
      }
      if (_dollarCloseWithSuffixPattern.hasMatch(line.content)) {
        return offset;
      }
      offset += 1;
    }
  }
}

class LatexInlineSyntax extends md.InlineSyntax {
  LatexInlineSyntax()
    : super(
        r'\\\[(.+?)\\\]|\\\((.+?)\\\)|\$\$([^\$]+?)\$\$|\$([^\s\$][^\$]*?[^\s\$])\$(?![0-9\$])|\$([^\s\$])\$(?![0-9\$])',
      );

  @override
  bool onMatch(md.InlineParser parser, Match match) {
    final bracketDisplayTex = match.group(1);
    final escapedInlineTex = match.group(2);
    final displayTex = match.group(3);
    final inlineTex = escapedInlineTex ?? match.group(4) ?? match.group(5);

    if (bracketDisplayTex != null) {
      final element = md.Element('latex-block', [
        md.Text(bracketDisplayTex.trim()),
      ]);
      element.attributes['tex'] = bracketDisplayTex.trim();
      parser.addNode(element);
    } else if (escapedInlineTex != null) {
      final element = md.Element('latex-inline', [
        md.Text(escapedInlineTex.trim()),
      ]);
      element.attributes['tex'] = escapedInlineTex.trim();
      parser.addNode(element);
    } else if (displayTex != null) {
      final element = md.Element('latex-block', [md.Text(displayTex.trim())]);
      element.attributes['tex'] = displayTex.trim();
      parser.addNode(element);
    } else if (inlineTex != null) {
      final element = md.Element('latex-inline', [md.Text(inlineTex.trim())]);
      element.attributes['tex'] = inlineTex.trim();
      parser.addNode(element);
    }
    return true;
  }
}
