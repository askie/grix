class ChatMarkdownLatexRenderNormalizer {
  const ChatMarkdownLatexRenderNormalizer._();

  static String normalizeForMathRenderer(String tex) {
    var normalized = tex.trim();
    if (normalized.startsWith(r'$$') &&
        normalized.endsWith(r'$$') &&
        normalized.length >= 4) {
      normalized = normalized.substring(2, normalized.length - 2).trim();
    }

    normalized = _replaceEnvironment(normalized, from: 'align', to: 'aligned');
    normalized = _replaceEnvironment(normalized, from: 'align*', to: 'aligned');
    normalized = _replaceEnvironment(normalized, from: 'gather', to: 'aligned');
    normalized =
        _replaceEnvironment(normalized, from: 'gather*', to: 'aligned');
    normalized =
        _replaceEnvironment(normalized, from: 'multline', to: 'aligned');
    normalized =
        _replaceEnvironment(normalized, from: 'multline*', to: 'aligned');
    normalized =
        _replaceEnvironment(normalized, from: 'flalign', to: 'aligned');
    normalized =
        _replaceEnvironment(normalized, from: 'flalign*', to: 'aligned');
    normalized =
        _replaceEnvironment(normalized, from: 'alignat', to: 'aligned');
    normalized =
        _replaceEnvironment(normalized, from: 'alignat*', to: 'aligned');

    return normalized;
  }

  static String _replaceEnvironment(
    String source, {
    required String from,
    required String to,
  }) {
    final escaped = RegExp.escape(from);
    final beginPattern = RegExp(r'\\begin\{' + escaped + r'\}');
    final endPattern = RegExp(r'\\end\{' + escaped + r'\}');
    return source
        .replaceAll(beginPattern, '\\begin{$to}')
        .replaceAll(endPattern, '\\end{$to}');
  }
}
