import 'chat_markdown_ast.dart';
import 'chat_markdown_lexer.dart';
import 'chat_markdown_segment.dart';

enum ChatMarkdownValidationSeverity { info, warning, error }

class ChatMarkdownValidationIssue {
  const ChatMarkdownValidationIssue({
    required this.severity,
    required this.code,
    required this.message,
  });

  final ChatMarkdownValidationSeverity severity;
  final String code;
  final String message;
}

class ChatMarkdownStructuralSnapshot {
  const ChatMarkdownStructuralSnapshot({
    required this.fencedCodeBlockCount,
    required this.unclosedFenceCount,
    required this.strongMarkerCount,
    required this.strikeMarkerCount,
    required this.mathFenceCount,
    required this.tableLineCount,
    required this.headingLineCount,
  });

  final int fencedCodeBlockCount;
  final int unclosedFenceCount;
  final int strongMarkerCount;
  final int strikeMarkerCount;
  final int mathFenceCount;
  final int tableLineCount;
  final int headingLineCount;
}

class ChatMarkdownValidationResult {
  const ChatMarkdownValidationResult({
    required this.preSnapshot,
    required this.postSnapshot,
    required this.issues,
  });

  final ChatMarkdownStructuralSnapshot preSnapshot;
  final ChatMarkdownStructuralSnapshot postSnapshot;
  final List<ChatMarkdownValidationIssue> issues;

  bool get hasErrors =>
      issues.any((i) => i.severity == ChatMarkdownValidationSeverity.error);

  bool get hasWarnings =>
      issues.any((i) => i.severity == ChatMarkdownValidationSeverity.warning);
}

class ChatMarkdownValidator {
  const ChatMarkdownValidator({this.lexer = const ChatMarkdownLexer()});

  final ChatMarkdownLexer lexer;

  ChatMarkdownStructuralSnapshot snapshot(String text) {
    final segments = lexer.lex(text);

    var fencedCodeBlockCount = 0;
    var unclosedFenceCount = 0;
    for (final segment in segments) {
      if (segment.type == ChatMarkdownSegmentType.fencedCode) {
        fencedCodeBlockCount += 1;
        if (!segment.closed) {
          unclosedFenceCount += 1;
        }
      }
    }

    var strongMarkerCount = 0;
    var strikeMarkerCount = 0;
    var mathFenceCount = 0;
    for (final segment in segments) {
      if (segment.type != ChatMarkdownSegmentType.text) {
        continue;
      }
      strongMarkerCount += _countMarkers(segment.text, '**');
      strikeMarkerCount += _countMarkers(segment.text, '~~');
      for (final line in segment.text.split('\n')) {
        if (line.trim() == r'$$') {
          mathFenceCount += 1;
        }
      }
    }

    var tableLineCount = 0;
    var headingLineCount = 0;
    var lineOffset = 0;
    for (final line in text.split('\n')) {
      final trimmed = line.trimLeft();
      if (trimmed.startsWith('#') && trimmed.contains('# ')) {
        headingLineCount += 1;
      }
      if (trimmed.contains('|') && !_isInsideFencedCode(segments, lineOffset)) {
        tableLineCount += 1;
      }
      lineOffset += line.length + 1; // +1 for the \n
    }

    return ChatMarkdownStructuralSnapshot(
      fencedCodeBlockCount: fencedCodeBlockCount,
      unclosedFenceCount: unclosedFenceCount,
      strongMarkerCount: strongMarkerCount,
      strikeMarkerCount: strikeMarkerCount,
      mathFenceCount: mathFenceCount,
      tableLineCount: tableLineCount,
      headingLineCount: headingLineCount,
    );
  }

  ChatMarkdownValidationResult validate({
    required String originalText,
    required String normalizedText,
    required ChatMarkdownDocument? document,
  }) {
    final pre = snapshot(originalText);
    final post = snapshot(normalizedText);
    final issues = <ChatMarkdownValidationIssue>[];

    // Code blocks gained by normalization (normalization introduced fake blocks)
    if (post.fencedCodeBlockCount > pre.fencedCodeBlockCount + 1) {
      issues.add(
        const ChatMarkdownValidationIssue(
          severity: ChatMarkdownValidationSeverity.error,
          code: 'code_block_gain',
          message: 'Normalization introduced unexpected code blocks',
        ),
      );
    }

    // Code blocks lost
    if (post.fencedCodeBlockCount < pre.fencedCodeBlockCount &&
        pre.unclosedFenceCount == 0) {
      issues.add(
        const ChatMarkdownValidationIssue(
          severity: ChatMarkdownValidationSeverity.warning,
          code: 'code_block_loss',
          message: 'Normalization reduced code block count',
        ),
      );
    }

    // Headings lost
    if (post.headingLineCount < pre.headingLineCount) {
      issues.add(
        const ChatMarkdownValidationIssue(
          severity: ChatMarkdownValidationSeverity.warning,
          code: 'heading_loss',
          message: 'Normalization reduced heading count',
        ),
      );
    }

    // Table lines significantly reduced
    if (pre.tableLineCount > 2 &&
        post.tableLineCount < pre.tableLineCount ~/ 2) {
      issues.add(
        const ChatMarkdownValidationIssue(
          severity: ChatMarkdownValidationSeverity.warning,
          code: 'table_loss',
          message: 'Normalization significantly reduced table content',
        ),
      );
    }

    // Strong/strike markers significantly changed (not just rebalanced)
    if (_markerCountDiverged(pre.strongMarkerCount, post.strongMarkerCount)) {
      issues.add(
        const ChatMarkdownValidationIssue(
          severity: ChatMarkdownValidationSeverity.warning,
          code: 'strong_marker_divergence',
          message:
              'Strong marker count significantly changed after normalization',
        ),
      );
    }

    if (_markerCountDiverged(pre.strikeMarkerCount, post.strikeMarkerCount)) {
      issues.add(
        const ChatMarkdownValidationIssue(
          severity: ChatMarkdownValidationSeverity.warning,
          code: 'strike_marker_divergence',
          message:
              'Strike marker count significantly changed after normalization',
        ),
      );
    }

    return ChatMarkdownValidationResult(
      preSnapshot: pre,
      postSnapshot: post,
      issues: issues,
    );
  }

  bool _markerCountDiverged(int pre, int post) {
    // Allow +-1 for rebalancing, flag larger changes
    return (pre - post).abs() > 1 && pre > 0;
  }

  int _countMarkers(String text, String marker) {
    var count = 0;
    var i = 0;
    while (i <= text.length - marker.length) {
      if (text.substring(i, i + marker.length) == marker) {
        count += 1;
        i += marker.length;
      } else {
        i += 1;
      }
    }
    return count;
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
