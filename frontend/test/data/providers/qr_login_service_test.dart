import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/qr_login_service.dart';
import 'package:grix/shared/utils/app_region_config.dart';

void main() {
  group('QrLoginService.isQrLoginPayload', () {
    test('returns true for valid qr login payload', () {
      final service = QrLoginService();
      final accepted = service.isQrLoginPayload(
        'grix://auth/qr-login?sid=session_1&qt=token_1',
      );
      expect(accepted, isTrue);
    });

    test('returns false for friend qr payload', () {
      final service = QrLoginService();
      final accepted = service.isQrLoginPayload('https://dhf.pub/u/abc123');
      expect(accepted, isFalse);
    });

    test('returns false when required query fields are missing', () {
      final service = QrLoginService();
      final accepted = service.isQrLoginPayload('grix://auth/qr-login?sid=');
      expect(accepted, isFalse);
    });
  });

  group('QrLoginService.qrPayloadRegion', () {
    test('extracts cn region marker', () {
      final service = QrLoginService();
      final region = service.qrPayloadRegion(
        'grix://auth/qr-login?sid=s1&qt=t1&rg=cn',
      );
      expect(region, AppRegion.cn);
    });

    test('extracts global region marker', () {
      final service = QrLoginService();
      final region = service.qrPayloadRegion(
        'grix://auth/qr-login?sid=s1&qt=t1&rg=global',
      );
      expect(region, AppRegion.global);
    });

    test('returns null for legacy payload without region marker', () {
      final service = QrLoginService();
      final region = service.qrPayloadRegion(
        'grix://auth/qr-login?sid=s1&qt=t1',
      );
      expect(region, isNull);
    });
  });
}
