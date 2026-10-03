import 'dart:convert';
import 'dart:typed_data';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_service.dart';

class Adapter implements HttpClientAdapter {
  Adapter(this.response);
  final Map<String, dynamic> response;
  RequestOptions? request;
  @override
  void close({bool force = false}) {}
  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    request = options;
    return ResponseBody.fromString(
      jsonEncode({'code': 0, 'data': response}),
      200,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }
}

void main() {
  test(
    'auth methods support legacy server, explicit closed and conservative malformed value',
    () {
      final legacy = AuthMethods.fromJson({
        'region': 'cn',
        'phone_login_enabled': true,
        'phone_register_enabled': true,
      });
      expect(legacy.registrationEnabled, true);
      final closed = AuthMethods.fromJson({
        'registration_enabled': false,
        'phone_login_enabled': true,
        'phone_register_enabled': true,
      });
      expect(closed.registrationEnabled, false);
      expect(closed.phoneRegisterEnabled, false);
      expect(closed.phoneLoginEnabled, true);
      expect(const AuthMethods.allDisabled().registrationEnabled, false);
      expect(
        AuthMethods.fromJson({
          'registration_enabled': null,
        }).registrationEnabled,
        false,
      );
      expect(
        AuthMethods.fromJson({
          'registration_enabled': 'true',
        }).registrationEnabled,
        false,
      );
    },
  );
  test(
    'public API reads minimum registration capability for requested region',
    () async {
      final service = AuthService();
      final adapter = Adapter({
        'region': 'global',
        'registration_enabled': false,
        'phone_login_enabled': true,
        'phone_register_enabled': false,
      });
      service.dioForTest.httpClientAdapter = adapter;
      final result = await service.fetchAuthMethods(region: 'global');
      expect(result.ok, true);
      expect(result.data!.registrationEnabled, false);
      expect(result.data!.phoneLoginEnabled, true);
      expect(adapter.request!.uri.path, '/v1/auth/methods');
      expect(adapter.request!.uri.queryParameters, {'region': 'global'});
      service.dioForTest.close();
    },
  );
}
