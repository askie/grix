import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _PhoneRegistrationAdapter implements HttpClientAdapter {
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
      jsonEncode({
        'code': 0,
        'data': {
          'requires_registration_confirmation': true,
          'registration_ticket': 'test-registration-ticket',
        },
      }),
      200,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('phone login parses registration challenge and opts in', () async {
    SharedPreferences.setMockInitialValues({
      'device_identity_id': 'phone-registration-test-device',
    });
    final service = AuthService();
    final adapter = _PhoneRegistrationAdapter();
    service.dioForTest.httpClientAdapter = adapter;

    final result = await service.phoneLoginWithCode(
      phoneE164: '+8613800140000',
      code: '654321',
    );

    expect(result.ok, true);
    final dynamic outcome = (result as dynamic).data;
    expect(outcome.requiresRegistrationConfirmation, true);
    expect(outcome.registrationTicket, 'test-registration-ticket');
    final requestData = Map<String, dynamic>.from(
      adapter.request!.data as Map<String, dynamic>,
    );
    expect(requestData['supports_registration_confirmation'], true);
    expect(requestData['code'], '654321');
    service.dioForTest.close();
  });
}
