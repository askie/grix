import 'chat_markdown_segment.dart';

class ChatMarkdownLexer {
  const ChatMarkdownLexer();

  List<ChatMarkdownSegment> lex(String input) {
    if (input.isEmpty) {
      return const <ChatMarkdownSegment>[];
    }

    final segments = <ChatMarkdownSegment>[];
    var cursor = 0;
    var textStart = 0;

    void flushText(int end) {
      if (end <= textStart) {
        return;
      }
      segments.add(
        ChatMarkdownSegment(
          type: ChatMarkdownSegmentType.text,
          text: input.substring(textStart, end),
          start: textStart,
          end: end,
        ),
      );
    }

    while (cursor < input.length) {
      final fencedCode = _tryReadFencedCode(input, cursor);
      if (fencedCode != null) {
        flushText(cursor);
        segments.add(fencedCode.segment);
        cursor = fencedCode.nextIndex;
        textStart = cursor;
        continue;
      }

      final referenceDefinition = _tryReadReferenceDefinition(input, cursor);
      if (referenceDefinition != null) {
        flushText(cursor);
        segments.add(referenceDefinition.segment);
        cursor = referenceDefinition.nextIndex;
        textStart = cursor;
        continue;
      }

      final escaped = _tryReadEscaped(input, cursor);
      if (escaped != null) {
        flushText(cursor);
        segments.add(escaped.segment);
        cursor = escaped.nextIndex;
        textStart = cursor;
        continue;
      }

      final inlineCode = _tryReadInlineCode(input, cursor);
      if (inlineCode != null) {
        flushText(cursor);
        segments.add(inlineCode.segment);
        cursor = inlineCode.nextIndex;
        textStart = cursor;
        continue;
      }

      final inlineLink = _tryReadInlineLink(input, cursor);
      if (inlineLink != null) {
        flushText(cursor);
        segments.add(inlineLink.segment);
        cursor = inlineLink.nextIndex;
        textStart = cursor;
        continue;
      }

      final htmlLike = _tryReadHtmlLike(input, cursor);
      if (htmlLike != null) {
        flushText(cursor);
        segments.add(htmlLike.segment);
        cursor = htmlLike.nextIndex;
        textStart = cursor;
        continue;
      }

      cursor += 1;
    }

    flushText(input.length);
    return segments;
  }

  _LexMatch? _tryReadFencedCode(String input, int start) {
    if (!_isLineStart(input, start)) {
      return null;
    }

    var markerStart = start;
    var leadingSpaces = 0;
    while (markerStart < input.length &&
        leadingSpaces < 4 &&
        input[markerStart] == ' ') {
      markerStart += 1;
      leadingSpaces += 1;
    }
    if (leadingSpaces > 3 || markerStart >= input.length) {
      return null;
    }

    final markerChar = input[markerStart];
    if (markerChar != '`' && markerChar != '~') {
      return null;
    }

    var markerEnd = markerStart;
    while (markerEnd < input.length && input[markerEnd] == markerChar) {
      markerEnd += 1;
    }
    final markerLength = markerEnd - markerStart;
    if (markerLength < 3) {
      return null;
    }

    final openingLineEnd = _lineEnd(input, start);
    final infoString = input.substring(markerEnd, openingLineEnd).trim();
    final language = infoString.isEmpty
        ? null
        : infoString.split(RegExp(r'\s+')).first;
    final fenceMarker = input.substring(markerStart, markerEnd);
    final contentStart = openingLineEnd < input.length
        ? openingLineEnd + 1
        : input.length;

    var lineStart = contentStart;
    while (lineStart <= input.length) {
      if (lineStart >= input.length) {
        final rawText = input.substring(start);
        final content = contentStart <= input.length
            ? input.substring(contentStart)
            : '';
        return _LexMatch(
          ChatMarkdownSegment(
            type: ChatMarkdownSegmentType.fencedCode,
            text: rawText,
            start: start,
            end: input.length,
            language: language,
            infoString: infoString.isEmpty ? null : infoString,
            content: content,
            fenceMarker: fenceMarker,
            closed: false,
          ),
          input.length,
        );
      }

      final lineEnd = _lineEnd(input, lineStart);
      final line = input.substring(lineStart, lineEnd);
      if (_isClosingFenceLine(line, markerChar, markerLength)) {
        final endExclusive = lineEnd < input.length ? lineEnd + 1 : lineEnd;
        final rawText = input.substring(start, endExclusive);
        final content = input.substring(contentStart, lineStart);
        return _LexMatch(
          ChatMarkdownSegment(
            type: ChatMarkdownSegmentType.fencedCode,
            text: rawText,
            start: start,
            end: endExclusive,
            language: language,
            infoString: infoString.isEmpty ? null : infoString,
            content: content,
            fenceMarker: fenceMarker,
          ),
          endExclusive,
        );
      }

      if (lineEnd >= input.length) {
        final rawText = input.substring(start);
        final content = contentStart <= input.length
            ? input.substring(contentStart)
            : '';
        return _LexMatch(
          ChatMarkdownSegment(
            type: ChatMarkdownSegmentType.fencedCode,
            text: rawText,
            start: start,
            end: input.length,
            language: language,
            infoString: infoString.isEmpty ? null : infoString,
            content: content,
            fenceMarker: fenceMarker,
            closed: false,
          ),
          input.length,
        );
      }

      lineStart = lineEnd + 1;
    }

    return null;
  }

  _LexMatch? _tryReadReferenceDefinition(String input, int start) {
    if (!_isLineStart(input, start)) {
      return null;
    }
    final lineEnd = _lineEnd(input, start);
    final line = input.substring(start, lineEnd);
    final matches = RegExp(r'^[ ]{0,3}\[[^\]]+\]:').hasMatch(line);
    if (!matches) {
      return null;
    }

    final endExclusive = lineEnd < input.length ? lineEnd + 1 : lineEnd;
    return _LexMatch(
      ChatMarkdownSegment(
        type: ChatMarkdownSegmentType.referenceDefinition,
        text: input.substring(start, endExclusive),
        start: start,
        end: endExclusive,
      ),
      endExclusive,
    );
  }

  _LexMatch? _tryReadEscaped(String input, int start) {
    if (input[start] != '\\') {
      return null;
    }
    if (start + 1 >= input.length) {
      return null;
    }
    return _LexMatch(
      ChatMarkdownSegment(
        type: ChatMarkdownSegmentType.escaped,
        text: input.substring(start, start + 2),
        start: start,
        end: start + 2,
      ),
      start + 2,
    );
  }

  _LexMatch? _tryReadInlineCode(String input, int start) {
    if (input[start] != '`') {
      return null;
    }

    var tickEnd = start;
    while (tickEnd < input.length && input[tickEnd] == '`') {
      tickEnd += 1;
    }
    final tickSequence = input.substring(start, tickEnd);
    final closeIndex = input.indexOf(tickSequence, tickEnd);
    if (closeIndex == -1) {
      return null;
    }

    final endExclusive = closeIndex + tickSequence.length;
    return _LexMatch(
      ChatMarkdownSegment(
        type: ChatMarkdownSegmentType.inlineCode,
        text: input.substring(start, endExclusive),
        start: start,
        end: endExclusive,
        content: input.substring(tickEnd, closeIndex),
      ),
      endExclusive,
    );
  }

  _LexMatch? _tryReadInlineLink(String input, int start) {
    var cursor = start;
    var isImage = false;
    if (input[cursor] == '!') {
      if (cursor + 1 >= input.length || input[cursor + 1] != '[') {
        return null;
      }
      isImage = true;
      cursor += 1;
    } else if (input[cursor] != '[') {
      return null;
    }

    final labelStart = cursor;
    final labelEnd = _findMatchingBracket(input, labelStart);
    if (labelEnd == -1 ||
        labelEnd + 1 >= input.length ||
        input[labelEnd + 1] != '(') {
      return null;
    }

    final destinationStart = labelEnd + 2;
    final destinationEnd = _findMatchingDestinationEnd(input, destinationStart);
    if (destinationEnd == -1) {
      return null;
    }

    final segmentStart = isImage ? start : labelStart;
    final endExclusive = destinationEnd + 1;
    final label = input.substring(labelStart + 1, labelEnd);
    final rawDestination = input.substring(destinationStart, destinationEnd);
    final destination = _normalizeDestination(rawDestination);

    return _LexMatch(
      ChatMarkdownSegment(
        type: isImage
            ? ChatMarkdownSegmentType.imageDestination
            : ChatMarkdownSegmentType.linkDestination,
        text: input.substring(segmentStart, endExclusive),
        start: segmentStart,
        end: endExclusive,
        label: label,
        destination: destination,
      ),
      endExclusive,
    );
  }

  _LexMatch? _tryReadHtmlLike(String input, int start) {
    if (input[start] != '<' || start + 1 >= input.length) {
      return null;
    }
    final next = input[start + 1];
    final isPotentialTag = RegExp(r'[A-Za-z/!?]').hasMatch(next);
    if (!isPotentialTag) {
      return null;
    }
    final end = input.indexOf('>', start + 1);
    if (end == -1) {
      return null;
    }
    final endExclusive = end + 1;
    return _LexMatch(
      ChatMarkdownSegment(
        type: ChatMarkdownSegmentType.htmlLike,
        text: input.substring(start, endExclusive),
        start: start,
        end: endExclusive,
      ),
      endExclusive,
    );
  }

  bool _isLineStart(String input, int index) {
    return index == 0 || input[index - 1] == '\n';
  }

  int _lineEnd(String input, int start) {
    final newlineIndex = input.indexOf('\n', start);
    return newlineIndex == -1 ? input.length : newlineIndex;
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
    if (line[cursor] != markerChar) {
      return false;
    }
    var markerEnd = cursor;
    while (markerEnd < line.length && line[markerEnd] == markerChar) {
      markerEnd += 1;
    }
    if (markerEnd - cursor < markerLength) {
      return false;
    }
    return line.substring(markerEnd).trim().isEmpty;
  }

  int _findMatchingBracket(String input, int openIndex) {
    var depth = 0;
    for (var i = openIndex; i < input.length; i += 1) {
      final char = input[i];
      if (char == '\\') {
        i += 1;
        continue;
      }
      if (char == '[') {
        depth += 1;
      } else if (char == ']') {
        depth -= 1;
        if (depth == 0) {
          return i;
        }
      }
    }
    return -1;
  }

  int _findMatchingDestinationEnd(String input, int start) {
    if (start >= input.length) {
      return -1;
    }
    if (input[start] == '<') {
      for (var i = start + 1; i < input.length; i += 1) {
        final char = input[i];
        if (char == '\\') {
          i += 1;
          continue;
        }
        if (char == '>') {
          return i + 1 < input.length && input[i + 1] == ')' ? i + 1 : -1;
        }
      }
      return -1;
    }

    var depth = 0;
    for (var i = start; i < input.length; i += 1) {
      final char = input[i];
      if (char == '\\') {
        i += 1;
        continue;
      }
      if (char == '(') {
        depth += 1;
        continue;
      }
      if (char == ')') {
        if (depth == 0) {
          return i;
        }
        depth -= 1;
      }
    }
    return -1;
  }

  String _normalizeDestination(String rawDestination) {
    final trimmed = rawDestination.trim();
    if (trimmed.startsWith('<') &&
        trimmed.endsWith('>') &&
        trimmed.length > 1) {
      return trimmed.substring(1, trimmed.length - 1);
    }
    return trimmed;
  }
}

class _LexMatch {
  const _LexMatch(this.segment, this.nextIndex);

  final ChatMarkdownSegment segment;
  final int nextIndex;
}
