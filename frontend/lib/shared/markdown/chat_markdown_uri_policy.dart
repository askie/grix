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

  /// Resolves an absolute path on the agent host. Plain absolute paths and
  /// `file://` URIs pointing at that same host are accepted; relative paths,
  /// other schemes, and remote `file://host/share` locations stay rejected.
  static String? resolveAgentFilePath(String raw) {
    final trimmed = raw.trim();
    if (trimmed.isEmpty || trimmed.contains('\u0000')) {
      return null;
    }

    if (trimmed.toLowerCase().startsWith('file:')) {
      return _resolveFileUriPath(trimmed);
    }

    String decoded;
    try {
      decoded = Uri.decodeFull(trimmed);
    } on FormatException {
      return null;
    }
    return _acceptAbsolutePath(decoded);
  }

  /// Converts a local `file://` URI into the plain path form the rest of the
  /// app uses. A host other than an empty host or `localhost` describes another
  /// machine, so it is rejected instead of silently resolved.
  static String? _resolveFileUriPath(String raw) {
    // `Uri` rewrites a scheme-relative `file:a.md` into an absolute path, so
    // only the explicit authority form is accepted.
    if (!raw.toLowerCase().startsWith('file://')) {
      return null;
    }
    final uri = Uri.tryParse(raw);
    if (uri == null || uri.scheme.toLowerCase() != 'file') {
      return null;
    }
    if (uri.hasPort ||
        uri.userInfo.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment) {
      return null;
    }
    final host = uri.host.toLowerCase();
    if (host.isNotEmpty && host != 'localhost') {
      return null;
    }

    String path;
    try {
      // `toFilePath` rejects any authority, so `file://localhost/...` is
      // normalized to the hostless form first.
      path = uri.replace(host: '').toFilePath(windows: false);
    } on UnsupportedError {
      return null;
    }

    // `file:///C:/work/a.md` decodes to `/C:/work/a.md` under POSIX rules.
    final windowsPath = RegExp(r'^/([A-Za-z]:[\\/].*)$').firstMatch(path);
    if (windowsPath != null) {
      return _acceptAbsolutePath(windowsPath.group(1)!);
    }
    return _acceptAbsolutePath(path);
  }

  static String? _acceptAbsolutePath(String path) {
    if (path.contains('\u0000')) {
      return null;
    }
    if (path.startsWith('/') && !path.startsWith('//')) {
      return path;
    }
    if (RegExp(r'^[A-Za-z]:[\\/]').hasMatch(path)) {
      return path;
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
