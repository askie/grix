class ChatForwardMentionSanitizer {
  const ChatForwardMentionSanitizer._();

  static final RegExp _numericMentionPattern = RegExp(r'@(\d+)');

  static String neutralizeNumericMentions(String content) {
    if (content.isEmpty || !content.contains('@')) {
      return content;
    }

    return content.replaceAllMapped(_numericMentionPattern, (match) {
      final numericPart = match.group(1) ?? '';
      return '＠$numericPart';
    });
  }
}
