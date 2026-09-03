typedef MentionDisplayNameResolver = String? Function(String userId);
typedef MentionAliasResolver = Iterable<String> Function(String userId);

class ChatNumericMentionResolver {
  static final RegExp _numericMentionPattern = RegExp(r'@(\d+)');

  static List<String> extractNumericMentionUserIds(String content) {
    if (content.isEmpty || !content.contains('@')) {
      return const <String>[];
    }

    final seen = <String>{};
    final mentions = <String>[];
    for (final match in _numericMentionPattern.allMatches(content)) {
      if (!isNumericMentionStart(content, match.start) ||
          !_isEndBoundary(content, match.end) ||
          isDottedNumberSegment(content, match.end)) {
        continue;
      }

      final userId = match.group(1)?.trim() ?? '';
      if (userId.isEmpty || !seen.add(userId)) {
        continue;
      }
      mentions.add(userId);
    }
    return mentions;
  }

  static String replaceNumericMentions(
    String content, {
    required MentionDisplayNameResolver resolveDisplayName,
    MentionAliasResolver? resolveAliases,
  }) {
    if (content.isEmpty || !content.contains('@')) {
      return content;
    }

    var cursor = 0;
    var replaced = false;
    final output = StringBuffer();

    for (final match in _numericMentionPattern.allMatches(content)) {
      if (!isNumericMentionStart(content, match.start) ||
          !_isEndBoundary(content, match.end) ||
          isDottedNumberSegment(content, match.end)) {
        continue;
      }

      final userId = match.group(1)?.trim() ?? '';
      if (userId.isEmpty) {
        continue;
      }

      final displayName = resolveDisplayName(userId)?.trim() ?? '';
      if (displayName.isEmpty || displayName == userId) {
        continue;
      }

      final replacementEnd = _resolveReplacementEnd(
        content,
        mentionEnd: match.end,
        aliases: resolveAliases?.call(userId) ?? const <String>[],
      );

      output.write(content.substring(cursor, match.start));
      output.write('@$displayName');
      cursor = replacementEnd;
      replaced = true;
    }

    if (!replaced) {
      return content;
    }

    output.write(content.substring(cursor));
    return output.toString();
  }

  /// Mirrors the backend rule (mention.isNumericMentionStart): an "@" glued to
  /// the tail of a word (email local part, database connection string, path)
  /// never starts a mention.
  static bool isNumericMentionStart(String content, int atIndex) {
    if (atIndex <= 0) {
      return true;
    }
    return !_isMentionPrefixChar(content.codeUnitAt(atIndex - 1));
  }

  /// Mirrors the backend rule (mention.isDottedNumberSegment): digits followed
  /// by "." plus another digit are only one segment of a dotted numeric literal
  /// (IP address, version number), never a mention.
  static bool isDottedNumberSegment(String content, int endIndex) {
    if (endIndex + 1 >= content.length) {
      return false;
    }
    return content.codeUnitAt(endIndex) == 46 &&
        _isDigit(content.codeUnitAt(endIndex + 1));
  }

  static bool _isMentionPrefixChar(int codeUnit) {
    if (_isTokenChar(codeUnit)) {
      return true;
    }
    switch (codeUnit) {
      case 46: // .
      case 43: // +
      case 45: // -
        return true;
      default:
        return false;
    }
  }

  static bool _isDigit(int codeUnit) {
    return codeUnit >= 48 && codeUnit <= 57;
  }

  static bool _isEndBoundary(String content, int endIndex) {
    if (endIndex >= content.length) {
      return true;
    }
    final next = content.codeUnitAt(endIndex);
    return !_isTokenChar(next);
  }

  static bool _isTokenChar(int codeUnit) {
    final isNumber = codeUnit >= 48 && codeUnit <= 57;
    final isUppercase = codeUnit >= 65 && codeUnit <= 90;
    final isLowercase = codeUnit >= 97 && codeUnit <= 122;
    return isNumber || isUppercase || isLowercase || codeUnit == 95;
  }

  static int _resolveReplacementEnd(
    String content, {
    required int mentionEnd,
    required Iterable<String> aliases,
  }) {
    final normalizedAliases = _normalizeAliases(aliases);
    if (normalizedAliases.isEmpty) {
      return mentionEnd;
    }

    final aliasStart = _skipInlineWhitespace(content, mentionEnd);
    if (aliasStart >= content.length) {
      return mentionEnd;
    }

    var replacementEnd = mentionEnd;
    for (final alias in normalizedAliases) {
      if (!_matchesAlias(content, aliasStart, alias)) {
        continue;
      }

      final aliasEnd = aliasStart + alias.length;
      if (!_isAliasBoundary(content, aliasEnd)) {
        continue;
      }
      if (aliasEnd > replacementEnd) {
        replacementEnd = aliasEnd;
      }
    }
    return replacementEnd;
  }

  static List<String> _normalizeAliases(Iterable<String> aliases) {
    final seen = <String>{};
    final normalized = <String>[];
    for (final alias in aliases) {
      final trimmed = alias.trim();
      if (trimmed.isEmpty || !seen.add(trimmed)) {
        continue;
      }
      normalized.add(trimmed);
    }
    normalized.sort((left, right) => right.length.compareTo(left.length));
    return normalized;
  }

  static int _skipInlineWhitespace(String content, int start) {
    var index = start;
    while (index < content.length) {
      final codeUnit = content.codeUnitAt(index);
      if (!_isInlineWhitespace(codeUnit)) {
        break;
      }
      index++;
    }
    return index;
  }

  static bool _isInlineWhitespace(int codeUnit) {
    return codeUnit == 32 || codeUnit == 9 || codeUnit == 12288;
  }

  static bool _matchesAlias(String content, int start, String alias) {
    if (start + alias.length > content.length) {
      return false;
    }
    return content.substring(start, start + alias.length) == alias;
  }

  static bool _isAliasBoundary(String content, int endIndex) {
    if (endIndex >= content.length) {
      return true;
    }

    final next = content.codeUnitAt(endIndex);
    if (_isInlineWhitespace(next)) {
      return true;
    }

    switch (next) {
      case 10:
      case 13:
      case 33:
      case 44:
      case 46:
      case 58:
      case 59:
      case 63:
      case 12289:
      case 12290:
      case 65281:
      case 65292:
      case 65306:
      case 65307:
      case 65311:
        return true;
      default:
        return false;
    }
  }
}
