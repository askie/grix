import 'chat_markdown_ast.dart';
import 'chat_markdown_normalizer.dart';
import 'chat_markdown_parser_adapter.dart';
import 'chat_markdown_section_splitter.dart';
import 'chat_markdown_segment.dart';
import 'chat_markdown_semantics.dart';
import 'chat_markdown_validator.dart';

class ChatMarkdownPipelineResult {
  const ChatMarkdownPipelineResult({
    required this.originalText,
    required this.normalizedText,
    required this.shouldUseMarkdown,
    this.document,
    this.semantics,
    this.validation,
  });

  final String originalText;
  final String normalizedText;
  final bool shouldUseMarkdown;
  final ChatMarkdownDocument? document;
  final ChatMarkdownSemanticSummary? semantics;
  final ChatMarkdownValidationResult? validation;
}

class ChatMarkdownPipeline {
  ChatMarkdownPipeline({
    required this.normalizer,
    required this.parser,
    this.semanticAnalyzer = const ChatMarkdownSemanticAnalyzer(),
    this.validator = const ChatMarkdownValidator(),
    this.maxRenderableCharacters = 100000,
    ChatMarkdownSectionRenderer? sectionRenderer,
  }) : sectionRenderer =
           sectionRenderer ??
           ChatMarkdownSectionRenderer(
             parser: parser,
             semanticAnalyzer: semanticAnalyzer,
           );

  final ChatMarkdownNormalizer normalizer;
  final ChatMarkdownParserAdapter parser;
  final ChatMarkdownSemanticAnalyzer semanticAnalyzer;
  final ChatMarkdownValidator validator;
  final ChatMarkdownSectionRenderer sectionRenderer;
  final int maxRenderableCharacters;

  ChatMarkdownPipelineResult preparePreview(String input) {
    final normalized = normalizer.normalizeForPreview(input);
    final fixed = normalizer.applyLightweightFixes(normalized.text);
    return ChatMarkdownPipelineResult(
      originalText: input,
      normalizedText: fixed,
      shouldUseMarkdown: false,
    );
  }

  ChatMarkdownPipelineResult prepareFinalRender(String input) {
    return _prepareFinalRender(input, trustInput: false);
  }

  ChatMarkdownPipelineResult prepareFinalRenderFromTrustedSource(String input) {
    return _prepareFinalRender(input, trustInput: true);
  }

  ChatMarkdownPipelineResult _prepareFinalRender(
    String input, {
    required bool trustInput,
  }) {
    final preview = normalizer.normalizeForPreview(input);
    final previewText = preview.text;
    if (previewText.length > maxRenderableCharacters) {
      return ChatMarkdownPipelineResult(
        originalText: input,
        normalizedText: previewText,
        shouldUseMarkdown: false,
      );
    }

    final normalized = trustInput
        ? preview
        : normalizer.normalizeForFinalRender(input);
    if (normalized.text.length > maxRenderableCharacters) {
      return ChatMarkdownPipelineResult(
        originalText: input,
        normalizedText: normalized.text,
        shouldUseMarkdown: false,
      );
    }

    try {
      final rawDocument = parser.parse(normalized.text);
      final document = _removeEmptyCodeBlocks(rawDocument);
      final validation = validator.validate(
        originalText: previewText,
        normalizedText: normalized.text,
        document: document,
      );
      if (_shouldFallbackToPlainText(previewText)) {
        return ChatMarkdownPipelineResult(
          originalText: input,
          normalizedText: previewText,
          shouldUseMarkdown: false,
          validation: validation,
        );
      }

      if (!trustInput && validation.hasErrors) {
        final rollback = _tryParseOriginal(previewText);
        if (rollback != null) {
          return rollback;
        }
      }

      final semantics = trustInput
          ? semanticAnalyzer.analyze(document)
          : _mergeLexicalSemantics(
              base: semanticAnalyzer.analyze(document),
              normalization: normalized,
            );
      return ChatMarkdownPipelineResult(
        originalText: input,
        normalizedText: normalized.text,
        shouldUseMarkdown: semantics.requiresRichRendering,
        document: document,
        semantics: semantics,
        validation: validation,
      );
    } catch (error, stackTrace) {
      assert(() {
        // ignore: avoid_print
        print(
          '[ChatMarkdownPipeline] ${trustInput ? 'Trusted ' : ''}parse error: '
          '$error\n$stackTrace',
        );
        return true;
      }());
      final rollback = _tryParseOriginal(previewText);
      if (rollback != null) {
        return rollback;
      }
      return ChatMarkdownPipelineResult(
        originalText: input,
        normalizedText: previewText,
        shouldUseMarkdown: false,
      );
    }
  }

  ChatMarkdownPipelineResult? _tryParseOriginal(String previewText) {
    // Try full-document parse on original text
    try {
      final doc = _removeEmptyCodeBlocks(parser.parse(previewText));
      final sem = semanticAnalyzer.analyze(doc);
      if (sem.requiresRichRendering) {
        return ChatMarkdownPipelineResult(
          originalText: previewText,
          normalizedText: previewText,
          shouldUseMarkdown: true,
          document: doc,
          semantics: sem,
        );
      }
    } catch (_) {
      // Full parse also fails, try section-by-section
    }

    // Try section-based partial parse as last resort
    final partialDoc = sectionRenderer.tryPartialParse(previewText);
    if (partialDoc != null) {
      return ChatMarkdownPipelineResult(
        originalText: previewText,
        normalizedText: previewText,
        shouldUseMarkdown: true,
        document: partialDoc,
        semantics: semanticAnalyzer.analyze(partialDoc),
      );
    }

    return null;
  }

  bool _shouldFallbackToPlainText(String text) {
    final lines = text.split('\n');
    final fencePattern = RegExp(r'^[ \t]{0,3}([`~]{3,})(.*)$');
    _OpenFence? openFence;

    for (final line in lines) {
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
        );
        if (tail.isNotEmpty && _looksSuspiciousFenceTail(tail)) {
          return true;
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

      if (_looksSuspiciousFenceTail(tail)) {
        return true;
      }
    }

    return false;
  }

  bool _looksSuspiciousFenceTail(String tail) {
    if (tail.contains('`')) {
      return true;
    }
    if (tail.contains('|') && !tail.trimLeft().startsWith('|')) {
      return true;
    }
    return false;
  }

  ChatMarkdownSemanticSummary _mergeLexicalSemantics({
    required ChatMarkdownSemanticSummary base,
    required ChatMarkdownNormalizationResult normalization,
  }) {
    final features = <ChatMarkdownFeature>{...base.features};
    for (final segment in normalization.segments) {
      if (segment.type == ChatMarkdownSegmentType.htmlLike &&
          !_isNativeMediaSegment(segment.text)) {
        features.add(ChatMarkdownFeature.html);
      }
    }
    return ChatMarkdownSemanticSummary(features: Set.unmodifiable(features));
  }

  // `<video>` / `<audio>` / `<source>` tags are rendered natively as a player
  // card, so they must not trigger the generic raw-HTML plain-text fallback.
  static final RegExp _nativeMediaSegmentPattern = RegExp(
    r'^</?(?:video|audio|source)\b',
    caseSensitive: false,
  );

  bool _isNativeMediaSegment(String text) {
    return _nativeMediaSegmentPattern.hasMatch(text.trimLeft());
  }

  ChatMarkdownDocument _removeEmptyCodeBlocks(ChatMarkdownDocument doc) {
    return ChatMarkdownDocument(children: _filterNodes(doc.children));
  }

  List<ChatMarkdownNode> _filterNodes(List<ChatMarkdownNode> nodes) {
    return nodes
        .where((n) {
          if (n.type == ChatMarkdownNodeType.codeBlock) {
            return (n.attrs['text']?.toString() ?? '').trim().isNotEmpty;
          }
          return true;
        })
        .map(
          (n) => n.children.isEmpty
              ? n
              : ChatMarkdownNode(
                  type: n.type,
                  children: _filterNodes(n.children),
                  attrs: n.attrs,
                  sourceRange: n.sourceRange,
                  normalized: n.normalized,
                  fallbackReason: n.fallbackReason,
                ),
        )
        .toList(growable: false);
  }
}

class _OpenFence {
  const _OpenFence({required this.markerChar, required this.markerLength});

  final String markerChar;
  final int markerLength;
}
