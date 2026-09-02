import 'dart:convert';

import 'chat_markdown_ast.dart';
import 'chat_markdown_pipeline.dart';
import 'chat_markdown_semantics.dart';

class ChatMarkdownRenderCacheCodec {
  const ChatMarkdownRenderCacheCodec._();

  static const int _schemaVersion = 1;

  static String encode(ChatMarkdownPipelineResult result) {
    final payload = <String, Object?>{
      'version': _schemaVersion,
      'original_text': result.originalText,
      'normalized_text': result.normalizedText,
      'should_use_markdown': result.shouldUseMarkdown,
      'document': result.document == null
          ? null
          : _encodeNode(result.document!),
      'semantics': result.semantics == null
          ? null
          : <String, Object?>{
              'features': result.semantics!.features
                  .map((feature) => feature.name)
                  .toList(growable: false),
            },
    };
    return jsonEncode(payload);
  }

  static ChatMarkdownPipelineResult? decode(String payload) {
    try {
      final decoded = jsonDecode(payload);
      if (decoded is! Map) {
        return null;
      }

      final map = Map<String, dynamic>.from(decoded.cast<String, dynamic>());
      final normalizedText = map['normalized_text']?.toString() ?? '';
      if (normalizedText.isEmpty) {
        return null;
      }
      final originalText = map['original_text']?.toString() ?? normalizedText;
      final shouldUseMarkdown = map['should_use_markdown'] == true;

      ChatMarkdownDocument? document;
      final rawDocument = map['document'];
      if (rawDocument is Map) {
        document = _decodeDocument(Map<String, dynamic>.from(rawDocument));
      }

      ChatMarkdownSemanticSummary? semantics;
      final rawSemantics = map['semantics'];
      if (rawSemantics is Map) {
        semantics = _decodeSemantics(Map<String, dynamic>.from(rawSemantics));
      }

      return ChatMarkdownPipelineResult(
        originalText: originalText,
        normalizedText: normalizedText,
        shouldUseMarkdown: shouldUseMarkdown,
        document: document,
        semantics: semantics,
      );
    } catch (_) {
      return null;
    }
  }

  static Map<String, Object?> _encodeNode(ChatMarkdownNode node) {
    return <String, Object?>{
      'type': node.type.name,
      'children': node.children.map(_encodeNode).toList(growable: false),
      'attrs': _encodeValue(node.attrs),
      'source_range': node.sourceRange == null
          ? null
          : <String, Object?>{
              'start': node.sourceRange!.start,
              'end': node.sourceRange!.end,
            },
      'normalized': node.normalized,
      'fallback_reason': node.fallbackReason,
    };
  }

  static ChatMarkdownDocument _decodeDocument(Map<String, dynamic> map) {
    final node = _decodeNode(map);
    if (node is ChatMarkdownDocument) {
      return node;
    }
    return ChatMarkdownDocument(children: <ChatMarkdownNode>[node]);
  }

  static ChatMarkdownNode _decodeNode(Map<String, dynamic> map) {
    final typeName = map['type']?.toString() ?? '';
    final type = ChatMarkdownNodeType.values.firstWhere(
      (candidate) => candidate.name == typeName,
      orElse: () => ChatMarkdownNodeType.unknown,
    );

    final children = <ChatMarkdownNode>[];
    final rawChildren = map['children'];
    if (rawChildren is List) {
      for (final child in rawChildren) {
        if (child is! Map) {
          continue;
        }
        children.add(_decodeNode(Map<String, dynamic>.from(child)));
      }
    }

    final attrs = <String, Object?>{};
    final rawAttrs = map['attrs'];
    if (rawAttrs is Map) {
      for (final entry in rawAttrs.entries) {
        final key = entry.key.toString();
        attrs[key] = _decodeValue(entry.value);
      }
    }

    ChatMarkdownSourceRange? sourceRange;
    final rawSourceRange = map['source_range'];
    if (rawSourceRange is Map) {
      final start = _parseInt(rawSourceRange['start']);
      final end = _parseInt(rawSourceRange['end']);
      if (start != null && end != null) {
        sourceRange = ChatMarkdownSourceRange(start: start, end: end);
      }
    }

    if (type == ChatMarkdownNodeType.document) {
      return ChatMarkdownDocument(children: List.unmodifiable(children));
    }

    return ChatMarkdownNode(
      type: type,
      children: List.unmodifiable(children),
      attrs: Map.unmodifiable(attrs),
      sourceRange: sourceRange,
      normalized: map['normalized'] != false,
      fallbackReason: map['fallback_reason']?.toString(),
    );
  }

  static ChatMarkdownSemanticSummary _decodeSemantics(
    Map<String, dynamic> map,
  ) {
    final features = <ChatMarkdownFeature>{};
    final rawFeatures = map['features'];
    if (rawFeatures is List) {
      for (final rawFeature in rawFeatures) {
        final name = rawFeature?.toString() ?? '';
        final matched = ChatMarkdownFeature.values.where(
          (feature) => feature.name == name,
        );
        if (matched.isNotEmpty) {
          features.add(matched.first);
        }
      }
    }
    return ChatMarkdownSemanticSummary(features: Set.unmodifiable(features));
  }

  static Object? _encodeValue(Object? value) {
    if (value == null || value is String || value is num || value is bool) {
      return value;
    }
    if (value is List) {
      return value.map((item) => _encodeValue(item)).toList(growable: false);
    }
    if (value is Map) {
      return Map<String, Object?>.fromEntries(
        value.entries.map(
          (entry) => MapEntry(entry.key.toString(), _encodeValue(entry.value)),
        ),
      );
    }
    return value.toString();
  }

  static Object? _decodeValue(dynamic value) {
    if (value == null || value is String || value is num || value is bool) {
      return value;
    }
    if (value is List) {
      return value.map((item) => _decodeValue(item)).toList(growable: false);
    }
    if (value is Map) {
      return Map<String, Object?>.fromEntries(
        value.entries.map(
          (entry) => MapEntry(entry.key.toString(), _decodeValue(entry.value)),
        ),
      );
    }
    return value.toString();
  }

  static int? _parseInt(dynamic value) {
    if (value is int) {
      return value;
    }
    if (value is num) {
      return value.toInt();
    }
    return int.tryParse(value?.toString() ?? '');
  }
}
