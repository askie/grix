import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/auth/services/desktop_qr_login_visibility.dart';

void main() {
  test('shows qr login for desktop web with enough width', () {
    final visible = shouldShowDesktopQrLogin(
      isWeb: true,
      isMobile: false,
      isDesktop: true,
      width: 720,
    );

    expect(visible, isTrue);
  });

  test('hides qr login for web mobile even when width is enough', () {
    final visible = shouldShowDesktopQrLogin(
      isWeb: true,
      isMobile: true,
      isDesktop: false,
      width: 1080,
    );

    expect(visible, isFalse);
  });

  test('hides qr login when width is below threshold', () {
    final visible = shouldShowDesktopQrLogin(
      isWeb: true,
      isMobile: false,
      isDesktop: true,
      width: 719,
    );

    expect(visible, isFalse);
  });

  test('shows qr login for desktop native app with enough width', () {
    final visible = shouldShowDesktopQrLogin(
      isWeb: false,
      isMobile: false,
      isDesktop: true,
      width: 720,
    );

    expect(visible, isTrue);
  });

  test('hides qr login for mobile native app', () {
    final visible = shouldShowDesktopQrLogin(
      isWeb: false,
      isMobile: true,
      isDesktop: false,
      width: 1200,
    );

    expect(visible, isFalse);
  });
}
