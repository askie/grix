import 'dart:convert';
import 'dart:io';

import 'package:grix/shared/markdown/chat_markdown_ast.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';
import 'package:grix/shared/markdown/chat_markdown_segment.dart';

const String _datasetRelativeRoot =
    'test/shared/markdown/testdata/markdown_parser_dataset_20260313_190200';

void main(List<String> args) {
  final outputPath = _parseOutputPath(args);
  final datasetRoot = _resolveDatasetRoot();
  final manifestEntries = _loadManifestEntries('$datasetRoot/manifest.jsonl');
  const normalizer = ChatMarkdownNormalizer();
  final parser = ChatMarkdownDialect.buildParserAdapter();
  final pipeline = ChatMarkdownPipeline(
    normalizer: normalizer,
    parser: parser,
  );

  final issues = <_AuditIssue>[];

  for (final entry in manifestEntries) {
    final casePath = '$datasetRoot/${entry.caseFile}';
    final raw = File(casePath).readAsStringSync();
    final normalization = normalizer.normalizeForFinalRender(raw);
    final visibleText = _maskProtectedSegments(
      normalization.text,
      normalization.segments,
      const <ChatMarkdownSegmentType>{ChatMarkdownSegmentType.fencedCode},
    );
    final structuralIssues = _detectFenceStructuralIssues(normalization.text);

    Object? parserError;
    ChatMarkdownDocument? directDocument;
    try {
      directDocument = parser.parse(normalization.text);
    } catch (error) {
      parserError = error;
    }

    final pipelineResult = pipeline.prepareFinalRender(raw);
    if (parserError != null ||
        pipelineResult.document == null ||
        pipelineResult.semantics == null) {
      issues.add(
        _buildParseFailureIssue(
          entry: entry,
          raw: raw,
          normalized: normalization.text,
          parserError: parserError,
        ),
      );
      continue;
    }

    final actualFeatures = _collectActualFeatures(directDocument!);
    for (final expectedFeature in entry.features) {
      if (actualFeatures.contains(expectedFeature)) {
        continue;
      }
      final diagnosis = _diagnoseFeatureMismatch(
        feature: expectedFeature,
        raw: raw,
        normalized: normalization.text,
        visibleText: visibleText,
        segments: normalization.segments,
        structuralIssues: structuralIssues,
      );
      issues.add(
        _AuditIssue(
          kind: 'feature_mismatch',
          caseFile: entry.caseFile,
          category: entry.category,
          feature: expectedFeature,
          owner: diagnosis.owner.name,
          reason: diagnosis.reason,
          evidence: diagnosis.evidence,
          excerpt: _buildExcerpt(raw),
        ),
      );
    }
  }

  final report = _buildReport(
    datasetRoot: datasetRoot,
    manifestEntries: manifestEntries,
    issues: issues,
  );
  final jsonText = const JsonEncoder.withIndent('  ').convert(report);

  if (outputPath != null) {
    File(outputPath)
      ..createSync(recursive: true)
      ..writeAsStringSync(jsonText);
  }

  stdout.writeln(jsonText);
}

String? _parseOutputPath(List<String> args) {
  for (var i = 0; i < args.length; i++) {
    if (args[i] == '--output' && i + 1 < args.length) {
      return args[i + 1];
    }
  }
  return null;
}

Map<String, Object?> _buildReport({
  required String datasetRoot,
  required List<_ManifestEntry> manifestEntries,
  required List<_AuditIssue> issues,
}) {
  final issuesByOwner = <String, int>{};
  final issuesByFeature = <String, int>{};
  final issuesByKind = <String, int>{};
  for (final issue in issues) {
    issuesByOwner.update(issue.owner, (value) => value + 1, ifAbsent: () => 1);
    issuesByKind.update(issue.kind, (value) => value + 1, ifAbsent: () => 1);
    final featureKey = issue.feature ?? '(parse_failure)';
    issuesByFeature.update(featureKey, (value) => value + 1, ifAbsent: () => 1);
  }

  return <String, Object?>{
    'dataset_root': datasetRoot,
    'total_cases': manifestEntries.length,
    'total_issues': issues.length,
    'issues_by_owner': _sortCountMap(issuesByOwner),
    'issues_by_kind': _sortCountMap(issuesByKind),
    'issues_by_feature': _sortCountMap(issuesByFeature),
    'issues': issues.map((issue) => issue.toJson()).toList(growable: false),
  };
}

Map<String, int> _sortCountMap(Map<String, int> input) {
  final entries = input.entries.toList()
    ..sort((left, right) {
      final byValue = right.value.compareTo(left.value);
      if (byValue != 0) {
        return byValue;
      }
      return left.key.compareTo(right.key);
    });
  return Map<String, int>.fromEntries(entries);
}

_AuditIssue _buildParseFailureIssue({
  required _ManifestEntry entry,
  required String raw,
  required String normalized,
  required Object? parserError,
}) {
  final structuralIssues = _detectFenceStructuralIssues(normalized);
  if (structuralIssues.isNotEmpty) {
    return _AuditIssue(
      kind: 'parse_failure',
      caseFile: entry.caseFile,
      category: entry.category,
      owner: _IssueOwner.markdown.name,
      reason: 'normalized markdown contains malformed fenced-block structure',
      evidence: structuralIssues,
      excerpt: _buildExcerpt(raw),
    );
  }

  return _AuditIssue(
    kind: 'parse_failure',
    caseFile: entry.caseFile,
    category: entry.category,
    owner: _IssueOwner.parser.name,
    reason: parserError == null
        ? 'pipeline downgraded without a parser exception'
        : 'parser threw on normalized markdown',
    evidence: <String>[
      if (parserError != null) '${parserError.runtimeType}: $parserError',
      if (parserError == null)
        'normalized markdown length=${normalized.length}',
    ],
    excerpt: _buildExcerpt(raw),
  );
}

Set<String> _collectActualFeatures(ChatMarkdownDocument document) {
  final features = <String>{};

  void visit(ChatMarkdownNode node) {
    switch (node.type) {
      case ChatMarkdownNodeType.heading:
        features.add('heading');
        break;
      case ChatMarkdownNodeType.blockquote:
        features.add('blockquote');
        break;
      case ChatMarkdownNodeType.list:
        final ordered = node.attrs['ordered'] == true;
        features.add(ordered ? 'ordered_list' : 'unordered_list');
        break;
      case ChatMarkdownNodeType.codeBlock:
      case ChatMarkdownNodeType.mermaidBlock:
        features.add('fenced_code');
        break;
      case ChatMarkdownNodeType.table:
        features.add('table');
        break;
      case ChatMarkdownNodeType.emphasis:
      case ChatMarkdownNodeType.strong:
      case ChatMarkdownNodeType.strike:
        features.add('emphasis');
        break;
      case ChatMarkdownNodeType.link:
      case ChatMarkdownNodeType.autolink:
        features.add('link');
        break;
      case ChatMarkdownNodeType.image:
        features.add('image');
        break;
      case ChatMarkdownNodeType.video:
        features.add('video');
        break;
      case ChatMarkdownNodeType.audio:
        features.add('audio');
        break;
      case ChatMarkdownNodeType.document:
      case ChatMarkdownNodeType.paragraph:
      case ChatMarkdownNodeType.thematicBreak:
      case ChatMarkdownNodeType.listItem:
      case ChatMarkdownNodeType.taskItem:
      case ChatMarkdownNodeType.tableHead:
      case ChatMarkdownNodeType.tableBody:
      case ChatMarkdownNodeType.tableRow:
      case ChatMarkdownNodeType.tableCell:
      case ChatMarkdownNodeType.mathBlock:
      case ChatMarkdownNodeType.htmlBlockText:
      case ChatMarkdownNodeType.footnoteDef:
      case ChatMarkdownNodeType.text:
      case ChatMarkdownNodeType.softBreak:
      case ChatMarkdownNodeType.hardBreak:
      case ChatMarkdownNodeType.inlineCode:
      case ChatMarkdownNodeType.mathInline:
      case ChatMarkdownNodeType.footnoteRef:
      case ChatMarkdownNodeType.escapedText:
      case ChatMarkdownNodeType.unknown:
        break;
    }

    for (final child in node.children) {
      visit(child);
    }
  }

  visit(document);
  return features;
}

_FeatureDiagnosis _diagnoseFeatureMismatch({
  required String feature,
  required String raw,
  required String normalized,
  required String visibleText,
  required List<ChatMarkdownSegment> segments,
  required List<String> structuralIssues,
}) {
  final analysis = switch (feature) {
    'heading' => _analyzeHeading(visibleText),
    'blockquote' => _analyzeBlockquote(visibleText),
    'unordered_list' => _analyzeUnorderedList(visibleText),
    'ordered_list' => _analyzeOrderedList(visibleText),
    'fenced_code' => _analyzeFencedCode(raw, normalized, segments),
    'table' => _analyzeTable(visibleText),
    'emphasis' => _analyzeEmphasis(visibleText),
    'link' => _analyzeLink(visibleText),
    'image' => _analyzeImage(visibleText),
    _ => const _DetectionResult(
        state: _DetectionState.absent,
        evidence: <String>['feature detector is not implemented'],
      ),
  };

  if (analysis.state == _DetectionState.valid &&
      feature != 'fenced_code' &&
      structuralIssues.isNotEmpty) {
    return _FeatureDiagnosis(
      owner: _IssueOwner.markdown,
      reason:
          'valid markdown syntax exists, but malformed fenced-block structure earlier in the document likely changes the parse result',
      evidence: List<String>.unmodifiable(
        <String>[
          ...analysis.evidence,
          ...structuralIssues.take(3),
        ],
      ),
    );
  }

  switch (analysis.state) {
    case _DetectionState.valid:
      return _FeatureDiagnosis(
        owner: _IssueOwner.parser,
        reason:
            'valid markdown syntax is present but the parser did not expose it',
        evidence: analysis.evidence,
      );
    case _DetectionState.malformed:
      return _FeatureDiagnosis(
        owner: _IssueOwner.markdown,
        reason: 'the source contains malformed markdown for this feature',
        evidence: analysis.evidence,
      );
    case _DetectionState.absent:
      return _FeatureDiagnosis(
        owner: _IssueOwner.manifest,
        reason:
            'the manifest labels this feature, but valid markdown syntax is not present',
        evidence: analysis.evidence,
      );
  }
}

_DetectionResult _analyzeHeading(String text) {
  final valid = _findFirstMatchingLine(
    text,
    RegExp(r'^[ \t]{0,3}#{1,6}(?:[ \t]+|$)'),
  );
  if (valid != null) {
    return _DetectionResult(
      state: _DetectionState.valid,
      evidence: <String>['line ${valid.$1}: ${valid.$2}'],
    );
  }

  final malformed = _findFirstMatchingLine(
    text,
    RegExp(r'^[ \t]{0,3}#{1,6}\S'),
  );
  if (malformed != null) {
    return _DetectionResult(
      state: _DetectionState.malformed,
      evidence: <String>[
        'line ${malformed.$1}: heading marker is not followed by whitespace: ${malformed.$2}',
      ],
    );
  }

  return const _DetectionResult(
    state: _DetectionState.absent,
    evidence: <String>['no ATX heading syntax found'],
  );
}

_DetectionResult _analyzeBlockquote(String text) {
  final valid = _findFirstMatchingLine(
    text,
    RegExp(r'^[ \t]{0,3}>(?:[ \t]|$)'),
  );
  if (valid != null) {
    return _DetectionResult(
      state: _DetectionState.valid,
      evidence: <String>['line ${valid.$1}: ${valid.$2}'],
    );
  }

  final malformed = _findFirstMatchingLine(
    text,
    RegExp(r'^[ \t]{0,3}>[^ \t]'),
  );
  if (malformed != null) {
    return _DetectionResult(
      state: _DetectionState.malformed,
      evidence: <String>[
        'line ${malformed.$1}: blockquote marker is not separated by whitespace: ${malformed.$2}',
      ],
    );
  }

  return const _DetectionResult(
    state: _DetectionState.absent,
    evidence: <String>['no blockquote syntax found'],
  );
}

_DetectionResult _analyzeUnorderedList(String text) {
  final valid = _findFirstMatchingLine(
    text,
    RegExp(r'^[ \t]{0,3}(?:-|\+|\*)(?![-+*])[ \t]+'),
  );
  if (valid != null) {
    return _DetectionResult(
      state: _DetectionState.valid,
      evidence: <String>['line ${valid.$1}: ${valid.$2}'],
    );
  }

  final malformed = _findFirstMatchingLine(
    text,
    RegExp(r'^[ \t]{0,3}(?:-|\+|\*)(?![-+*])\S'),
  );
  if (malformed != null) {
    return _DetectionResult(
      state: _DetectionState.malformed,
      evidence: <String>[
        'line ${malformed.$1}: list marker is not followed by whitespace: ${malformed.$2}',
      ],
    );
  }

  return const _DetectionResult(
    state: _DetectionState.absent,
    evidence: <String>['no unordered-list syntax found'],
  );
}

_DetectionResult _analyzeOrderedList(String text) {
  final lines = text.split('\n');
  final validPattern = RegExp(r'^[ \t]{0,3}(\d{1,9})[.)][ \t]+');
  final malformedPattern = RegExp(r'^[ \t]{0,3}\d{1,9}[.)]\S');

  for (var i = 0; i < lines.length; i++) {
    final line = lines[i];
    final validMatch = validPattern.firstMatch(line);
    if (validMatch != null) {
      final number = int.parse(validMatch.group(1)!);
      final previousNonEmptyLine = _findPreviousNonEmptyLine(lines, i);
      final previousAllowsInterruption = previousNonEmptyLine == null ||
          _isListLine(previousNonEmptyLine) ||
          previousNonEmptyLine.trim().isEmpty;
      if (number == 1 || previousAllowsInterruption) {
        return _DetectionResult(
          state: _DetectionState.valid,
          evidence: <String>[
            'line ${i + 1}: ${_truncateLine(line.trimRight())}'
          ],
        );
      }
      return _DetectionResult(
        state: _DetectionState.malformed,
        evidence: <String>[
          'line ${i + 1}: ordered list interrupts a paragraph with start number $number instead of 1: ${_truncateLine(line.trimRight())}',
        ],
      );
    }

    if (malformedPattern.hasMatch(line)) {
      return _DetectionResult(
        state: _DetectionState.malformed,
        evidence: <String>[
          'line ${i + 1}: ordered-list marker is not followed by whitespace: ${_truncateLine(line.trimRight())}',
        ],
      );
    }
  }

  return const _DetectionResult(
    state: _DetectionState.absent,
    evidence: <String>['no ordered-list syntax found'],
  );
}

_DetectionResult _analyzeFencedCode(
  String raw,
  String normalized,
  List<ChatMarkdownSegment> segments,
) {
  if (segments
      .any((segment) => segment.type == ChatMarkdownSegmentType.fencedCode)) {
    return const _DetectionResult(
      state: _DetectionState.valid,
      evidence: <String>[
        'lexer found fenced-code segments after normalization'
      ],
    );
  }

  final structuralIssues = _detectFenceStructuralIssues(normalized);
  if (structuralIssues.isNotEmpty) {
    return _DetectionResult(
      state: _DetectionState.malformed,
      evidence: structuralIssues,
    );
  }

  if (RegExp(r'[`~]{3,}').hasMatch(raw)) {
    return const _DetectionResult(
      state: _DetectionState.malformed,
      evidence: <String>[
        'raw text contains fence markers, but they do not form a valid fenced block after normalization',
      ],
    );
  }

  return const _DetectionResult(
    state: _DetectionState.absent,
    evidence: <String>['no fenced-code syntax found'],
  );
}

_DetectionResult _analyzeTable(String text) {
  final lines = text.split('\n');
  for (var i = 0; i + 1 < lines.length; i++) {
    final header = lines[i].trim();
    final separator = lines[i + 1].trim();
    if (!_looksLikeTableRow(header)) {
      continue;
    }
    if (_looksLikeTableSeparator(separator)) {
      return _DetectionResult(
        state: _DetectionState.valid,
        evidence: <String>[
          'lines ${i + 1}-${i + 2}: `${_truncateLine(header)}` / `${_truncateLine(separator)}`',
        ],
      );
    }
  }

  final pipeHeavy = _findFirstMatchingLine(text, RegExp(r'^\s*\|.*\|'));
  if (pipeHeavy != null) {
    return _DetectionResult(
      state: _DetectionState.malformed,
      evidence: <String>[
        'line ${pipeHeavy.$1}: pipe-delimited row found without a valid separator row: ${pipeHeavy.$2}',
      ],
    );
  }

  return const _DetectionResult(
    state: _DetectionState.absent,
    evidence: <String>['no GFM table syntax found'],
  );
}

_DetectionResult _analyzeEmphasis(String text) {
  final patterns = <RegExp>[
    RegExp(r'(?<!\*)\*\*[^*\n]+?\*\*(?!\*)'),
    RegExp(r'(?<!_)__[^_\n]+?__(?!_)'),
    RegExp(r'(?<!~)~~[^~\n]+?~~(?!~)'),
    RegExp(r'(?<!\w)\*[^*\n]+?\*(?!\w)'),
    RegExp(r'(?<!\w)_[^_\n]+?_(?!\w)'),
  ];
  for (final pattern in patterns) {
    final match = pattern.firstMatch(text);
    if (match != null) {
      return _DetectionResult(
        state: _DetectionState.valid,
        evidence: <String>[
          'inline emphasis fragment: `${_truncateLine(match.group(0)!)}`'
        ],
      );
    }
  }

  final malformedPatterns = <RegExp>[
    RegExp(r'\*\*[^*\n]+$'),
    RegExp(r'^[^*\n]+\*\*'),
    RegExp(r'__[^_\n]+$'),
    RegExp(r'~~[^~\n]+$'),
  ];
  for (final pattern in malformedPatterns) {
    final match = pattern.firstMatch(text);
    if (match != null) {
      return _DetectionResult(
        state: _DetectionState.malformed,
        evidence: <String>[
          'unbalanced emphasis marker fragment: `${_truncateLine(match.group(0)!)}`',
        ],
      );
    }
  }

  return const _DetectionResult(
    state: _DetectionState.absent,
    evidence: <String>['no balanced emphasis syntax found'],
  );
}

_DetectionResult _analyzeLink(String text) {
  final textWithoutImages = text.replaceAllMapped(
    RegExp(r'!\[[^\]\n]*\]\([^)]+\)'),
    (match) => _spaces(match.group(0)!.length),
  );
  final inlineLink = RegExp(
    r'\[[^\]\n]+\]\((?:https?:\/\/|\/)[^)\s]+(?:\s+"[^"\n]*")?\)',
  ).firstMatch(textWithoutImages);
  if (inlineLink != null) {
    return _DetectionResult(
      state: _DetectionState.valid,
      evidence: <String>[
        'inline link: `${_truncateLine(inlineLink.group(0)!)}`'
      ],
    );
  }

  final autoLink =
      RegExp(r'https?:\/\/[^\s<>()]+').firstMatch(textWithoutImages);
  if (autoLink != null) {
    return _DetectionResult(
      state: _DetectionState.valid,
      evidence: <String>['autolink: `${_truncateLine(autoLink.group(0)!)}`'],
    );
  }

  if (textWithoutImages.contains('](') || textWithoutImages.contains('[')) {
    return const _DetectionResult(
      state: _DetectionState.malformed,
      evidence: <String>[
        'link-like markers are present, but no valid inline-link syntax was detected',
      ],
    );
  }

  return const _DetectionResult(
    state: _DetectionState.absent,
    evidence: <String>['no link syntax found'],
  );
}

String? _findPreviousNonEmptyLine(List<String> lines, int beforeIndex) {
  for (var i = beforeIndex - 1; i >= 0; i--) {
    final line = lines[i];
    if (line.trim().isNotEmpty) {
      return line;
    }
  }
  return null;
}

bool _isListLine(String line) {
  return RegExp(r'^[ \t]{0,3}(?:[-+*]|\d{1,9}[.)])[ \t]+').hasMatch(line);
}

_DetectionResult _analyzeImage(String text) {
  final image = RegExp(
    r'!\[[^\]\n]*\]\((?:https?:\/\/|\/)[^)\s]+(?:\s+"[^"\n]*")?\)',
  ).firstMatch(text);
  if (image != null) {
    return _DetectionResult(
      state: _DetectionState.valid,
      evidence: <String>['image: `${_truncateLine(image.group(0)!)}`'],
    );
  }

  if (text.contains('![')) {
    return const _DetectionResult(
      state: _DetectionState.malformed,
      evidence: <String>[
        'image-like markers are present, but no valid image syntax was detected',
      ],
    );
  }

  return const _DetectionResult(
    state: _DetectionState.absent,
    evidence: <String>['no image syntax found'],
  );
}

List<String> _detectFenceStructuralIssues(String markdown) {
  final issues = <String>[];
  final lines = markdown.split('\n');
  final fencePattern = RegExp(r'^[ \t]{0,3}([`~]{3,})(.*)$');
  _OpenFence? openFence;

  for (var i = 0; i < lines.length; i++) {
    final lineNo = i + 1;
    final line = lines[i];
    final match = fencePattern.firstMatch(line);
    if (match == null) {
      continue;
    }

    final marker = match.group(1)!;
    final tail = (match.group(2) ?? '').trim();
    final markerChar = marker[0];
    final markerLength = marker.length;

    if (openFence == null) {
      openFence = _OpenFence(
        markerChar: markerChar,
        markerLength: markerLength,
        line: lineNo,
      );
      if (tail.isNotEmpty && _looksSuspiciousFenceInfo(tail)) {
        issues.add(
          'line $lineNo: suspicious fence info string `${_truncateLine(tail)}`',
        );
      }
      continue;
    }

    final canClose = markerChar == openFence.markerChar &&
        markerLength >= openFence.markerLength;
    if (!canClose) {
      continue;
    }

    if (tail.isEmpty) {
      openFence = null;
      continue;
    }

    issues.add(
      'line $lineNo: closing fence has trailing content `${_truncateLine(tail)}`',
    );
  }

  if (openFence != null) {
    issues.add('line ${openFence.line}: opened fence is never closed');
  }

  return List.unmodifiable(issues);
}

String _maskProtectedSegments(
  String text,
  List<ChatMarkdownSegment> segments,
  Set<ChatMarkdownSegmentType> maskedTypes,
) {
  if (segments.isEmpty || maskedTypes.isEmpty) {
    return text;
  }

  final buffer = StringBuffer();
  var cursor = 0;

  for (final segment in segments) {
    if (!maskedTypes.contains(segment.type)) {
      continue;
    }
    if (segment.start > cursor) {
      buffer.write(text.substring(cursor, segment.start));
    }
    buffer.write(_maskText(text.substring(segment.start, segment.end)));
    cursor = segment.end;
  }

  if (cursor < text.length) {
    buffer.write(text.substring(cursor));
  }
  return buffer.toString();
}

String _maskText(String text) {
  final buffer = StringBuffer();
  for (final rune in text.runes) {
    final char = String.fromCharCode(rune);
    buffer.write(char == '\n' ? '\n' : ' ');
  }
  return buffer.toString();
}

bool _looksSuspiciousFenceInfo(String tail) {
  if (RegExp(r'^[|#>*-]').hasMatch(tail)) {
    return true;
  }
  if (RegExp(r'^[A-Za-z0-9_+\-]+#\s+\S').hasMatch(tail)) {
    return true;
  }
  return false;
}

(int, String)? _findFirstMatchingLine(String text, RegExp pattern) {
  final lines = text.split('\n');
  for (var i = 0; i < lines.length; i++) {
    final line = lines[i];
    if (pattern.hasMatch(line)) {
      return (i + 1, _truncateLine(line.trimRight()));
    }
  }
  return null;
}

bool _looksLikeTableRow(String line) {
  if (line.isEmpty) {
    return false;
  }
  final pipeCount = '|'.allMatches(line).length;
  return pipeCount >= 2;
}

bool _looksLikeTableSeparator(String line) {
  if (!line.contains('|')) {
    return false;
  }
  final cells = line
      .split('|')
      .map((cell) => cell.trim())
      .where((cell) => cell.isNotEmpty)
      .toList(growable: false);
  if (cells.isEmpty) {
    return false;
  }
  for (final cell in cells) {
    if (!RegExp(r'^:?-{2,}:?$').hasMatch(cell)) {
      return false;
    }
  }
  return true;
}

String _buildExcerpt(String text, {int maxLines = 8}) {
  final lines = text.split('\n').take(maxLines).toList(growable: false);
  return lines.join('\n');
}

String _spaces(int length) => List.filled(length, ' ').join();

String _truncateLine(String text, {int maxChars = 120}) {
  if (text.length <= maxChars) {
    return text;
  }
  return '${text.substring(0, maxChars)}...';
}

List<_ManifestEntry> _loadManifestEntries(String manifestPath) {
  final file = File(manifestPath);
  if (!file.existsSync()) {
    throw StateError('manifest not found: $manifestPath');
  }

  final entries = <_ManifestEntry>[];
  for (final line in file.readAsLinesSync()) {
    if (line.trim().isEmpty) {
      continue;
    }
    final raw = jsonDecode(line);
    if (raw is! Map<String, dynamic>) {
      throw const FormatException('manifest line must be a JSON object');
    }
    final caseFile = raw['case_file'];
    final category = raw['category'];
    final features = raw['features'];
    if (caseFile is! String || category is! String || features is! List) {
      throw const FormatException('manifest line is missing required fields');
    }
    entries.add(
      _ManifestEntry(
        caseFile: caseFile,
        category: category,
        features:
            features.map((item) => item.toString()).toList(growable: false),
      ),
    );
  }

  entries.sort((left, right) => left.caseFile.compareTo(right.caseFile));
  return List.unmodifiable(entries);
}

String _resolveDatasetRoot() {
  final direct = Directory(_datasetRelativeRoot);
  if (direct.existsSync()) {
    return _normalizePath(_datasetRelativeRoot);
  }

  final prefixed = Directory('frontend/$_datasetRelativeRoot');
  if (prefixed.existsSync()) {
    return _normalizePath('frontend/$_datasetRelativeRoot');
  }

  throw StateError('markdown dataset not found: $_datasetRelativeRoot');
}

String _normalizePath(String path) => path.replaceAll('\\', '/');

class _ManifestEntry {
  const _ManifestEntry({
    required this.caseFile,
    required this.category,
    required this.features,
  });

  final String caseFile;
  final String category;
  final List<String> features;
}

class _AuditIssue {
  const _AuditIssue({
    required this.kind,
    required this.caseFile,
    required this.category,
    required this.owner,
    required this.reason,
    required this.evidence,
    required this.excerpt,
    this.feature,
  });

  final String kind;
  final String caseFile;
  final String category;
  final String owner;
  final String reason;
  final List<String> evidence;
  final String excerpt;
  final String? feature;

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'kind': kind,
      'case_file': caseFile,
      'category': category,
      'feature': feature,
      'owner': owner,
      'reason': reason,
      'evidence': evidence,
      'excerpt': excerpt,
    };
  }
}

class _FeatureDiagnosis {
  const _FeatureDiagnosis({
    required this.owner,
    required this.reason,
    required this.evidence,
  });

  final _IssueOwner owner;
  final String reason;
  final List<String> evidence;
}

class _DetectionResult {
  const _DetectionResult({
    required this.state,
    required this.evidence,
  });

  final _DetectionState state;
  final List<String> evidence;
}

class _OpenFence {
  const _OpenFence({
    required this.markerChar,
    required this.markerLength,
    required this.line,
  });

  final String markerChar;
  final int markerLength;
  final int line;
}

enum _IssueOwner {
  parser,
  markdown,
  manifest,
}

enum _DetectionState {
  valid,
  malformed,
  absent,
}
