import 'dart:convert';

class ChatMessageContent {
  static final RegExp _jsonCandidatePattern = RegExp(r'^\s*[\[{]');
  static const String _dispatchResultOpen = '[dispatch-result]';
  static const String _dispatchResultClose = '[/dispatch-result]';

  static String unwrapStructuredText(String raw) {
    final trimmed = raw.trim();
    if (trimmed.isEmpty || !_jsonCandidatePattern.hasMatch(trimmed)) {
      return raw;
    }
    try {
      final decoded = jsonDecode(trimmed);
      final extracted = _extractTextNode(decoded);
      if (extracted.trim().isEmpty) {
        return raw;
      }
      return extracted;
    } catch (_) {
      return raw;
    }
  }

  /// Whether [raw] is entirely a `[dispatch-result]…[/dispatch-result]` block.
  static bool isDispatchResultMessage(String raw) {
    return tryUnwrapDispatchResult(raw) != null;
  }

  /// Returns the inner body when [raw] is a full dispatch-result wrapper;
  /// otherwise returns null.
  static String? tryUnwrapDispatchResult(String raw) {
    final trimmed = raw.trim();
    if (trimmed.length <
            _dispatchResultOpen.length + _dispatchResultClose.length ||
        !trimmed.startsWith(_dispatchResultOpen) ||
        !trimmed.endsWith(_dispatchResultClose)) {
      return null;
    }

    var inner = trimmed.substring(
      _dispatchResultOpen.length,
      trimmed.length - _dispatchResultClose.length,
    );
    // Drop one wrapper newline on each side; keep the body otherwise as-is.
    if (inner.startsWith('\r\n')) {
      inner = inner.substring(2);
    } else if (inner.startsWith('\n') || inner.startsWith('\r')) {
      inner = inner.substring(1);
    }
    if (inner.endsWith('\r\n')) {
      inner = inner.substring(0, inner.length - 2);
    } else if (inner.endsWith('\n') || inner.endsWith('\r')) {
      inner = inner.substring(0, inner.length - 1);
    }
    return inner;
  }

  /// Strips a full dispatch-result wrapper when present; otherwise returns [raw].
  static String unwrapDispatchResult(String raw) {
    return tryUnwrapDispatchResult(raw) ?? raw;
  }

  static String _extractTextNode(dynamic value) {
    if (value == null) return '';

    if (value is String) {
      return value;
    }

    if (value is List) {
      final parts = <String>[];
      for (final item in value) {
        final text = _extractTextNode(item);
        if (text.trim().isNotEmpty) {
          parts.add(text);
        }
      }
      return _joinStructuredParts(parts);
    }

    if (value is Map) {
      for (final key in const [
        'text',
        'content',
        'message',
        'answer',
        'response',
        'output',
        'markdown',
        'final_content',
        'delta_content',
      ]) {
        final text = _extractTextNode(value[key]);
        if (text.trim().isNotEmpty) {
          return text;
        }
      }

      for (final entry in value.entries) {
        final text = _extractTextNode(entry.value);
        if (text.trim().isNotEmpty) {
          return text;
        }
      }
    }

    return '';
  }

  static String _joinStructuredParts(List<String> parts) {
    if (parts.isEmpty) {
      return '';
    }

    final buffer = StringBuffer();
    for (final part in parts) {
      if (part.isEmpty) {
        continue;
      }
      // 相邻块之间补换行：完整段落块直接拼接会塌成一行；流式分片通常
      // 已以 \n 收尾或以 \n 开头，此时保持原样不多加空行。
      if (buffer.isNotEmpty &&
          !_endsWithLineBreak(buffer) &&
          !_startsWithLineBreak(part)) {
        buffer.write('\n');
      }
      buffer.write(part);
    }
    return buffer.toString();
  }

  static bool _endsWithLineBreak(StringBuffer buffer) {
    final last = buffer.toString().codeUnitAt(buffer.length - 1);
    return last == 0x0A || last == 0x0D;
  }

  static bool _startsWithLineBreak(String text) {
    return text.startsWith('\n') || text.startsWith('\r');
  }
}
