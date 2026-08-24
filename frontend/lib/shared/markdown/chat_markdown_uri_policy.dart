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
    if (scheme == 'sinaweibo') {
      return _isSafeWeiboDetailUri(uri) ? uri : null;
    }
    if (scheme.isEmpty || !_allowedLinkSchemes.contains(scheme)) {
      return null;
    }
    return uri;
  }

  /// Resolves an absolute path on the agent host without treating it as a
  /// local-device URI. Relative paths and URI-like values stay rejected.
  static String? resolveAgentFilePath(String raw) {
    final trimmed = raw.trim();
    if (trimmed.isEmpty || trimmed.contains('\u0000')) {
      return null;
    }

    String decoded;
    try {
      decoded = Uri.decodeFull(trimmed);
    } on FormatException {
      return null;
    }
    if (decoded.contains('\u0000')) {
      return null;
    }

    if (decoded.startsWith('/') && !decoded.startsWith('//')) {
      return decoded;
    }
    if (RegExp(r'^[A-Za-z]:[\\/]').hasMatch(decoded)) {
      return decoded;
    }
    return null;
  }

  static bool _isSafeWeiboDetailUri(Uri uri) {
    if (uri.host.toLowerCase() != 'detail' ||
        uri.path.isNotEmpty ||
        uri.hasFragment ||
        uri.hasPort ||
        uri.userInfo.isNotEmpty) {
      return false;
    }

    final query = uri.queryParametersAll;
    final mblogIds = query['mblogid'];
    return query.length == 1 &&
        mblogIds != null &&
        mblogIds.length == 1 &&
        RegExp(r'^\d{1,32}$').hasMatch(mblogIds.single);
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
