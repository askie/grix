import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/bootstrap/app_bootstrap.dart';
import 'package:grix/app/bootstrap/app_initializer.dart';
import 'package:grix/app/locale/locale_service.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    Get.reset();
  });

  tearDown(() {
    TestWidgetsFlutterBinding.instance.platformDispatcher
        .clearLocalesTestValue();
    Get.reset();
  });

  testWidgets(
    'AppBootstrap privacy gate follows system Chinese when no saved locale',
    (tester) async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      try {
        const systemLocales = [
          Locale.fromSubtags(
            languageCode: 'zh',
            scriptCode: 'Hans',
            countryCode: 'CN',
          ),
        ];
        tester.binding.platformDispatcher.localesTestValue = systemLocales;

        await tester.pumpWidget(
          AppBootstrap(
            bootstrapLoader: () async {
              final locale = await LocaleService.resolveEffectiveLocale(
                systemLocales:
                    WidgetsBinding.instance.platformDispatcher.locales,
              );
              return AppBootstrapData(
                initialLocale: locale,
                initialRoute: AppRoutes.login,
                translations: AppTranslations(),
              );
            },
          ),
        );
        await tester.pumpAndSettle();

        expect(find.byKey(const Key('privacy_consent_gate')), findsOneWidget);
        expect(Get.locale, const Locale('zh', 'CN'));
        expect(find.text('用户协议与隐私政策'), findsOneWidget);
        expect(find.text('同意'), findsOneWidget);
        expect(find.text('不同意'), findsOneWidget);
        expect(await LocaleService.hasSavedLocale(), isFalse);
        expect(tester.takeException(), isNull);
      } finally {
        debugDefaultTargetPlatformOverride = null;
      }
    },
  );
}
