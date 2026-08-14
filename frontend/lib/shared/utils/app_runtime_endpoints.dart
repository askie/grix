import 'package:flutter/foundation.dart';

class AppRuntimeEndpoints {
  static const String _defaultApiBaseUrl = 'http://localhost:27180/v1';
  static const String _defaultWsUrl = 'ws://localhost:27189/ws';
  static const int _localApiPort = 27180;
  static const String _configuredApiBaseUrl = String.fromEnvironment(
    'API_BASE_URL',
    defaultValue: '',
  );
  static const String _configuredWsUrl = String.fromEnvironment(
    'WS_URL',
    defaultValue: '',
  );
  static const String _configuredFriendQrLinkPrefix = String.fromEnvironment(
    'FRIEND_QR_LINK_PREFIX',
    defaultValue: 'https://dhf.pub/u/',
  );
  static const String _configuredGroupQrLinkPrefix = String.fromEnvironment(
    'GROUP_QR_LINK_PREFIX',
    defaultValue: 'https://dhf.pub/g/',
  );

  static String get apiBaseUrl => resolveApiBaseUrlForBaseUri(Uri.base);

  static String get wsUrl => resolveWsUrlForBaseUri(Uri.base);

  static String get publicOrigin => resolvePublicOriginForBaseUri(Uri.base);

  static String get friendQrLinkPrefix => _configuredFriendQrLinkPrefix.trim();

  static String get groupQrLinkPrefix => _configuredGroupQrLinkPrefix.trim();

  @visibleForTesting
  static String resolveApiBaseUrlForBaseUri(
    Uri baseUri, {
    bool isWeb = kIsWeb,
  }) {
    return _resolveEndpoint(
      configuredValue: _configuredApiBaseUrl,
      baseUri: baseUri,
      sameOriginPath: '/v1',
      defaultValue: _defaultApiBaseUrl,
      useWebSocketScheme: false,
      isWeb: isWeb,
    );
  }

  @visibleForTesting
  static String resolveWsUrlForBaseUri(Uri baseUri, {bool isWeb = kIsWeb}) {
    return _resolveEndpoint(
      configuredValue: _configuredWsUrl,
      baseUri: baseUri,
      sameOriginPath: '/ws',
      defaultValue: _defaultWsUrl,
      useWebSocketScheme: true,
      isWeb: isWeb,
    );
  }

  @visibleForTesting
  static String resolvePublicOriginForBaseUri(
    Uri baseUri, {
    bool isWeb = kIsWeb,
  }) {
    final origin = _originFromBaseUri(baseUri, isWeb: isWeb);
    if (origin.isNotEmpty) {
      return origin;
    }

    final apiBaseUrl = resolveApiBaseUrlForBaseUri(
      baseUri,
      isWeb: isWeb,
    ).trim();
    final apiUri = Uri.tryParse(apiBaseUrl);
    if (apiUri == null || apiUri.scheme.isEmpty || apiUri.host.isEmpty) {
      return '';
    }

    return Uri(
      scheme: apiUri.scheme,
      userInfo: apiUri.userInfo,
      host: apiUri.host,
      port: apiUri.hasPort ? apiUri.port : null,
    ).toString();
  }

  static String _resolveEndpoint({
    required String configuredValue,
    required Uri baseUri,
    required String sameOriginPath,
    required String defaultValue,
    required bool useWebSocketScheme,
    required bool isWeb,
  }) {
    final normalized = configuredValue.trim();
    if (normalized.isNotEmpty) {
      final absolute = _resolveConfiguredAbsoluteUrl(
        configuredValue: normalized,
        baseUri: baseUri,
        useWebSocketScheme: useWebSocketScheme,
        isWeb: isWeb,
      );
      if (absolute.isNotEmpty) {
        return absolute;
      }
      return normalized;
    }

    if (_shouldUseLocalDefaultForLoopbackWebOrigin(
      baseUri: baseUri,
      isWeb: isWeb,
    )) {
      return defaultValue;
    }

    final origin = _originFromBaseUri(
      baseUri,
      useWebSocketScheme: useWebSocketScheme,
      isWeb: isWeb,
    );
    if (origin.isEmpty) {
      return defaultValue;
    }

    return '$origin$sameOriginPath';
  }

  static String _resolveConfiguredAbsoluteUrl({
    required String configuredValue,
    required Uri baseUri,
    required bool useWebSocketScheme,
    required bool isWeb,
  }) {
    final parsed = Uri.tryParse(configuredValue);
    if (parsed != null && parsed.scheme.isNotEmpty && parsed.host.isNotEmpty) {
      return configuredValue;
    }

    if (!isWeb || !configuredValue.startsWith('/')) {
      return '';
    }

    final baseOrigin = _originFromBaseUri(
      baseUri,
      useWebSocketScheme: useWebSocketScheme,
      isWeb: isWeb,
    );
    if (baseOrigin.isEmpty) {
      return '';
    }
    return '$baseOrigin$configuredValue';
  }

  static bool _shouldUseLocalDefaultForLoopbackWebOrigin({
    required Uri baseUri,
    required bool isWeb,
  }) {
    if (!isWeb) {
      return false;
    }
    if (baseUri.scheme != 'http') {
      return false;
    }

    final host = baseUri.host.trim().toLowerCase();
    final isLoopbackHost =
        host == 'localhost' || host == '127.0.0.1' || host == '::1';
    if (!isLoopbackHost) {
      return false;
    }
    if (!baseUri.hasPort) {
      return false;
    }

    return baseUri.port != _localApiPort;
  }

  static String _originFromBaseUri(
    Uri baseUri, {
    bool useWebSocketScheme = false,
    bool isWeb = kIsWeb,
  }) {
    if (!isWeb || baseUri.scheme.isEmpty || baseUri.host.isEmpty) {
      return '';
    }

    var scheme = baseUri.scheme;
    if (useWebSocketScheme) {
      if (scheme == 'https') {
        scheme = 'wss';
      } else if (scheme == 'http') {
        scheme = 'ws';
      }
    }

    return Uri(
      scheme: scheme,
      userInfo: baseUri.userInfo,
      host: baseUri.host,
      port: baseUri.hasPort ? baseUri.port : null,
    ).toString();
  }
}
