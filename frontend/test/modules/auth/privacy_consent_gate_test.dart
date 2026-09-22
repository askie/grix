import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/auth/privacy_consent_gate_view.dart';
import 'package:grix/modules/auth/user_agreement_view.dart';
import 'package:grix/shared/services/privacy_consent_store.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  tearDown(() {
    debugDefaultTargetPlatformOverride = null;
    Get.reset();
  });

  test('Android requires privacy gate until accepted version is stored', () async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    expect(PrivacyConsentStore.isGateRequired, isTrue);
    expect(await PrivacyConsentStore.hasAcceptedCurrentVersion(), isFalse);

    await PrivacyConsentStore.acceptCurrentVersion();
    expect(await PrivacyConsentStore.hasAcceptedCurrentVersion(), isTrue);

    await PrivacyConsentStore.clearForTest();
    expect(await PrivacyConsentStore.hasAcceptedCurrentVersion(), isFalse);
  });

  testWidgets('shows first-launch privacy gate and persists agree', (
    tester,
  ) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    try {
      var accepted = false;
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          fallbackLocale: const Locale('en', 'US'),
          initialRoute: AppRoutes.privacyConsent,
          getPages: [
            GetPage(
              name: AppRoutes.privacyConsent,
              page: () => PrivacyConsentGateView(
                onAccepted: () async {
                  accepted = true;
                },
              ),
            ),
            GetPage(
              name: AppRoutes.userAgreement,
              page: () => const UserAgreementView(),
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('privacy_consent_gate')), findsOneWidget);
      expect(find.byKey(const Key('privacy_consent_title')), findsOneWidget);
      expect(
        find.byKey(const Key('privacy_consent_user_agreement_link')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('privacy_consent_privacy_policy_link')),
        findsOneWidget,
      );

      await tester.tap(find.byKey(const Key('privacy_consent_agree_button')));
      await tester.pumpAndSettle();

      expect(accepted, isTrue);
      expect(await PrivacyConsentStore.hasAcceptedCurrentVersion(), isTrue);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  testWidgets('agree once prevents needing gate again', (tester) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    try {
      await PrivacyConsentStore.acceptCurrentVersion();
      expect(await PrivacyConsentStore.hasAcceptedCurrentVersion(), isTrue);

      final stillNeedsGate = PrivacyConsentStore.isGateRequired &&
          !await PrivacyConsentStore.hasAcceptedCurrentVersion();
      expect(stillNeedsGate, isFalse);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  testWidgets('disagree exits via SystemNavigator.pop', (tester) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    try {
      final messenger = tester.binding.defaultBinaryMessenger;
      const channel = SystemChannels.platform;
      Object? popped;
      messenger.setMockMethodCallHandler(channel, (call) async {
        if (call.method == 'SystemNavigator.pop') {
          popped = call.method;
        }
        return null;
      });
      addTearDown(() => messenger.setMockMethodCallHandler(channel, null));

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          fallbackLocale: const Locale('en', 'US'),
          home: PrivacyConsentGateView(onAccepted: () async {}),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('privacy_consent_disagree_button')));
      await tester.pump();

      expect(popped, 'SystemNavigator.pop');
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });
}
