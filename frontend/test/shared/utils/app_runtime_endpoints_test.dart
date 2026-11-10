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

    test(
      'web ignores a compiled absolute API origin and follows the page host',
      () {
        final apiBaseUrl = AppRuntimeEndpoints.resolveApiBaseUrlForBaseUri(
          Uri.parse('https://gb.grix.im/#/home'),
          isWeb: true,
          configuredValue: 'https://grix.dhf.pub/v1',
        );

        expect(apiBaseUrl, 'https://gb.grix.im/v1');
      },
    );

    test('non-web still uses the compiled absolute API origin', () {
      final apiBaseUrl = AppRuntimeEndpoints.resolveApiBaseUrlForBaseUri(
        Uri.parse('https://gb.grix.im/#/home'),
        isWeb: false,
        configuredValue: 'https://grix.dhf.pub/v1',
      );

      expect(apiBaseUrl, 'https://grix.dhf.pub/v1');
    });
  });
}
