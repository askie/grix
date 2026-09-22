import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/locale/app_material_localizations.dart';
import 'package:grix/app/settings/theme_preference_service.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/auth/privacy_consent_gate_view.dart';
import 'package:grix/modules/auth/privacy_policy_view.dart';
import 'package:grix/modules/auth/user_agreement_view.dart';
import 'package:grix/shared/services/privacy_consent_store.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Stand-in for [GrixApp] that still uses GetMaterialApp like production.
class _FakeGrixApp extends StatelessWidget {
  const _FakeGrixApp({required this.translations});

  final AppTranslations translations;

  @override
  Widget build(BuildContext context) {
    final themePreferenceService = Get.find<ThemePreferenceService>();
    return Obx(
      () => GetMaterialApp(
        debugShowCheckedModeBanner: false,
        theme: AppTheme.lightTheme,
        darkTheme: AppTheme.darkTheme,
        themeMode: themePreferenceService.themeMode,
        translations: translations,
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        localizationsDelegates: AppMaterialLocalizations.delegates,
        supportedLocales: AppMaterialLocalizations.supportedLocales,
        home: const Scaffold(
          key: Key('fake_grix_home'),
          body: Text('login-or-home'),
        ),
      ),
    );
  }
}

/// Mirrors production AppBootstrap: MaterialApp gate → GetMaterialApp app.
class _GateBootstrap extends StatefulWidget {
  const _GateBootstrap({required this.translations});

  final AppTranslations translations;

  @override
  State<_GateBootstrap> createState() => _GateBootstrapState();
}

class _GateBootstrapState extends State<_GateBootstrap> {
  bool _awaiting = true;

  @override
  void initState() {
    super.initState();
    const locale = Locale('en', 'US');
    Get.locale = locale;
    Get.fallbackLocale = locale;
    Get.addTranslations(widget.translations.keys);
  }

  @override
  Widget build(BuildContext context) {
    if (_awaiting) {
      return MaterialApp(
        theme: AppTheme.lightTheme,
        locale: const Locale('en', 'US'),
        localizationsDelegates: AppMaterialLocalizations.delegates,
        supportedLocales: AppMaterialLocalizations.supportedLocales,
        home: PrivacyConsentGateView(
          onAccepted: () async {
            setState(() => _awaiting = false);
          },
        ),
      );
    }
    return _FakeGrixApp(translations: widget.translations);
  }
}

Widget _productionGateMaterialApp({
  required Locale locale,
  required AppTranslations translations,
}) {
  Get.locale = locale;
  Get.fallbackLocale = const Locale('en', 'US');
  Get.addTranslations(translations.keys);
  return MaterialApp(
    theme: AppTheme.lightTheme,
    locale: locale,
    localizationsDelegates: AppMaterialLocalizations.delegates,
    supportedLocales: AppMaterialLocalizations.supportedLocales,
    home: PrivacyConsentGateView(onAccepted: () async {}),
  );
}

Future<void> _openPrivacyThenAgreement(
  WidgetTester tester, {
  required String privacyTitle,
  required String agreementTitle,
}) async {
  await tester.tap(
    find.byKey(const Key('privacy_consent_privacy_policy_link')),
  );
  await tester.pumpAndSettle();
  expect(find.byKey(const Key('privacy_policy_page')), findsOneWidget);
  expect(find.text(privacyTitle), findsWidgets);
  expect(tester.takeException(), isNull);

  await tester.tap(find.byType(BackButton).first);
  await tester.pumpAndSettle();
  expect(find.byKey(const Key('privacy_consent_gate')), findsOneWidget);
  expect(tester.takeException(), isNull);

  await tester.tap(
    find.byKey(const Key('privacy_consent_user_agreement_link')),
  );
  await tester.pumpAndSettle();
  expect(find.byType(UserAgreementView), findsOneWidget);
  expect(find.text(agreementTitle), findsWidgets);
  expect(tester.takeException(), isNull);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    Get.reset();
    Get.put(ThemePreferenceService());
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
      final translations = AppTranslations();
      Get.locale = const Locale('en', 'US');
      Get.fallbackLocale = const Locale('en', 'US');
      Get.addTranslations(translations.keys);
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: AppMaterialLocalizations.delegates,
          supportedLocales: AppMaterialLocalizations.supportedLocales,
          home: PrivacyConsentGateView(
            onAccepted: () async {
              accepted = true;
            },
          ),
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
      expect(tester.takeException(), isNull);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  testWidgets('agree switches MaterialApp gate to GetMaterialApp app', (
    tester,
  ) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    try {
      await tester.pumpWidget(
        _GateBootstrap(translations: AppTranslations()),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('privacy_consent_gate')), findsOneWidget);

      await tester.tap(find.byKey(const Key('privacy_consent_agree_button')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('fake_grix_home')), findsOneWidget);
      expect(find.byKey(const Key('privacy_consent_gate')), findsNothing);
      expect(await PrivacyConsentStore.hasAcceptedCurrentVersion(), isTrue);
      expect(tester.takeException(), isNull);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  // Production path: plain MaterialApp gate (not GetMaterialApp). Opening
  // PrivacyPolicyView / UserAgreementView requires MaterialLocalizations.
  testWidgets(
    'MaterialApp gate zh: open privacy, back, then agreement without exception',
    (tester) async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      try {
        const locale = Locale('zh', 'CN');
        await tester.pumpWidget(
          _productionGateMaterialApp(
            locale: locale,
            translations: AppTranslations(),
          ),
        );
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull);

        await _openPrivacyThenAgreement(
          tester,
          privacyTitle: '隐私政策',
          agreementTitle: '用户协议',
        );
        expect(tester.takeException(), isNull);
      } finally {
        debugDefaultTargetPlatformOverride = null;
      }
    },
  );

  testWidgets(
    'MaterialApp gate en: open privacy, back, then agreement without exception',
    (tester) async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      try {
        const locale = Locale('en', 'US');
        await tester.pumpWidget(
          _productionGateMaterialApp(
            locale: locale,
            translations: AppTranslations(),
          ),
        );
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull);

        await _openPrivacyThenAgreement(
          tester,
          privacyTitle: 'Privacy Policy',
          agreementTitle: 'User Agreement',
        );
        expect(tester.takeException(), isNull);
      } finally {
        debugDefaultTargetPlatformOverride = null;
      }
    },
  );

  testWidgets('privacy policy page shows Chinese for zh locale', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        locale: const Locale('zh', 'CN'),
        localizationsDelegates: AppMaterialLocalizations.delegates,
        supportedLocales: AppMaterialLocalizations.supportedLocales,
        home: Builder(
          builder: (context) {
            Get.locale = const Locale('zh', 'CN');
            Get.addTranslations(AppTranslations().keys);
            return const PrivacyPolicyView();
          },
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('隐私政策'), findsWidgets);
    expect(find.text('数据类别'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('privacy policy page shows English for en locale', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        locale: const Locale('en', 'US'),
        localizationsDelegates: AppMaterialLocalizations.delegates,
        supportedLocales: AppMaterialLocalizations.supportedLocales,
        home: Builder(
          builder: (context) {
            Get.locale = const Locale('en', 'US');
            Get.addTranslations(AppTranslations().keys);
            return const PrivacyPolicyView();
          },
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('Privacy Policy'), findsWidgets);
    expect(find.text('Data categories'), findsOneWidget);
    expect(tester.takeException(), isNull);
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

      final translations = AppTranslations();
      Get.locale = const Locale('en', 'US');
      Get.addTranslations(translations.keys);
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: AppMaterialLocalizations.delegates,
          supportedLocales: AppMaterialLocalizations.supportedLocales,
          home: PrivacyConsentGateView(onAccepted: () async {}),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('privacy_consent_disagree_button')));
      await tester.pump();

      expect(popped, 'SystemNavigator.pop');
      expect(tester.takeException(), isNull);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });
}
