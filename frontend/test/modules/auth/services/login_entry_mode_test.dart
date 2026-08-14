import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/auth/services/login_entry_mode.dart';

void main() {
  test('returns qrScan mode when route parameters include sid and qt', () {
    final mode = resolveLoginEntryMode(
      routeParameters: <String, String?>{
        'sid': 'session_1',
        'qt': 'token_1',
      },
      baseUri: Uri.parse('https://example.com/#/login'),
    );

    expect(mode, LoginEntryMode.qrScan);
  });

  test('returns qrScan mode when base uri query includes sid and qt', () {
    final mode = resolveLoginEntryMode(
      routeParameters: const <String, String?>{},
      baseUri: Uri.parse('https://example.com/login?sid=session_1&qt=token_1'),
    );

    expect(mode, LoginEntryMode.qrScan);
  });

  test('returns normal mode when sid or qt is missing', () {
    final mode = resolveLoginEntryMode(
      routeParameters: <String, String?>{
        'sid': 'session_1',
      },
      baseUri: Uri.parse('https://example.com/login'),
    );

    expect(mode, LoginEntryMode.normal);
  });
}
