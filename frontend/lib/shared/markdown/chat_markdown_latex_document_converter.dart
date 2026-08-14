class ChatMarkdownLatexDocumentConverter {
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

  const ChatMarkdownLatexDocumentConverter();

  String convert(String input) {
    if (input.isEmpty) {
      return '';
    }

    final lines = input.split('\n');
    final output = <String>[];
    final listStack = <_LatexListKind>[];
    var quoteDepth = 0;

    void appendLine(String line) {
      if (line.trim().isEmpty) {
        output.add('');
        return;
      }
      if (quoteDepth <= 0) {
        output.add(line);
        return;
      }
      output.add('${'> ' * quoteDepth}$line');
    }

    var inDollarDisplayMath = false;
    var inBracketDisplayMath = false;
    var mathEnvironmentDepth = 0;
    var documentFenceOpen = false;
    String? title;
    String? author;
    String? date;

    for (final rawLine in lines) {
      final escapedNormalizedLine = _normalizeEscapedCommandPrefixes(rawLine);
      final normalizedLine =
          _normalizeWrappedMathEnvironmentBoundary(escapedNormalizedLine);
      final trimmed = normalizedLine.trim();

      final beginEnvironment = _readBeginEnvironment(trimmed);
      final endEnvironment = _readEndEnvironment(trimmed);
      final entersMathEnvironment = beginEnvironment != null &&
          _mathEnvironments.contains(beginEnvironment);
      final exitsMathEnvironment =
          endEnvironment != null && _mathEnvironments.contains(endEnvironment);
      final startsBracketDisplayMath = trimmed == r'\[';
      final endsBracketDisplayMath = trimmed == r'\]';
      final standaloneDollarFence = _isStandaloneDollarFence(trimmed);

      final inMathContext = inDollarDisplayMath ||
          inBracketDisplayMath ||
          mathEnvironmentDepth > 0 ||
          entersMathEnvironment ||
          startsBracketDisplayMath ||
          endsBracketDisplayMath ||
          standaloneDollarFence;
      if (inMathContext) {
        appendLine(normalizedLine);
        if (standaloneDollarFence) {
          inDollarDisplayMath = !inDollarDisplayMath;
        }
        if (startsBracketDisplayMath) {
          inBracketDisplayMath = true;
        } else if (endsBracketDisplayMath) {
          inBracketDisplayMath = false;
        }
        if (entersMathEnvironment) {
          mathEnvironmentDepth += 1;
        }
        if (exitsMathEnvironment && mathEnvironmentDepth > 0) {
          mathEnvironmentDepth -= 1;
        }
        continue;
      }

      if (trimmed.isEmpty) {
        appendLine('');
        continue;
      }

      if (trimmed.startsWith(r'$$%')) {
        documentFenceOpen = true;
        final comment = _stripCommentPrefix(trimmed.substring(2).trimLeft());
        if (comment.isNotEmpty) {
          appendLine(comment);
        }
        continue;
      }
      if (documentFenceOpen && trimmed == r'$$') {
        documentFenceOpen = false;
        continue;
      }

      if (_isCommentLine(trimmed)) {
        final comment = _stripCommentPrefix(trimmed);
        if (comment.isNotEmpty) {
          appendLine(comment);
        }
        continue;
      }

      if (_isDocumentClassLine(trimmed) || _isUsePackageLine(trimmed)) {
        continue;
      }
      if (_isDocumentBoundaryLine(trimmed)) {
        continue;
      }

      final extractedTitle = _extractSingleArgCommand(trimmed, 'title');
      if (extractedTitle != null) {
        title = _convertInlineCommands(extractedTitle);
        continue;
      }
      final extractedAuthor = _extractSingleArgCommand(trimmed, 'author');
      if (extractedAuthor != null) {
        author = _convertInlineCommands(extractedAuthor);
        continue;
      }
      final extractedDate = _extractSingleArgCommand(trimmed, 'date');
      if (extractedDate != null) {
        date = _convertInlineCommands(extractedDate);
        continue;
      }

      if (_isMakeTitleLine(trimmed)) {
        for (final line in _buildTitleBlock(
          title: title,
          author: author,
          date: date,
        )) {
          appendLine(line);
        }
        continue;
      }

      if (beginEnvironment == 'itemize') {
        listStack.add(_LatexListKind.unordered);
        continue;
      }
      if (beginEnvironment == 'enumerate') {
        listStack.add(_LatexListKind.ordered);
        continue;
      }
      if (beginEnvironment == 'description') {
        listStack.add(_LatexListKind.description);
        continue;
      }
      if (beginEnvironment == 'quote' || beginEnvironment == 'quotation') {
        quoteDepth += 1;
        continue;
      }

      if (endEnvironment == 'itemize' ||
          endEnvironment == 'enumerate' ||
          endEnvironment == 'description') {
        if (listStack.isNotEmpty) {
          listStack.removeLast();
        }
        continue;
      }
      if (endEnvironment == 'quote' || endEnvironment == 'quotation') {
        if (quoteDepth > 0) {
          quoteDepth -= 1;
        }
        continue;
      }

      if (beginEnvironment == 'center' ||
          beginEnvironment == 'flushleft' ||
          beginEnvironment == 'flushright' ||
          endEnvironment == 'center' ||
          endEnvironment == 'flushleft' ||
          endEnvironment == 'flushright') {
        continue;
      }

      final heading = _mapHeadingCommand(trimmed);
      if (heading != null) {
        appendLine(
            '${'#' * heading.level} ${_convertInlineCommands(heading.title)}');
        continue;
      }

      final item = _extractListItem(trimmed);
      if (item != null && listStack.isNotEmpty) {
        final indent = '  ' * (listStack.length - 1);
        final content = _convertInlineCommands(item.content);
        switch (listStack.last) {
          case _LatexListKind.unordered:
            appendLine(content.isEmpty ? '$indent-' : '$indent- $content');
            break;
          case _LatexListKind.ordered:
            appendLine(
                content.isEmpty ? '${indent}1.' : '${indent}1. $content');
            break;
          case _LatexListKind.description:
            final term = item.label?.trim() ?? '';
            if (term.isNotEmpty) {
              appendLine(
                content.isEmpty
                    ? '$indent- **$term**'
                    : '$indent- **$term**: $content',
              );
            } else {
              appendLine(content.isEmpty ? '$indent-' : '$indent- $content');
            }
            break;
        }
        continue;
      }

      if (listStack.isNotEmpty) {
        final indent = '  ' * listStack.length;
        appendLine('$indent${_convertInlineCommands(trimmed)}');
        continue;
      }

      if (_isTableOfContentsCommand(trimmed)) {
        appendLine('## 目录');
        continue;
      }

      final mappedLineCommand = _mapGenericLineCommand(trimmed);
      if (mappedLineCommand != null) {
        appendLine(mappedLineCommand);
        continue;
      }

      appendLine(_convertInlineCommands(normalizedLine));
    }

    return _cleanupOutput(output);
  }

  String _normalizeWrappedMathEnvironmentBoundary(String line) {
    var normalized = line;

    final openMatch = RegExp(
      r'^(\s*)\$\$\s*\\+begin\{([A-Za-z*]+)\}(.*)$',
    ).firstMatch(normalized);
    if (openMatch != null) {
      final environment = openMatch.group(2);
      if (environment != null && _mathEnvironments.contains(environment)) {
        normalized =
            '${openMatch.group(1) ?? ''}\\begin{$environment}${openMatch.group(3) ?? ''}';
      }
    }

    final closeMatch = RegExp(
      r'^(.*)\\+end\{([A-Za-z*]+)\}\s*\$\$\s*$',
    ).firstMatch(normalized);
    if (closeMatch != null) {
      final environment = closeMatch.group(2);
      if (environment != null && _mathEnvironments.contains(environment)) {
        normalized = '${closeMatch.group(1) ?? ''}\\end{$environment}';
      }
    }

    return normalized;
  }

  String _normalizeEscapedCommandPrefixes(String line) {
    var normalized = line;
    normalized = normalized.replaceFirstMapped(
      RegExp(r'^(\s*)\\{2,}(?=[A-Za-z\[\]])'),
      (match) => '${match.group(1) ?? ''}\\',
    );
    normalized = normalized.replaceFirstMapped(
      RegExp(r'^(\s*\$\$\s*)\\{2,}(?=[A-Za-z])'),
      (match) => '${match.group(1) ?? ''}\\',
    );
    return normalized;
  }

  bool _isCommentLine(String line) => line.startsWith('%');

  String _stripCommentPrefix(String line) {
    if (!line.startsWith('%')) {
      return line;
    }
    return line.substring(1).trimLeft();
  }

  bool _isDocumentClassLine(String line) {
    return RegExp(r'^\\documentclass(?:\[[^\]]*\])?\{[^}]+\}(?:\s*%.*)?\s*$')
        .hasMatch(line);
  }

  bool _isUsePackageLine(String line) {
    return RegExp(r'^\\usepackage(?:\[[^\]]*\])?\{[^}]+\}(?:\s*%.*)?\s*$')
        .hasMatch(line);
  }

  bool _isMakeTitleLine(String line) {
    return RegExp(r'^\\maketitle(?:\s*%.*)?\s*$').hasMatch(line);
  }

  bool _isTableOfContentsCommand(String line) {
    return RegExp(r'^\\tableofcontents(?:\s*%.*)?\s*$').hasMatch(line);
  }

  bool _isDocumentBoundaryLine(String line) {
    return RegExp(r'^\\(?:begin|end)\{document\}(?:\s*%.*)?\s*$')
        .hasMatch(line);
  }

  List<String> _buildTitleBlock({
    required String? title,
    required String? author,
    required String? date,
  }) {
    final lines = <String>[];
    if (title != null && title.trim().isNotEmpty) {
      lines.add('# ${title.trim()}');
    }

    final metaParts = <String>[];
    if (author != null && author.trim().isNotEmpty) {
      metaParts.add(author.trim());
    }
    if (date != null && date.trim().isNotEmpty) {
      metaParts.add(date.trim());
    }
    if (metaParts.isNotEmpty) {
      lines.add('> ${metaParts.join(' · ')}');
    }
    return lines;
  }

  String? _readBeginEnvironment(String line) {
    final match =
        RegExp(r'^\\+begin\{([A-Za-z*]+)\}(?:\s+.*)?$').firstMatch(line);
    return match?.group(1);
  }

  String? _readEndEnvironment(String line) {
    final match =
        RegExp(r'^\\+end\{([A-Za-z*]+)\}(?:\s+.*)?$').firstMatch(line);
    return match?.group(1);
  }

  bool _isStandaloneDollarFence(String line) => line == r'$$';

  _LatexListItem? _extractListItem(String line) {
    final match =
        RegExp(r'^\\item(?:\s*\[([^\]]+)\])?(?:\s+(.*))?$').firstMatch(line);
    if (match == null) {
      return null;
    }
    return _LatexListItem(
      label: match.group(1)?.trim(),
      content: (match.group(2) ?? '').trim(),
    );
  }

  String? _extractSingleArgCommand(String line, String command) {
    final prefix = '\\$command';
    if (!line.startsWith(prefix)) {
      return null;
    }
    var cursor = prefix.length;
    if (cursor < line.length && line[cursor] == '*') {
      cursor += 1;
    }
    while (cursor < line.length && line[cursor].trim().isEmpty) {
      cursor += 1;
    }
    if (cursor >= line.length || line[cursor] != '{') {
      return null;
    }
    final segment = _readBracedSegment(line, cursor);
    if (segment == null) {
      return null;
    }
    final trailing = line.substring(segment.endIndex + 1).trim();
    if (trailing.isNotEmpty && !trailing.startsWith('%')) {
      return null;
    }
    return segment.content;
  }

  String _convertInlineCommands(String input) {
    final buffer = StringBuffer();
    var cursor = 0;
    while (cursor < input.length) {
      final converted = _readInlineCommand(input, cursor);
      if (converted != null) {
        buffer.write(converted.rendered);
        cursor = converted.endIndex + 1;
        continue;
      }
      buffer.write(input[cursor]);
      cursor += 1;
    }
    return buffer.toString();
  }

  _InlineCommandMatch? _readInlineCommand(String input, int start) {
    final href = _readHrefInlineCommand(input, start);
    if (href != null) {
      return href;
    }
    final url = _readUrlInlineCommand(input, start);
    if (url != null) {
      return url;
    }

    final zeroArg = _readZeroArgInlineCommand(input, start);
    if (zeroArg != null) {
      return zeroArg;
    }

    const commands = <_InlineCommandSpec>[
      _InlineCommandSpec(command: 'textbf', marker: '**'),
      _InlineCommandSpec(command: 'emph', marker: '*'),
      _InlineCommandSpec(command: 'textit', marker: '*'),
      _InlineCommandSpec(command: 'underline', marker: '*'),
      _InlineCommandSpec(command: 'textsc', marker: '**'),
      _InlineCommandSpec(command: 'texttt', marker: '`'),
      _InlineCommandSpec(command: 'textsf', marker: ''),
      _InlineCommandSpec(command: 'textrm', marker: ''),
    ];

    for (final spec in commands) {
      final prefix = '\\${spec.command}';
      if (!input.startsWith(prefix, start)) {
        continue;
      }
      var cursor = start + prefix.length;
      while (cursor < input.length && input[cursor].trim().isEmpty) {
        cursor += 1;
      }
      if (cursor >= input.length || input[cursor] != '{') {
        continue;
      }
      final segment = _readBracedSegment(input, cursor);
      if (segment == null) {
        continue;
      }
      final nested = _convertInlineCommands(segment.content);
      if (spec.marker.isEmpty) {
        return _InlineCommandMatch(
          rendered: nested,
          endIndex: segment.endIndex,
        );
      }
      return _InlineCommandMatch(
        rendered: '${spec.marker}$nested${spec.marker}',
        endIndex: segment.endIndex,
      );
    }

    final generic = _readGenericInlineCommand(input, start);
    if (generic != null) {
      return generic;
    }
    return null;
  }

  _InlineCommandMatch? _readZeroArgInlineCommand(String input, int start) {
    var cursor = start;
    while (cursor < input.length && input[cursor] == '\\') {
      cursor += 1;
    }
    if (cursor == start || !input.startsWith('today', cursor)) {
      return null;
    }
    final end = cursor + 'today'.length;
    if (end < input.length && RegExp(r'[A-Za-z@]').hasMatch(input[end])) {
      return null;
    }
    return _InlineCommandMatch(
      rendered: _formatCurrentDate(),
      endIndex: end - 1,
    );
  }

  String _formatCurrentDate() {
    final now = DateTime.now().toLocal();
    final year = now.year.toString().padLeft(4, '0');
    final month = now.month.toString().padLeft(2, '0');
    final day = now.day.toString().padLeft(2, '0');
    return '$year-$month-$day';
  }

  _InlineCommandMatch? _readHrefInlineCommand(String input, int start) {
    const prefix = r'\href';
    if (!input.startsWith(prefix, start)) {
      return null;
    }

    var cursor = start + prefix.length;
    while (cursor < input.length && input[cursor].trim().isEmpty) {
      cursor += 1;
    }
    if (cursor >= input.length || input[cursor] != '{') {
      return null;
    }
    final urlSegment = _readBracedSegment(input, cursor);
    if (urlSegment == null) {
      return null;
    }
    cursor = urlSegment.endIndex + 1;
    while (cursor < input.length && input[cursor].trim().isEmpty) {
      cursor += 1;
    }
    if (cursor >= input.length || input[cursor] != '{') {
      return null;
    }
    final labelSegment = _readBracedSegment(input, cursor);
    if (labelSegment == null) {
      return null;
    }

    final url = _convertInlineCommands(urlSegment.content).trim();
    final label = _convertInlineCommands(labelSegment.content).trim();
    if (url.isEmpty) {
      return _InlineCommandMatch(
        rendered: label,
        endIndex: labelSegment.endIndex,
      );
    }
    final effectiveLabel = label.isEmpty ? url : label;
    return _InlineCommandMatch(
      rendered: '[$effectiveLabel]($url)',
      endIndex: labelSegment.endIndex,
    );
  }

  _InlineCommandMatch? _readUrlInlineCommand(String input, int start) {
    const prefix = r'\url';
    if (!input.startsWith(prefix, start)) {
      return null;
    }

    var cursor = start + prefix.length;
    while (cursor < input.length && input[cursor].trim().isEmpty) {
      cursor += 1;
    }
    if (cursor >= input.length || input[cursor] != '{') {
      return null;
    }
    final urlSegment = _readBracedSegment(input, cursor);
    if (urlSegment == null) {
      return null;
    }

    final url = _convertInlineCommands(urlSegment.content).trim();
    return _InlineCommandMatch(
      rendered: url.isEmpty ? '' : '[$url]($url)',
      endIndex: urlSegment.endIndex,
    );
  }

  _InlineCommandMatch? _readGenericInlineCommand(String input, int start) {
    final match = RegExp(r'^\\([A-Za-z@]+)\*?').matchAsPrefix(input, start);
    if (match == null) {
      return null;
    }
    var cursor = match.end;
    while (cursor < input.length && input[cursor].trim().isEmpty) {
      cursor += 1;
    }
    if (cursor >= input.length || input[cursor] != '{') {
      return null;
    }
    final segment = _readBracedSegment(input, cursor);
    if (segment == null) {
      return null;
    }
    return _InlineCommandMatch(
      rendered: _convertInlineCommands(segment.content),
      endIndex: segment.endIndex,
    );
  }

  _MappedHeading? _mapHeadingCommand(String line) {
    const headingLevels = <String, int>{
      'part': 1,
      'chapter': 1,
      'section': 2,
      'subsection': 3,
      'subsubsection': 4,
      'paragraph': 5,
      'subparagraph': 6,
    };

    for (final entry in headingLevels.entries) {
      final title = _extractSingleArgCommand(line, entry.key);
      if (title != null) {
        return _MappedHeading(level: entry.value, title: title);
      }
    }
    return null;
  }

  String? _mapGenericLineCommand(String line) {
    final command = _parseLineCommand(line);
    if (command == null) {
      return null;
    }

    if (command.name == 'begin' ||
        command.name == 'end' ||
        command.name == 'item' ||
        command.name == 'documentclass' ||
        command.name == 'usepackage' ||
        command.name == 'title' ||
        command.name == 'author' ||
        command.name == 'date' ||
        command.name == 'maketitle' ||
        command.name == 'part' ||
        command.name == 'chapter' ||
        command.name == 'section' ||
        command.name == 'subsection' ||
        command.name == 'subsubsection' ||
        command.name == 'paragraph' ||
        command.name == 'subparagraph') {
      return null;
    }

    if (command.name == 'label' ||
        command.name == 'pageref' ||
        command.name == 'ref' ||
        command.name == 'cite') {
      return null;
    }

    if (command.name == 'newline' || command.name == 'linebreak') {
      return '';
    }

    final payloadParts = <String>[];
    for (final arg in command.optionalArgs) {
      final value = _convertInlineCommands(arg).trim();
      if (value.isNotEmpty) {
        payloadParts.add(value);
      }
    }
    for (final arg in command.braceArgs) {
      final value = _convertInlineCommands(arg).trim();
      if (value.isNotEmpty) {
        payloadParts.add(value);
      }
    }
    if (command.trailing.trim().isNotEmpty) {
      payloadParts.add(_convertInlineCommands(command.trailing.trim()));
    }

    if (payloadParts.isEmpty) {
      return '**${command.name}**';
    }
    return '**${command.name}**: ${payloadParts.join(' / ')}';
  }

  _LineCommand? _parseLineCommand(String line) {
    final trimmed = line.trim();
    final match = RegExp(r'^\\([A-Za-z@]+)\*?').firstMatch(trimmed);
    if (match == null) {
      return null;
    }

    final name = match.group(1)!;
    var cursor = match.end;
    final optionalArgs = <String>[];
    final braceArgs = <String>[];

    while (cursor < trimmed.length) {
      while (cursor < trimmed.length && trimmed[cursor].trim().isEmpty) {
        cursor += 1;
      }
      if (cursor >= trimmed.length) {
        break;
      }
      if (trimmed[cursor] == '[') {
        final segment = _readBalancedSegment(
          input: trimmed,
          openIndex: cursor,
          openChar: '[',
          closeChar: ']',
        );
        if (segment == null) {
          break;
        }
        optionalArgs.add(segment.content);
        cursor = segment.endIndex + 1;
        continue;
      }
      if (trimmed[cursor] == '{') {
        final segment = _readBalancedSegment(
          input: trimmed,
          openIndex: cursor,
          openChar: '{',
          closeChar: '}',
        );
        if (segment == null) {
          break;
        }
        braceArgs.add(segment.content);
        cursor = segment.endIndex + 1;
        continue;
      }
      break;
    }

    final trailing = cursor < trimmed.length ? trimmed.substring(cursor) : '';
    return _LineCommand(
      name: name,
      optionalArgs: List.unmodifiable(optionalArgs),
      braceArgs: List.unmodifiable(braceArgs),
      trailing: trailing,
    );
  }

  _BalancedSegment? _readBalancedSegment({
    required String input,
    required int openIndex,
    required String openChar,
    required String closeChar,
  }) {
    if (openIndex >= input.length || input[openIndex] != openChar) {
      return null;
    }

    var depth = 0;
    for (var i = openIndex; i < input.length; i += 1) {
      final char = input[i];
      if (char == '\\') {
        i += 1;
        continue;
      }
      if (char == openChar) {
        depth += 1;
        continue;
      }
      if (char == closeChar) {
        depth -= 1;
        if (depth == 0) {
          return _BalancedSegment(
            content: input.substring(openIndex + 1, i),
            endIndex: i,
          );
        }
      }
    }
    return null;
  }

  _BalancedSegment? _readBracedSegment(String input, int openBraceIndex) {
    return _readBalancedSegment(
      input: input,
      openIndex: openBraceIndex,
      openChar: '{',
      closeChar: '}',
    );
  }

  String _cleanupOutput(List<String> lines) {
    final cleaned = <String>[];
    var previousWasBlank = false;
    for (final line in lines) {
      final isBlank = line.trim().isEmpty;
      if (isBlank) {
        if (previousWasBlank) {
          continue;
        }
        cleaned.add('');
        previousWasBlank = true;
        continue;
      }
      cleaned.add(line);
      previousWasBlank = false;
    }

    while (cleaned.isNotEmpty && cleaned.first.trim().isEmpty) {
      cleaned.removeAt(0);
    }
    while (cleaned.isNotEmpty && cleaned.last.trim().isEmpty) {
      cleaned.removeLast();
    }

    return cleaned.join('\n');
  }
}

enum _LatexListKind { unordered, ordered, description }

class _LatexListItem {
  const _LatexListItem({
    required this.label,
    required this.content,
  });

  final String? label;
  final String content;
}

class _MappedHeading {
  const _MappedHeading({
    required this.level,
    required this.title,
  });

  final int level;
  final String title;
}

class _LineCommand {
  const _LineCommand({
    required this.name,
    required this.optionalArgs,
    required this.braceArgs,
    required this.trailing,
  });

  final String name;
  final List<String> optionalArgs;
  final List<String> braceArgs;
  final String trailing;
}

class _BalancedSegment {
  const _BalancedSegment({
    required this.content,
    required this.endIndex,
  });

  final String content;
  final int endIndex;
}

class _InlineCommandSpec {
  const _InlineCommandSpec({
    required this.command,
    required this.marker,
  });

  final String command;
  final String marker;
}

class _InlineCommandMatch {
  const _InlineCommandMatch({
    required this.rendered,
    required this.endIndex,
  });

  final String rendered;
  final int endIndex;
}
