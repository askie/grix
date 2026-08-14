import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/app_runtime_endpoints.dart';

void main() {
  group('AppRuntimeEndpoints', () {
    test('resolveWsUrlForBaseUri converts same-origin http to ws', () {
      final wsUrl = AppRuntimeEndpoints.resolveWsUrlForBaseUri(
        Uri.parse('http://127.0.0.1:27180/#/login'),
        isWeb: true,
      );

      expect(wsUrl, 'ws://127.0.0.1:27180/ws');
    });

    test('resolveWsUrlForBaseUri converts same-origin https to wss', () {
      final wsUrl = AppRuntimeEndpoints.resolveWsUrlForBaseUri(
        Uri.parse('https://app.example.com/login'),
        isWeb: true,
      );

      expect(wsUrl, 'wss://app.example.com/ws');
    });

    test('resolveApiBaseUrlForBaseUri keeps http scheme for api endpoint', () {
      final apiBaseUrl = AppRuntimeEndpoints.resolveApiBaseUrlForBaseUri(
        Uri.parse('http://127.0.0.1:27180/#/login'),
        isWeb: true,
      );

      expect(apiBaseUrl, 'http://127.0.0.1:27180/v1');
    });

    test('resolvePublicOriginForBaseUri keeps browser origin scheme', () {
      final publicOrigin = AppRuntimeEndpoints.resolvePublicOriginForBaseUri(
        Uri.parse('https://app.example.com/login'),
        isWeb: true,
      );

      expect(publicOrigin, 'https://app.example.com');
    });

    test(
      'resolveApiBaseUrlForBaseUri falls back to local backend on loopback dev origins',
      () {
        final apiBaseUrl = AppRuntimeEndpoints.resolveApiBaseUrlForBaseUri(
          Uri.parse('http://127.0.0.1:34123/#/login'),
          isWeb: true,
        );

        expect(apiBaseUrl, 'http://localhost:27180/v1');
      },
    );

    test(
      'resolveWsUrlForBaseUri falls back to local ws endpoint on loopback dev origins',
      () {
        final wsUrl = AppRuntimeEndpoints.resolveWsUrlForBaseUri(
          Uri.parse('http://127.0.0.1:34123/#/login'),
          isWeb: true,
        );

        expect(wsUrl, 'ws://localhost:27189/ws');
      },
    );

    test(
      'resolveApiBaseUrlForBaseUri still keeps same-origin on non-loopback origins',
      () {
        final apiBaseUrl = AppRuntimeEndpoints.resolveApiBaseUrlForBaseUri(
          Uri.parse('https://app.example.com/#/login'),
          isWeb: true,
        );

        expect(apiBaseUrl, 'https://app.example.com/v1');
      },
    );
  });
}
