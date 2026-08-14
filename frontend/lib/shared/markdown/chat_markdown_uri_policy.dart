class ChatMarkdownUriPolicy {
  const ChatMarkdownUriPolicy._();

  static const Set<String> _allowedLinkSchemes = {
    'http',
    'https',
    'mailto',
    'tel',
    'grix',
  };
  static const Set<String> _allowedImageSchemes = {
    'http',
    'https',
  };
  static const Set<String> _allowedVideoSchemes = {
    'http',
    'https',
  };
  static const Set<String> _allowedAudioSchemes = {
    'http',
    'https',
  };

  static Uri? resolveSafeLinkUri(String raw) {
    final uri = Uri.tryParse(raw);
    if (uri == null) {
      return null;
    }
    final scheme = uri.scheme.toLowerCase();
    if (scheme.isEmpty || !_allowedLinkSchemes.contains(scheme)) {
      return null;
    }
    return uri;
  }

  static Uri? resolveSafeImageUri(String raw) {
    final uri = Uri.tryParse(raw);
    if (uri == null) {
      return null;
    }
    final scheme = uri.scheme.toLowerCase();
    if (scheme.isEmpty || !_allowedImageSchemes.contains(scheme)) {
      return null;
    }
    return uri;
  }

  static Uri? resolveSafeVideoUri(String raw) {
    final uri = Uri.tryParse(raw);
    if (uri == null) {
      return null;
    }
    final scheme = uri.scheme.toLowerCase();
    if (scheme.isEmpty || !_allowedVideoSchemes.contains(scheme)) {
      return null;
    }
    return uri;
  }

  static Uri? resolveSafeAudioUri(String raw) {
    final uri = Uri.tryParse(raw);
    if (uri == null) {
      return null;
    }
    final scheme = uri.scheme.toLowerCase();
    if (scheme.isEmpty || !_allowedAudioSchemes.contains(scheme)) {
      return null;
    }
    return uri;
  }
}
