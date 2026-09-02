import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';

const String _datasetRelativeRoot =
    'test/shared/markdown/testdata/markdown_parser_dataset_20260313_190200';

void main() {
  final datasetRoot = _tryResolveDatasetRoot();
  const normalizer = ChatMarkdownNormalizer();
  final parser = ChatMarkdownDialect.buildParserAdapter();
  final pipeline = ChatMarkdownPipeline(normalizer: normalizer, parser: parser);

  test(
    '遇到解析异常时立即停止并判定责任归因',
    () {
      final resolvedDatasetRoot = datasetRoot;
      if (resolvedDatasetRoot == null) {
        return;
      }
      final caseFiles = _listMarkdownCaseFiles(
        '$resolvedDatasetRoot/markdown_cases',
      );

      for (final caseFile in caseFiles) {
        final casePath = '$resolvedDatasetRoot/markdown_cases/$caseFile';
        final markdown = File(casePath).readAsStringSync();
        final result = pipeline.prepareFinalRender(markdown);
        if (result.document != null && result.semantics != null) {
          continue;
        }

        final normalized = normalizer.normalizeForFinalRender(markdown).text;
        Object? parserError;
        try {
          parser.parse(normalized);
        } catch (error) {
          parserError = error;
        }

        final diagnosis = _diagnoseFailure(
          markdown: normalized,
          parserError: parserError,
        );

        fail(
          _buildFailureMessage(
            caseFile: 'markdown_cases/$caseFile',
            normalizedLength: normalized.length,
            parserError: parserError,
            diagnosis: diagnosis,
          ),
        );
      }
    },
    skip: datasetRoot == null ? 'markdown dataset not found locally' : false,
  );
}

enum _FailureOwner { parser, markdown }

class _FailureDiagnosis {
  const _FailureDiagnosis({required this.owner, required this.evidence});

  final _FailureOwner owner;
  final List<String> evidence;
}

class _FenceOpenState {
  const _FenceOpenState({
    required this.markerChar,
    required this.markerLength,
    required this.openLine,
  });

  final String markerChar;
  final int markerLength;
  final int openLine;
}

_FailureDiagnosis _diagnoseFailure({
  required String markdown,
  required Object? parserError,
}) {
  final markdownIssues = _detectMarkdownStructuralIssues(markdown);
  if (markdownIssues.isNotEmpty) {
    return _FailureDiagnosis(
      owner: _FailureOwner.markdown,
      evidence: markdownIssues,
    );
  }
  if (parserError != null) {
    return const _FailureDiagnosis(
      owner: _FailureOwner.parser,
      evidence: <String>['markdown 结构检查未发现明显问题，但解析器抛出了异常。'],
    );
  }
  return const _FailureDiagnosis(
    owner: _FailureOwner.parser,
    evidence: <String>['pipeline 发生降级，但无法复现底层 parser 异常，按解析器问题处理。'],
  );
}

List<String> _detectMarkdownStructuralIssues(String markdown) {
  final issues = <String>[];
  final lines = markdown.split('\n');
  final fencePattern = RegExp(r'^[ \t]{0,3}([`~]{3,})(.*)$');
  _FenceOpenState? openFence;

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
      openFence = _FenceOpenState(
        markerChar: markerChar,
        markerLength: markerLength,
        openLine: lineNo,
      );
      if (tail.isNotEmpty && _looksSuspiciousFenceInfo(tail)) {
        issues.add('第 $lineNo 行代码围栏信息可疑（可能缺少换行）：`${_truncate(tail)}`');
      }
      continue;
    }

    final canClose =
        markerChar == openFence.markerChar &&
        markerLength >= openFence.markerLength;
    if (!canClose) {
      continue;
    }

    if (tail.isEmpty) {
      openFence = null;
      continue;
    }

    issues.add('第 $lineNo 行疑似错误闭合代码围栏（闭合标记后仍有内容）：`${_truncate(tail)}`');
  }

  if (openFence != null) {
    issues.add(
      '第 ${openFence.openLine} 行开启的代码围栏未闭合：'
      '`${_repeat(openFence.markerChar, openFence.markerLength)}`',
    );
  }

  return List.unmodifiable(issues);
}

bool _looksSuspiciousFenceInfo(String tail) {
  if (tail.isEmpty) {
    return false;
  }
  if (RegExp(r'^[|#>*-]').hasMatch(tail)) {
    return true;
  }
  if (RegExp(r'^[A-Za-z0-9_+\-]+#\s+\S').hasMatch(tail)) {
    return true;
  }
  return false;
}

String _buildFailureMessage({
  required String caseFile,
  required int normalizedLength,
  required Object? parserError,
  required _FailureDiagnosis diagnosis,
}) {
  final owner = diagnosis.owner == _FailureOwner.markdown
      ? 'markdown问题'
      : '解析器问题';
  final parserText = parserError == null
      ? '无（pipeline 降级但 parser 未复现异常）'
      : '${parserError.runtimeType}: $parserError';
  final evidenceText = diagnosis.evidence.map((item) => '- $item').join('\n');
  return '发现解析异常，已在首个问题案例停止。\n'
      'case: $caseFile\n'
      '判定: $owner\n'
      'normalizedLength: $normalizedLength\n'
      'parserError: $parserText\n'
      '判定依据:\n'
      '$evidenceText\n'
      '处理建议: 先修复当前案例，再重新运行测试继续定位下一个问题。';
}

List<String> _listMarkdownCaseFiles(String markdownCasesDir) {
  final directory = Directory(markdownCasesDir);
  if (!directory.existsSync()) {
    throw StateError('markdown cases directory not found: $markdownCasesDir');
  }

  final caseFiles =
      directory
          .listSync(recursive: true, followLinks: false)
          .whereType<File>()
          .map((file) => _normalizePath(file.path))
          .where((path) => path.endsWith('.md'))
          .map((path) => _toRelative(markdownCasesDir, path))
          .toList()
        ..sort();

  if (caseFiles.isEmpty) {
    throw StateError('no markdown test cases found in $markdownCasesDir');
  }
  return caseFiles;
}

String? _tryResolveDatasetRoot() {
  final direct = Directory(_datasetRelativeRoot);
  if (direct.existsSync()) {
    return _normalizePath(_datasetRelativeRoot);
  }

  final prefixed = Directory('frontend/$_datasetRelativeRoot');
  if (prefixed.existsSync()) {
    return _normalizePath('frontend/$_datasetRelativeRoot');
  }

  return null;
}

String _toRelative(String rootPath, String fullPath) {
  final normalizedRoot = _normalizePath(rootPath).replaceAll(RegExp(r'/$'), '');
  final normalizedFull = _normalizePath(fullPath);
  final prefix = '$normalizedRoot/';
  if (!normalizedFull.startsWith(prefix)) {
    throw StateError('path is outside root: $fullPath');
  }
  return normalizedFull.substring(prefix.length);
}

String _truncate(String text, {int maxChars = 80}) {
  if (text.length <= maxChars) {
    return text;
  }
  return '${text.substring(0, maxChars)}...';
}

String _repeat(String unit, int times) => List.filled(times, unit).join();

String _normalizePath(String path) => path.replaceAll('\\', '/');
