import 'chat_markdown_code_language.dart';
import 'chat_markdown_lexer.dart';
import 'chat_markdown_latex_document_converter.dart';
import 'chat_markdown_segment.dart';

class ChatMarkdownNormalizationResult {
  const ChatMarkdownNormalizationResult({
    required this.input,
    required this.text,
    required this.segments,
  });

  final String input;
  final String text;
  final List<ChatMarkdownSegment> segments;

  bool get changed => input != text;
}

class ChatMarkdownNormalizer {
  static const Set<String> _tableFriendlyLanguages = {
    '',
    'markdown',
    'md',
    'text',
    'plain',
    'plaintext',
  };
  static const Set<String> _tablePipeAlternatives = {'｜', '¦', '∣'};
  static const Set<String> _latexFriendlyLanguages = {'latex', 'tex'};
  static final RegExp _orderedListLinePattern = RegExp(
    r'^[ \t]{0,3}(\d{1,9})[.)](?:\s+|(?=[^\s\d]))',
  );
  static final RegExp _orderedListInlineMarkerPattern = RegExp(
    r'(?<!\d)(\d{1,9}[.)])(?=[^\d])',
  );
  static final RegExp _orderedListMissingSpacePattern = RegExp(
    r'^([ \t]{0,3}\d{1,9}[.)])(?=[^\s\d])',
  );
  static final RegExp _tableSeparatorCellPattern = RegExp(r'^:?-+:?$');
  static final RegExp _tableSeparatorFragmentPattern = RegExp(r'^:?-:?$');
  static final RegExp _tableSeparatorDashLikePattern = RegExp(r'[=—–]');
  static final List<RegExp> _latexSourcePatterns = [
    RegExp(r'\\+documentclass\b'),
    RegExp(r'\\+usepackage\b'),
    RegExp(r'\\+begin\{document\}'),
    RegExp(r'\\+end\{document\}'),
    RegExp(r'\\+(title|author|date|maketitle|tableofcontents)\b'),
    RegExp(r'\\+(chapter|section|subsection|subsubsection|paragraph)\*?\{'),
  ];
  static final RegExp _latexInlineMathPattern = RegExp(r'\$[^$\n]+?\$');
  static final RegExp _latexMathEnvironmentPattern = RegExp(
    r'\\+begin\{(?:equation|align|aligned|gather|multline|flalign|eqnarray)\*?\}',
  );
  static final List<RegExp> _latexDocumentSignalPatterns = [
    RegExp(r'\\+documentclass\b'),
    RegExp(r'\\+usepackage\b'),
    RegExp(r'\\+begin\{document\}'),
    RegExp(r'\\+end\{document\}'),
    RegExp(r'\\+maketitle\b'),
    RegExp(r'\\+title\b'),
    RegExp(r'\\+author\b'),
    RegExp(r'\\+date\b'),
  ];

  const ChatMarkdownNormalizer({
    this.lexer = const ChatMarkdownLexer(),
    this.latexDocumentConverter = const ChatMarkdownLatexDocumentConverter(),
  });

  final ChatMarkdownLexer lexer;
  final ChatMarkdownLatexDocumentConverter latexDocumentConverter;

  ChatMarkdownNormalizationResult normalizeForPreview(String input) {
    final normalizedInput = _normalizeIngress(input);
    return ChatMarkdownNormalizationResult(
      input: input,
      text: normalizedInput,
      segments: lexer.lex(normalizedInput),
    );
  }

  /// Applies only additive, non-destructive fixes (fence closure, marker
  /// balancing). Safe to call on partial/streaming content since it never
  /// modifies existing text — it only appends closing markers.
  String applyLightweightFixes(String text) {
    var fixed = _closeUnclosedFences(text);
    fixed = _closeUnbalancedTextMarkers(fixed);
    return fixed;
  }

  ChatMarkdownNormalizationResult normalizeForFinalRender(String input) {
    if (input.isEmpty) {
      return const ChatMarkdownNormalizationResult(
        input: '',
        text: '',
        segments: <ChatMarkdownSegment>[],
      );
    }

    var normalizedInput = _normalizeFenceLayout(_normalizeIngress(input));
    normalizedInput = _unwrapAccidentalTableFenceBlocks(normalizedInput);
    final initialSegments = lexer.lex(normalizedInput);
    final pieces = <String>[];
    final textLikeBuffer = StringBuffer();

    void flushTextLikeBuffer() {
      if (textLikeBuffer.isEmpty) {
        return;
      }
      pieces.add(_normalizeTextSegment(textLikeBuffer.toString()));
      textLikeBuffer.clear();
    }

    for (final segment in initialSegments) {
      switch (segment.type) {
        case ChatMarkdownSegmentType.text:
        case ChatMarkdownSegmentType.escaped:
          textLikeBuffer.write(segment.text);
          break;
        case ChatMarkdownSegmentType.fencedCode:
          flushTextLikeBuffer();
          pieces.add(_normalizeFencedCode(segment));
          break;
        case ChatMarkdownSegmentType.inlineCode:
          textLikeBuffer.write(segment.text);
          break;
        case ChatMarkdownSegmentType.linkDestination:
        case ChatMarkdownSegmentType.imageDestination:
        case ChatMarkdownSegmentType.referenceDefinition:
        case ChatMarkdownSegmentType.htmlLike:
          flushTextLikeBuffer();
          pieces.add(segment.text);
          break;
      }
    }

    flushTextLikeBuffer();

    var fixed = pieces.join();
    fixed = _closeUnclosedFences(fixed);
    fixed = _closeUnbalancedTextMarkers(fixed);

    return ChatMarkdownNormalizationResult(
      input: input,
      text: fixed,
      segments: lexer.lex(fixed),
    );
  }

  ChatMarkdownNormalizationResult normalizeForRendering(String input) {
    return normalizeForFinalRender(input);
  }

  String _normalizeIngress(String input) {
    var normalized = input;
    if (normalized.startsWith('\uFEFF')) {
      normalized = normalized.substring(1);
    }
    return normalized.replaceAll('\r\n', '\n').replaceAll('\r', '\n');
  }

  String _normalizeLatexDocumentText(String input) {
    if (!_looksLikeLatexDocumentSource(input)) {
      return input;
    }
    final stripped = _stripDocumentLikeMathFence(input.trim());
    final converted = latexDocumentConverter.convert(stripped);
    if (converted.trim().isEmpty) {
      return input;
    }
    return converted;
  }

  bool _looksLikeLatexDocumentSource(String text) {
    for (final pattern in _latexDocumentSignalPatterns) {
      if (pattern.hasMatch(text)) {
        return true;
      }
    }
    return false;
  }

  String _normalizeFenceLayout(String input) {
    final lines = input.replaceAll('\r\n', '\n').split('\n');
    final output = <String>[];
    String? activeFenceChar;
    var activeFenceLength = 0;
    String? activeFenceInfoString;

    for (final line in lines) {
      if (activeFenceChar == null) {
        final singleLineFence = _splitSingleLineInlineFence(line);
        if (singleLineFence != null) {
          if (singleLineFence.prefix.isNotEmpty) {
            output.add(singleLineFence.prefix);
          }
          output.add(singleLineFence.openingFenceLine);
          output.add(singleLineFence.content);
          output.add(singleLineFence.closingFenceLine);
          if (singleLineFence.suffix.isNotEmpty) {
            output.add(singleLineFence.suffix);
          }
          continue;
        }

        final inlineOpener = _splitInlineFenceOpener(line);
        if (inlineOpener != null) {
          if (inlineOpener.prefix.isNotEmpty) {
            output.add(inlineOpener.prefix);
          }
          output.add(inlineOpener.fenceLine);
          activeFenceChar = inlineOpener.markerChar;
          activeFenceLength = inlineOpener.markerLength;
          activeFenceInfoString = inlineOpener.infoString;
          continue;
        }

        output.add(line);
        final standaloneFence = _parseFenceLine(line);
        if (standaloneFence != null) {
          activeFenceChar = standaloneFence.markerChar;
          activeFenceLength = standaloneFence.markerLength;
          activeFenceInfoString = _extractFenceInfoString(
            line,
            standaloneFence,
          );
        }
        continue;
      }

      final inlineCloser = _splitInlineFenceCloser(
        line,
        markerChar: activeFenceChar,
        markerLength: activeFenceLength,
        activeInfoString: activeFenceInfoString,
      );
      if (inlineCloser != null) {
        output.add(inlineCloser.fenceLine);
        if (inlineCloser.suffix.isNotEmpty) {
          output.add(inlineCloser.suffix);
        }
        activeFenceChar = null;
        activeFenceLength = 0;
        activeFenceInfoString = null;
        continue;
      }

      output.add(line);
      if (_isClosingFenceLine(line, activeFenceChar, activeFenceLength)) {
        activeFenceChar = null;
        activeFenceLength = 0;
        activeFenceInfoString = null;
      }
    }

    return output.join('\n');
  }

  String _unwrapAccidentalTableFenceBlocks(String input) {
    final lines = input.split('\n');
    final output = <String>[];
    var index = 0;

    while (index < lines.length) {
      final fence = _parseFenceLine(lines[index]);
      if (fence == null) {
        output.add(lines[index]);
        index += 1;
        continue;
      }

      final infoString = _extractFenceInfoString(lines[index], fence);
      if (!_tableFriendlyLanguages.contains(
        normalizeCodeFenceLanguage(infoString),
      )) {
        // 非「表格友好」语言（如 mermaid、真实代码块）是一个完整的围栏块，
        // 必须整块原样输出、并把它的收尾围栏一并消费掉。否则该块的收尾裸
        // ``` 会被下一轮循环误当成「accidental table fence」的开头，向后一直
        // 吞到下一个真实围栏的收尾，把中间的表格/标题/另一张图整段解开，
        // 破坏两个围栏的边界——这正是线上多图混排消息里后面的 mermaid
        // 流程图渲染丢失的根因。
        output.add(lines[index]);
        index += 1;
        while (index < lines.length) {
          final line = lines[index];
          output.add(line);
          index += 1;
          if (_isClosingFenceLine(line, fence.markerChar, fence.markerLength)) {
            break;
          }
        }
        continue;
      }

      final body = <String>[];
      final originalBlock = <String>[lines[index]];
      index += 1;
      var closed = false;

      while (index < lines.length) {
        final line = lines[index];
        originalBlock.add(line);
        if (_isClosingFenceLine(line, fence.markerChar, fence.markerLength)) {
          closed = true;
          index += 1;
          break;
        }
        body.add(line);
        index += 1;
      }

      if (_shouldUnwrapAccidentalTableFenceBody(body)) {
        output.addAll(body);
        continue;
      }

      if (!closed && body.isNotEmpty) {
        output.addAll(originalBlock);
        continue;
      }

      output.addAll(originalBlock);
    }

    return output.join('\n');
  }

  bool _shouldUnwrapAccidentalTableFenceBody(List<String> bodyLines) {
    final expandedLines = _expandCollapsedTableBlockLines(bodyLines);
    if (expandedLines.length < 2) {
      return false;
    }

    final nonEmptyLines = expandedLines
        .where((line) => line.trim().isNotEmpty)
        .toList();
    if (nonEmptyLines.length < 2) {
      return false;
    }

    if (_isPotentialTableLine(nonEmptyLines.first) &&
        _isMarkdownTableSeparatorLine(nonEmptyLines[1])) {
      return true;
    }

    final normalized = _normalizeTableLines(
      expandedLines,
      allowSeparatorSynthesis: true,
    );
    return normalized != null;
  }

  _FenceLine? _parseFenceLine(String line) {
    final trimmedLeft = line.trimLeft();
    final leadingSpaces = line.length - trimmedLeft.length;
    if (leadingSpaces > 3 || trimmedLeft.isEmpty) {
      return null;
    }

    final token = _readFenceMarkerToken(trimmedLeft);
    if (token == null) {
      return null;
    }

    return _FenceLine(
      markerChar: token.markerChar,
      markerLength: token.markerLength,
    );
  }

  _InlineFenceOpener? _splitInlineFenceOpener(String line) {
    final match = RegExp(
      r'^(.+\S)(\s*)([`~]{3,}|(?:\\[`~]){3,})([^\n]*)$',
    ).firstMatch(line);
    if (match == null) {
      return null;
    }

    final prefix = match.group(1)?.trimRight() ?? '';
    final markerToken = match.group(3) ?? '';
    if (prefix.isEmpty || markerToken.isEmpty) {
      return null;
    }

    final marker = _decodeFenceMarkerToken(markerToken);
    if (marker == null) {
      return null;
    }
    if (!_looksLikeInlineFenceLead(prefix)) {
      return null;
    }

    final suffix = match.group(4) ?? '';
    if (!_looksLikeInlineFenceInfoString(suffix.trim())) {
      return null;
    }
    if (_containsFenceMarkerLaterInLine(suffix, marker)) {
      return null;
    }
    return _InlineFenceOpener(
      prefix: prefix,
      fenceLine: '$marker$suffix',
      markerChar: marker[0],
      markerLength: marker.length,
      infoString: suffix.trim(),
    );
  }

  _SingleLineInlineFence? _splitSingleLineInlineFence(String line) {
    final match = RegExp(
      r'^(.+\S)(\s*)([`~]{3,}|(?:\\[`~]){3,})([^\n]*?)([`~]{3,}|(?:\\[`~]){3,})(\s*\S.*)?$',
    ).firstMatch(line);
    if (match == null) {
      return null;
    }

    final prefix = match.group(1)?.trimRight() ?? '';
    final openerToken = match.group(3) ?? '';
    final middle = match.group(4) ?? '';
    final closerToken = match.group(5) ?? '';
    final suffix = (match.group(6) ?? '').trimLeft();
    if (prefix.isEmpty || openerToken.isEmpty || closerToken.isEmpty) {
      return null;
    }
    if (!_looksLikeInlineFenceLead(prefix)) {
      return null;
    }

    final openerMarker = _decodeFenceMarkerToken(openerToken);
    final closerMarker = _decodeFenceMarkerToken(closerToken);
    if (openerMarker == null ||
        closerMarker == null ||
        openerMarker != closerMarker) {
      return null;
    }

    final payload = _parseSingleLineFencePayload(middle);
    if (payload == null) {
      return null;
    }

    final openingFenceLine = payload.infoString.isEmpty
        ? openerMarker
        : '$openerMarker${payload.infoString}';
    return _SingleLineInlineFence(
      prefix: prefix,
      openingFenceLine: openingFenceLine,
      content: payload.content,
      closingFenceLine: closerMarker,
      suffix: suffix,
    );
  }

  _SingleLineFencePayload? _parseSingleLineFencePayload(String raw) {
    final payload = raw.trim();
    if (payload.isEmpty) {
      return null;
    }

    final firstWhitespace = payload.indexOf(RegExp(r'\s'));
    if (firstWhitespace <= 0) {
      return null;
    }

    final infoString = payload.substring(0, firstWhitespace).trimRight();
    final content = payload.substring(firstWhitespace).trim();
    if (infoString.isEmpty || content.isEmpty) {
      return null;
    }
    if (!_looksLikeInlineFenceInfoString(infoString)) {
      return null;
    }

    return _SingleLineFencePayload(infoString: infoString, content: content);
  }

  bool _looksLikeInlineFenceLead(String prefix) {
    final trimmed = prefix.trimRight();
    if (trimmed.isEmpty) {
      return false;
    }
    if (trimmed.endsWith(':') || trimmed.endsWith('：')) {
      return true;
    }
    if (RegExp(r'^#{1,6}\s+\S').hasMatch(trimmed)) {
      return true;
    }
    if (RegExp(r'^#{1,6}\S').hasMatch(trimmed)) {
      return true;
    }
    return RegExp(r'^[ \t]{0,3}(?:[-+*]|\d+[.)])\s+\S').hasMatch(trimmed);
  }

  bool _looksLikeInlineFenceInfoString(String infoString) {
    if (infoString.isEmpty) {
      return true;
    }

    return RegExp(
      "^[A-Za-z0-9][A-Za-z0-9_+.#/-]*(?:[ \\t]+(?:[A-Za-z0-9_+.#/-]+|[A-Za-z0-9_+.#/-]+=(?:\"[^\"\\n]*\"|'[^'\\n]*')))*\$",
    ).hasMatch(infoString);
  }

  _InlineFenceCloser? _splitInlineFenceCloser(
    String line, {
    required String markerChar,
    required int markerLength,
    required String? activeInfoString,
  }) {
    final trimmedLeft = line.trimLeft();
    final leadingSpaces = line.length - trimmedLeft.length;
    if (leadingSpaces > 3 || trimmedLeft.isEmpty) {
      return null;
    }

    final token = _readFenceMarkerToken(trimmedLeft);
    if (token == null ||
        token.markerChar != markerChar ||
        token.markerLength < markerLength) {
      return null;
    }

    if (trimmedLeft.length <= token.sourceLength) {
      return null;
    }

    final suffix = trimmedLeft.substring(token.sourceLength).trimLeft();
    if (suffix.isEmpty) {
      return null;
    }
    if (activeInfoString != null &&
        activeInfoString.isNotEmpty &&
        suffix == activeInfoString) {
      return null;
    }

    return _InlineFenceCloser(
      fenceLine:
          '${List.filled(leadingSpaces, ' ').join()}${List.filled(token.markerLength, markerChar).join()}',
      suffix: suffix,
    );
  }

  bool _isClosingFenceLine(String line, String markerChar, int markerLength) {
    var cursor = 0;
    var leadingSpaces = 0;
    while (cursor < line.length && line[cursor] == ' ' && leadingSpaces < 4) {
      cursor += 1;
      leadingSpaces += 1;
    }
    if (leadingSpaces > 3 || cursor >= line.length) {
      return false;
    }

    final token = _readFenceMarkerToken(line.substring(cursor));
    if (token == null ||
        token.markerChar != markerChar ||
        token.markerLength < markerLength) {
      return false;
    }

    return line.substring(cursor + token.sourceLength).trim().isEmpty;
  }

  String _normalizeFencedCode(ChatMarkdownSegment segment) {
    final language = (segment.language ?? '').trim().toLowerCase();
    final content = segment.content ?? '';

    if (_latexFriendlyLanguages.contains(language)) {
      return _normalizeFencedLatex(segment);
    }

    if (_shouldUnwrapMarkdownWrappedMermaid(language, content)) {
      final normalizedContent = _normalizeFenceLayout(content).trim();
      return '\n$normalizedContent\n';
    }

    final normalizedTable = _normalizeTableContentBlock(content);
    if (_tableFriendlyLanguages.contains(language) && normalizedTable != null) {
      return '\n$normalizedTable\n';
    }

    final repairedEchoedFence = _repairEchoedFenceFragments(segment);
    if (repairedEchoedFence != null) {
      return repairedEchoedFence;
    }

    if (!segment.closed && segment.fenceMarker != null) {
      final separator = segment.text.endsWith('\n') ? '' : '\n';
      return '${segment.text}$separator${segment.fenceMarker!}';
    }

    return segment.text;
  }

  bool _shouldUnwrapMarkdownWrappedMermaid(String language, String content) {
    if (!_tableFriendlyLanguages.contains(language)) {
      return false;
    }

    final normalizedContent = _normalizeFenceLayout(content).trim();
    if (normalizedContent.isEmpty) {
      return false;
    }

    final lines = normalizedContent
        .split('\n')
        .map((line) => line.trim())
        .where((line) => line.isNotEmpty)
        .toList(growable: false);
    if (lines.length < 3) {
      return false;
    }

    final openingFence = _parseFenceLine(lines.first);
    if (openingFence == null) {
      return false;
    }

    final openingInfo = _extractFenceInfoString(lines.first, openingFence);
    if (normalizeCodeFenceLanguage(openingInfo) != 'mermaid') {
      return false;
    }

    return _isClosingFenceLine(
      lines.last,
      openingFence.markerChar,
      openingFence.markerLength,
    );
  }

  String _extractFenceInfoString(String line, _FenceLine fence) {
    final trimmedLeft = line.trimLeft();
    final token = _readFenceMarkerToken(trimmedLeft);
    if (token == null ||
        token.markerChar != fence.markerChar ||
        token.markerLength != fence.markerLength) {
      return '';
    }
    return trimmedLeft.substring(token.sourceLength).trim().toLowerCase();
  }

  _FenceMarkerToken? _readFenceMarkerToken(String text) {
    if (text.isEmpty) {
      return null;
    }

    final directMatch = RegExp(r'^([`~]{3,})').firstMatch(text);
    if (directMatch != null) {
      final marker = directMatch.group(1)!;
      if (_isUniformFenceMarker(marker)) {
        return _FenceMarkerToken(
          markerChar: marker[0],
          markerLength: marker.length,
          sourceLength: marker.length,
        );
      }
    }

    final escapedMatch = RegExp(r'^((?:\\[`~]){3,})').firstMatch(text);
    if (escapedMatch == null) {
      return null;
    }

    final marker = _decodeFenceMarkerToken(escapedMatch.group(1)!);
    if (marker == null) {
      return null;
    }

    return _FenceMarkerToken(
      markerChar: marker[0],
      markerLength: marker.length,
      sourceLength: escapedMatch.group(1)!.length,
    );
  }

  String? _decodeFenceMarkerToken(String token) {
    final marker = token.replaceAll(r'\', '');
    if (!_isUniformFenceMarker(marker)) {
      return null;
    }
    return marker;
  }

  bool _isUniformFenceMarker(String marker) {
    if (marker.length < 3) {
      return false;
    }
    final markerChar = marker[0];
    if (markerChar != '`' && markerChar != '~') {
      return false;
    }
    for (var index = 1; index < marker.length; index += 1) {
      if (marker[index] != markerChar) {
        return false;
      }
    }
    return true;
  }

  bool _containsFenceMarkerLaterInLine(String text, String marker) {
    if (text.isEmpty) {
      return false;
    }
    if (text.contains(marker)) {
      return true;
    }
    final escapedMarker = marker.split('').map((char) => '\\$char').join();
    return text.contains(escapedMarker);
  }

  String _normalizeFencedLatex(ChatMarkdownSegment segment) {
    final content = segment.content ?? '';
    final trimmed = content.trim();
    if (trimmed.isEmpty) {
      return segment.text;
    }

    if (_looksLikeLatexSource(trimmed)) {
      final source = _stripDocumentLikeMathFence(trimmed);
      final converted = latexDocumentConverter.convert(source);
      if (converted.trim().isEmpty) {
        return '\n$source\n';
      }
      return '\n$converted\n';
    }

    if (_containsLatexMathDelimiters(trimmed)) {
      return '\n$trimmed\n';
    }

    return '\n\$\$\n$trimmed\n\$\$\n';
  }

  bool _looksLikeLatexSource(String content) {
    for (final pattern in _latexSourcePatterns) {
      if (pattern.hasMatch(content)) {
        return true;
      }
    }
    return false;
  }

  bool _containsLatexMathDelimiters(String content) {
    if (content.contains(r'$$')) {
      return true;
    }
    if (_latexInlineMathPattern.hasMatch(content)) {
      return true;
    }
    if (content.contains(r'\[') || content.contains(r'\(')) {
      return true;
    }
    if (_latexMathEnvironmentPattern.hasMatch(content)) {
      return true;
    }
    return false;
  }

  String _stripDocumentLikeMathFence(String content) {
    final lines = content.split('\n').toList(growable: true);
    final firstIndex = _firstNonEmptyLineIndex(lines);
    if (firstIndex != null) {
      final firstLine = lines[firstIndex];
      final trimmedLeft = firstLine.trimLeft();
      if (trimmedLeft.startsWith(r'$$')) {
        final indent = firstLine.length - trimmedLeft.length;
        var remainder = trimmedLeft.substring(2);
        if (remainder.startsWith(' ')) {
          remainder = remainder.substring(1);
        }
        if (remainder.isEmpty) {
          lines.removeAt(firstIndex);
        } else {
          lines[firstIndex] = '${''.padLeft(indent)}$remainder';
        }
      }
    }

    final lastIndex = _lastNonEmptyLineIndex(lines);
    if (lastIndex != null && lines[lastIndex].trim() == r'$$') {
      lines.removeAt(lastIndex);
    }

    return lines.join('\n').trim();
  }

  int? _firstNonEmptyLineIndex(List<String> lines) {
    for (var i = 0; i < lines.length; i += 1) {
      if (lines[i].trim().isNotEmpty) {
        return i;
      }
    }
    return null;
  }

  int? _lastNonEmptyLineIndex(List<String> lines) {
    for (var i = lines.length - 1; i >= 0; i -= 1) {
      if (lines[i].trim().isNotEmpty) {
        return i;
      }
    }
    return null;
  }

  String _normalizeTextSegment(String text) {
    var fixed = _normalizeLatexDocumentText(text);
    fixed = _normalizeOrderedListBlocks(fixed);
    fixed = _unwrapQuotedTableBlocks(fixed);
    fixed = _normalizeLooseTableBlocks(fixed);
    fixed = _ensureBlankLinesAroundMathBlocks(fixed);
    fixed = _breakAccidentalSetextHeadings(fixed);
    return fixed;
  }

  String _normalizeOrderedListBlocks(String text) {
    var fixed = _splitInlineOrderedListRuns(text);
    fixed = _ensureWhitespaceAfterOrderedListMarkers(fixed);
    fixed = _insertBlankLineBeforeOrderedListContinuations(fixed);
    return fixed;
  }

  String _splitInlineOrderedListRuns(String text) {
    final lines = text.split('\n');
    final output = <String>[];

    for (final line in lines) {
      output.add(_splitCollapsedOrderedListLine(line));
    }

    return output.join('\n');
  }

  String _splitCollapsedOrderedListLine(String line) {
    if (_hasExplicitTableEdges(line)) {
      return line;
    }
    final matches = _orderedListInlineMarkerPattern.allMatches(line).toList();
    if (matches.isEmpty) {
      return line;
    }

    final splitIndexes = <int>{};
    var sawListMarker = false;

    for (final match in matches) {
      if (match.start == 0) {
        sawListMarker = true;
        continue;
      }

      final prefix = line.substring(0, match.start);
      if (!sawListMarker && _looksLikeInlineOrderedListLead(prefix)) {
        splitIndexes.add(match.start);
        sawListMarker = true;
        continue;
      }

      if (sawListMarker && _isWhitespace(line[match.start - 1])) {
        splitIndexes.add(match.start);
      }
    }

    if (splitIndexes.isEmpty) {
      return line;
    }

    final orderedIndexes = splitIndexes.toList()..sort();
    final buffer = StringBuffer();
    var cursor = 0;

    for (final splitIndex in orderedIndexes) {
      buffer.write(line.substring(cursor, splitIndex).trimRight());
      buffer.write('\n');
      cursor = splitIndex;
    }

    buffer.write(line.substring(cursor));
    return buffer.toString();
  }

  bool _isWhitespace(String char) => char.trim().isEmpty;

  bool _looksLikeInlineOrderedListLead(String prefix) {
    final trimmed = prefix.trimRight();
    if (trimmed.isEmpty) {
      return false;
    }

    final markerStripped = trimmed.replaceFirst(RegExp(r'[*_~`]+$'), '');
    if (!markerStripped.endsWith(':') && !markerStripped.endsWith('：')) {
      return false;
    }
    if (markerStripped.length >= 2) {
      final beforeColon = markerStripped[markerStripped.length - 2];
      if (RegExp(r'\d').hasMatch(beforeColon)) {
        return false;
      }
    }
    return true;
  }

  String _ensureWhitespaceAfterOrderedListMarkers(String text) {
    final lines = text.split('\n');
    final output = <String>[];

    for (final line in lines) {
      output.add(
        line.replaceFirstMapped(
          _orderedListMissingSpacePattern,
          (match) => '${match.group(1)!} ',
        ),
      );
    }

    return output.join('\n');
  }

  String _insertBlankLineBeforeOrderedListContinuations(String text) {
    final lines = text.split('\n');
    final output = <String>[];

    for (var index = 0; index < lines.length; index += 1) {
      final line = lines[index];
      final match = _orderedListLinePattern.firstMatch(line.trimLeft());
      if (match != null) {
        final number = int.parse(match.group(1)!);
        final previousNonEmptyLine = _findPreviousNonEmptyOutputLine(output);
        final nextNonEmptyLine = _findNextNonEmptyLine(lines, index + 1);
        final startsListRun =
            nextNonEmptyLine != null &&
            (_looksLikeOrderedListLine(nextNonEmptyLine) ||
                _looksLikeIndentedBulletLine(nextNonEmptyLine));

        if (number > 1 &&
            previousNonEmptyLine != null &&
            !_looksLikeOrderedListLine(previousNonEmptyLine) &&
            !_looksLikeUnorderedListLine(previousNonEmptyLine) &&
            startsListRun &&
            output.isNotEmpty &&
            output.last.trim().isNotEmpty) {
          output.add('');
        }
      }

      output.add(line);
    }

    return output.join('\n');
  }

  String? _findPreviousNonEmptyOutputLine(List<String> lines) {
    for (var index = lines.length - 1; index >= 0; index -= 1) {
      if (lines[index].trim().isNotEmpty) {
        return lines[index];
      }
    }
    return null;
  }

  String? _findNextNonEmptyLine(List<String> lines, int startIndex) {
    for (var index = startIndex; index < lines.length; index += 1) {
      if (lines[index].trim().isNotEmpty) {
        return lines[index];
      }
    }
    return null;
  }

  bool _looksLikeOrderedListLine(String line) {
    return _orderedListLinePattern.hasMatch(line.trimLeft());
  }

  bool _looksLikeUnorderedListLine(String line) {
    return RegExp(r'^[ \t]{0,3}(?:-|\+|\*)[ \t]+').hasMatch(line.trimLeft());
  }

  bool _looksLikeIndentedBulletLine(String line) {
    return RegExp(r'^[ \t]{2,}[•·▪◦-][ \t]+').hasMatch(line);
  }

  String _closeUnclosedFences(String text) {
    final segments = lexer.lex(text);
    final openFences = <ChatMarkdownSegment>[];
    for (final segment in segments) {
      if (segment.type == ChatMarkdownSegmentType.fencedCode &&
          !segment.closed) {
        openFences.add(segment);
      }
    }
    if (openFences.isEmpty) {
      return text;
    }
    var fixed = text;
    for (final fence in openFences.reversed) {
      if (fence.fenceMarker == null) {
        continue;
      }
      final separator = fixed.endsWith('\n') ? '' : '\n';
      fixed = '$fixed$separator${fence.fenceMarker!}';
    }
    return fixed;
  }

  String _closeUnbalancedTextMarkers(String text) {
    final segments = lexer.lex(text);
    var standaloneMathFenceCount = 0;
    var strongCount = 0;
    var strikeCount = 0;

    for (final segment in segments) {
      if (segment.type != ChatMarkdownSegmentType.text) {
        continue;
      }
      standaloneMathFenceCount += _countStandaloneMathFenceLines(segment.text);
      strongCount += _countUnescapedMarkers(segment.text, '**');
      strikeCount += _countUnescapedMarkers(segment.text, '~~');
    }

    var fixed = text;
    if (standaloneMathFenceCount.isOdd) {
      fixed = '$fixed\n\$\$';
    }
    if (strongCount.isOdd) {
      fixed = '$fixed**';
    }
    if (strikeCount.isOdd) {
      fixed = '$fixed~~';
    }
    return fixed;
  }

  int _countUnescapedMarkers(String text, String marker) {
    var count = 0;
    var i = 0;
    while (i <= text.length - marker.length) {
      if (text.substring(i, i + marker.length) == marker) {
        if (i > 0 && text[i - 1] == r'\') {
          i += marker.length;
          continue;
        }
        count += 1;
        i += marker.length;
      } else {
        i += 1;
      }
    }
    return count;
  }

  int _countStandaloneMathFenceLines(String text) {
    var count = 0;
    for (final line in text.split('\n')) {
      if (line.trim() == r'$$') {
        count += 1;
      }
    }
    return count;
  }

  String _ensureBlankLinesAroundMathBlocks(String text) {
    var fixed = text;
    fixed = fixed.replaceAllMapped(
      RegExp(r'([^\n])\n(\$\$)'),
      (match) => '${match.group(1)!}\n\n${match.group(2)!}',
    );
    fixed = fixed.replaceAllMapped(
      RegExp(r'(\$\$)\n([^\n])'),
      (match) => '${match.group(1)!}\n\n${match.group(2)!}',
    );
    return fixed;
  }

  String _breakAccidentalSetextHeadings(String text) {
    if (!text.contains('\n---')) {
      return text;
    }

    final lines = text.split('\n');
    final output = <String>[];

    for (final line in lines) {
      final trimmed = line.trim();
      if (_isHyphenSetextUnderline(trimmed) &&
          output.isNotEmpty &&
          _looksLikeCollapsedTableLikeLine(output.last)) {
        final hasBlankLineBefore =
            output.length >= 2 && output[output.length - 2].trim().isEmpty;
        if (!hasBlankLineBefore) {
          output.add('');
        }
      }
      output.add(line);
    }

    return output.join('\n');
  }

  bool _isHyphenSetextUnderline(String line) {
    return RegExp(r'^-{3,}$').hasMatch(line);
  }

  bool _looksLikeCollapsedTableLikeLine(String line) {
    final trimmed = line.trim();
    if (trimmed.isEmpty || !trimmed.contains('|')) {
      return false;
    }
    if (trimmed.contains('||')) {
      return true;
    }
    if (RegExp(r'\|\s*:?-{3,}:?\s*\|').hasMatch(trimmed)) {
      return true;
    }

    final cells = _splitTableCells(trimmed);
    final nonEmptyCells = cells.where((cell) => cell.isNotEmpty).length;
    if (nonEmptyCells < 2) {
      return false;
    }

    return trimmed.startsWith('|') || trimmed.endsWith('|');
  }

  String? _normalizeTableContentBlock(String content) {
    final lines = content
        .replaceAll('\r\n', '\n')
        .split('\n')
        .map((line) => line.trim())
        .where((line) => line.isNotEmpty)
        .toList(growable: false);
    final expandedLines = _expandCollapsedTableBlockLines(lines);
    final normalized = _normalizeTableLines(
      expandedLines,
      allowSeparatorSynthesis: true,
    );
    if (normalized == null) {
      return null;
    }
    return normalized.join('\n');
  }

  String _normalizeLooseTableBlocks(String text) {
    if (!_containsTablePipeDelimiter(text)) {
      return text;
    }

    final lines = text.split('\n');
    final output = <String>[];
    var index = 0;

    while (index < lines.length) {
      if (!_isPotentialTableLine(lines[index])) {
        output.add(lines[index]);
        index += 1;
        continue;
      }

      final block = <String>[];
      while (index < lines.length && _isPotentialTableLine(lines[index])) {
        block.add(lines[index]);
        index += 1;
      }

      final expandedBlock = _expandCollapsedTableBlockLines(block);
      final splitBlock = _splitLooseTableBlock(expandedBlock);
      final tableLines = splitBlock.tableLines;
      final trailingLines = splitBlock.trailingLines;

      final normalized = _normalizeTableLines(
        tableLines,
        allowSeparatorSynthesis: true,
      );
      if (normalized == null) {
        output.addAll(block);
      } else {
        output.addAll(normalized);
        if (_shouldInsertBlankLineAfterNormalizedTable(
          trailingLines: trailingLines,
          nextLine: index < lines.length ? lines[index] : null,
        )) {
          output.add('');
        }
        output.addAll(trailingLines);
      }
    }

    return output.join('\n');
  }

  _TableBlockSplit _splitLooseTableBlock(List<String> lines) {
    if (lines.isEmpty) {
      return const _TableBlockSplit(
        tableLines: <String>[],
        trailingLines: <String>[],
      );
    }

    final tableLines = <String>[];
    var expectedColumnCount = 0;

    for (var index = 0; index < lines.length; index += 1) {
      final line = lines[index];
      if (!_isPotentialTableLine(line)) {
        return _TableBlockSplit(
          tableLines: List.unmodifiable(tableLines),
          trailingLines: List.unmodifiable(lines.sublist(index)),
        );
      }

      final cells = _splitTableCells(line);
      if (cells.length < 2) {
        return _TableBlockSplit(
          tableLines: List.unmodifiable(tableLines),
          trailingLines: List.unmodifiable(lines.sublist(index)),
        );
      }

      if (tableLines.isEmpty) {
        expectedColumnCount = cells.length;
        tableLines.add(line);
        continue;
      }

      final hasExplicitEdges = _hasExplicitTableEdges(line);
      if (!hasExplicitEdges && cells.length != expectedColumnCount) {
        return _TableBlockSplit(
          tableLines: List.unmodifiable(tableLines),
          trailingLines: List.unmodifiable(lines.sublist(index)),
        );
      }

      tableLines.add(line);
      if (_isMarkdownTableSeparatorLine(line) &&
          cells.length > expectedColumnCount) {
        expectedColumnCount = cells.length;
      }
    }

    return _TableBlockSplit(
      tableLines: List.unmodifiable(tableLines),
      trailingLines: const <String>[],
    );
  }

  bool _shouldInsertBlankLineAfterNormalizedTable({
    required List<String> trailingLines,
    required String? nextLine,
  }) {
    if (trailingLines.isNotEmpty) {
      return trailingLines.first.trim().isNotEmpty;
    }
    if (nextLine == null) {
      return false;
    }
    return nextLine.trim().isNotEmpty;
  }

  List<String> _expandCollapsedTableBlockLines(List<String> rawLines) {
    final lines = rawLines
        .map((line) => _normalizeTablePipeDelimiters(line).trim())
        .where((line) => line.isNotEmpty)
        .toList(growable: false);
    if (lines.isEmpty) {
      return const <String>[];
    }

    final expectedColumns = _splitTableCells(lines.first).length;
    if (expectedColumns < 2) {
      return lines;
    }

    final output = <String>[];
    for (final line in lines) {
      output.addAll(
        _splitCollapsedTableLine(line, expectedColumns: expectedColumns),
      );
    }
    return output;
  }

  List<String> _splitCollapsedTableLine(
    String line, {
    required int expectedColumns,
  }) {
    final trimmed = line.trim();
    if (trimmed.isEmpty || !trimmed.startsWith('|')) {
      return <String>[trimmed];
    }

    final pipePositions = _findUnescapedPipePositions(trimmed);
    if (pipePositions.length <= expectedColumns) {
      return <String>[trimmed];
    }

    final boundaryIndex = pipePositions[expectedColumns];
    final prefix = trimmed.substring(0, boundaryIndex + 1).trimRight();
    final suffix = trimmed.substring(boundaryIndex + 1).trimLeft();
    if (suffix.isEmpty) {
      return <String>[trimmed];
    }

    if (_splitTableCells(prefix).length != expectedColumns ||
        !_looksLikeCollapsedTableSuffix(suffix)) {
      return <String>[trimmed];
    }

    if (_isMarkdownTableSeparatorLine(prefix) &&
        _isIgnorableTableSeparatorFragment(suffix)) {
      return <String>[prefix];
    }

    return <String>[
      prefix,
      ..._splitCollapsedTableLine(suffix, expectedColumns: expectedColumns),
    ];
  }

  bool _looksLikeCollapsedTableSuffix(String suffix) {
    if (suffix.startsWith('|')) {
      return true;
    }
    if (suffix.startsWith('---') || suffix.startsWith('***')) {
      return true;
    }
    if (suffix.startsWith('**') ||
        suffix.startsWith('__') ||
        suffix.startsWith('~~') ||
        suffix.startsWith('#')) {
      return true;
    }
    return !suffix.contains('|');
  }

  List<int> _findUnescapedPipePositions(String text) {
    final positions = <int>[];
    var escaped = false;

    for (var index = 0; index < text.length; index += 1) {
      final char = text[index];
      if (escaped) {
        escaped = false;
        continue;
      }
      if (char == '\\') {
        escaped = true;
        continue;
      }
      if (char == '|') {
        positions.add(index);
      }
    }

    return positions;
  }

  List<String>? _normalizeTableLines(
    List<String> rawLines, {
    required bool allowSeparatorSynthesis,
  }) {
    final lines = rawLines
        .map((line) => _normalizeTablePipeDelimiters(line).trim())
        .where((line) => line.isNotEmpty)
        .toList(growable: false);
    if (lines.length < 2) {
      return null;
    }

    final rowCells = <List<String>>[];
    for (final line in lines) {
      final cells = _splitTableCells(line);
      if (cells.length < 2) {
        return null;
      }
      rowCells.add(cells);
    }

    final effectiveRowCells = <List<String>>[
      rowCells.first,
      for (var i = 1; i < rowCells.length; i += 1)
        i == 1
            ? _trimDanglingTableSeparatorFragments(
                rowCells[i],
                columnCount: rowCells.first.length,
              )
            : rowCells[i],
    ];

    final separatorIndex = effectiveRowCells.indexWhere(
      _isMarkdownTableSeparatorCells,
    );
    if (separatorIndex == -1) {
      if (!allowSeparatorSynthesis ||
          !_canSynthesizeTableSeparator(lines, effectiveRowCells)) {
        return null;
      }

      final columnCount = effectiveRowCells.first.length;
      final output = <String>[
        _buildMarkdownTableRowLine(
          _fitTableCells(effectiveRowCells.first, columnCount: columnCount),
        ),
        _buildMarkdownTableSeparatorLine(
          List<_TableColumnAlignment>.filled(
            columnCount,
            _TableColumnAlignment.none,
          ),
        ),
      ];
      for (var i = 1; i < rowCells.length; i += 1) {
        output.add(
          _buildMarkdownTableRowLine(
            _fitTableCells(rowCells[i], columnCount: columnCount),
          ),
        );
      }
      return output;
    }

    if (separatorIndex != 1 || separatorIndex == lines.length - 1) {
      return null;
    }

    final columnCount = _resolveTableColumnCount(
      effectiveRowCells,
      separatorIndex: separatorIndex,
    );
    if (columnCount < 2) {
      return null;
    }

    final output = <String>[
      _buildMarkdownTableRowLine(
        _fitTableCells(effectiveRowCells.first, columnCount: columnCount),
      ),
      _buildMarkdownTableSeparatorLine(
        _parseTableSeparatorAlignments(
          effectiveRowCells[separatorIndex],
          columnCount: columnCount,
        ),
      ),
    ];
    for (var i = separatorIndex + 1; i < rowCells.length; i += 1) {
      output.add(
        _buildMarkdownTableRowLine(
          _fitTableCells(rowCells[i], columnCount: columnCount),
        ),
      );
    }

    if (output.length < 3) {
      return null;
    }
    return output;
  }

  bool _canSynthesizeTableSeparator(
    List<String> lines,
    List<List<String>> rowCells,
  ) {
    if (lines.length < 3 || rowCells.isEmpty) {
      return false;
    }
    if (!lines.any((line) => line.startsWith('|') || line.endsWith('|'))) {
      return false;
    }

    final columnCount = rowCells.first.length;
    if (columnCount < 2) {
      return false;
    }
    for (final cells in rowCells) {
      if (cells.length != columnCount) {
        return false;
      }
    }
    return true;
  }

  int _resolveTableColumnCount(
    List<List<String>> rowCells, {
    required int separatorIndex,
  }) {
    var columnCount = rowCells.first.length;
    if (separatorIndex >= 0 && separatorIndex < rowCells.length) {
      final separatorCount = rowCells[separatorIndex].length;
      if (separatorCount > columnCount) {
        columnCount = separatorCount;
      }
    }
    for (var i = 0; i < rowCells.length; i += 1) {
      if (i == separatorIndex) {
        continue;
      }
      if (rowCells[i].length > columnCount) {
        columnCount = rowCells[i].length;
      }
    }
    return columnCount;
  }

  List<String> _fitTableCells(List<String> cells, {required int columnCount}) {
    if (cells.length == columnCount) {
      return cells.map((cell) => cell.trim()).toList(growable: false);
    }
    if (cells.length < columnCount) {
      return <String>[
        ...cells.map((cell) => cell.trim()),
        ...List<String>.filled(columnCount - cells.length, ''),
      ];
    }
    if (columnCount <= 1) {
      return <String>[cells.join(' | ').trim()];
    }
    return <String>[
      ...cells.take(columnCount - 1).map((cell) => cell.trim()),
      cells.skip(columnCount - 1).join(' | ').trim(),
    ];
  }

  String _buildMarkdownTableRowLine(List<String> cells) {
    return '| ${cells.join(' | ')} |';
  }

  String _buildMarkdownTableSeparatorLine(
    List<_TableColumnAlignment> alignments,
  ) {
    final cells = <String>[];
    for (final alignment in alignments) {
      switch (alignment) {
        case _TableColumnAlignment.left:
          cells.add(':---');
          break;
        case _TableColumnAlignment.center:
          cells.add(':---:');
          break;
        case _TableColumnAlignment.right:
          cells.add('---:');
          break;
        case _TableColumnAlignment.none:
          cells.add('---');
          break;
      }
    }
    return '| ${cells.join(' | ')} |';
  }

  List<_TableColumnAlignment> _parseTableSeparatorAlignments(
    List<String> rawCells, {
    required int columnCount,
  }) {
    final alignments = List<_TableColumnAlignment>.filled(
      columnCount,
      _TableColumnAlignment.none,
    );
    final cells = _fitTableCells(rawCells, columnCount: columnCount);
    for (var i = 0; i < cells.length; i += 1) {
      final token = _normalizeTableSeparatorToken(cells[i]);
      final left = token.startsWith(':');
      final right = token.endsWith(':');
      if (left && right) {
        alignments[i] = _TableColumnAlignment.center;
      } else if (right) {
        alignments[i] = _TableColumnAlignment.right;
      } else if (left) {
        alignments[i] = _TableColumnAlignment.left;
      }
    }
    return alignments;
  }

  bool _isPotentialTableLine(String line) {
    final trimmed = _normalizeTablePipeDelimiters(line).trim();
    if (trimmed.isEmpty || !_containsTablePipeDelimiter(trimmed)) {
      return false;
    }
    return _splitTableCells(trimmed).length >= 2;
  }

  bool _containsTablePipeDelimiter(String line) {
    if (line.contains('|')) {
      return true;
    }
    for (final alternative in _tablePipeAlternatives) {
      if (line.contains(alternative)) {
        return true;
      }
    }
    return false;
  }

  bool _hasExplicitTableEdges(String line) {
    final trimmed = _normalizeTablePipeDelimiters(line).trim();
    if (trimmed.isEmpty) {
      return false;
    }
    return trimmed.startsWith('|') || trimmed.endsWith('|');
  }

  String _normalizeTablePipeDelimiters(String line) {
    var normalized = line;
    for (final alternative in _tablePipeAlternatives) {
      normalized = normalized.replaceAll(alternative, '|');
    }
    return normalized;
  }

  String _normalizeTableSeparatorToken(String cell) {
    return cell.trim().replaceAllMapped(
      _tableSeparatorDashLikePattern,
      (_) => '-',
    );
  }

  String? _repairEchoedFenceFragments(ChatMarkdownSegment segment) {
    final fenceMarker = segment.fenceMarker;
    if (fenceMarker == null) {
      return null;
    }

    final infoString = segment.infoString?.trim() ?? '';
    if (infoString.isEmpty) {
      return null;
    }

    final rawContent = segment.content ?? '';
    if (rawContent.isEmpty) {
      return null;
    }

    final fenceLine = '$fenceMarker$infoString';
    final lines = rawContent.replaceAll('\r\n', '\n').split('\n').toList();
    while (lines.isNotEmpty && lines.last.isEmpty) {
      lines.removeLast();
    }
    if (lines.length < 3) {
      return null;
    }

    final fragments = <String>[];
    var echoedFenceCount = 0;
    var expectFragment = true;

    for (final line in lines) {
      if (line.trim() == fenceLine) {
        if (expectFragment) {
          return null;
        }
        echoedFenceCount += 1;
        expectFragment = true;
        continue;
      }

      if (!expectFragment ||
          !_looksLikeEchoedFenceFragment(line, fenceMarker)) {
        return null;
      }

      fragments.add(line);
      expectFragment = false;
    }

    if (echoedFenceCount < 2 || fragments.length < 2) {
      return null;
    }

    final rebuiltContent = fragments.join();
    return '$fenceLine\n$rebuiltContent\n$fenceMarker';
  }

  bool _looksLikeEchoedFenceFragment(String line, String fenceMarker) {
    if (line.length > 24) {
      return false;
    }
    return !line.contains(fenceMarker);
  }

  String _unwrapQuotedTableBlocks(String text) {
    if (!text.contains('>')) {
      return text;
    }

    final lines = text.split('\n');
    final output = <String>[];
    var index = 0;

    while (index < lines.length) {
      final line = lines[index];
      if (!_isBlockquoteLine(line)) {
        output.add(line);
        index += 1;
        continue;
      }

      final block = <String>[];
      while (index < lines.length && _isBlockquoteLine(lines[index])) {
        block.add(lines[index]);
        index += 1;
      }

      final normalized = _normalizeTableLines(
        block.map(_stripBlockquotePrefix).toList(growable: false),
        allowSeparatorSynthesis: true,
      );
      if (normalized != null) {
        output.addAll(normalized);
      } else {
        output.addAll(block);
      }
    }

    return output.join('\n');
  }

  bool _isBlockquoteLine(String line) {
    return RegExp(r'^[ \t]*>').hasMatch(line);
  }

  String _stripBlockquotePrefix(String line) {
    return line.replaceFirst(RegExp(r'^[ \t]*>[ \t]?'), '');
  }

  bool _isMarkdownTableSeparatorLine(String line) {
    final cells = _splitTableCells(line);
    return _isMarkdownTableSeparatorCells(cells);
  }

  bool _isMarkdownTableSeparatorCells(List<String> cells) {
    if (cells.isEmpty) {
      return false;
    }
    return cells.every(
      (cell) => _tableSeparatorCellPattern.hasMatch(
        _normalizeTableSeparatorToken(cell),
      ),
    );
  }

  List<String> _trimDanglingTableSeparatorFragments(
    List<String> cells, {
    required int columnCount,
  }) {
    if (cells.length <= columnCount || columnCount < 2) {
      return cells;
    }

    final leadingCells = cells.take(columnCount).toList(growable: false);
    if (!_isMarkdownTableSeparatorCells(leadingCells)) {
      return cells;
    }

    final trailingCells = cells.skip(columnCount);
    if (!trailingCells.every(_isIgnorableTableSeparatorFragment)) {
      return cells;
    }

    return leadingCells;
  }

  bool _isIgnorableTableSeparatorFragment(String cell) {
    final token = _normalizeTableSeparatorToken(cell);
    return _tableSeparatorFragmentPattern.hasMatch(token);
  }

  List<String> _splitTableCells(String line) {
    final trimmed = _normalizeTablePipeDelimiters(line).trim();
    if (!trimmed.contains('|')) {
      return const <String>[];
    }

    final content = trimmed.startsWith('|') ? trimmed.substring(1) : trimmed;
    final normalized = content.endsWith('|')
        ? content.substring(0, content.length - 1)
        : content;

    final cells = <String>[];
    final buffer = StringBuffer();
    var escaped = false;
    var backtickRun = 0;
    var inMath = false;

    for (var i = 0; i < normalized.length; i += 1) {
      final char = normalized[i];
      if (escaped) {
        buffer.write(char);
        escaped = false;
        continue;
      }
      if (char == r'\') {
        buffer.write(char);
        escaped = true;
        continue;
      }
      if (char == '`') {
        var run = 1;
        while (i + run < normalized.length && normalized[i + run] == '`') {
          run += 1;
        }
        for (var k = 0; k < run; k += 1) {
          buffer.write('`');
        }
        if (backtickRun == 0) {
          if (_hasMatchingBacktickClose(normalized, i + run, run)) {
            backtickRun = run;
          }
        } else if (run == backtickRun) {
          backtickRun = 0;
        }
        i += run - 1;
        continue;
      }
      if (backtickRun > 0) {
        buffer.write(char);
        continue;
      }
      if (char == r'$') {
        if (inMath) {
          buffer.write(char);
          inMath = false;
          continue;
        }
        if (_hasMatchingMathClose(normalized, i + 1)) {
          inMath = true;
        }
        buffer.write(char);
        continue;
      }
      if (inMath) {
        buffer.write(char);
        continue;
      }
      if (char == '|') {
        cells.add(buffer.toString().trim());
        buffer.clear();
        continue;
      }
      buffer.write(char);
    }

    cells.add(buffer.toString().trim());
    return cells;
  }

  bool _hasMatchingBacktickClose(String text, int start, int run) {
    var i = start;
    while (i < text.length) {
      if (text[i] != '`') {
        i += 1;
        continue;
      }
      var closingRun = 1;
      while (i + closingRun < text.length && text[i + closingRun] == '`') {
        closingRun += 1;
      }
      if (closingRun == run) {
        return true;
      }
      i += closingRun;
    }
    return false;
  }

  bool _hasMatchingMathClose(String text, int start) {
    var localEscaped = false;
    for (var i = start; i < text.length; i += 1) {
      final char = text[i];
      if (localEscaped) {
        localEscaped = false;
        continue;
      }
      if (char == r'\') {
        localEscaped = true;
        continue;
      }
      if (char == r'$') {
        return true;
      }
    }
    return false;
  }
}

enum _TableColumnAlignment { none, left, center, right }

class _FenceLine {
  const _FenceLine({required this.markerChar, required this.markerLength});

  final String markerChar;
  final int markerLength;
}

class _InlineFenceOpener {
  const _InlineFenceOpener({
    required this.prefix,
    required this.fenceLine,
    required this.markerChar,
    required this.markerLength,
    required this.infoString,
  });

  final String prefix;
  final String fenceLine;
  final String markerChar;
  final int markerLength;
  final String infoString;
}

class _InlineFenceCloser {
  const _InlineFenceCloser({required this.fenceLine, required this.suffix});

  final String fenceLine;
  final String suffix;
}

class _TableBlockSplit {
  const _TableBlockSplit({
    required this.tableLines,
    required this.trailingLines,
  });

  final List<String> tableLines;
  final List<String> trailingLines;
}

class _SingleLineInlineFence {
  const _SingleLineInlineFence({
    required this.prefix,
    required this.openingFenceLine,
    required this.content,
    required this.closingFenceLine,
    required this.suffix,
  });

  final String prefix;
  final String openingFenceLine;
  final String content;
  final String closingFenceLine;
  final String suffix;
}

class _SingleLineFencePayload {
  const _SingleLineFencePayload({
    required this.infoString,
    required this.content,
  });

  final String infoString;
  final String content;
}

class _FenceMarkerToken {
  const _FenceMarkerToken({
    required this.markerChar,
    required this.markerLength,
    required this.sourceLength,
  });

  final String markerChar;
  final int markerLength;
  final int sourceLength;
}
