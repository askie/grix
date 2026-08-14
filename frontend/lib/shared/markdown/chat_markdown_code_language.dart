String normalizeCodeFenceLanguage(String? raw) {
  if (raw == null) {
    return '';
  }
  final trimmed = raw.trim();
  if (trimmed.isEmpty) {
    return '';
  }
  final primaryToken = trimmed.split(RegExp(r'\s+')).first;
  final lowered = primaryToken.toLowerCase();
  if (lowered.startsWith('language-')) {
    return lowered.substring('language-'.length);
  }
  return lowered;
}

String resolveCodeFenceLanguageFromClass(String? classAttr) {
  if (classAttr == null || classAttr.trim().isEmpty) {
    return '';
  }

  final classTokens =
      classAttr.trim().split(RegExp(r'\s+')).where((token) => token.isNotEmpty);
  for (final token in classTokens) {
    final normalized = normalizeCodeFenceLanguage(token);
    if (normalized.isEmpty) {
      continue;
    }
    if (token.toLowerCase().startsWith('language-')) {
      return normalized;
    }
  }

  return normalizeCodeFenceLanguage(classAttr);
}
