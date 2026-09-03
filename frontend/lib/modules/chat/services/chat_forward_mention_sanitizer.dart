import '../../../shared/utils/chat_numeric_mention_resolver.dart';

class ChatForwardMentionSanitizer {
  const ChatForwardMentionSanitizer._();

  static final RegExp _numericMentionPattern = RegExp(r'@(\d+)');

  /// Neutralizes numeric mentions so a forwarded message cannot wake the
  /// original targets in the destination session. Only tokens the backend
  /// would actually treat as a mention are rewritten: an "@" glued to a word
  /// or a dotted numeric literal (connection string, IP address, version
  /// number) is left byte-for-byte intact, otherwise forwarding would corrupt
  /// the text the user is passing on.
  static String neutralizeNumericMentions(String content) {
    if (content.isEmpty || !content.contains('@')) {
      return content;
    }

    var cursor = 0;
    var replaced = false;
    final output = StringBuffer();

    for (final match in _numericMentionPattern.allMatches(content)) {
      if (!ChatNumericMentionResolver.isNumericMentionStart(
            content,
            match.start,
          ) ||
          ChatNumericMentionResolver.isDottedNumberSegment(
            content,
            match.end,
          )) {
        continue;
      }

      output.write(content.substring(cursor, match.start));
      output.write('＠${match.group(1) ?? ''}');
      cursor = match.end;
      replaced = true;
    }

    if (!replaced) {
      return content;
    }

    output.write(content.substring(cursor));
    return output.toString();
  }
}
