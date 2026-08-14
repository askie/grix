import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/bootstrap/app_initializer.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';

void main() {
  group('AppBootstrapData.resolveInitialRoute', () {
    test('returns explicit route when present', () {
      final data = AppBootstrapData(
        initialLocale: null,
        initialRoute: AppRoutes.home,
        translations: AppTranslations(),
      );

      expect(data.resolveInitialRoute(isLoggedIn: false), AppRoutes.home);
    });

    test('falls back to home when logged in and route is null', () {
      final data = AppBootstrapData(
        initialLocale: null,
        initialRoute: null,
        translations: AppTranslations(),
      );

      expect(data.resolveInitialRoute(isLoggedIn: true), AppRoutes.home);
    });

    test('falls back to login when not logged in and route is blank', () {
      final data = AppBootstrapData(
        initialLocale: null,
        initialRoute: '   ',
        translations: AppTranslations(),
      );

      expect(data.resolveInitialRoute(isLoggedIn: false), AppRoutes.login);
    });
  });
}
