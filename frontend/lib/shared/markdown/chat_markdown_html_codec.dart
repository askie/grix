class ChatMarkdownHtmlCodec {
  const ChatMarkdownHtmlCodec._();

  static final RegExp _htmlCharacterPattern = RegExp(
    r'&(?:([a-z0-9]+)|#([0-9]{1,7})|#x([a-f0-9]{1,6}));',
    caseSensitive: false,
  );

  static const Map<String, String> _namedEntities = <String, String>{
    'amp': '&',
    'lt': '<',
    'gt': '>',
    'quot': '"',
    'apos': "'",
  };

  static String decode(String input) {
    if (input.isEmpty || !input.contains('&')) {
      return input;
    }
    return input.replaceAllMapped(_htmlCharacterPattern, _decodeMatch);
  }

  static String? decodeNullable(String? input) {
    if (input == null) {
      return null;
    }
    return decode(input);
  }

  static String _decodeMatch(Match match) {
    final namedEntity = match.group(1);
    if (namedEntity != null) {
      return _namedEntities[namedEntity.toLowerCase()] ?? match.group(0)!;
    }

    final decimalNumber = match.group(2);
    if (decimalNumber != null) {
      final value = int.parse(decimalNumber);
      if (value <= 1 || value > 0x10ffff) {
        return '\uFFFD';
      }
      return String.fromCharCode(value);
    }

    final hexadecimalNumber = match.group(3);
    if (hexadecimalNumber != null) {
      final value = int.parse(hexadecimalNumber, radix: 16);
      if (value == 0 || value > 0x10ffff) {
        return '\uFFFD';
      }
      return String.fromCharCode(value);
    }

    return match.group(0)!;
  }
}
